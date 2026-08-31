package api

import (
	"errors"
	"strconv"

	"github.com/0funct0ry/hivemind/internal/api/httpx"
	"github.com/0funct0ry/hivemind/internal/store"
	"github.com/gin-gonic/gin"
)

// publicBot renders a store.Bot with string-serialized ids. It never includes any token.
func publicBot(b store.Bot) gin.H {
	return gin.H{
		"user_id":      strconv.FormatInt(b.UserID, 10),
		"created_by":   strconv.FormatInt(b.CreatedBy, 10),
		"username":     b.Username,
		"display_name": b.DisplayName,
		"avatar_color": b.AvatarColor,
		"description":  b.Description,
		"status":       b.Status,
		"created_at":   b.CreatedAt,
		"updated_at":   b.UpdatedAt,
	}
}

// publicBotShowOnce is publicBot plus the plaintext bearer token, shown exactly once at
// create/regenerate time — never retrievable again afterward.
func publicBotShowOnce(b store.Bot, plaintext string) gin.H {
	res := publicBot(b)
	res["token"] = plaintext
	return res
}

// botList is GET /bots — admin-only, per SPEC.md §4.12/§7.2.
func botList(s *store.Store) gin.HandlerFunc {
	return func(c *gin.Context) {
		list, err := s.ListBots(c.Request.Context())
		if err != nil {
			httpx.Fail(c, 500, "internal_error", "Could not list bots.")
			return
		}
		data := make([]gin.H, 0, len(list))
		for _, b := range list {
			data = append(data, publicBot(b))
		}
		c.JSON(200, gin.H{"data": data})
	}
}

type botCreateBody struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

// botCreate is POST /bots — admin-only. Returns the plaintext bearer token once.
func botCreate(s *store.Store) gin.HandlerFunc {
	return func(c *gin.Context) {
		me, _ := CurrentUser(c)
		var in botCreateBody
		if err := c.ShouldBindJSON(&in); err != nil {
			httpx.Fail(c, 400, "invalid_request", "Invalid request body.")
			return
		}
		if in.Name == "" {
			httpx.FailField(c, 400, "invalid_request", "name is required.", "name")
			return
		}

		bot, plaintext, err := s.CreateBot(c.Request.Context(), store.BotInput{
			Name:        in.Name,
			Description: in.Description,
		}, me.ID)
		if err != nil {
			httpx.FailField(c, 400, "invalid_bot", err.Error(), "name")
			return
		}
		c.JSON(201, gin.H{"bot": publicBotShowOnce(bot, plaintext)})
	}
}

// botRegenerateToken is POST /bots/:id/regenerate-token — admin-only.
func botRegenerateToken(s *store.Store) gin.HandlerFunc {
	return func(c *gin.Context) {
		id, err := strconv.ParseInt(c.Param("id"), 10, 64)
		if err != nil {
			httpx.Fail(c, 404, "bot_not_found", "Bot not found.")
			return
		}
		bot, err := s.GetBotByUserID(c.Request.Context(), id)
		if err != nil {
			if errors.Is(err, store.ErrNotFound) {
				httpx.Fail(c, 404, "bot_not_found", "Bot not found.")
				return
			}
			httpx.Fail(c, 500, "internal_error", "Could not fetch bot.")
			return
		}
		plaintext, err := s.RegenerateBotToken(c.Request.Context(), bot.UserID)
		if err != nil {
			if errors.Is(err, store.ErrNotFound) {
				httpx.Fail(c, 404, "bot_not_found", "Bot not found.")
				return
			}
			httpx.Fail(c, 500, "internal_error", "Could not regenerate bot token.")
			return
		}
		c.JSON(200, gin.H{"bot": publicBotShowOnce(bot, plaintext)})
	}
}

// botRevoke is POST /bots/:id/revoke — admin-only. Soft: the underlying user is deactivated, not
// deleted, so messages the bot already posted keep rendering.
func botRevoke(s *store.Store) gin.HandlerFunc {
	return func(c *gin.Context) {
		id, err := strconv.ParseInt(c.Param("id"), 10, 64)
		if err != nil {
			httpx.Fail(c, 404, "bot_not_found", "Bot not found.")
			return
		}
		if err := s.RevokeBot(c.Request.Context(), id); err != nil {
			if errors.Is(err, store.ErrNotFound) {
				httpx.Fail(c, 404, "bot_not_found", "Bot not found.")
				return
			}
			httpx.Fail(c, 500, "internal_error", "Could not revoke bot.")
			return
		}
		c.JSON(200, gin.H{"ok": true})
	}
}

// botDelete is DELETE /bots/:id — admin-only, idempotent. A hard delete of the bot's
// registration row; it requires the bot to already be revoked (store.ErrBotNotRevoked
// otherwise), since a revoked bot's token can no longer authenticate — deleting an active bot's
// row would leave a still-working credential with no UI left to manage it. Also fails with
// store.ErrBotInUse if a slash command still posts as this bot.
func botDelete(s *store.Store) gin.HandlerFunc {
	return func(c *gin.Context) {
		id, err := strconv.ParseInt(c.Param("id"), 10, 64)
		if err != nil {
			httpx.Fail(c, 404, "bot_not_found", "Bot not found.")
			return
		}
		if err := s.DeleteBot(c.Request.Context(), id); err != nil {
			if errors.Is(err, store.ErrBotNotRevoked) {
				httpx.Fail(c, 409, "bot_not_revoked", "Revoke this bot before deleting it.")
				return
			}
			if errors.Is(err, store.ErrBotInUse) {
				httpx.Fail(c, 409, "bot_in_use", "This bot still has slash commands registered to it — delete or reassign them first.")
				return
			}
			httpx.Fail(c, 500, "internal_error", "Could not delete bot.")
			return
		}
		c.JSON(200, gin.H{"ok": true})
	}
}
