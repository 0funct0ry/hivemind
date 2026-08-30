package store

import (
	"bytes"
	"context"
	"crypto/hmac"
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
	"log/slog"
	"net/http"
	"strings"
	"time"
)

// Outgoing webhook status values (SPEC.md §3.2c/§4.11).
const (
	OutgoingWebhookStatusActive    = "active"
	OutgoingWebhookStatusDisabled  = "disabled"
	OutgoingWebhookStatusUnhealthy = "unhealthy"
)

// outgoingWebhookUnhealthyThreshold is the number of consecutive failed deliveries after which
// an outgoing webhook auto-flips to status='unhealthy' and stops dispatching (SPEC.md §4.11).
const outgoingWebhookUnhealthyThreshold = 20

// OutgoingWebhook is a persisted outgoing-webhook subscription. Secret is the plaintext signing
// key — unlike an incoming webhook's token, dispatch must recompute an HMAC from the real key,
// not merely compare a hash, so it cannot be discarded after creation (see the migration
// comment in 0009_outgoing_webhooks.sql). It is never included in any API response; only
// SecretLast4 is, for masked display ("whsec_••••••••ab12").
type OutgoingWebhook struct {
	ID                  string
	ChannelID           int64
	CreatedBy           int64
	Name                string
	TargetURL           string
	Secret              string
	SecretLast4         string
	EventType           string
	KeywordFilter       string
	Status              string
	ConsecutiveFailures int
	CreatedAt           int64
	UpdatedAt           int64
	LastTriggeredAt     *int64
	LastSuccessAt       *int64
}

// OutgoingWebhookInput contains the fields accepted when creating an outgoing webhook.
type OutgoingWebhookInput struct {
	ChannelID     int64
	CreatedBy     int64
	Name          string
	TargetURL     string
	KeywordFilter string
}

// OutgoingWebhookPatch contains the optional fields PATCH /outgoing-webhooks/:id may update; a
// nil field is left unchanged. channel_id is intentionally absent — immutable after creation,
// same posture as an incoming webhook's channel_id (SPEC.md §4.11).
type OutgoingWebhookPatch struct {
	Name          *string
	TargetURL     *string
	KeywordFilter *string
	Status        *string
}

// OutgoingEvent is the event dispatched to an outgoing webhook's target URL.
type OutgoingEvent struct {
	MessageID int64
	ChannelID int64
	Channel   OutgoingEventChannel
	Message   any // the same public JSON shape the REST message resource renders
}

// OutgoingEventChannel is the minimal channel identity carried in an outgoing payload.
type OutgoingEventChannel struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// outgoingWebhookPayload is the exact wire shape SPEC.md §4.11 documents.
type outgoingWebhookPayload struct {
	Event   string               `json:"event"`
	SentAt  int64                `json:"sent_at"`
	Channel OutgoingEventChannel `json:"channel"`
	Message any                  `json:"message"`
}

// DeliveryAttempt is one recorded attempt at delivering an outgoing webhook event.
type DeliveryAttempt struct {
	MessageID           int64
	AttemptNumber       int
	RequestBody         string
	ResponseStatus      *int
	ResponseBodySnippet string
	LatencyMs           int64
}

// Delivery is one row from outgoing_webhook_deliveries, as returned by ListDeliveries.
type Delivery struct {
	ID                  int64
	WebhookID           string
	MessageID           *int64
	AttemptNumber       int
	RequestBody         string
	ResponseStatus      *int
	ResponseBodySnippet string
	LatencyMs           *int64
	CreatedAt           int64
}

const outgoingWebhookSecretPrefix = "whsec_"

// generateOutgoingWebhookSecret returns a fresh "whsec_<32 random bytes base64url>" plaintext.
func generateOutgoingWebhookSecret() (string, error) {
	b := make([]byte, 32)
	if _, err := io.ReadFull(rand.Reader, b); err != nil {
		return "", fmt.Errorf("generate outgoing webhook secret: %w", err)
	}
	return outgoingWebhookSecretPrefix + base64.RawURLEncoding.EncodeToString(b), nil
}

// hashOutgoingWebhookSecret returns the hex-encoded sha256 digest of a plaintext secret.
func hashOutgoingWebhookSecret(plaintext string) string {
	sum := sha256.Sum256([]byte(plaintext))
	return hex.EncodeToString(sum[:])
}

// generateOutgoingWebhookID returns a 16-byte random, base32-encoded id — the same opaque-id
// shape as webhooks.id, since outgoing-webhook ids are API-exposed and must not be enumerable.
func generateOutgoingWebhookID() (string, error) {
	b := make([]byte, 16)
	if _, err := io.ReadFull(rand.Reader, b); err != nil {
		return "", fmt.Errorf("generate outgoing webhook id: %w", err)
	}
	return base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(b), nil
}

const outgoingWebhookColumns = `id, channel_id, created_by, name, target_url, secret, secret_last4, event_type, keyword_filter, status, consecutive_failures, created_at, updated_at, last_triggered_at, last_success_at`

func scanOutgoingWebhook(row interface{ Scan(...any) error }) (OutgoingWebhook, error) {
	var w OutgoingWebhook
	var keywordFilter sql.NullString
	var lastTriggeredAt, lastSuccessAt sql.NullInt64
	err := row.Scan(&w.ID, &w.ChannelID, &w.CreatedBy, &w.Name, &w.TargetURL, &w.Secret, &w.SecretLast4,
		&w.EventType, &keywordFilter, &w.Status, &w.ConsecutiveFailures, &w.CreatedAt, &w.UpdatedAt,
		&lastTriggeredAt, &lastSuccessAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return OutgoingWebhook{}, ErrNotFound
		}
		return OutgoingWebhook{}, err
	}
	if keywordFilter.Valid {
		w.KeywordFilter = keywordFilter.String
	}
	if lastTriggeredAt.Valid {
		w.LastTriggeredAt = &lastTriggeredAt.Int64
	}
	if lastSuccessAt.Valid {
		w.LastSuccessAt = &lastSuccessAt.Int64
	}
	return w, nil
}

// CreateOutgoingWebhook validates target_url against the SSRF guard, generates a fresh
// "whsec_<random>" secret, and stores it alongside its sha256 hash and last-4 plaintext
// characters. The plaintext is returned once and never persisted anywhere except the secret
// column dispatch itself reads (see 0009_outgoing_webhooks.sql's column comment).
func (s *Store) CreateOutgoingWebhook(ctx context.Context, in OutgoingWebhookInput) (OutgoingWebhook, string, error) {
	if strings.TrimSpace(in.Name) == "" {
		return OutgoingWebhook{}, "", fmt.Errorf("name is required")
	}
	if err := validateOutgoingWebhookTargetURL(ctx, in.TargetURL); err != nil {
		return OutgoingWebhook{}, "", err
	}

	id, err := generateOutgoingWebhookID()
	if err != nil {
		return OutgoingWebhook{}, "", err
	}
	secret, err := generateOutgoingWebhookSecret()
	if err != nil {
		return OutgoingWebhook{}, "", err
	}
	hash := hashOutgoingWebhookSecret(secret)
	last4 := secret[len(secret)-4:]
	now := nowMillis()

	var wh OutgoingWebhook
	err = s.Tx(ctx, func(tx *sql.Tx) error {
		var chExists int
		if err := tx.QueryRowContext(ctx, "SELECT 1 FROM channels WHERE id = ?", in.ChannelID).Scan(&chExists); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return ErrNotFound
			}
			return fmt.Errorf("check channel: %w", err)
		}
		_, err := tx.ExecContext(ctx, `
			INSERT INTO outgoing_webhooks(id, channel_id, created_by, name, target_url, secret, secret_hash, secret_last4, keyword_filter, status, created_at, updated_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, 'active', ?, ?)`,
			id, in.ChannelID, in.CreatedBy, in.Name, in.TargetURL, secret, hash, last4, nullableString(in.KeywordFilter), now, now)
		if err != nil {
			return fmt.Errorf("insert outgoing webhook: %w", err)
		}
		wh, err = getOutgoingWebhookTx(ctx, tx, id)
		return err
	})
	if err != nil {
		return OutgoingWebhook{}, "", err
	}
	return wh, secret, nil
}

func nullableString(v string) any {
	if v == "" {
		return nil
	}
	return v
}

func getOutgoingWebhookTx(ctx context.Context, tx *sql.Tx, id string) (OutgoingWebhook, error) {
	row := tx.QueryRowContext(ctx, "SELECT "+outgoingWebhookColumns+" FROM outgoing_webhooks WHERE id = ?", id)
	return scanOutgoingWebhook(row)
}

// GetOutgoingWebhookByID fetches a single outgoing webhook by id.
func (s *Store) GetOutgoingWebhookByID(ctx context.Context, id string) (OutgoingWebhook, error) {
	row := s.reader.QueryRowContext(ctx, "SELECT "+outgoingWebhookColumns+" FROM outgoing_webhooks WHERE id = ?", id)
	w, err := scanOutgoingWebhook(row)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return OutgoingWebhook{}, ErrNotFound
		}
		return OutgoingWebhook{}, fmt.Errorf("get outgoing webhook: %w", err)
	}
	return w, nil
}

// ListOutgoingWebhooksForChannel lists every outgoing webhook targeting a channel.
func (s *Store) ListOutgoingWebhooksForChannel(ctx context.Context, channelID int64) ([]OutgoingWebhook, error) {
	rows, err := s.reader.QueryContext(ctx, "SELECT "+outgoingWebhookColumns+" FROM outgoing_webhooks WHERE channel_id = ? ORDER BY id", channelID)
	if err != nil {
		return nil, fmt.Errorf("list outgoing webhooks for channel: %w", err)
	}
	defer rows.Close()
	return scanOutgoingWebhooks(rows)
}

// ListOutgoingWebhooksForUser lists every outgoing webhook the caller can manage across every
// channel — powers the workspace-wide GET /outgoing-webhooks. An admin sees every outgoing
// webhook in the workspace; a non-admin sees only those on channels they own.
func (s *Store) ListOutgoingWebhooksForUser(ctx context.Context, userID int64, isAdmin bool) ([]OutgoingWebhook, error) {
	var rows *sql.Rows
	var err error
	if isAdmin {
		rows, err = s.reader.QueryContext(ctx, "SELECT "+outgoingWebhookColumns+" FROM outgoing_webhooks ORDER BY id")
	} else {
		rows, err = s.reader.QueryContext(ctx, `
			SELECT `+outgoingWebhookColumns+`
			FROM outgoing_webhooks w
			WHERE EXISTS (
				SELECT 1 FROM channel_members cm
				WHERE cm.channel_id = w.channel_id AND cm.user_id = ? AND cm.role = 'owner'
			)
			ORDER BY w.id`, userID)
	}
	if err != nil {
		return nil, fmt.Errorf("list outgoing webhooks for user: %w", err)
	}
	defer rows.Close()
	return scanOutgoingWebhooks(rows)
}

func scanOutgoingWebhooks(rows *sql.Rows) ([]OutgoingWebhook, error) {
	var out []OutgoingWebhook
	for rows.Next() {
		w, err := scanOutgoingWebhook(rows)
		if err != nil {
			return nil, fmt.Errorf("scan outgoing webhook: %w", err)
		}
		out = append(out, w)
	}
	return out, rows.Err()
}

// UpdateOutgoingWebhook applies a partial patch and returns the updated row. status may only be
// set to active or disabled here — unhealthy is server-assigned by DispatchOutgoingWebhook.
// Setting status=active while the current status is unhealthy is "the dedicated action" SPEC.md
// §4.11 describes for re-enabling: there is no separate endpoint, PATCH plays that role and
// resets consecutive_failures to 0 in the same statement.
func (s *Store) UpdateOutgoingWebhook(ctx context.Context, id string, patch OutgoingWebhookPatch) (OutgoingWebhook, error) {
	if patch.Status != nil && *patch.Status != OutgoingWebhookStatusActive && *patch.Status != OutgoingWebhookStatusDisabled {
		return OutgoingWebhook{}, fmt.Errorf("status must be active or disabled")
	}
	if patch.TargetURL != nil {
		if err := validateOutgoingWebhookTargetURL(ctx, *patch.TargetURL); err != nil {
			return OutgoingWebhook{}, err
		}
	}

	var wh OutgoingWebhook
	err := s.Tx(ctx, func(tx *sql.Tx) error {
		current, err := getOutgoingWebhookTx(ctx, tx, id)
		if err != nil {
			return err
		}

		name := current.Name
		if patch.Name != nil {
			name = *patch.Name
		}
		targetURL := current.TargetURL
		if patch.TargetURL != nil {
			targetURL = *patch.TargetURL
		}
		keywordFilter := current.KeywordFilter
		if patch.KeywordFilter != nil {
			keywordFilter = *patch.KeywordFilter
		}
		status := current.Status
		resetFailures := false
		if patch.Status != nil {
			status = *patch.Status
			// Re-enabling from unhealthy is "the dedicated action" SPEC.md §4.11 describes —
			// there is no separate endpoint for it, PATCH status=active plays that role.
			if status == OutgoingWebhookStatusActive && current.Status == OutgoingWebhookStatusUnhealthy {
				resetFailures = true
			}
		}

		if resetFailures {
			_, err = tx.ExecContext(ctx, `
				UPDATE outgoing_webhooks
				SET name = ?, target_url = ?, keyword_filter = ?, status = ?, consecutive_failures = 0, updated_at = ?
				WHERE id = ?`,
				name, targetURL, nullableString(keywordFilter), status, nowMillis(), id)
		} else {
			_, err = tx.ExecContext(ctx, `
				UPDATE outgoing_webhooks
				SET name = ?, target_url = ?, keyword_filter = ?, status = ?, updated_at = ?
				WHERE id = ?`,
				name, targetURL, nullableString(keywordFilter), status, nowMillis(), id)
		}
		if err != nil {
			return fmt.Errorf("update outgoing webhook: %w", err)
		}
		wh, err = getOutgoingWebhookTx(ctx, tx, id)
		return err
	})
	if err != nil {
		return OutgoingWebhook{}, err
	}
	return wh, nil
}

// RegenerateOutgoingWebhookSecret replaces an outgoing webhook's signing secret in place,
// invalidating the previous plaintext immediately — same shown-once shape as
// RegenerateWebhookToken (SPEC.md §4.9).
func (s *Store) RegenerateOutgoingWebhookSecret(ctx context.Context, id string) (OutgoingWebhook, string, error) {
	secret, err := generateOutgoingWebhookSecret()
	if err != nil {
		return OutgoingWebhook{}, "", err
	}
	hash := hashOutgoingWebhookSecret(secret)
	last4 := secret[len(secret)-4:]
	now := nowMillis()

	var wh OutgoingWebhook
	err = s.Tx(ctx, func(tx *sql.Tx) error {
		if _, err := getOutgoingWebhookTx(ctx, tx, id); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `
			UPDATE outgoing_webhooks SET secret = ?, secret_hash = ?, secret_last4 = ?, updated_at = ?
			WHERE id = ?`, secret, hash, last4, now, id); err != nil {
			return fmt.Errorf("regenerate outgoing webhook secret: %w", err)
		}
		var err2 error
		wh, err2 = getOutgoingWebhookTx(ctx, tx, id)
		return err2
	})
	if err != nil {
		return OutgoingWebhook{}, "", err
	}
	return wh, secret, nil
}

// DeleteOutgoingWebhook deletes an outgoing webhook. Idempotent, matching DeleteWebhook's
// posture — deleting an already-gone webhook is a no-op success.
func (s *Store) DeleteOutgoingWebhook(ctx context.Context, id string) error {
	_, err := s.writer.ExecContext(ctx, "DELETE FROM outgoing_webhooks WHERE id = ?", id)
	if err != nil {
		return fmt.Errorf("delete outgoing webhook: %w", err)
	}
	return nil
}

// MatchOutgoingWebhooksForMessage returns every active outgoing webhook on channelID whose
// keyword_filter matches body (a case-insensitive substring match; an empty filter always
// matches). Called from POST /messages after the message commits, never inside the message
// transaction itself (SPEC.md §4.11).
func (s *Store) MatchOutgoingWebhooksForMessage(ctx context.Context, channelID int64, body string) ([]OutgoingWebhook, error) {
	rows, err := s.reader.QueryContext(ctx, "SELECT "+outgoingWebhookColumns+" FROM outgoing_webhooks WHERE channel_id = ? AND status = 'active'", channelID)
	if err != nil {
		return nil, fmt.Errorf("match outgoing webhooks for message: %w", err)
	}
	defer rows.Close()
	all, err := scanOutgoingWebhooks(rows)
	if err != nil {
		return nil, err
	}

	lowerBody := strings.ToLower(body)
	var matched []OutgoingWebhook
	for _, w := range all {
		if w.KeywordFilter == "" || strings.Contains(lowerBody, strings.ToLower(w.KeywordFilter)) {
			matched = append(matched, w)
		}
	}
	return matched, nil
}

// outgoingWebhookHTTPClient is a package-level http.Client so tests can swap its Transport.
var outgoingWebhookHTTPClient = &http.Client{Timeout: 5 * time.Second}

// outgoingWebhookRetryDelays is the fixed backoff schedule between the 3 delivery attempts
// (immediate, +5s, +25s), per SPEC.md §4.11.
var outgoingWebhookRetryDelays = []time.Duration{0, 5 * time.Second, 25 * time.Second}

// DispatchOutgoingWebhook builds the signed payload for event, attempts delivery up to 3 times
// with backoff, records one outgoing_webhook_deliveries row per attempt, and updates the
// webhook's health bookkeeping on the final outcome. This is what the fixed-size dispatch
// worker pool (internal/dispatch) calls; it must never run on a request goroutine for a
// message that isn't an explicit user-triggered test send.
func (s *Store) DispatchOutgoingWebhook(ctx context.Context, hook OutgoingWebhook, event OutgoingEvent) error {
	sentAt := time.Now().UnixMilli()
	payload := outgoingWebhookPayload{
		Event:   "message.created",
		SentAt:  sentAt,
		Channel: event.Channel,
		Message: event.Message,
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal outgoing webhook payload: %w", err)
	}
	signature := signOutgoingWebhookBody(hook.Secret, body)

	var lastErr error
	for attempt := 1; attempt <= len(outgoingWebhookRetryDelays); attempt++ {
		if d := outgoingWebhookRetryDelays[attempt-1]; d > 0 {
			select {
			case <-time.After(d):
			case <-ctx.Done():
				return ctx.Err()
			}
		}

		status, respSnippet, latency, sendErr := sendOutgoingWebhookRequest(ctx, hook.TargetURL, body, signature, sentAt)
		record := DeliveryAttempt{
			MessageID:           event.MessageID,
			AttemptNumber:       attempt,
			RequestBody:         truncateUTF8(string(body), 4096),
			ResponseBodySnippet: respSnippet,
			LatencyMs:           latency.Milliseconds(),
		}
		if status > 0 {
			record.ResponseStatus = &status
		}
		if err := s.RecordDelivery(ctx, hook.ID, record); err != nil {
			slog.Error("record outgoing webhook delivery", "webhook_id", hook.ID, "error", err)
		}

		if sendErr == nil && status >= 200 && status < 300 {
			return s.MarkDeliverySuccess(ctx, hook.ID)
		}
		lastErr = sendErr
		if sendErr == nil {
			lastErr = fmt.Errorf("non-2xx response: %d", status)
		}
	}

	if err := s.MarkDeliveryFailure(ctx, hook.ID); err != nil {
		slog.Error("mark outgoing webhook delivery failure", "webhook_id", hook.ID, "error", err)
	}
	return lastErr
}

// signOutgoingWebhookBody returns the "sha256=<hex hmac>" signature SPEC.md §4.11 specifies.
func signOutgoingWebhookBody(secret string, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}

func sendOutgoingWebhookRequest(ctx context.Context, targetURL string, body []byte, signature string, sentAt int64) (status int, respSnippet string, latency time.Duration, err error) {
	start := time.Now()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, targetURL, bytes.NewReader(body))
	if err != nil {
		return 0, "", time.Since(start), fmt.Errorf("build outgoing webhook request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Hivemind-Signature", signature)
	req.Header.Set("X-Hivemind-Timestamp", fmt.Sprintf("%d", sentAt))

	resp, err := outgoingWebhookHTTPClient.Do(req)
	latency = time.Since(start)
	if err != nil {
		return 0, "", latency, err
	}
	defer resp.Body.Close()

	snippet := make([]byte, 500)
	n, _ := io.ReadFull(resp.Body, snippet)
	return resp.StatusCode, string(snippet[:n]), latency, nil
}

// truncateUTF8 truncates s to at most n bytes without splitting a multi-byte rune.
func truncateUTF8(s string, n int) string {
	if len(s) <= n {
		return s
	}
	b := []byte(s)[:n]
	for len(b) > 0 && !isUTF8Boundary(b) {
		b = b[:len(b)-1]
	}
	return string(b)
}

func isUTF8Boundary(b []byte) bool {
	return len(b) == 0 || b[len(b)-1]&0xC0 != 0x80
}

// RecordDelivery inserts one outgoing_webhook_deliveries row and prunes that webhook's rows
// beyond the most recent 50 in the same batch (SPEC.md §3.2c) — a debugging aid, not an audit
// trail, so unbounded retention buys nothing.
func (s *Store) RecordDelivery(ctx context.Context, webhookID string, attempt DeliveryAttempt) error {
	now := nowMillis()
	_, err := s.writer.ExecContext(ctx, `
		INSERT INTO outgoing_webhook_deliveries(webhook_id, message_id, attempt_number, request_body, response_status, response_body_snippet, latency_ms, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		webhookID, attempt.MessageID, attempt.AttemptNumber, attempt.RequestBody, attempt.ResponseStatus,
		attempt.ResponseBodySnippet, attempt.LatencyMs, now)
	if err != nil {
		return fmt.Errorf("record outgoing webhook delivery: %w", err)
	}
	_, err = s.writer.ExecContext(ctx, `
		DELETE FROM outgoing_webhook_deliveries
		WHERE webhook_id = ? AND id NOT IN (
			SELECT id FROM outgoing_webhook_deliveries WHERE webhook_id = ? ORDER BY id DESC LIMIT 50
		)`, webhookID, webhookID)
	if err != nil {
		return fmt.Errorf("prune outgoing webhook deliveries: %w", err)
	}
	return nil
}

// MarkDeliverySuccess resets consecutive_failures to 0 and stamps last_triggered_at/
// last_success_at, per SPEC.md §4.11.
func (s *Store) MarkDeliverySuccess(ctx context.Context, webhookID string) error {
	now := nowMillis()
	_, err := s.writer.ExecContext(ctx, `
		UPDATE outgoing_webhooks
		SET consecutive_failures = 0, last_triggered_at = ?, last_success_at = ?, updated_at = ?
		WHERE id = ?`, now, now, now, webhookID)
	if err != nil {
		return fmt.Errorf("mark outgoing webhook delivery success: %w", err)
	}
	return nil
}

// MarkDeliveryFailure increments consecutive_failures and, at the 20-consecutive-failure
// threshold, flips status to 'unhealthy' — SPEC.md §4.11's health policy.
func (s *Store) MarkDeliveryFailure(ctx context.Context, webhookID string) error {
	now := nowMillis()
	_, err := s.writer.ExecContext(ctx, `
		UPDATE outgoing_webhooks
		SET consecutive_failures = consecutive_failures + 1,
		    last_triggered_at = ?,
		    updated_at = ?,
		    status = CASE WHEN consecutive_failures + 1 >= ? THEN 'unhealthy' ELSE status END
		WHERE id = ?`, now, now, outgoingWebhookUnhealthyThreshold, webhookID)
	if err != nil {
		return fmt.Errorf("mark outgoing webhook delivery failure: %w", err)
	}
	return nil
}

// ListDeliveries returns a webhook's delivery log, cursor-paginated on id DESC (before, if set,
// returns rows with id < before), capped to limit rows (max 100).
func (s *Store) ListDeliveries(ctx context.Context, webhookID string, before *int64, limit int) ([]Delivery, error) {
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	var rows *sql.Rows
	var err error
	if before != nil {
		rows, err = s.reader.QueryContext(ctx, `
			SELECT id, webhook_id, message_id, attempt_number, request_body, response_status, response_body_snippet, latency_ms, created_at
			FROM outgoing_webhook_deliveries WHERE webhook_id = ? AND id < ? ORDER BY id DESC LIMIT ?`, webhookID, *before, limit)
	} else {
		rows, err = s.reader.QueryContext(ctx, `
			SELECT id, webhook_id, message_id, attempt_number, request_body, response_status, response_body_snippet, latency_ms, created_at
			FROM outgoing_webhook_deliveries WHERE webhook_id = ? ORDER BY id DESC LIMIT ?`, webhookID, limit)
	}
	if err != nil {
		return nil, fmt.Errorf("list outgoing webhook deliveries: %w", err)
	}
	defer rows.Close()

	var out []Delivery
	for rows.Next() {
		var d Delivery
		var messageID, latencyMs sql.NullInt64
		var responseStatus sql.NullInt64
		var responseSnippet sql.NullString
		if err := rows.Scan(&d.ID, &d.WebhookID, &messageID, &d.AttemptNumber, &d.RequestBody, &responseStatus, &responseSnippet, &latencyMs, &d.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan outgoing webhook delivery: %w", err)
		}
		if messageID.Valid {
			d.MessageID = &messageID.Int64
		}
		if responseStatus.Valid {
			v := int(responseStatus.Int64)
			d.ResponseStatus = &v
		}
		if responseSnippet.Valid {
			d.ResponseBodySnippet = responseSnippet.String
		}
		if latencyMs.Valid {
			d.LatencyMs = &latencyMs.Int64
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

// SetOutgoingWebhookTargetURLForTest bypasses the SSRF guard to point an existing outgoing
// webhook at a URL directly — for tests only, so internal/api tests can exercise real HTTP
// delivery against a plain httptest.Server without fighting the https-only production rule.
func (s *Store) SetOutgoingWebhookTargetURLForTest(ctx context.Context, id, targetURL string) error {
	_, err := s.writer.ExecContext(ctx, "UPDATE outgoing_webhooks SET target_url = ?, updated_at = ? WHERE id = ?", targetURL, nowMillis(), id)
	if err != nil {
		return fmt.Errorf("set outgoing webhook target url for test: %w", err)
	}
	return nil
}
