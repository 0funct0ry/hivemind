package api

import (
	"errors"
	"strconv"

	"github.com/0funct0ry/hivemind/internal/api/httpx"
	"github.com/0funct0ry/hivemind/internal/store"
	"github.com/gin-gonic/gin"
)

// publicWebhook renders a store.Webhook with string-serialized ids and a masked secret. It
// never includes the plaintext token or hash — the only way to get a usable secret is
// create/regenerate, which layer ingest_url on top of this via publicWebhookWithIngestURL.
func publicWebhook(w store.Webhook, baseURL string) gin.H {
	res := gin.H{
		"id":                         w.ID,
		"channel_id":                 strconv.FormatInt(w.ChannelID, 10),
		"bot_user_id":                strconv.FormatInt(w.BotUserID, 10),
		"created_by":                 strconv.FormatInt(w.CreatedBy, 10),
		"name":                       w.Name,
		"format_preset":              w.FormatPreset,
		"default_display_name":       w.DefaultDisplayName,
		"default_avatar_color":       w.DefaultAvatarColor,
		"allow_payload_override":     w.AllowPayloadOverride,
		"default_severity":           w.DefaultSeverity,
		"notify_channel_on_critical": w.NotifyChannelOnCritical,
		"thread_id":                  nil,
		"masked_token":               "whk_" + "••••••••" + w.SecretLast4,
		"status":                     w.Status,
		"created_at":                 w.CreatedAt,
		"updated_at":                 w.UpdatedAt,
		"regenerated_at":             w.RegeneratedAt,
		"last_used_at":               w.LastUsedAt,
	}
	if w.ThreadID != nil {
		res["thread_id"] = strconv.FormatInt(*w.ThreadID, 10)
	}
	return res
}

// publicWebhookShowOnce is publicWebhook plus the ingest URL carrying the plaintext token,
// shown exactly once at create/regenerate time — never retrievable again afterward.
func publicWebhookShowOnce(w store.Webhook, plaintext, baseURL string) gin.H {
	res := publicWebhook(w, baseURL)
	res["ingest_url"] = baseURL + "/api/v1/webhooks/" + w.ID + "/ingest/" + plaintext
	return res
}

// webhookBaseURL derives the scheme+host to build an ingest_url from the current request,
// honoring the same reverse-proxy header the rest of the app trusts for its own URLs.
func webhookBaseURL(c *gin.Context) string {
	scheme := "http"
	if c.Request.TLS != nil || c.GetHeader("X-Forwarded-Proto") == "https" {
		scheme = "https"
	}
	host := c.GetHeader("X-Forwarded-Host")
	if host == "" {
		host = c.Request.Host
	}
	return scheme + "://" + host
}

// loadWebhookForManagement resolves a webhook by id and enforces the owner-or-admin rule
// (SPEC.md §4.10/§7.2): CanAccessChannel is checked first so a private channel the caller can't
// see returns 404 exactly like every other channel-scoped handler, then role is checked, mirroring
// messageDelete's inline owner-or-admin pattern. It writes the failure response itself; callers
// should return immediately when ok is false.
func loadWebhookForManagement(c *gin.Context, s *store.Store, me store.User) (store.Webhook, bool) {
	id := c.Param("id")
	wh, err := s.GetWebhookByID(c.Request.Context(), id)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			httpx.Fail(c, 404, "webhook_not_found", "Webhook not found.")
			return store.Webhook{}, false
		}
		httpx.Fail(c, 500, "internal_error", "Could not fetch webhook.")
		return store.Webhook{}, false
	}

	access, err := s.CanAccessChannel(c.Request.Context(), me.ID, wh.ChannelID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			httpx.Fail(c, 404, "webhook_not_found", "Webhook not found.")
			return store.Webhook{}, false
		}
		httpx.Fail(c, 500, "internal_error", "Could not check channel access.")
		return store.Webhook{}, false
	}
	if !access.CanRead && me.Role != "admin" {
		httpx.Fail(c, 404, "webhook_not_found", "Webhook not found.")
		return store.Webhook{}, false
	}
	if !access.IsOwner && me.Role != "admin" {
		httpx.Fail(c, 403, "forbidden", "Only the channel owner or an administrator can manage this webhook.")
		return store.Webhook{}, false
	}

	return wh, true
}

// webhookList is the workspace-wide GET /webhooks — every webhook the caller can manage
// across every channel (SPEC.md §4.2).
func webhookList(s *store.Store) gin.HandlerFunc {
	return func(c *gin.Context) {
		me, _ := CurrentUser(c)
		list, err := s.ListWebhooksForUser(c.Request.Context(), me.ID, me.Role == "admin")
		if err != nil {
			httpx.Fail(c, 500, "internal_error", "Could not list webhooks.")
			return
		}
		base := webhookBaseURL(c)
		data := make([]gin.H, 0, len(list))
		for _, w := range list {
			data = append(data, publicWebhook(w, base))
		}
		c.JSON(200, gin.H{"data": data})
	}
}

// channelWebhookList is GET /channels/:id/webhooks — owner/admin only.
func channelWebhookList(s *store.Store) gin.HandlerFunc {
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
			httpx.Fail(c, 403, "forbidden", "Only the channel owner or an administrator can manage webhooks.")
			return
		}
		list, err := s.ListWebhooksForChannel(c.Request.Context(), ch.ID)
		if err != nil {
			httpx.Fail(c, 500, "internal_error", "Could not list webhooks.")
			return
		}
		base := webhookBaseURL(c)
		data := make([]gin.H, 0, len(list))
		for _, w := range list {
			data = append(data, publicWebhook(w, base))
		}
		c.JSON(200, gin.H{"data": data})
	}
}

type webhookCreateBody struct {
	Name                    string  `json:"name"`
	FormatPreset            string  `json:"format_preset"`
	DefaultDisplayName      string  `json:"default_display_name"`
	DefaultAvatarColor      string  `json:"default_avatar_color"`
	AllowPayloadOverride    *bool   `json:"allow_payload_override"`
	DefaultSeverity         string  `json:"default_severity"`
	NotifyChannelOnCritical bool    `json:"notify_channel_on_critical"`
	ThreadID                *string `json:"thread_id"`
}

// channelWebhookCreate is POST /channels/:id/webhooks — owner/admin only.
func channelWebhookCreate(s *store.Store) gin.HandlerFunc {
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
			httpx.Fail(c, 403, "forbidden", "Only the channel owner or an administrator can manage webhooks.")
			return
		}

		var in webhookCreateBody
		if err := c.ShouldBindJSON(&in); err != nil {
			httpx.Fail(c, 400, "invalid_request", "Invalid request body.")
			return
		}
		if in.Name == "" {
			httpx.FailField(c, 400, "invalid_request", "name is required.", "name")
			return
		}

		var threadID *int64
		if in.ThreadID != nil && *in.ThreadID != "" {
			val, err := strconv.ParseInt(*in.ThreadID, 10, 64)
			if err != nil {
				httpx.FailField(c, 400, "invalid_request", "Invalid thread_id.", "thread_id")
				return
			}
			threadID = &val
		}

		allowOverride := true
		if in.AllowPayloadOverride != nil {
			allowOverride = *in.AllowPayloadOverride
		}

		wh, plaintext, err := s.CreateWebhook(c.Request.Context(), store.WebhookInput{
			ChannelID:               ch.ID,
			CreatedBy:               me.ID,
			Name:                    in.Name,
			FormatPreset:            in.FormatPreset,
			DefaultDisplayName:      in.DefaultDisplayName,
			DefaultAvatarColor:      in.DefaultAvatarColor,
			AllowPayloadOverride:    allowOverride,
			DefaultSeverity:         in.DefaultSeverity,
			NotifyChannelOnCritical: in.NotifyChannelOnCritical,
			ThreadID:                threadID,
		})
		if err != nil {
			if errors.Is(err, store.ErrNotFound) {
				httpx.Fail(c, 404, "channel_not_found", "Channel not found.")
				return
			}
			httpx.Fail(c, 400, "invalid_webhook", err.Error())
			return
		}

		c.JSON(201, gin.H{"webhook": publicWebhookShowOnce(wh, plaintext, webhookBaseURL(c))})
	}
}

// webhookGet is GET /webhooks/:id.
func webhookGet(s *store.Store) gin.HandlerFunc {
	return func(c *gin.Context) {
		me, _ := CurrentUser(c)
		wh, ok := loadWebhookForManagement(c, s, me)
		if !ok {
			return
		}
		c.JSON(200, gin.H{"webhook": publicWebhook(wh, webhookBaseURL(c))})
	}
}

type webhookPatchBody struct {
	Name                    *string `json:"name"`
	FormatPreset            *string `json:"format_preset"`
	DefaultDisplayName      *string `json:"default_display_name"`
	DefaultAvatarColor      *string `json:"default_avatar_color"`
	AllowPayloadOverride    *bool   `json:"allow_payload_override"`
	DefaultSeverity         *string `json:"default_severity"`
	NotifyChannelOnCritical *bool   `json:"notify_channel_on_critical"`
	ThreadID                *string `json:"thread_id"`
	Status                  *string `json:"status"`
}

// webhookUpdate is PATCH /webhooks/:id.
func webhookUpdate(s *store.Store) gin.HandlerFunc {
	return func(c *gin.Context) {
		me, _ := CurrentUser(c)
		wh, ok := loadWebhookForManagement(c, s, me)
		if !ok {
			return
		}

		var in webhookPatchBody
		if err := c.ShouldBindJSON(&in); err != nil {
			httpx.Fail(c, 400, "invalid_request", "Invalid request body.")
			return
		}

		patch := store.WebhookPatch{
			Name:                    in.Name,
			FormatPreset:            in.FormatPreset,
			DefaultDisplayName:      in.DefaultDisplayName,
			DefaultAvatarColor:      in.DefaultAvatarColor,
			AllowPayloadOverride:    in.AllowPayloadOverride,
			DefaultSeverity:         in.DefaultSeverity,
			NotifyChannelOnCritical: in.NotifyChannelOnCritical,
			Status:                  in.Status,
		}
		if in.ThreadID != nil {
			if *in.ThreadID == "" {
				var nilVal *int64
				patch.ThreadID = &nilVal
			} else {
				val, err := strconv.ParseInt(*in.ThreadID, 10, 64)
				if err != nil {
					httpx.FailField(c, 400, "invalid_request", "Invalid thread_id.", "thread_id")
					return
				}
				v := &val
				patch.ThreadID = &v
			}
		}

		updated, err := s.UpdateWebhook(c.Request.Context(), wh.ID, patch)
		if err != nil {
			if errors.Is(err, store.ErrNotFound) {
				httpx.Fail(c, 404, "webhook_not_found", "Webhook not found.")
				return
			}
			httpx.Fail(c, 400, "invalid_webhook", err.Error())
			return
		}
		c.JSON(200, gin.H{"webhook": publicWebhook(updated, webhookBaseURL(c))})
	}
}

// webhookRegenerate is POST /webhooks/:id/regenerate.
func webhookRegenerate(s *store.Store) gin.HandlerFunc {
	return func(c *gin.Context) {
		me, _ := CurrentUser(c)
		wh, ok := loadWebhookForManagement(c, s, me)
		if !ok {
			return
		}
		updated, plaintext, err := s.RegenerateWebhookToken(c.Request.Context(), wh.ID)
		if err != nil {
			if errors.Is(err, store.ErrNotFound) {
				httpx.Fail(c, 404, "webhook_not_found", "Webhook not found.")
				return
			}
			httpx.Fail(c, 500, "internal_error", "Could not regenerate webhook token.")
			return
		}
		c.JSON(200, gin.H{"webhook": publicWebhookShowOnce(updated, plaintext, webhookBaseURL(c))})
	}
}

// webhookClaim is POST /webhooks/:id/claim — admin-only, per SPEC.md §4.2.
func webhookClaim(s *store.Store) gin.HandlerFunc {
	return func(c *gin.Context) {
		me, _ := CurrentUser(c)
		id := c.Param("id")
		if _, err := s.GetWebhookByID(c.Request.Context(), id); err != nil {
			if errors.Is(err, store.ErrNotFound) {
				httpx.Fail(c, 404, "webhook_not_found", "Webhook not found.")
				return
			}
			httpx.Fail(c, 500, "internal_error", "Could not fetch webhook.")
			return
		}
		claimed, err := s.ClaimWebhook(c.Request.Context(), id, me.ID)
		if err != nil {
			httpx.Fail(c, 400, "invalid_claim", err.Error())
			return
		}
		c.JSON(200, gin.H{"webhook": publicWebhook(claimed, webhookBaseURL(c))})
	}
}

// webhookDelete is DELETE /webhooks/:id — idempotent, per §4.7's delete posture.
func webhookDelete(s *store.Store) gin.HandlerFunc {
	return func(c *gin.Context) {
		me, _ := CurrentUser(c)
		id := c.Param("id")

		// If the webhook still exists, enforce owner-or-admin the normal way. If it's already
		// gone, deleting again is a no-op success — nothing left to authorize against.
		if wh, err := s.GetWebhookByID(c.Request.Context(), id); err == nil {
			access, err := s.CanAccessChannel(c.Request.Context(), me.ID, wh.ChannelID)
			if err != nil {
				if errors.Is(err, store.ErrNotFound) {
					httpx.Fail(c, 404, "webhook_not_found", "Webhook not found.")
					return
				}
				httpx.Fail(c, 500, "internal_error", "Could not check channel access.")
				return
			}
			if !access.CanRead && me.Role != "admin" {
				httpx.Fail(c, 404, "webhook_not_found", "Webhook not found.")
				return
			}
			if !access.IsOwner && me.Role != "admin" {
				httpx.Fail(c, 403, "forbidden", "Only the channel owner or an administrator can manage this webhook.")
				return
			}
		} else if !errors.Is(err, store.ErrNotFound) {
			httpx.Fail(c, 500, "internal_error", "Could not fetch webhook.")
			return
		}

		if err := s.DeleteWebhook(c.Request.Context(), id); err != nil {
			httpx.Fail(c, 500, "internal_error", "Could not delete webhook.")
			return
		}
		c.JSON(200, gin.H{"ok": true})
	}
}
