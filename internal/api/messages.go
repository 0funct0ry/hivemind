package api

import (
	"database/sql"
	"errors"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/0funct0ry/hivemind/internal/api/httpx"
	"github.com/0funct0ry/hivemind/internal/realtime"
	"github.com/0funct0ry/hivemind/internal/store"
	"github.com/gin-gonic/gin"
)

type postLimiter struct {
	mu    sync.Mutex
	posts map[int64][]time.Time
}

func newPostLimiter() *postLimiter {
	return &postLimiter{posts: make(map[int64][]time.Time)}
}

func (l *postLimiter) Allow(userID int64, now time.Time) (time.Duration, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()

	cutoff := now.Add(-time.Minute)
	history := l.posts[userID]
	i := 0
	for i < len(history) && history[i].Before(cutoff) {
		i++
	}
	history = history[i:]

	if len(history) >= 30 {
		nextAvailable := history[0].Add(time.Minute)
		l.posts[userID] = history
		return nextAvailable.Sub(now), false
	}

	l.posts[userID] = append(history, now)
	return 0, true
}

var DisableRateLimits = false
var msgLimiter = newPostLimiter()

// ResetMsgLimiter clears the message rate limit history.
func ResetMsgLimiter() {
	msgLimiter.mu.Lock()
	defer msgLimiter.mu.Unlock()
	msgLimiter.posts = make(map[int64][]time.Time)
}

func publicMessage(m store.Message) gin.H {
	var userVal any = nil
	if m.User != nil {
		userVal = gin.H{
			"id":           strconv.FormatInt(m.User.ID, 10),
			"username":     m.User.Username,
			"display_name": m.User.DisplayName,
			"avatar_color": m.User.AvatarColor,
			"is_bot":       m.User.IsBot,
		}
	}

	atts := make([]gin.H, 0, len(m.Attachments))
	for _, a := range m.Attachments {
		attVal := gin.H{
			"id":   a.ID,
			"name": a.Name,
			"mime": a.Mime,
			"size": a.Size,
			"url":  a.URL,
		}
		if a.Width != nil {
			attVal["width"] = *a.Width
		}
		if a.Height != nil {
			attVal["height"] = *a.Height
		}
		atts = append(atts, attVal)
	}

	res := gin.H{
		"id":              strconv.FormatInt(m.ID, 10),
		"channel_id":      strconv.FormatInt(m.ChannelID, 10),
		"user_id":         strconv.FormatInt(m.UserID, 10),
		"user":            userVal,
		"body":            m.Body,
		"thread_id":       nil,
		"reply_count":     m.ReplyCount,
		"last_reply_id":   nil,
		"has_attachments": m.HasAttachments,
		"broadcast":       m.Broadcast,
		"attachments":     atts,
		"edited_at":       m.EditedAt,
		"deleted_at":      m.DeletedAt,
		"created_at":      m.CreatedAt,
		"client_msg_id":   m.ClientMsgID,
		"mentions":        []any{},
	}
	if m.ThreadID != nil {
		res["thread_id"] = strconv.FormatInt(*m.ThreadID, 10)
	}
	if m.LastReplyID != nil {
		res["last_reply_id"] = strconv.FormatInt(*m.LastReplyID, 10)
	}
	return res
}

var createMsgMu sync.Mutex

func messageCreate(s *store.Store, pub realtime.Publisher) gin.HandlerFunc {
	return func(c *gin.Context) {
		me, _ := CurrentUser(c)

		// 1. Rate Limit
		if !DisableRateLimits {
			if wait, ok := msgLimiter.Allow(me.ID, time.Now()); !ok {
				c.Header("Retry-After", strconv.Itoa(int(wait.Seconds())+1))
				httpx.Fail(c, 429, "rate_limited", "Too many messages. Please wait before posting again.")
				return
			}
		}

		// 2. Resolve Channel
		ch, _, ok := resolveChannel(c, s, me.ID)
		if !ok {
			return
		}

		// 3. Auto-join user to public channel if they are not yet a member
		isMem, err := s.IsMember(c.Request.Context(), ch.ID, me.ID)
		if err != nil {
			httpx.Fail(c, 500, "internal_error", "Could not verify membership.")
			return
		}
		if !isMem {
			if ch.Kind == "public" && ch.ArchivedAt == nil {
				if err := s.AddMembers(c.Request.Context(), ch.ID, []int64{me.ID}); err != nil {
					httpx.Fail(c, 500, "internal_error", "Could not auto-join channel.")
					return
				}
			} else {
				httpx.Fail(c, 404, "channel_not_found", "Channel not found.")
				return
			}
		}

		var in struct {
			Body              string   `json:"body"`
			ThreadID          *string  `json:"thread_id"`
			ClientMsgID       *string  `json:"client_msg_id"`
			FileIDs           []string `json:"file_ids"`
			AlsoSendToChannel bool     `json:"also_send_to_channel"`
		}
		if err := c.ShouldBindJSON(&in); err != nil {
			httpx.Fail(c, 400, "invalid_request", "Invalid request body.")
			return
		}

		var tID *int64
		if in.ThreadID != nil && *in.ThreadID != "" {
			val, err := strconv.ParseInt(*in.ThreadID, 10, 64)
			if err != nil {
				httpx.Fail(c, 400, "invalid_thread_id", "Invalid thread ID.")
				return
			}
			tID = &val
		}

		var isOnline func(int64) bool
		if hub, ok := pub.(*realtime.Hub); ok {
			isOnline = func(userID int64) bool {
				return hub.IsOnline(userID)
			}
		}

		msgIn := store.MessageInput{
			ChannelID:   ch.ID,
			UserID:      me.ID,
			Body:        in.Body,
			ThreadID:    tID,
			ClientMsgID: in.ClientMsgID,
			FileIDs:     in.FileIDs,
			Broadcast:   in.AlsoSendToChannel,
			IsOnline:    isOnline,
		}

		createMsgMu.Lock()
		msg, existed, err := s.CreateMessage(c.Request.Context(), msgIn)
		if err != nil {
			createMsgMu.Unlock()
			if errors.Is(err, store.ErrThreadChannelMismatch) {
				httpx.Fail(c, 400, "thread_channel_mismatch", "The parent thread and the reply must be in the same channel.")
				return
			}
			if errors.Is(err, store.ErrThreadDeleted) {
				httpx.Fail(c, 400, "thread_deleted", "Cannot reply to a deleted thread.")
				return
			}
			if errors.Is(err, store.ErrUserDeactivated) {
				httpx.Fail(c, 400, "user_deactivated", "Cannot post to a DM containing a deactivated user.")
				return
			}
			if errors.Is(err, store.ErrAttachmentNotFound) {
				httpx.Fail(c, 400, "attachment_not_found", "One or more attachments were not found.")
				return
			}
			if errors.Is(err, store.ErrAttachmentForbidden) {
				httpx.Fail(c, 403, "forbidden", "You do not have permission to attach one or more of the specified files.")
				return
			}
			httpx.Fail(c, 400, "invalid_message", err.Error())
			return
		}

		status := http.StatusCreated
		if existed {
			status = http.StatusOK
		}

		c.JSON(status, gin.H{"message": publicMessage(msg)})

		// Publish realtime event if this is a newly created message
		if !existed {
			if msg.ThreadID == nil {
				pub.Publish(realtime.Event{
					Type:      "message.created",
					Payload:   publicMessage(msg),
					ChannelID: msg.ChannelID,
				})
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

			// Publish mention.created to mentioned users
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
		createMsgMu.Unlock()
	}
}

func messageList(s *store.Store) gin.HandlerFunc {
	return func(c *gin.Context) {
		me, _ := CurrentUser(c)
		ch, access, ok := resolveChannel(c, s, me.ID)
		if !ok {
			return
		}
		if !access.CanRead {
			httpx.Fail(c, 404, "channel_not_found", "Channel not found.")
			return
		}

		var before *int64
		if val := c.Query("before"); val != "" {
			parsed, err := strconv.ParseInt(val, 10, 64)
			if err != nil {
				httpx.Fail(c, 400, "invalid_cursor", "Invalid before cursor.")
				return
			}
			before = &parsed
		}

		var after *int64
		if val := c.Query("after"); val != "" {
			parsed, err := strconv.ParseInt(val, 10, 64)
			if err != nil {
				httpx.Fail(c, 400, "invalid_cursor", "Invalid after cursor.")
				return
			}
			after = &parsed
		}

		// around takes precedence over before/after when both are supplied — it
		// fetches a symmetric window centered on the anchor id rather than paging.
		var around *int64
		if val := c.Query("around"); val != "" {
			parsed, err := strconv.ParseInt(val, 10, 64)
			if err != nil {
				httpx.Fail(c, 400, "invalid_cursor", "Invalid around cursor.")
				return
			}
			around = &parsed
		}

		limit := 50
		if val := c.Query("limit"); val != "" {
			parsed, err := strconv.Atoi(val)
			if err == nil {
				limit = parsed
			}
		}
		if limit <= 0 {
			limit = 50
		}
		if limit > 200 {
			limit = 200
		}

		if around != nil {
			msgs, err := s.ListChannelMessages(c.Request.Context(), ch.ID, nil, nil, around, limit)
			if err != nil {
				httpx.Fail(c, 500, "internal_error", "Could not fetch messages.")
				return
			}
			data := make([]gin.H, 0, len(msgs))
			for _, m := range msgs {
				data = append(data, publicMessage(m))
			}
			var nextBefore string
			if len(msgs) > 0 {
				nextBefore = strconv.FormatInt(msgs[0].ID, 10)
			}
			c.JSON(200, gin.H{
				"data":        data,
				"has_more":    false,
				"next_before": nextBefore,
			})
			return
		}

		// Fetch limit + 1 to verify has_more
		msgs, err := s.ListChannelMessages(c.Request.Context(), ch.ID, before, after, nil, limit+1)
		if err != nil {
			httpx.Fail(c, 500, "internal_error", "Could not fetch messages.")
			return
		}

		hasMore := false
		var nextBefore string

		if len(msgs) > limit {
			hasMore = true
			if after != nil {
				// Paging forward, extra message is at the end
				msgs = msgs[:limit]
			} else {
				// Paging backward, extra message is at the start (reversed ASC view)
				nextBefore = strconv.FormatInt(msgs[0].ID, 10)
				msgs = msgs[1:]
			}
		}

		if len(msgs) > 0 && nextBefore == "" {
			nextBefore = strconv.FormatInt(msgs[0].ID, 10)
		}

		data := make([]gin.H, 0, len(msgs))
		for _, m := range msgs {
			data = append(data, publicMessage(m))
		}

		c.JSON(200, gin.H{
			"data":        data,
			"has_more":    hasMore,
			"next_before": nextBefore,
		})
	}
}

func messageGet(s *store.Store) gin.HandlerFunc {
	return func(c *gin.Context) {
		me, _ := CurrentUser(c)
		id, err := strconv.ParseInt(c.Param("id"), 10, 64)
		if err != nil {
			httpx.Fail(c, 404, "message_not_found", "Message not found.")
			return
		}

		msg, err := s.GetMessage(c.Request.Context(), id)
		if err != nil {
			if errors.Is(err, store.ErrNotFound) || errors.Is(err, sql.ErrNoRows) {
				httpx.Fail(c, 404, "message_not_found", "Message not found.")
				return
			}
			httpx.Fail(c, 500, "internal_error", "Could not fetch message.")
			return
		}

		// Check channel access
		access, err := s.CanAccessChannel(c.Request.Context(), me.ID, msg.ChannelID)
		if err != nil {
			if errors.Is(err, store.ErrNotFound) || errors.Is(err, sql.ErrNoRows) {
				httpx.Fail(c, 404, "message_not_found", "Message not found.")
				return
			}
			httpx.Fail(c, 500, "internal_error", "Could not check channel access.")
			return
		}
		if !access.CanRead {
			httpx.Fail(c, 404, "message_not_found", "Message not found.")
			return
		}

		c.JSON(200, gin.H{"message": publicMessage(msg)})
	}
}

func messageListReplies(s *store.Store) gin.HandlerFunc {
	return func(c *gin.Context) {
		me, _ := CurrentUser(c)
		id, err := strconv.ParseInt(c.Param("id"), 10, 64)
		if err != nil {
			httpx.Fail(c, 404, "message_not_found", "Message not found.")
			return
		}

		// To check access, we must first find the message's channel ID.
		msg, err := s.GetMessage(c.Request.Context(), id)
		if err != nil {
			if errors.Is(err, store.ErrNotFound) || errors.Is(err, sql.ErrNoRows) {
				httpx.Fail(c, 404, "message_not_found", "Message not found.")
				return
			}
			httpx.Fail(c, 500, "internal_error", "Could not fetch message.")
			return
		}

		// Check channel access
		access, err := s.CanAccessChannel(c.Request.Context(), me.ID, msg.ChannelID)
		if err != nil {
			if errors.Is(err, store.ErrNotFound) || errors.Is(err, sql.ErrNoRows) {
				httpx.Fail(c, 404, "message_not_found", "Message not found.")
				return
			}
			httpx.Fail(c, 500, "internal_error", "Could not check channel access.")
			return
		}
		if !access.CanRead {
			httpx.Fail(c, 404, "message_not_found", "Message not found.")
			return
		}

		var after *int64
		if val := c.Query("after"); val != "" {
			parsed, err := strconv.ParseInt(val, 10, 64)
			if err != nil {
				httpx.Fail(c, 400, "invalid_cursor", "Invalid after cursor.")
				return
			}
			after = &parsed
		}

		limit := 50
		if val := c.Query("limit"); val != "" {
			parsed, err := strconv.Atoi(val)
			if err == nil {
				limit = parsed
			}
		}
		if limit <= 0 {
			limit = 50
		}
		if limit > 200 {
			limit = 200
		}

		// Fetch limit + 1 to determine has_more
		msgs, err := s.ListReplies(c.Request.Context(), id, after, limit+1)
		if err != nil {
			httpx.Fail(c, 500, "internal_error", "Could not fetch replies.")
			return
		}

		var root gin.H
		var replies []store.Message

		if after == nil {
			if len(msgs) > 0 {
				root = publicMessage(msgs[0])
				replies = msgs[1:]
			}
		} else {
			replies = msgs
		}

		hasMore := false
		if len(replies) > limit {
			hasMore = true
			replies = replies[:limit]
		}

		data := make([]gin.H, 0, len(replies))
		for _, m := range replies {
			data = append(data, publicMessage(m))
		}

		c.JSON(200, gin.H{
			"root":     root,
			"data":     data,
			"has_more": hasMore,
		})
	}
}
