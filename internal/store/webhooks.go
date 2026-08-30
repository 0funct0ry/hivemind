package store

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base32"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"time"
)

// ErrWebhookNotFound is returned for every ingest-authentication failure mode (wrong token,
// disabled, orphaned-but-disabled, nonexistent) so the ingest handler can return one 404
// regardless of which it was — matching §7.2's non-member confirmation-avoidance posture.
var ErrWebhookNotFound = errors.New("webhook not found")

// Webhook status values (SPEC.md §3.2b).
const (
	WebhookStatusActive   = "active"
	WebhookStatusDisabled = "disabled"
	WebhookStatusOrphaned = "orphaned"
)

// Webhook is a persisted incoming-webhook configuration. It never carries the plaintext token
// or its hash — only SecretLast4, for masked display ("whk_••••••••ab12").
type Webhook struct {
	ID                      string
	ChannelID               int64
	BotUserID               int64
	CreatedBy               int64
	Name                    string
	FormatPreset            string
	DefaultDisplayName      string
	DefaultAvatarColor      string
	AllowPayloadOverride    bool
	DefaultSeverity         string
	NotifyChannelOnCritical bool
	ThreadID                *int64
	SecretLast4             string
	Status                  string
	CreatedAt               int64
	UpdatedAt               int64
	RegeneratedAt           *int64
	LastUsedAt              *int64
}

// WebhookInput contains the fields accepted when creating a webhook.
type WebhookInput struct {
	ChannelID               int64
	CreatedBy               int64
	Name                    string
	FormatPreset            string
	DefaultDisplayName      string
	DefaultAvatarColor      string
	AllowPayloadOverride    bool
	DefaultSeverity         string
	NotifyChannelOnCritical bool
	ThreadID                *int64
}

// WebhookPatch contains the optional fields PATCH /webhooks/:id may update; a nil field is left
// unchanged. channel_id is intentionally absent — it is immutable after creation (SPEC.md §4.10).
type WebhookPatch struct {
	Name                    *string
	FormatPreset            *string
	DefaultDisplayName      *string
	DefaultAvatarColor      *string
	AllowPayloadOverride    *bool
	DefaultSeverity         *string
	NotifyChannelOnCritical *bool
	ThreadID                **int64 // set to a non-nil pointer-to-pointer to change; nil pointee clears it
	Status                  *string
}

const webhookTokenPrefix = "whk_"

// generateWebhookToken returns a fresh "whk_<32 random bytes base64url>" plaintext secret.
func generateWebhookToken() (string, error) {
	b := make([]byte, 32)
	if _, err := io.ReadFull(rand.Reader, b); err != nil {
		return "", fmt.Errorf("generate webhook token: %w", err)
	}
	return webhookTokenPrefix + base64.RawURLEncoding.EncodeToString(b), nil
}

// hashWebhookToken returns the hex-encoded sha256 digest of a plaintext webhook token.
func hashWebhookToken(plaintext string) string {
	sum := sha256.Sum256([]byte(plaintext))
	return hex.EncodeToString(sum[:])
}

// generateWebhookID returns a 16-byte random, base32-encoded id — the same opaque-id shape as
// files.id, chosen because webhook ids are URL-exposed in the ingest path and must not be
// enumerable (SPEC.md §3.2b).
func generateWebhookID() (string, error) {
	b := make([]byte, 16)
	if _, err := io.ReadFull(rand.Reader, b); err != nil {
		return "", fmt.Errorf("generate webhook id: %w", err)
	}
	return base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(b), nil
}

const webhookColumns = `id, channel_id, bot_user_id, created_by, name, format_preset, default_display_name, default_avatar_color, allow_payload_override, default_severity, notify_channel_on_critical, thread_id, secret_last4, status, created_at, updated_at, regenerated_at, last_used_at`

func scanWebhook(row interface{ Scan(...any) error }) (Webhook, error) {
	var w Webhook
	var threadID, regeneratedAt, lastUsedAt sql.NullInt64
	var allowOverride, notifyCritical int
	err := row.Scan(&w.ID, &w.ChannelID, &w.BotUserID, &w.CreatedBy, &w.Name, &w.FormatPreset,
		&w.DefaultDisplayName, &w.DefaultAvatarColor, &allowOverride, &w.DefaultSeverity, &notifyCritical,
		&threadID, &w.SecretLast4, &w.Status, &w.CreatedAt, &w.UpdatedAt, &regeneratedAt, &lastUsedAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Webhook{}, ErrNotFound
		}
		return Webhook{}, err
	}
	w.AllowPayloadOverride = allowOverride != 0
	w.NotifyChannelOnCritical = notifyCritical != 0
	if threadID.Valid {
		w.ThreadID = &threadID.Int64
	}
	if regeneratedAt.Valid {
		w.RegeneratedAt = &regeneratedAt.Int64
	}
	if lastUsedAt.Valid {
		w.LastUsedAt = &lastUsedAt.Int64
	}
	return w, nil
}

// CreateWebhook creates the webhook's dedicated is_bot=1 bot user and the webhooks row in one
// transaction, generates a fresh "whk_<random>" token, and stores only its sha256 hash plus the
// last 4 plaintext characters. The plaintext is returned once and never persisted.
func (s *Store) CreateWebhook(ctx context.Context, in WebhookInput) (Webhook, string, error) {
	if strings.TrimSpace(in.Name) == "" {
		return Webhook{}, "", fmt.Errorf("webhook name is required")
	}
	preset := in.FormatPreset
	if preset == "" {
		preset = "generic"
	}
	if preset != "generic" && preset != "slack_compatible" {
		return Webhook{}, "", fmt.Errorf("format_preset must be generic or slack_compatible")
	}
	severity := in.DefaultSeverity
	if severity == "" {
		severity = "neutral"
	}
	if !validSeverity(severity) {
		return Webhook{}, "", fmt.Errorf("default_severity must be one of critical, warning, info, success, neutral")
	}

	plaintext, err := generateWebhookToken()
	if err != nil {
		return Webhook{}, "", err
	}
	hash := hashWebhookToken(plaintext)
	last4 := plaintext[len(plaintext)-4:]

	id, err := generateWebhookID()
	if err != nil {
		return Webhook{}, "", err
	}

	now := nowMillis()
	var wh Webhook

	err = s.Tx(ctx, func(tx *sql.Tx) error {
		var chExists int
		if err := tx.QueryRowContext(ctx, "SELECT 1 FROM channels WHERE id = ?", in.ChannelID).Scan(&chExists); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return ErrNotFound
			}
			return fmt.Errorf("check channel: %w", err)
		}

		botUsername := "webhook-" + strings.ToLower(id)
		res, err := tx.ExecContext(ctx, `
			INSERT INTO users(username, email, display_name, password_hash, avatar_color, role, is_bot, status, created_at, updated_at)
			VALUES (?, ?, ?, '', ?, 'member', 1, 'active', ?, ?)`,
			botUsername, botUsername+"@webhooks.invalid", in.Name, AvatarColor(botUsername), now, now)
		if err != nil {
			return fmt.Errorf("create webhook bot user: %w", err)
		}
		botUserID, err := res.LastInsertId()
		if err != nil {
			return fmt.Errorf("get webhook bot user id: %w", err)
		}

		allowOverride := 1
		if !in.AllowPayloadOverride {
			allowOverride = 0
		}
		notifyCritical := 0
		if in.NotifyChannelOnCritical {
			notifyCritical = 1
		}

		_, err = tx.ExecContext(ctx, `
			INSERT INTO webhooks(id, channel_id, bot_user_id, created_by, name, format_preset, default_display_name, default_avatar_color, allow_payload_override, default_severity, notify_channel_on_critical, thread_id, token_hash, secret_last4, status, created_at, updated_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 'active', ?, ?)`,
			id, in.ChannelID, botUserID, in.CreatedBy, in.Name, preset, in.DefaultDisplayName, in.DefaultAvatarColor,
			allowOverride, severity, notifyCritical, in.ThreadID, hash, last4, now, now)
		if err != nil {
			return fmt.Errorf("insert webhook: %w", err)
		}

		wh, err = getWebhookTx(ctx, tx, id)
		return err
	})
	if err != nil {
		return Webhook{}, "", err
	}

	return wh, plaintext, nil
}

func validSeverity(v string) bool {
	switch v {
	case "critical", "warning", "info", "success", "neutral":
		return true
	}
	return false
}

func getWebhookTx(ctx context.Context, tx *sql.Tx, id string) (Webhook, error) {
	row := tx.QueryRowContext(ctx, "SELECT "+webhookColumns+" FROM webhooks WHERE id = ?", id)
	return scanWebhook(row)
}

// GetWebhookByID fetches a single webhook by id.
func (s *Store) GetWebhookByID(ctx context.Context, id string) (Webhook, error) {
	row := s.reader.QueryRowContext(ctx, "SELECT "+webhookColumns+" FROM webhooks WHERE id = ?", id)
	w, err := scanWebhook(row)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return Webhook{}, ErrNotFound
		}
		return Webhook{}, fmt.Errorf("get webhook: %w", err)
	}
	return w, nil
}

// ListWebhooksForChannel lists every webhook targeting a channel.
func (s *Store) ListWebhooksForChannel(ctx context.Context, channelID int64) ([]Webhook, error) {
	rows, err := s.reader.QueryContext(ctx, "SELECT "+webhookColumns+" FROM webhooks WHERE channel_id = ? ORDER BY id", channelID)
	if err != nil {
		return nil, fmt.Errorf("list webhooks for channel: %w", err)
	}
	defer rows.Close()
	return scanWebhooks(rows)
}

// ListWebhooksForUser lists every webhook targeting a channel the user is owner/admin for —
// powers the workspace-wide GET /webhooks (SPEC.md §4.2/§4.10). An admin sees every webhook in
// the workspace; a non-admin sees only webhooks on channels they own.
func (s *Store) ListWebhooksForUser(ctx context.Context, userID int64, isAdmin bool) ([]Webhook, error) {
	var rows *sql.Rows
	var err error
	if isAdmin {
		rows, err = s.reader.QueryContext(ctx, "SELECT "+webhookColumns+" FROM webhooks ORDER BY id")
	} else {
		rows, err = s.reader.QueryContext(ctx, `
			SELECT `+webhookColumns+`
			FROM webhooks w
			WHERE EXISTS (
				SELECT 1 FROM channel_members cm
				WHERE cm.channel_id = w.channel_id AND cm.user_id = ? AND cm.role = 'owner'
			)
			ORDER BY w.id`, userID)
	}
	if err != nil {
		return nil, fmt.Errorf("list webhooks for user: %w", err)
	}
	defer rows.Close()
	return scanWebhooks(rows)
}

func scanWebhooks(rows *sql.Rows) ([]Webhook, error) {
	var out []Webhook
	for rows.Next() {
		w, err := scanWebhook(rows)
		if err != nil {
			return nil, fmt.Errorf("scan webhook: %w", err)
		}
		out = append(out, w)
	}
	return out, rows.Err()
}

// UpdateWebhook applies a partial patch to a webhook and returns the updated row. status may
// only toggle active<->disabled here — the third state, orphaned, is server-assigned via
// OrphanWebhooksForUser/ClaimWebhook, never set directly through this path.
func (s *Store) UpdateWebhook(ctx context.Context, id string, patch WebhookPatch) (Webhook, error) {
	if patch.FormatPreset != nil && *patch.FormatPreset != "generic" && *patch.FormatPreset != "slack_compatible" {
		return Webhook{}, fmt.Errorf("format_preset must be generic or slack_compatible")
	}
	if patch.DefaultSeverity != nil && !validSeverity(*patch.DefaultSeverity) {
		return Webhook{}, fmt.Errorf("default_severity must be one of critical, warning, info, success, neutral")
	}
	if patch.Status != nil && *patch.Status != WebhookStatusActive && *patch.Status != WebhookStatusDisabled {
		return Webhook{}, fmt.Errorf("status must be active or disabled")
	}

	var wh Webhook
	err := s.Tx(ctx, func(tx *sql.Tx) error {
		current, err := getWebhookTx(ctx, tx, id)
		if err != nil {
			return err
		}

		name := current.Name
		if patch.Name != nil {
			name = *patch.Name
		}
		preset := current.FormatPreset
		if patch.FormatPreset != nil {
			preset = *patch.FormatPreset
		}
		displayName := current.DefaultDisplayName
		if patch.DefaultDisplayName != nil {
			displayName = *patch.DefaultDisplayName
		}
		avatarColor := current.DefaultAvatarColor
		if patch.DefaultAvatarColor != nil {
			avatarColor = *patch.DefaultAvatarColor
		}
		allowOverride := current.AllowPayloadOverride
		if patch.AllowPayloadOverride != nil {
			allowOverride = *patch.AllowPayloadOverride
		}
		severity := current.DefaultSeverity
		if patch.DefaultSeverity != nil {
			severity = *patch.DefaultSeverity
		}
		notifyCritical := current.NotifyChannelOnCritical
		if patch.NotifyChannelOnCritical != nil {
			notifyCritical = *patch.NotifyChannelOnCritical
		}
		threadID := current.ThreadID
		if patch.ThreadID != nil {
			threadID = *patch.ThreadID
		}
		status := current.Status
		if patch.Status != nil {
			status = *patch.Status
		}

		_, err = tx.ExecContext(ctx, `
			UPDATE webhooks
			SET name = ?, format_preset = ?, default_display_name = ?, default_avatar_color = ?,
			    allow_payload_override = ?, default_severity = ?, notify_channel_on_critical = ?,
			    thread_id = ?, status = ?, updated_at = ?
			WHERE id = ?`,
			name, preset, displayName, avatarColor, boolInt(allowOverride), severity, boolInt(notifyCritical),
			threadID, status, nowMillis(), id)
		if err != nil {
			return fmt.Errorf("update webhook: %w", err)
		}

		wh, err = getWebhookTx(ctx, tx, id)
		return err
	})
	if err != nil {
		return Webhook{}, err
	}
	return wh, nil
}

// RegenerateWebhookToken replaces a webhook's token hash in place, invalidating the previous
// plaintext immediately — same shown-once shape as RotateAPIToken (§4.9).
func (s *Store) RegenerateWebhookToken(ctx context.Context, id string) (Webhook, string, error) {
	plaintext, err := generateWebhookToken()
	if err != nil {
		return Webhook{}, "", err
	}
	hash := hashWebhookToken(plaintext)
	last4 := plaintext[len(plaintext)-4:]
	now := nowMillis()

	var wh Webhook
	err = s.Tx(ctx, func(tx *sql.Tx) error {
		if _, err := getWebhookTx(ctx, tx, id); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `
			UPDATE webhooks SET token_hash = ?, secret_last4 = ?, regenerated_at = ?, updated_at = ?
			WHERE id = ?`, hash, last4, now, now, id); err != nil {
			return fmt.Errorf("regenerate webhook token: %w", err)
		}
		var err2 error
		wh, err2 = getWebhookTx(ctx, tx, id)
		return err2
	})
	if err != nil {
		return Webhook{}, "", err
	}
	return wh, plaintext, nil
}

// DeleteWebhook deletes a webhook. Idempotent — deleting an already-gone webhook is a no-op,
// matching §4.7's delete posture.
func (s *Store) DeleteWebhook(ctx context.Context, id string) error {
	_, err := s.writer.ExecContext(ctx, "DELETE FROM webhooks WHERE id = ?", id)
	if err != nil {
		return fmt.Errorf("delete webhook: %w", err)
	}
	return nil
}

// AuthenticateWebhook is the ingest endpoint's only auth call: a single point lookup on
// (id, token_hash, status='active'). Every failure mode — wrong token, wrong id, disabled,
// orphaned, nonexistent — collapses to ErrWebhookNotFound so the caller returns one 404
// regardless of which it was.
func (s *Store) AuthenticateWebhook(ctx context.Context, id, token string) (Webhook, error) {
	hash := hashWebhookToken(token)
	row := s.reader.QueryRowContext(ctx, "SELECT "+webhookColumns+" FROM webhooks WHERE id = ? AND token_hash = ? AND status = 'active'", id, hash)
	w, err := scanWebhook(row)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return Webhook{}, ErrWebhookNotFound
		}
		return Webhook{}, fmt.Errorf("authenticate webhook: %w", err)
	}
	if _, err := s.writer.ExecContext(ctx, "UPDATE webhooks SET last_used_at = ? WHERE id = ?", nowMillis(), w.ID); err != nil {
		return Webhook{}, fmt.Errorf("update webhook last used: %w", err)
	}
	return w, nil
}

// OrphanWebhosForUser is a package-private typo guard; see OrphanWebhooksForUser below.

// OrphanWebhooksForUser flips status='orphaned' for every active/active-ish webhook owned by
// userID, leaving already-disabled webhooks alone (SPEC.md §4.10's "Deactivated creator" edge
// case) — called from POST /users/:id/deactivate's existing transaction.
func (s *Store) OrphanWebhooksForUser(ctx context.Context, tx *sql.Tx, userID int64) error {
	_, err := tx.ExecContext(ctx, `
		UPDATE webhooks SET status = 'orphaned', updated_at = ?
		WHERE created_by = ? AND status = 'active'`, nowMillis(), userID)
	if err != nil {
		return fmt.Errorf("orphan webhooks for user: %w", err)
	}
	return nil
}

// ClaimWebhook reassigns an orphaned webhook to adminID and restores status='active', unless it
// was separately disabled (claim does not override an explicit disable). No-op error if the
// webhook is not currently orphaned.
func (s *Store) ClaimWebhook(ctx context.Context, id string, adminID int64) (Webhook, error) {
	res, err := s.writer.ExecContext(ctx, `
		UPDATE webhooks SET created_by = ?, status = 'active', updated_at = ?
		WHERE id = ? AND status = 'orphaned'`, adminID, nowMillis(), id)
	if err != nil {
		return Webhook{}, fmt.Errorf("claim webhook: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return Webhook{}, fmt.Errorf("claim webhook rows affected: %w", err)
	}
	if n == 0 {
		if _, err := s.GetWebhookByID(ctx, id); err != nil {
			return Webhook{}, err
		}
		return Webhook{}, fmt.Errorf("webhook is not orphaned")
	}
	return s.GetWebhookByID(ctx, id)
}

// webhookFallbackChannelName is the lazily-created channel a webhook post redirects to when its
// target channel is archived (SPEC.md §4.10).
const webhookFallbackChannelSlug = "webhook-fallback"

// getOrCreateWebhookFallbackChannel returns the workspace's admin-membered private
// #webhook-fallback channel, creating it once on first use.
func getOrCreateWebhookFallbackChannel(ctx context.Context, tx *sql.Tx, createdBy int64) (int64, error) {
	var id int64
	err := tx.QueryRowContext(ctx, "SELECT id FROM channels WHERE slug = ?", webhookFallbackChannelSlug).Scan(&id)
	if err == nil {
		return id, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return 0, fmt.Errorf("lookup webhook fallback channel: %w", err)
	}

	now := nowMillis()
	res, err := tx.ExecContext(ctx, `
		INSERT INTO channels(kind, slug, name, topic, dm_key, created_by, created_at, updated_at)
		VALUES ('private', ?, ?, ?, NULL, ?, ?, ?)`,
		webhookFallbackChannelSlug, webhookFallbackChannelSlug, "Alerts redirected here from archived channels", createdBy, now, now)
	if err != nil {
		// Lost the create race against a concurrent caller — re-select.
		var raceID int64
		if selErr := tx.QueryRowContext(ctx, "SELECT id FROM channels WHERE slug = ?", webhookFallbackChannelSlug).Scan(&raceID); selErr == nil {
			return raceID, nil
		}
		return 0, fmt.Errorf("create webhook fallback channel: %w", err)
	}
	id, err = res.LastInsertId()
	if err != nil {
		return 0, fmt.Errorf("get webhook fallback channel id: %w", err)
	}

	// Membership: every workspace admin, so the redirect is actually visible to someone.
	rows, err := tx.QueryContext(ctx, "SELECT id FROM users WHERE role = 'admin' AND status = 'active'")
	if err != nil {
		return 0, fmt.Errorf("list admins for webhook fallback channel: %w", err)
	}
	var adminIDs []int64
	for rows.Next() {
		var aid int64
		if err := rows.Scan(&aid); err != nil {
			rows.Close()
			return 0, fmt.Errorf("scan admin id: %w", err)
		}
		adminIDs = append(adminIDs, aid)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return 0, err
	}
	seen := map[int64]bool{createdBy: true}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO channel_members(channel_id, user_id, role, joined_at)
		VALUES (?, ?, 'owner', ?)`, id, createdBy, now); err != nil {
		return 0, fmt.Errorf("add webhook fallback channel owner: %w", err)
	}
	for _, aid := range adminIDs {
		if seen[aid] {
			continue
		}
		seen[aid] = true
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO channel_members(channel_id, user_id, role, joined_at)
			VALUES (?, ?, 'member', ?)`, id, aid, now); err != nil {
			return 0, fmt.Errorf("add webhook fallback channel admin: %w", err)
		}
	}
	return id, nil
}

// WebhookCard is the JSON shape stored in messages.card (SPEC.md §4.10).
type WebhookCard struct {
	Title          string         `json:"title"`
	Severity       string         `json:"severity"`
	Fields         []WebhookField `json:"fields,omitempty"`
	Body           string         `json:"body"`
	DisplayName    string         `json:"display_name,omitempty"`
	AvatarURL      string         `json:"avatar_url,omitempty"`
	Fallback       bool           `json:"fallback"`
	RedirectNotice bool           `json:"redirect_notice"`
}

// WebhookField is one label/value pair in a WebhookCard's field grid.
type WebhookField struct {
	Label string `json:"label"`
	Value string `json:"value"`
}

// WebhookMessageInput carries a normalized payload card plus the owning webhook, ready for
// CreateWebhookMessage to resolve into a concrete message.
type WebhookMessageInput struct {
	Webhook Webhook
	// Card fields as parsed from the raw payload (internal/webhooks.Card, kept untyped here to
	// avoid an import cycle — internal/api maps webhooks.Card into these fields).
	Title           string
	Severity        string // "" if the payload did not resolve one
	Fields          []WebhookField
	Body            string
	DisplayName     string // "" if the payload did not supply one
	AvatarURL       string // "" if the payload did not supply one
	PayloadThreadID string // "" if the payload did not supply one
	Fallback        bool
}

// CreateWebhookMessage resolves the target channel (redirecting to the lazily-created
// #webhook-fallback channel if the target is archived), resolves severity/display-identity/
// thread per the webhook's configuration and SPEC.md §4.10, builds body+card, and posts through
// the existing CreateMessage path as the webhook's bot user. If the resolved severity is
// critical and the webhook has notify_channel_on_critical=1, an @channel mention row is inserted
// in the same logical operation, reusing the existing mentions table rather than a new pipeline.
func (s *Store) CreateWebhookMessage(ctx context.Context, in WebhookMessageInput) (Message, error) {
	wh := in.Webhook

	ch, err := s.GetChannel(ctx, wh.ChannelID)
	if err != nil {
		return Message{}, fmt.Errorf("get webhook target channel: %w", err)
	}

	redirectNotice := false
	targetChannelID := wh.ChannelID
	var originalTargetLabel string
	if ch.ArchivedAt != nil {
		redirectNotice = true
		originalTargetLabel = channelDisplayLabel(ch)
		var fallbackID int64
		err := s.Tx(ctx, func(tx *sql.Tx) error {
			id, err := getOrCreateWebhookFallbackChannel(ctx, tx, wh.CreatedBy)
			fallbackID = id
			return err
		})
		if err != nil {
			return Message{}, err
		}
		targetChannelID = fallbackID
		// Ensure the bot user is a member so CreateMessage's channel checks succeed.
		if err := s.AddMembers(ctx, targetChannelID, []int64{wh.BotUserID}); err != nil {
			return Message{}, fmt.Errorf("add webhook bot to fallback channel: %w", err)
		}
	} else {
		if err := s.AddMembers(ctx, targetChannelID, []int64{wh.BotUserID}); err != nil {
			return Message{}, fmt.Errorf("add webhook bot to channel: %w", err)
		}
	}

	severity := in.Severity
	if !validSeverity(severity) {
		severity = wh.DefaultSeverity
	}

	displayName := wh.DefaultDisplayName
	avatarURL := wh.DefaultAvatarColor
	if wh.AllowPayloadOverride {
		if in.DisplayName != "" {
			displayName = in.DisplayName
		}
		if in.AvatarURL != "" {
			avatarURL = in.AvatarURL
		}
	}

	var threadID *int64
	if in.PayloadThreadID != "" {
		if id, err := parseThreadIDInChannel(ctx, s, in.PayloadThreadID, targetChannelID); err == nil {
			threadID = &id
		}
	}
	if threadID == nil && wh.ThreadID != nil {
		threadID = wh.ThreadID
	}

	fields := in.Fields
	if redirectNotice {
		fields = append(fields, WebhookField{Label: "Original target", Value: originalTargetLabel + " (archived)"})
	}

	card := WebhookCard{
		Title:          in.Title,
		Severity:       severity,
		Fields:         fields,
		Body:           in.Body,
		DisplayName:    displayName,
		AvatarURL:      avatarURL,
		Fallback:       in.Fallback,
		RedirectNotice: redirectNotice,
	}
	cardJSON, err := json.Marshal(card)
	if err != nil {
		return Message{}, fmt.Errorf("marshal webhook card: %w", err)
	}
	cardStr := string(cardJSON)

	body := buildWebhookBody(in.Title, in.Body)
	whID := wh.ID

	msgIn := MessageInput{
		ChannelID: targetChannelID,
		UserID:    wh.BotUserID,
		Body:      body,
		ThreadID:  threadID,
		WebhookID: &whID,
		Card:      &cardStr,
	}
	msg, _, err := s.CreateMessage(ctx, msgIn)
	if err != nil {
		return Message{}, err
	}

	if severity == "critical" && wh.NotifyChannelOnCritical {
		if err := s.Tx(ctx, func(tx *sql.Tx) error {
			_, err := tx.ExecContext(ctx, `
				INSERT INTO mentions (message_id, user_id, channel_id, kind, created_at)
				SELECT ?, cm.user_id, ?, 'channel', ?
				FROM channel_members cm
				WHERE cm.channel_id = ? AND cm.user_id != ?`,
				msg.ID, targetChannelID, nowMillis(), targetChannelID, wh.BotUserID)
			return err
		}); err != nil {
			return Message{}, fmt.Errorf("insert critical webhook channel mentions: %w", err)
		}
	}

	return msg, nil
}

// channelDisplayLabel renders a human label for a channel in redirect-notice fields.
func channelDisplayLabel(ch Channel) string {
	if ch.Slug != nil && *ch.Slug != "" {
		return "#" + *ch.Slug
	}
	return ch.Name
}

// buildWebhookBody constructs the fallback markdown body stored in messages.body for a webhook
// message (always populated, per §3.2b, so FTS/CLI/notifications need no special-casing).
func buildWebhookBody(title, body string) string {
	switch {
	case title != "" && body != "":
		return title + "\n\n" + body
	case title != "":
		return title
	case body != "":
		return body
	default:
		return "(webhook alert)"
	}
}

// parseThreadIDInChannel resolves a payload-supplied thread_id string to an int64, only if it
// names an existing root message in the given channel — a stale id from an external tool must
// never hard-fail delivery (SPEC.md §4.10), so any failure here is the caller's cue to fall back.
func parseThreadIDInChannel(ctx context.Context, s *Store, raw string, channelID int64) (int64, error) {
	var id int64
	if _, err := fmt.Sscanf(raw, "%d", &id); err != nil {
		return 0, err
	}
	var gotChannelID int64
	var threadID sql.NullInt64
	err := s.reader.QueryRowContext(ctx, "SELECT channel_id, thread_id FROM messages WHERE id = ?", id).Scan(&gotChannelID, &threadID)
	if err != nil {
		return 0, err
	}
	if gotChannelID != channelID || threadID.Valid {
		return 0, fmt.Errorf("thread_id does not name a root message in this channel")
	}
	return id, nil
}

// --- Webhook flood collapse (SPEC.md §4.10, §5.5) ---
//
// The hot-path check is an in-memory sliding window keyed by webhook_id, mirroring
// internal/api/messages.go's postLimiter shape: 10 payloads within a 3-second debounce window
// collapse the 11th and subsequent payloads into one continuously-updating summary row. Only the
// pending summary's identity needs to survive a process restart (webhook_flood_state); the
// window itself is process-local and resets on restart, which is an acceptable cold-start reset.

type webhookFloodWindow struct {
	firstAt   time.Time
	count     int
	summaryID int64
}

type webhookFloodTracker struct {
	mu      sync.Mutex
	windows map[string]*webhookFloodWindow
}

var floodTracker = &webhookFloodTracker{windows: make(map[string]*webhookFloodWindow)}

const (
	webhookFloodThreshold = 10
	webhookFloodWindowDur = 3 * time.Second
)

// CheckWebhookFlood reports whether the payload arriving now for webhookID should collapse into
// an existing in-flight summary message rather than posting a fresh one. The window is a
// debounce — it resets on every new payload — so a sustained flood produces one
// continuously-updating summary instead of one per fixed window.
func (s *Store) CheckWebhookFlood(webhookID string, now time.Time) (collapse bool, summaryMessageID *int64) {
	floodTracker.mu.Lock()
	defer floodTracker.mu.Unlock()

	w, ok := floodTracker.windows[webhookID]
	if !ok || now.Sub(w.firstAt) > webhookFloodWindowDur {
		floodTracker.windows[webhookID] = &webhookFloodWindow{firstAt: now, count: 1}
		return false, nil
	}

	w.firstAt = now // debounce: reset on every payload
	w.count++
	if w.count <= webhookFloodThreshold {
		return false, nil
	}
	if w.summaryID == 0 {
		return false, nil // caller will set the summary id once it creates the first collapsed message
	}
	id := w.summaryID
	return true, &id
}

// NoteWebhookFloodSummary records which message id is the in-flight flood summary for a
// webhook, so subsequent CheckWebhookFlood calls in the same window can find it.
func (s *Store) NoteWebhookFloodSummary(ctx context.Context, webhookID string, messageID int64) error {
	floodTracker.mu.Lock()
	if w, ok := floodTracker.windows[webhookID]; ok {
		w.summaryID = messageID
	}
	floodTracker.mu.Unlock()

	_, err := s.writer.ExecContext(ctx, `
		INSERT INTO webhook_flood_state (webhook_id, window_started_at, collapsed_count, summary_message_id)
		VALUES (?, ?, 1, ?)
		ON CONFLICT(webhook_id) DO UPDATE SET
			window_started_at = excluded.window_started_at,
			collapsed_count = 1,
			summary_message_id = excluded.summary_message_id`,
		webhookID, nowMillis(), messageID)
	if err != nil {
		return fmt.Errorf("note webhook flood summary: %w", err)
	}
	return nil
}

// UpdateWebhookFloodSummary updates the tracked summary message's card in place rather than
// inserting a new row, collapsing repeated floods into one continuously-updating message.
func (s *Store) UpdateWebhookFloodSummary(ctx context.Context, webhookID string, card WebhookCard) (Message, error) {
	floodTracker.mu.Lock()
	w, ok := floodTracker.windows[webhookID]
	var messageID int64
	if ok {
		messageID = w.summaryID
	}
	floodTracker.mu.Unlock()

	if messageID == 0 {
		var summaryIDVal sql.NullInt64
		if err := s.reader.QueryRowContext(ctx, "SELECT summary_message_id FROM webhook_flood_state WHERE webhook_id = ?", webhookID).Scan(&summaryIDVal); err != nil {
			return Message{}, fmt.Errorf("lookup webhook flood summary: %w", err)
		}
		if !summaryIDVal.Valid {
			return Message{}, fmt.Errorf("no webhook flood summary in flight for %s", webhookID)
		}
		messageID = summaryIDVal.Int64
	}

	cardJSON, err := json.Marshal(card)
	if err != nil {
		return Message{}, fmt.Errorf("marshal webhook flood summary card: %w", err)
	}
	cardStr := string(cardJSON)
	body := buildWebhookBody(card.Title, card.Body)

	_, err = s.writer.ExecContext(ctx, `
		UPDATE messages SET body = ?, card = ?, edited_at = ?
		WHERE id = ?`, body, cardStr, nowMillis(), messageID)
	if err != nil {
		return Message{}, fmt.Errorf("update webhook flood summary message: %w", err)
	}

	if _, err := s.writer.ExecContext(ctx, `
		UPDATE webhook_flood_state
		SET collapsed_count = collapsed_count + 1
		WHERE webhook_id = ?`, webhookID); err != nil {
		return Message{}, fmt.Errorf("update webhook flood state count: %w", err)
	}

	return s.GetMessage(ctx, messageID)
}

// ResetWebhookFloodTracker clears the in-memory flood window state, for test isolation.
func ResetWebhookFloodTracker() {
	floodTracker.mu.Lock()
	floodTracker.windows = make(map[string]*webhookFloodWindow)
	floodTracker.mu.Unlock()
}
