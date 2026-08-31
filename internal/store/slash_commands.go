package store

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base32"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"regexp"
	"strings"
	"time"
)

// Slash command status values (SPEC.md §3.2d/§4.12).
const (
	SlashCommandStatusActive   = "active"
	SlashCommandStatusDisabled = "disabled"
)

// ErrTriggerTaken is returned when a slash command's trigger collides case-insensitively with
// an existing one — the API layer maps this to 409 trigger_taken.
var ErrTriggerTaken = errors.New("trigger already taken")

var triggerPattern = regexp.MustCompile(`^/\S+$`)

// SlashCommand is a persisted slash-command registration. Secret is the plaintext HMAC signing
// key — like an outgoing webhook's secret, execution must recompute an HMAC from the real key,
// not merely compare a hash, so it is never discarded after creation. It is never included in
// any API response; only SecretLast4 is, for masked display.
type SlashCommand struct {
	ID          string
	Trigger     string
	BotID       int64
	Description string
	SyntaxHint  string
	WebhookURL  string
	Secret      string
	SecretLast4 string
	AdminOnly   bool
	Status      string
	CreatedBy   int64
	CreatedAt   int64
	UpdatedAt   int64
}

// SlashCommandInput contains the fields accepted when creating a slash command.
type SlashCommandInput struct {
	Trigger     string
	BotID       int64
	Description string
	SyntaxHint  string
	WebhookURL  string
	AdminOnly   bool
	CreatedBy   int64
}

// SlashCommandPatch contains the optional fields PATCH /slash-commands/:id may update; a nil
// field is left unchanged. trigger and bot_id are intentionally absent — immutable after
// creation (SPEC.md §4.12).
type SlashCommandPatch struct {
	Description *string
	SyntaxHint  *string
	WebhookURL  *string
	AdminOnly   *bool
	Status      *string
}

const slashCommandSecretPrefix = "whsec_"

func generateSlashCommandSecret() (string, error) {
	b := make([]byte, 32)
	if _, err := io.ReadFull(rand.Reader, b); err != nil {
		return "", fmt.Errorf("generate slash command secret: %w", err)
	}
	return slashCommandSecretPrefix + base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(b), nil
}

func hashSlashCommandSecret(plaintext string) string {
	sum := sha256.Sum256([]byte(plaintext))
	return hex.EncodeToString(sum[:])
}

func generateSlashCommandID() (string, error) {
	b := make([]byte, 16)
	if _, err := io.ReadFull(rand.Reader, b); err != nil {
		return "", fmt.Errorf("generate slash command id: %w", err)
	}
	return base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(b), nil
}

const slashCommandColumns = `id, trigger, bot_id, description, syntax_hint, webhook_url, secret, secret_last4, admin_only, status, created_by, created_at, updated_at`

func scanSlashCommand(row interface{ Scan(...any) error }) (SlashCommand, error) {
	var c SlashCommand
	var adminOnly int
	err := row.Scan(&c.ID, &c.Trigger, &c.BotID, &c.Description, &c.SyntaxHint, &c.WebhookURL,
		&c.Secret, &c.SecretLast4, &adminOnly, &c.Status, &c.CreatedBy, &c.CreatedAt, &c.UpdatedAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return SlashCommand{}, ErrNotFound
		}
		return SlashCommand{}, err
	}
	c.AdminOnly = adminOnly != 0
	return c, nil
}

func validateTrigger(trigger string) error {
	if !triggerPattern.MatchString(trigger) {
		return fmt.Errorf("trigger must start with / and contain no whitespace")
	}
	return nil
}

// CreateSlashCommand validates trigger and webhook_url, generates a signing secret, and stores
// it alongside its sha256 hash and last-4 plaintext characters. The plaintext is returned once.
func (s *Store) CreateSlashCommand(ctx context.Context, in SlashCommandInput) (SlashCommand, string, error) {
	if err := validateTrigger(in.Trigger); err != nil {
		return SlashCommand{}, "", err
	}
	if strings.TrimSpace(in.Description) == "" {
		return SlashCommand{}, "", fmt.Errorf("description is required")
	}
	if err := validateOutgoingWebhookTargetURL(ctx, in.WebhookURL); err != nil {
		return SlashCommand{}, "", err
	}

	id, err := generateSlashCommandID()
	if err != nil {
		return SlashCommand{}, "", err
	}
	secret, err := generateSlashCommandSecret()
	if err != nil {
		return SlashCommand{}, "", err
	}
	hash := hashSlashCommandSecret(secret)
	last4 := secret[len(secret)-4:]
	now := nowMillis()

	var cmd SlashCommand
	err = s.Tx(ctx, func(tx *sql.Tx) error {
		var botExists int
		if err := tx.QueryRowContext(ctx, "SELECT 1 FROM bots WHERE user_id = ?", in.BotID).Scan(&botExists); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return ErrNotFound
			}
			return fmt.Errorf("check bot: %w", err)
		}
		_, err := tx.ExecContext(ctx, `
			INSERT INTO slash_commands(id, trigger, bot_id, description, syntax_hint, webhook_url, secret, secret_hash, secret_last4, admin_only, status, created_by, created_at, updated_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 'active', ?, ?, ?)`,
			id, in.Trigger, in.BotID, in.Description, in.SyntaxHint, in.WebhookURL, secret, hash, last4,
			boolInt(in.AdminOnly), in.CreatedBy, now, now)
		if err != nil {
			if isUniqueConstraintError(err) {
				return ErrTriggerTaken
			}
			return fmt.Errorf("insert slash command: %w", err)
		}
		cmd, err = getSlashCommandTx(ctx, tx, id)
		return err
	})
	if err != nil {
		return SlashCommand{}, "", err
	}
	return cmd, secret, nil
}

func getSlashCommandTx(ctx context.Context, tx *sql.Tx, id string) (SlashCommand, error) {
	row := tx.QueryRowContext(ctx, "SELECT "+slashCommandColumns+" FROM slash_commands WHERE id = ?", id)
	return scanSlashCommand(row)
}

// GetSlashCommandByID fetches a single slash command by id, for management endpoints.
func (s *Store) GetSlashCommandByID(ctx context.Context, id string) (SlashCommand, error) {
	row := s.reader.QueryRowContext(ctx, "SELECT "+slashCommandColumns+" FROM slash_commands WHERE id = ?", id)
	c, err := scanSlashCommand(row)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return SlashCommand{}, ErrNotFound
		}
		return SlashCommand{}, fmt.Errorf("get slash command: %w", err)
	}
	return c, nil
}

// GetSlashCommandByTrigger is the hot path for POST /commands/execute: a case-insensitive
// lookup that also requires status='active', so a disabled command's existence is not leaked
// (SPEC.md §4.12 point 1's not-403 confirmation-avoidance posture).
func (s *Store) GetSlashCommandByTrigger(ctx context.Context, trigger string) (SlashCommand, error) {
	row := s.reader.QueryRowContext(ctx, "SELECT "+slashCommandColumns+" FROM slash_commands WHERE trigger = ? COLLATE NOCASE AND status = 'active'", trigger)
	c, err := scanSlashCommand(row)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return SlashCommand{}, ErrNotFound
		}
		return SlashCommand{}, fmt.Errorf("get slash command by trigger: %w", err)
	}
	return c, nil
}

func scanSlashCommands(rows *sql.Rows) ([]SlashCommand, error) {
	var out []SlashCommand
	for rows.Next() {
		c, err := scanSlashCommand(rows)
		if err != nil {
			return nil, fmt.Errorf("scan slash command: %w", err)
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// ListSlashCommands returns every active command, for the composer's GET /slash-commands
// autocomplete — visible to every authenticated caller regardless of admin_only (SPEC.md §4.12:
// admin_only restricts execution, not visibility).
func (s *Store) ListSlashCommands(ctx context.Context) ([]SlashCommand, error) {
	rows, err := s.reader.QueryContext(ctx, "SELECT "+slashCommandColumns+" FROM slash_commands WHERE status = 'active' ORDER BY trigger")
	if err != nil {
		return nil, fmt.Errorf("list slash commands: %w", err)
	}
	defer rows.Close()
	return scanSlashCommands(rows)
}

// ListSlashCommandsForAdmin returns every command regardless of status, for the Settings
// management UI.
func (s *Store) ListSlashCommandsForAdmin(ctx context.Context) ([]SlashCommand, error) {
	rows, err := s.reader.QueryContext(ctx, "SELECT "+slashCommandColumns+" FROM slash_commands ORDER BY trigger")
	if err != nil {
		return nil, fmt.Errorf("list slash commands for admin: %w", err)
	}
	defer rows.Close()
	return scanSlashCommands(rows)
}

// UpdateSlashCommand applies a partial patch and returns the updated row. trigger and bot_id
// are immutable after creation.
func (s *Store) UpdateSlashCommand(ctx context.Context, id string, patch SlashCommandPatch) (SlashCommand, error) {
	if patch.WebhookURL != nil {
		if err := validateOutgoingWebhookTargetURL(ctx, *patch.WebhookURL); err != nil {
			return SlashCommand{}, err
		}
	}
	if patch.Status != nil && *patch.Status != SlashCommandStatusActive && *patch.Status != SlashCommandStatusDisabled {
		return SlashCommand{}, fmt.Errorf("status must be active or disabled")
	}

	var cmd SlashCommand
	err := s.Tx(ctx, func(tx *sql.Tx) error {
		current, err := getSlashCommandTx(ctx, tx, id)
		if err != nil {
			return err
		}
		description := current.Description
		if patch.Description != nil {
			description = *patch.Description
		}
		syntaxHint := current.SyntaxHint
		if patch.SyntaxHint != nil {
			syntaxHint = *patch.SyntaxHint
		}
		webhookURL := current.WebhookURL
		if patch.WebhookURL != nil {
			webhookURL = *patch.WebhookURL
		}
		adminOnly := current.AdminOnly
		if patch.AdminOnly != nil {
			adminOnly = *patch.AdminOnly
		}
		status := current.Status
		if patch.Status != nil {
			status = *patch.Status
		}

		_, err = tx.ExecContext(ctx, `
			UPDATE slash_commands
			SET description = ?, syntax_hint = ?, webhook_url = ?, admin_only = ?, status = ?, updated_at = ?
			WHERE id = ?`,
			description, syntaxHint, webhookURL, boolInt(adminOnly), status, nowMillis(), id)
		if err != nil {
			return fmt.Errorf("update slash command: %w", err)
		}
		cmd, err = getSlashCommandTx(ctx, tx, id)
		return err
	})
	if err != nil {
		return SlashCommand{}, err
	}
	return cmd, nil
}

// RegenerateSlashCommandSecret replaces a slash command's signing secret in place, invalidating
// the previous plaintext immediately.
func (s *Store) RegenerateSlashCommandSecret(ctx context.Context, id string) (SlashCommand, string, error) {
	secret, err := generateSlashCommandSecret()
	if err != nil {
		return SlashCommand{}, "", err
	}
	hash := hashSlashCommandSecret(secret)
	last4 := secret[len(secret)-4:]
	now := nowMillis()

	var cmd SlashCommand
	err = s.Tx(ctx, func(tx *sql.Tx) error {
		if _, err := getSlashCommandTx(ctx, tx, id); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `
			UPDATE slash_commands SET secret = ?, secret_hash = ?, secret_last4 = ?, updated_at = ?
			WHERE id = ?`, secret, hash, last4, now, id); err != nil {
			return fmt.Errorf("regenerate slash command secret: %w", err)
		}
		var err2 error
		cmd, err2 = getSlashCommandTx(ctx, tx, id)
		return err2
	})
	if err != nil {
		return SlashCommand{}, "", err
	}
	return cmd, secret, nil
}

// DeleteSlashCommand deletes a slash command. Idempotent, matching DeleteWebhook's posture.
func (s *Store) DeleteSlashCommand(ctx context.Context, id string) error {
	_, err := s.writer.ExecContext(ctx, "DELETE FROM slash_commands WHERE id = ?", id)
	if err != nil {
		return fmt.Errorf("delete slash command: %w", err)
	}
	return nil
}

// CommandExecRequest is the resolved, authenticated context POST /commands/execute passes to
// ExecuteSlashCommand.
type CommandExecRequest struct {
	UserID    int64
	Username  string
	ChannelID int64
	ThreadID  *int64
	Args      []string
}

// CommandExecResult is the outcome of a synchronous slash-command execution. Failed is true for
// a non-2xx response or timeout — the API layer turns that into a synthesized ephemeral card
// rather than a 500, per SPEC.md §4.12 point 5 ("this endpoint always returns 200").
type CommandExecResult struct {
	ResponseType string // "ephemeral" | "in_channel"
	Text         string
	Attachments  json.RawMessage
	Failed       bool
	FailureKind  string // "timeout" | "error"
}

type commandExecPayload struct {
	UserID    string   `json:"user_id"`
	Username  string   `json:"username"`
	ChannelID string   `json:"channel_id"`
	ThreadID  *string  `json:"thread_id"`
	Args      []string `json:"args"`
}

type commandExecResponse struct {
	ResponseType string          `json:"response_type"`
	Text         string          `json:"text"`
	Attachments  json.RawMessage `json:"attachments"`
}

// slashCommandHTTPClient is a package-level http.Client so tests can swap its Transport.
var slashCommandHTTPClient = &http.Client{Timeout: 5 * time.Second}

// SetSlashCommandHTTPClientTimeoutForTest overrides the 5s execution timeout for the duration
// of a test, so a timeout path can be exercised without an actual 5s sleep. Returns a restore
// func; tests should defer it.
func SetSlashCommandHTTPClientTimeoutForTest(d time.Duration) (restore func()) {
	orig := slashCommandHTTPClient
	slashCommandHTTPClient = &http.Client{Timeout: d}
	return func() { slashCommandHTTPClient = orig }
}

// ExecuteSlashCommand makes exactly one synchronous, signed HTTP call to cmd's webhook_url with
// a 5s timeout — deliberately not the async retrying dispatch DispatchOutgoingWebhook uses,
// because the caller is blocked on the JSON response to render (SPEC.md §3.3/§4.12 point 4). A
// non-2xx response or timeout is returned as a value, not a Go error, so the caller can always
// respond 200 to the browser.
func (s *Store) ExecuteSlashCommand(ctx context.Context, cmd SlashCommand, req CommandExecRequest) (CommandExecResult, error) {
	var threadID *string
	if req.ThreadID != nil {
		v := fmt.Sprintf("%d", *req.ThreadID)
		threadID = &v
	}
	payload := commandExecPayload{
		UserID:    fmt.Sprintf("%d", req.UserID),
		Username:  req.Username,
		ChannelID: fmt.Sprintf("%d", req.ChannelID),
		ThreadID:  threadID,
		Args:      req.Args,
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return CommandExecResult{}, fmt.Errorf("marshal slash command payload: %w", err)
	}
	sentAt := time.Now().UnixMilli()
	signature := signOutgoingWebhookBody(cmd.Secret, body)

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, cmd.WebhookURL, bytes.NewReader(body))
	if err != nil {
		return CommandExecResult{}, fmt.Errorf("build slash command request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("X-Hivemind-Signature", signature)
	httpReq.Header.Set("X-Hivemind-Timestamp", fmt.Sprintf("%d", sentAt))

	resp, err := slashCommandHTTPClient.Do(httpReq)
	if err != nil {
		kind := "error"
		var netErr net.Error
		if errors.As(err, &netErr) && netErr.Timeout() {
			kind = "timeout"
		}
		slog.Warn("slash command webhook call failed", "trigger", cmd.Trigger, "command_id", cmd.ID, "kind", kind, "error", err)
		return CommandExecResult{Failed: true, FailureKind: kind}, nil
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		snippet := make([]byte, 500)
		n, _ := io.ReadFull(resp.Body, snippet)
		slog.Warn("slash command webhook returned a non-2xx status", "trigger", cmd.Trigger, "command_id", cmd.ID, "status", resp.StatusCode, "body", string(snippet[:n]))
		return CommandExecResult{Failed: true, FailureKind: "error"}, nil
	}

	var out commandExecResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		slog.Warn("slash command webhook response was not valid JSON", "trigger", cmd.Trigger, "command_id", cmd.ID, "error", err)
		return CommandExecResult{Failed: true, FailureKind: "error"}, nil
	}
	if out.ResponseType != "ephemeral" && out.ResponseType != "in_channel" {
		slog.Warn("slash command webhook response had an unrecognized response_type", "trigger", cmd.Trigger, "command_id", cmd.ID, "response_type", out.ResponseType)
		return CommandExecResult{Failed: true, FailureKind: "error"}, nil
	}
	return CommandExecResult{
		ResponseType: out.ResponseType,
		Text:         out.Text,
		Attachments:  out.Attachments,
	}, nil
}

// SetSlashCommandWebhookURLForTest bypasses the SSRF guard to point an existing slash command
// at a URL directly — for tests only, mirroring SetOutgoingWebhookTargetURLForTest so
// ExecuteSlashCommand can be exercised against a plain httptest.Server.
func (s *Store) SetSlashCommandWebhookURLForTest(ctx context.Context, id, webhookURL string) error {
	_, err := s.writer.ExecContext(ctx, "UPDATE slash_commands SET webhook_url = ?, updated_at = ? WHERE id = ?", webhookURL, nowMillis(), id)
	if err != nil {
		return fmt.Errorf("set slash command webhook url for test: %w", err)
	}
	return nil
}
