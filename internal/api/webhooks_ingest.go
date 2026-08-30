package api

import (
	"io"
	"strconv"
	"sync"
	"time"

	"github.com/0funct0ry/hivemind/internal/api/httpx"
	"github.com/0funct0ry/hivemind/internal/dispatch"
	"github.com/0funct0ry/hivemind/internal/realtime"
	"github.com/0funct0ry/hivemind/internal/store"
	"github.com/0funct0ry/hivemind/internal/webhooks"
	"github.com/gin-gonic/gin"
)

// ingestLimiter is a raw per-webhook hard rate limiter (60 req/s, SPEC.md §4.10) that exists
// purely to protect the single-writer SQLite connection from abuse — separate from, and much
// tighter than, the 10-in-3s flood-collapse debounce that CheckWebhookFlood implements as a UX
// behavior. Same sliding-window shape as internal/api/messages.go's postLimiter.
type ingestLimiter struct {
	mu    sync.Mutex
	posts map[string][]time.Time
}

func newIngestLimiter() *ingestLimiter {
	return &ingestLimiter{posts: make(map[string][]time.Time)}
}

func (l *ingestLimiter) Allow(webhookID string, now time.Time) bool {
	l.mu.Lock()
	defer l.mu.Unlock()

	cutoff := now.Add(-time.Second)
	history := l.posts[webhookID]
	i := 0
	for i < len(history) && history[i].Before(cutoff) {
		i++
	}
	history = history[i:]

	if len(history) >= 60 {
		l.posts[webhookID] = history
		return false
	}
	l.posts[webhookID] = append(history, now)
	return true
}

var webhookIngestLimiter = newIngestLimiter()

// ResetWebhookIngestLimiter clears the ingest rate-limit history, for test isolation.
func ResetWebhookIngestLimiter() {
	webhookIngestLimiter.mu.Lock()
	defer webhookIngestLimiter.mu.Unlock()
	webhookIngestLimiter.posts = make(map[string][]time.Time)
}

const maxIngestBodyBytes = 1 << 20 // 1 MiB; generous enough for any legitimate alert payload

// webhookIngest is POST /webhooks/:id/ingest/:token, registered outside the session/CSRF
// route group (SPEC.md §4.10 — no ambient credential to protect here, same posture as
// /auth/login and /setup). Flow: hard rate limit -> AuthenticateWebhook (one 404 for every
// auth failure mode) -> pick PayloadParser by format_preset -> Parse (never hard-fails; a
// malformed payload becomes a fallback card) -> CheckWebhookFlood -> either a fresh
// CreateWebhookMessage+message.created, or an in-place UpdateWebhookFloodSummary+message.updated
// -> always 200 {ok:true, message_id}.
func webhookIngest(s *store.Store, pub realtime.Publisher, outgoingDispatcher *dispatch.Dispatcher) gin.HandlerFunc {
	return func(c *gin.Context) {
		id := c.Param("id")
		token := c.Param("token")

		if !DisableRateLimits && !webhookIngestLimiter.Allow(id, time.Now()) {
			httpx.Fail(c, 429, "rate_limited", "Too many requests to this webhook.")
			return
		}

		wh, err := s.AuthenticateWebhook(c.Request.Context(), id, token)
		if err != nil {
			// Every auth failure mode — wrong token, wrong id, disabled — collapses to the
			// same 404, matching §7.2's non-member confirmation-avoidance posture.
			httpx.Fail(c, 404, "webhook_not_found", "Webhook not found.")
			return
		}

		raw, err := io.ReadAll(io.LimitReader(c.Request.Body, maxIngestBodyBytes+1))
		if err != nil {
			httpx.Fail(c, 400, "invalid_request", "Could not read request body.")
			return
		}
		if len(raw) > maxIngestBodyBytes {
			httpx.Fail(c, 413, "payload_too_large", "Webhook payload exceeds the maximum allowed size.")
			return
		}

		parser := webhooks.ParserFor(wh.FormatPreset)
		card, _ := parser.Parse(raw) // Parse never hard-fails; a bad payload yields a fallback card.

		var fields []store.WebhookField
		for _, f := range card.Fields {
			fields = append(fields, store.WebhookField{Label: f.Label, Value: f.Value})
		}

		now := time.Now()
		collapse, summaryID := s.CheckWebhookFlood(wh.ID, now)

		if collapse && summaryID != nil {
			whCard := store.WebhookCard{
				Title: card.Title, Severity: resolveSeverity(card.Severity, wh.DefaultSeverity),
				Fields: fields, Body: card.Body, DisplayName: resolveDisplayName(wh, card.DisplayName),
				AvatarURL: resolveAvatarURL(wh, card.AvatarURL), Fallback: card.Fallback,
			}
			msg, err := s.UpdateWebhookFloodSummary(c.Request.Context(), wh.ID, whCard)
			if err != nil {
				httpx.Fail(c, 500, "internal_error", "Could not update webhook summary message.")
				return
			}
			c.JSON(200, gin.H{"ok": true, "message_id": strconv.FormatInt(msg.ID, 10)})
			pub.Publish(realtime.Event{
				Type:      "message.updated",
				Payload:   publicMessage(msg),
				ChannelID: msg.ChannelID,
			})
			return
		}

		msg, err := s.CreateWebhookMessage(c.Request.Context(), store.WebhookMessageInput{
			Webhook: wh, Title: card.Title, Severity: card.Severity, Fields: fields, Body: card.Body,
			DisplayName: card.DisplayName, AvatarURL: card.AvatarURL, PayloadThreadID: card.ThreadID,
			Fallback: card.Fallback,
		})
		if err != nil {
			httpx.Fail(c, 500, "internal_error", "Could not post webhook message.")
			return
		}
		if err := s.NoteWebhookFloodSummary(c.Request.Context(), wh.ID, msg.ID); err != nil {
			// Non-fatal: the flood-collapse bookkeeping is a UX nicety, not delivery-critical.
			_ = err
		}

		c.JSON(200, gin.H{"ok": true, "message_id": strconv.FormatInt(msg.ID, 10)})

		if msg.ThreadID == nil {
			pub.Publish(realtime.Event{
				Type:      "message.created",
				Payload:   publicMessage(msg),
				ChannelID: msg.ChannelID,
			})
			if targetCh, err := s.GetChannel(c.Request.Context(), msg.ChannelID); err == nil {
				channelName := targetCh.Name
				if targetCh.Slug != nil && *targetCh.Slug != "" {
					channelName = *targetCh.Slug
				}
				enqueueOutgoingWebhooks(s, outgoingDispatcher, msg, channelName)
			}
		} else {
			rootMsg, err := s.GetMessage(c.Request.Context(), *msg.ThreadID)
			if err == nil {
				pub.Publish(realtime.Event{
					Type: "thread.reply",
					Payload: gin.H{
						"root_id":       strconv.FormatInt(*msg.ThreadID, 10),
						"channel_id":    strconv.FormatInt(msg.ChannelID, 10),
						"reply_count":   rootMsg.ReplyCount,
						"last_reply_id": strconv.FormatInt(msg.ID, 10),
						"message":       publicMessage(msg),
					},
					ChannelID: msg.ChannelID,
				})
			}
		}

		mentionsList, err := s.GetMessageMentions(c.Request.Context(), msg.ID)
		if err == nil {
			for _, m := range mentionsList {
				pub.Publish(realtime.Event{
					Type: "mention.created",
					Payload: gin.H{
						"message_id": strconv.FormatInt(m.MessageID, 10),
						"channel_id": strconv.FormatInt(m.ChannelID, 10),
					},
					UserID: m.UserID,
				})
			}
		}
	}
}

func resolveSeverity(payloadSeverity, defaultSeverity string) string {
	switch payloadSeverity {
	case "critical", "warning", "info", "success", "neutral":
		return payloadSeverity
	default:
		return defaultSeverity
	}
}

func resolveDisplayName(wh store.Webhook, payloadDisplayName string) string {
	if wh.AllowPayloadOverride && payloadDisplayName != "" {
		return payloadDisplayName
	}
	return wh.DefaultDisplayName
}

func resolveAvatarURL(wh store.Webhook, payloadAvatarURL string) string {
	if wh.AllowPayloadOverride && payloadAvatarURL != "" {
		return payloadAvatarURL
	}
	return wh.DefaultAvatarColor
}
