package api

import (
	"context"
	"errors"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"github.com/0funct0ry/hivemind/internal/api/httpx"
	"github.com/0funct0ry/hivemind/internal/dispatch"
	"github.com/0funct0ry/hivemind/internal/store"
	"github.com/gin-gonic/gin"
)

// publicOutgoingWebhook renders a store.OutgoingWebhook with string-serialized ids and a masked
// secret. It never includes the plaintext secret or its hash — the only way to get a usable
// secret is create/regenerate, which layer the plaintext on top via
// publicOutgoingWebhookShowOnce.
func publicOutgoingWebhook(w store.OutgoingWebhook) gin.H {
	res := gin.H{
		"id":                   w.ID,
		"channel_id":           strconv.FormatInt(w.ChannelID, 10),
		"created_by":           strconv.FormatInt(w.CreatedBy, 10),
		"name":                 w.Name,
		"target_url":           w.TargetURL,
		"masked_secret":        "whsec_" + "••••••••" + w.SecretLast4,
		"event_type":           w.EventType,
		"keyword_filter":       w.KeywordFilter,
		"status":               w.Status,
		"consecutive_failures": w.ConsecutiveFailures,
		"created_at":           w.CreatedAt,
		"updated_at":           w.UpdatedAt,
		"last_triggered_at":    w.LastTriggeredAt,
		"last_success_at":      w.LastSuccessAt,
	}
	return res
}

// publicOutgoingWebhookShowOnce is publicOutgoingWebhook plus the plaintext signing secret,
// shown exactly once at create/regenerate time — never retrievable again afterward.
func publicOutgoingWebhookShowOnce(w store.OutgoingWebhook, plaintext string) gin.H {
	res := publicOutgoingWebhook(w)
	res["secret"] = plaintext
	return res
}

// publicDelivery renders a store.Delivery for the delivery-log API.
func publicDelivery(d store.Delivery) gin.H {
	res := gin.H{
		"id":                    strconv.FormatInt(d.ID, 10),
		"attempt_number":        d.AttemptNumber,
		"response_status":       d.ResponseStatus,
		"response_body_snippet": d.ResponseBodySnippet,
		"latency_ms":            d.LatencyMs,
		"created_at":            d.CreatedAt,
	}
	if d.MessageID != nil {
		res["message_id"] = strconv.FormatInt(*d.MessageID, 10)
	}
	return res
}

// loadOutgoingWebhookForManagement resolves an outgoing webhook by id and enforces the
// owner-or-admin rule (SPEC.md §4.11/§7.2), matching loadWebhookForManagement's exact
// 404-then-403 shape. It writes the failure response itself; callers should return immediately
// when ok is false.
func loadOutgoingWebhookForManagement(c *gin.Context, s *store.Store, me store.User) (store.OutgoingWebhook, bool) {
	id := c.Param("id")
	wh, err := s.GetOutgoingWebhookByID(c.Request.Context(), id)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			httpx.Fail(c, 404, "outgoing_webhook_not_found", "Outgoing webhook not found.")
			return store.OutgoingWebhook{}, false
		}
		httpx.Fail(c, 500, "internal_error", "Could not fetch outgoing webhook.")
		return store.OutgoingWebhook{}, false
	}

	access, err := s.CanAccessChannel(c.Request.Context(), me.ID, wh.ChannelID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			httpx.Fail(c, 404, "outgoing_webhook_not_found", "Outgoing webhook not found.")
			return store.OutgoingWebhook{}, false
		}
		httpx.Fail(c, 500, "internal_error", "Could not check channel access.")
		return store.OutgoingWebhook{}, false
	}
	if !access.CanRead && me.Role != "admin" {
		httpx.Fail(c, 404, "outgoing_webhook_not_found", "Outgoing webhook not found.")
		return store.OutgoingWebhook{}, false
	}
	if !access.IsOwner && me.Role != "admin" {
		httpx.Fail(c, 403, "forbidden", "Only the channel owner or an administrator can manage this outgoing webhook.")
		return store.OutgoingWebhook{}, false
	}

	return wh, true
}

// outgoingWebhookList is the workspace-wide GET /outgoing-webhooks — every outgoing webhook the
// caller can manage across every channel (SPEC.md §4.2).
func outgoingWebhookList(s *store.Store) gin.HandlerFunc {
	return func(c *gin.Context) {
		me, _ := CurrentUser(c)
		list, err := s.ListOutgoingWebhooksForUser(c.Request.Context(), me.ID, me.Role == "admin")
		if err != nil {
			httpx.Fail(c, 500, "internal_error", "Could not list outgoing webhooks.")
			return
		}
		data := make([]gin.H, 0, len(list))
		for _, w := range list {
			data = append(data, publicOutgoingWebhook(w))
		}
		c.JSON(200, gin.H{"data": data})
	}
}

// channelOutgoingWebhookList is GET /channels/:id/outgoing-webhooks — owner/admin only.
func channelOutgoingWebhookList(s *store.Store) gin.HandlerFunc {
	return func(c *gin.Context) {
		me, _ := CurrentUser(c)
		ch, access, ok := resolveChannel(c, s, me.ID)
		if !ok {
			return
		}
		if !access.CanRead && me.Role != "admin" {
			httpx.Fail(c, 404, "channel_not_found", "Channel not found.")
			return
		}
		if !access.IsOwner && me.Role != "admin" {
			httpx.Fail(c, 403, "forbidden", "Only the channel owner or an administrator can manage outgoing webhooks.")
			return
		}
		list, err := s.ListOutgoingWebhooksForChannel(c.Request.Context(), ch.ID)
		if err != nil {
			httpx.Fail(c, 500, "internal_error", "Could not list outgoing webhooks.")
			return
		}
		data := make([]gin.H, 0, len(list))
		for _, w := range list {
			data = append(data, publicOutgoingWebhook(w))
		}
		c.JSON(200, gin.H{"data": data})
	}
}

type outgoingWebhookCreateBody struct {
	Name          string `json:"name"`
	TargetURL     string `json:"target_url"`
	KeywordFilter string `json:"keyword_filter"`
}

// channelOutgoingWebhookCreate is POST /channels/:id/outgoing-webhooks — owner/admin only.
func channelOutgoingWebhookCreate(s *store.Store) gin.HandlerFunc {
	return func(c *gin.Context) {
		me, _ := CurrentUser(c)
		ch, access, ok := resolveChannel(c, s, me.ID)
		if !ok {
			return
		}
		if !access.CanRead && me.Role != "admin" {
			httpx.Fail(c, 404, "channel_not_found", "Channel not found.")
			return
		}
		if !access.IsOwner && me.Role != "admin" {
			httpx.Fail(c, 403, "forbidden", "Only the channel owner or an administrator can manage outgoing webhooks.")
			return
		}

		var in outgoingWebhookCreateBody
		if err := c.ShouldBindJSON(&in); err != nil {
			httpx.Fail(c, 400, "invalid_request", "Invalid request body.")
			return
		}
		if in.Name == "" {
			httpx.FailField(c, 400, "invalid_request", "name is required.", "name")
			return
		}
		if in.TargetURL == "" {
			httpx.FailField(c, 400, "invalid_request", "target_url is required.", "target_url")
			return
		}

		wh, secret, err := s.CreateOutgoingWebhook(c.Request.Context(), store.OutgoingWebhookInput{
			ChannelID:     ch.ID,
			CreatedBy:     me.ID,
			Name:          in.Name,
			TargetURL:     in.TargetURL,
			KeywordFilter: in.KeywordFilter,
		})
		if err != nil {
			if errors.Is(err, store.ErrNotFound) {
				httpx.Fail(c, 404, "channel_not_found", "Channel not found.")
				return
			}
			httpx.FailField(c, 400, "invalid_outgoing_webhook", err.Error(), "target_url")
			return
		}

		c.JSON(201, gin.H{"webhook": publicOutgoingWebhookShowOnce(wh, secret)})
	}
}

// outgoingWebhookGet is GET /outgoing-webhooks/:id.
func outgoingWebhookGet(s *store.Store) gin.HandlerFunc {
	return func(c *gin.Context) {
		me, _ := CurrentUser(c)
		wh, ok := loadOutgoingWebhookForManagement(c, s, me)
		if !ok {
			return
		}
		c.JSON(200, gin.H{"webhook": publicOutgoingWebhook(wh)})
	}
}

type outgoingWebhookPatchBody struct {
	Name          *string `json:"name"`
	TargetURL     *string `json:"target_url"`
	KeywordFilter *string `json:"keyword_filter"`
	Status        *string `json:"status"`
}

// outgoingWebhookUpdate is PATCH /outgoing-webhooks/:id.
func outgoingWebhookUpdate(s *store.Store) gin.HandlerFunc {
	return func(c *gin.Context) {
		me, _ := CurrentUser(c)
		wh, ok := loadOutgoingWebhookForManagement(c, s, me)
		if !ok {
			return
		}

		var in outgoingWebhookPatchBody
		if err := c.ShouldBindJSON(&in); err != nil {
			httpx.Fail(c, 400, "invalid_request", "Invalid request body.")
			return
		}

		updated, err := s.UpdateOutgoingWebhook(c.Request.Context(), wh.ID, store.OutgoingWebhookPatch{
			Name:          in.Name,
			TargetURL:     in.TargetURL,
			KeywordFilter: in.KeywordFilter,
			Status:        in.Status,
		})
		if err != nil {
			if errors.Is(err, store.ErrNotFound) {
				httpx.Fail(c, 404, "outgoing_webhook_not_found", "Outgoing webhook not found.")
				return
			}
			httpx.FailField(c, 400, "invalid_outgoing_webhook", err.Error(), "target_url")
			return
		}
		c.JSON(200, gin.H{"webhook": publicOutgoingWebhook(updated)})
	}
}

// outgoingWebhookRegenerateSecret is POST /outgoing-webhooks/:id/regenerate-secret.
func outgoingWebhookRegenerateSecret(s *store.Store) gin.HandlerFunc {
	return func(c *gin.Context) {
		me, _ := CurrentUser(c)
		wh, ok := loadOutgoingWebhookForManagement(c, s, me)
		if !ok {
			return
		}
		updated, secret, err := s.RegenerateOutgoingWebhookSecret(c.Request.Context(), wh.ID)
		if err != nil {
			if errors.Is(err, store.ErrNotFound) {
				httpx.Fail(c, 404, "outgoing_webhook_not_found", "Outgoing webhook not found.")
				return
			}
			httpx.Fail(c, 500, "internal_error", "Could not regenerate outgoing webhook secret.")
			return
		}
		c.JSON(200, gin.H{"webhook": publicOutgoingWebhookShowOnce(updated, secret)})
	}
}

// outgoingWebhookDelete is DELETE /outgoing-webhooks/:id — idempotent, per §4.7's delete
// posture.
func outgoingWebhookDelete(s *store.Store) gin.HandlerFunc {
	return func(c *gin.Context) {
		me, _ := CurrentUser(c)
		id := c.Param("id")

		// If the outgoing webhook still exists, enforce owner-or-admin the normal way. If it's
		// already gone, deleting again is a no-op success — nothing left to authorize against.
		if wh, err := s.GetOutgoingWebhookByID(c.Request.Context(), id); err == nil {
			access, err := s.CanAccessChannel(c.Request.Context(), me.ID, wh.ChannelID)
			if err != nil {
				if errors.Is(err, store.ErrNotFound) {
					httpx.Fail(c, 404, "outgoing_webhook_not_found", "Outgoing webhook not found.")
					return
				}
				httpx.Fail(c, 500, "internal_error", "Could not check channel access.")
				return
			}
			if !access.CanRead && me.Role != "admin" {
				httpx.Fail(c, 404, "outgoing_webhook_not_found", "Outgoing webhook not found.")
				return
			}
			if !access.IsOwner && me.Role != "admin" {
				httpx.Fail(c, 403, "forbidden", "Only the channel owner or an administrator can manage this outgoing webhook.")
				return
			}
		} else if !errors.Is(err, store.ErrNotFound) {
			httpx.Fail(c, 500, "internal_error", "Could not fetch outgoing webhook.")
			return
		}

		if err := s.DeleteOutgoingWebhook(c.Request.Context(), id); err != nil {
			httpx.Fail(c, 500, "internal_error", "Could not delete outgoing webhook.")
			return
		}
		c.JSON(200, gin.H{"ok": true})
	}
}

// outgoingWebhookTest is POST /outgoing-webhooks/:id/test — synthesizes a fake message.created
// event from the most recent real message in the target channel (or a placeholder body if the
// channel has none) and dispatches it through the real pipeline, inline on the request
// goroutine: an explicit, rate-limited user action, not traffic on the hot message-post path
// (SPEC.md §4.11).
func outgoingWebhookTest(s *store.Store) gin.HandlerFunc {
	return func(c *gin.Context) {
		me, _ := CurrentUser(c)
		wh, ok := loadOutgoingWebhookForManagement(c, s, me)
		if !ok {
			return
		}

		if !DisableRateLimits {
			if wait, ok := msgLimiter.Allow(me.ID, time.Now()); !ok {
				c.Header("Retry-After", strconv.Itoa(int(wait.Seconds())+1))
				httpx.Fail(c, 429, "rate_limited", "Too many test events. Please wait before sending another.")
				return
			}
		}

		ch, err := s.GetChannel(c.Request.Context(), wh.ChannelID)
		if err != nil {
			httpx.Fail(c, 500, "internal_error", "Could not fetch channel.")
			return
		}

		var msgPayload any
		var messageID int64
		if list, err := s.ListChannelMessages(c.Request.Context(), wh.ChannelID, nil, nil, nil, 1); err == nil && len(list) > 0 {
			msgPayload = publicMessage(list[0])
			messageID = list[0].ID
		} else {
			msgPayload = gin.H{
				"id":         "0",
				"channel_id": strconv.FormatInt(ch.ID, 10),
				"body":       "This is a test event from hivemind.",
			}
		}

		channelName := ch.Name
		if ch.Slug != nil && *ch.Slug != "" {
			channelName = *ch.Slug
		}

		err = s.DispatchOutgoingWebhook(c.Request.Context(), wh, store.OutgoingEvent{
			MessageID: messageID,
			ChannelID: ch.ID,
			Channel:   store.OutgoingEventChannel{ID: strconv.FormatInt(ch.ID, 10), Name: channelName},
			Message:   msgPayload,
		})
		if err != nil {
			c.JSON(200, gin.H{"ok": false, "error": err.Error()})
			return
		}
		c.JSON(200, gin.H{"ok": true})
	}
}

// outgoingWebhookDeliveries is GET /outgoing-webhooks/:id/deliveries, cursor-paginated on
// delivery id (SPEC.md §4.1/§4.11).
func outgoingWebhookDeliveries(s *store.Store) gin.HandlerFunc {
	return func(c *gin.Context) {
		me, _ := CurrentUser(c)
		wh, ok := loadOutgoingWebhookForManagement(c, s, me)
		if !ok {
			return
		}

		var before *int64
		if v := strings.TrimSpace(c.Query("before")); v != "" {
			parsed, err := strconv.ParseInt(v, 10, 64)
			if err != nil {
				httpx.FailField(c, 400, "invalid_request", "Invalid before cursor.", "before")
				return
			}
			before = &parsed
		}
		limit := 50
		if v := strings.TrimSpace(c.Query("limit")); v != "" {
			parsed, err := strconv.Atoi(v)
			if err == nil && parsed > 0 {
				limit = parsed
			}
		}

		list, err := s.ListDeliveries(c.Request.Context(), wh.ID, before, limit)
		if err != nil {
			httpx.Fail(c, 500, "internal_error", "Could not list deliveries.")
			return
		}
		data := make([]gin.H, 0, len(list))
		for _, d := range list {
			data = append(data, publicDelivery(d))
		}
		hasMore := len(list) == limit
		var nextBefore *string
		if hasMore {
			v := strconv.FormatInt(list[len(list)-1].ID, 10)
			nextBefore = &v
		}
		c.JSON(200, gin.H{"data": data, "has_more": hasMore, "next_before": nextBefore})
	}
}

// enqueueOutgoingWebhooks looks up every active outgoing webhook matching msg's channel and
// keyword filter, then enqueues a dispatch job for each onto the shared worker pool. Called
// from POST /messages and the incoming-webhook ingest handler after their message commits —
// never inline, so a slow or unreachable external URL can never add latency to a post.
func enqueueOutgoingWebhooks(s *store.Store, dispatcher *dispatch.Dispatcher, msg store.Message, channelName string) {
	if dispatcher == nil {
		return
	}
	matches, err := s.MatchOutgoingWebhooksForMessage(context.Background(), msg.ChannelID, msg.Body)
	if err != nil || len(matches) == 0 {
		return
	}
	event := store.OutgoingEvent{
		MessageID: msg.ID,
		ChannelID: msg.ChannelID,
		Channel:   store.OutgoingEventChannel{ID: strconv.FormatInt(msg.ChannelID, 10), Name: channelName},
		Message:   publicMessage(msg),
	}
	for _, wh := range matches {
		if !dispatcher.Enqueue(dispatch.Job{Webhook: wh, Event: event}) {
			slog.Warn("outgoing webhook dispatch queue full, dropping job", "webhook_id", wh.ID, "channel_id", msg.ChannelID)
		}
	}
}
