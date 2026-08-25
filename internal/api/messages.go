package api

import (
	"database/sql"
	"errors"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/0funct0ry/hivemind/internal/api/httpx"
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

var msgLimiter = newPostLimiter()

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

func messageCreate(s *store.Store) gin.HandlerFunc {
	return func(c *gin.Context) {
		me, _ := CurrentUser(c)

		// 1. Rate Limit
		if wait, ok := msgLimiter.Allow(me.ID, time.Now()); !ok {
			c.Header("Retry-After", strconv.Itoa(int(wait.Seconds())+1))
			httpx.Fail(c, 429, "rate_limited", "Too many messages. Please wait before posting again.")
			return
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
			Body        string   `json:"body"`
			ThreadID    *string  `json:"thread_id"`
			ClientMsgID *string  `json:"client_msg_id"`
			FileIDs     []string `json:"file_ids"`
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

		msgIn := store.MessageInput{
			ChannelID:   ch.ID,
			UserID:      me.ID,
			Body:        in.Body,
			ThreadID:    tID,
			ClientMsgID: in.ClientMsgID,
			FileIDs:     in.FileIDs,
		}

		msg, existed, err := s.CreateMessage(c.Request.Context(), msgIn)
		if err != nil {
			httpx.Fail(c, 400, "invalid_message", err.Error())
			return
		}

		status := http.StatusCreated
		if existed {
			status = http.StatusOK
		}

		c.JSON(status, gin.H{"message": publicMessage(msg)})
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

		// Fetch limit + 1 to verify has_more
		msgs, err := s.ListChannelMessages(c.Request.Context(), ch.ID, before, after, limit+1)
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
