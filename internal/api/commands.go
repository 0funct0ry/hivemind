package api

import (
	"encoding/json"
	"errors"
	"strconv"

	"github.com/0funct0ry/hivemind/internal/api/httpx"
	"github.com/0funct0ry/hivemind/internal/realtime"
	"github.com/0funct0ry/hivemind/internal/store"
	"github.com/gin-gonic/gin"
)

// publicSlashCommand renders a store.SlashCommand for the composer's autocomplete — trigger,
// description, syntax hint, admin_only only, never webhook_url or the secret (SPEC.md §4.12:
// visibility is unrestricted, execution is what's gated).
func publicSlashCommand(cmd store.SlashCommand) gin.H {
	return gin.H{
		"trigger":     cmd.Trigger,
		"description": cmd.Description,
		"syntax_hint": cmd.SyntaxHint,
		"admin_only":  cmd.AdminOnly,
	}
}

// publicSlashCommandAdmin renders full management detail with a masked secret, for the
// Settings page.
func publicSlashCommandAdmin(cmd store.SlashCommand) gin.H {
	return gin.H{
		"id":            cmd.ID,
		"trigger":       cmd.Trigger,
		"bot_id":        strconv.FormatInt(cmd.BotID, 10),
		"description":   cmd.Description,
		"syntax_hint":   cmd.SyntaxHint,
		"webhook_url":   cmd.WebhookURL,
		"masked_secret": "whsec_" + "••••••••" + cmd.SecretLast4,
		"admin_only":    cmd.AdminOnly,
		"status":        cmd.Status,
		"created_by":    strconv.FormatInt(cmd.CreatedBy, 10),
		"created_at":    cmd.CreatedAt,
		"updated_at":    cmd.UpdatedAt,
	}
}

// publicSlashCommandAdminShowOnce is publicSlashCommandAdmin plus the plaintext signing secret,
// shown exactly once at create/regenerate time.
func publicSlashCommandAdminShowOnce(cmd store.SlashCommand, plaintext string) gin.H {
	res := publicSlashCommandAdmin(cmd)
	res["secret"] = plaintext
	return res
}

// slashCommandList is GET /slash-commands — any authenticated caller, feeding the composer's
// autocomplete menu (SPEC.md §4.12).
func slashCommandList(s *store.Store) gin.HandlerFunc {
	return func(c *gin.Context) {
		list, err := s.ListSlashCommands(c.Request.Context())
		if err != nil {
			httpx.Fail(c, 500, "internal_error", "Could not list slash commands.")
			return
		}
		data := make([]gin.H, 0, len(list))
		for _, cmd := range list {
			data = append(data, publicSlashCommand(cmd))
		}
		c.JSON(200, gin.H{"data": data})
	}
}

// slashCommandListAdmin is GET /slash-commands/admin — admin-only, for Settings management.
func slashCommandListAdmin(s *store.Store) gin.HandlerFunc {
	return func(c *gin.Context) {
		list, err := s.ListSlashCommandsForAdmin(c.Request.Context())
		if err != nil {
			httpx.Fail(c, 500, "internal_error", "Could not list slash commands.")
			return
		}
		data := make([]gin.H, 0, len(list))
		for _, cmd := range list {
			data = append(data, publicSlashCommandAdmin(cmd))
		}
		c.JSON(200, gin.H{"data": data})
	}
}

type slashCommandCreateBody struct {
	Trigger     string `json:"trigger"`
	BotID       string `json:"bot_id"`
	Description string `json:"description"`
	SyntaxHint  string `json:"syntax_hint"`
	WebhookURL  string `json:"webhook_url"`
	AdminOnly   bool   `json:"admin_only"`
}

// slashCommandCreate is POST /slash-commands — admin-only.
func slashCommandCreate(s *store.Store) gin.HandlerFunc {
	return func(c *gin.Context) {
		me, _ := CurrentUser(c)
		var in slashCommandCreateBody
		if err := c.ShouldBindJSON(&in); err != nil {
			httpx.Fail(c, 400, "invalid_request", "Invalid request body.")
			return
		}
		if in.Trigger == "" {
			httpx.FailField(c, 400, "invalid_request", "trigger is required.", "trigger")
			return
		}
		if in.WebhookURL == "" {
			httpx.FailField(c, 400, "invalid_request", "webhook_url is required.", "webhook_url")
			return
		}
		botID, err := strconv.ParseInt(in.BotID, 10, 64)
		if err != nil {
			httpx.FailField(c, 400, "invalid_request", "Invalid bot_id.", "bot_id")
			return
		}

		cmd, plaintext, err := s.CreateSlashCommand(c.Request.Context(), store.SlashCommandInput{
			Trigger:     in.Trigger,
			BotID:       botID,
			Description: in.Description,
			SyntaxHint:  in.SyntaxHint,
			WebhookURL:  in.WebhookURL,
			AdminOnly:   in.AdminOnly,
			CreatedBy:   me.ID,
		})
		if err != nil {
			if errors.Is(err, store.ErrTriggerTaken) {
				httpx.FailField(c, 409, "trigger_taken", "That trigger is already registered.", "trigger")
				return
			}
			if errors.Is(err, store.ErrNotFound) {
				httpx.FailField(c, 400, "invalid_request", "Bot not found.", "bot_id")
				return
			}
			httpx.FailField(c, 400, "invalid_slash_command", err.Error(), "webhook_url")
			return
		}
		c.JSON(201, gin.H{"command": publicSlashCommandAdminShowOnce(cmd, plaintext)})
	}
}

type slashCommandPatchBody struct {
	Description *string `json:"description"`
	SyntaxHint  *string `json:"syntax_hint"`
	WebhookURL  *string `json:"webhook_url"`
	AdminOnly   *bool   `json:"admin_only"`
	Status      *string `json:"status"`
}

// slashCommandUpdate is PATCH /slash-commands/:id — admin-only.
func slashCommandUpdate(s *store.Store) gin.HandlerFunc {
	return func(c *gin.Context) {
		id := c.Param("id")
		var in slashCommandPatchBody
		if err := c.ShouldBindJSON(&in); err != nil {
			httpx.Fail(c, 400, "invalid_request", "Invalid request body.")
			return
		}
		cmd, err := s.UpdateSlashCommand(c.Request.Context(), id, store.SlashCommandPatch{
			Description: in.Description,
			SyntaxHint:  in.SyntaxHint,
			WebhookURL:  in.WebhookURL,
			AdminOnly:   in.AdminOnly,
			Status:      in.Status,
		})
		if err != nil {
			if errors.Is(err, store.ErrNotFound) {
				httpx.Fail(c, 404, "command_not_found", "Slash command not found.")
				return
			}
			httpx.FailField(c, 400, "invalid_slash_command", err.Error(), "webhook_url")
			return
		}
		c.JSON(200, gin.H{"command": publicSlashCommandAdmin(cmd)})
	}
}

// slashCommandRegenerateSecret is POST /slash-commands/:id/regenerate-secret — admin-only.
func slashCommandRegenerateSecret(s *store.Store) gin.HandlerFunc {
	return func(c *gin.Context) {
		id := c.Param("id")
		cmd, plaintext, err := s.RegenerateSlashCommandSecret(c.Request.Context(), id)
		if err != nil {
			if errors.Is(err, store.ErrNotFound) {
				httpx.Fail(c, 404, "command_not_found", "Slash command not found.")
				return
			}
			httpx.Fail(c, 500, "internal_error", "Could not regenerate slash command secret.")
			return
		}
		c.JSON(200, gin.H{"command": publicSlashCommandAdminShowOnce(cmd, plaintext)})
	}
}

// slashCommandDelete is DELETE /slash-commands/:id — admin-only, idempotent.
func slashCommandDelete(s *store.Store) gin.HandlerFunc {
	return func(c *gin.Context) {
		id := c.Param("id")
		if err := s.DeleteSlashCommand(c.Request.Context(), id); err != nil {
			httpx.Fail(c, 500, "internal_error", "Could not delete slash command.")
			return
		}
		c.JSON(200, gin.H{"ok": true})
	}
}

type commandExecBody struct {
	ChannelID string   `json:"channel_id"`
	ThreadID  *string  `json:"thread_id"`
	Trigger   string   `json:"trigger"`
	Args      []string `json:"args"`
}

func ephemeralResult(text string) gin.H {
	return gin.H{"response_type": "ephemeral", "text": text, "attachments": []any{}}
}

// commandExecute is POST /commands/execute — any member, per SPEC.md §4.12. This endpoint
// always returns 200: internal failures (unknown command, permission denied, webhook timeout or
// error) are communicated inside the response body as a synthesized ephemeral card, never as an
// HTTP error, so the client needs only one rendering path. The two genuine HTTP-level failures
// are an unresolvable channel/command (404, matching the private-channel confirmation-avoidance
// posture elsewhere in this codebase).
func commandExecute(s *store.Store, pub realtime.Publisher) gin.HandlerFunc {
	return func(c *gin.Context) {
		me, _ := CurrentUser(c)
		var in commandExecBody
		if err := c.ShouldBindJSON(&in); err != nil {
			httpx.Fail(c, 400, "invalid_request", "Invalid request body.")
			return
		}
		channelID, err := strconv.ParseInt(in.ChannelID, 10, 64)
		if err != nil {
			httpx.Fail(c, 404, "channel_not_found", "Channel not found.")
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

		cmd, err := s.GetSlashCommandByTrigger(c.Request.Context(), in.Trigger)
		if err != nil {
			if errors.Is(err, store.ErrNotFound) {
				httpx.Fail(c, 404, "command_not_found", "Slash command not found.")
				return
			}
			httpx.Fail(c, 500, "internal_error", "Could not resolve slash command.")
			return
		}

		access, err := s.CanAccessChannel(c.Request.Context(), me.ID, channelID)
		if err != nil {
			if errors.Is(err, store.ErrNotFound) {
				httpx.Fail(c, 404, "channel_not_found", "Channel not found.")
				return
			}
			httpx.Fail(c, 500, "internal_error", "Could not check channel access.")
			return
		}
		if !access.CanRead {
			httpx.Fail(c, 404, "channel_not_found", "Channel not found.")
			return
		}

		if cmd.AdminOnly && !access.IsOwner && me.Role != "admin" {
			c.JSON(200, ephemeralResult("You don't have permission to run "+cmd.Trigger+"."))
			return
		}

		result, err := s.ExecuteSlashCommand(c.Request.Context(), cmd, store.CommandExecRequest{
			UserID:    me.ID,
			Username:  me.Username,
			ChannelID: channelID,
			ThreadID:  threadID,
			Args:      in.Args,
		})
		if err != nil {
			httpx.Fail(c, 500, "internal_error", "Could not execute slash command.")
			return
		}
		if result.Failed {
			text := "That command failed to respond."
			if result.FailureKind == "timeout" {
				text = cmd.Trigger + " timed out waiting for a response."
			}
			c.JSON(200, ephemeralResult(text))
			return
		}

		if result.ResponseType == "ephemeral" {
			c.JSON(200, gin.H{"response_type": "ephemeral", "text": result.Text, "attachments": attachmentsOrEmpty(result.Attachments)})
			return
		}

		// in_channel: post through the same store.CreateMessage path POST /messages uses,
		// authored as the command's bot, then publish the realtime event ourselves since this
		// bypasses the messageCreate Gin handler entirely (SPEC.md §4.12 point 6).
		cardJSON, err := json.Marshal(store.WebhookCard{
			Title: "", Severity: "neutral", Body: result.Text, Fallback: false,
		})
		if err != nil {
			httpx.Fail(c, 500, "internal_error", "Could not build response card.")
			return
		}
		cardStr := string(cardJSON)
		if err := s.AddMembers(c.Request.Context(), channelID, []int64{cmd.BotID}); err != nil {
			httpx.Fail(c, 500, "internal_error", "Could not post command response.")
			return
		}
		msg, _, err := s.CreateMessage(c.Request.Context(), store.MessageInput{
			ChannelID: channelID,
			UserID:    cmd.BotID,
			Body:      result.Text,
			ThreadID:  threadID,
			Card:      &cardStr,
		})
		if err != nil {
			httpx.Fail(c, 500, "internal_error", "Could not post command response.")
			return
		}
		if msg.ThreadID == nil {
			pub.Publish(realtime.Event{
				Type:      "message.created",
				Payload:   publicMessage(msg),
				ChannelID: msg.ChannelID,
			})
		} else {
			if rootMsg, err := s.GetMessage(c.Request.Context(), *msg.ThreadID); err == nil {
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

		c.JSON(200, gin.H{"response_type": "in_channel"})
	}
}

func attachmentsOrEmpty(raw json.RawMessage) any {
	if len(raw) == 0 {
		return []any{}
	}
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return []any{}
	}
	return v
}
