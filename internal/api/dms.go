package api

import (
	"database/sql"
	"errors"
	"strconv"

	"github.com/0funct0ry/hivemind/internal/api/httpx"
	"github.com/0funct0ry/hivemind/internal/store"
	"github.com/gin-gonic/gin"
)

func publicDM(c store.ChannelDetails) gin.H {
	var peerVal any = nil
	if c.Peer != nil {
		peerVal = gin.H{
			"id":           strconv.FormatInt(c.Peer.ID, 10),
			"username":     c.Peer.Username,
			"email":        c.Peer.Email,
			"display_name": c.Peer.DisplayName,
			"avatar_color": c.Peer.AvatarColor,
			"avatar_url":   c.Peer.AvatarURL,
			"role":         c.Peer.Role,
			"is_bot":       c.Peer.IsBot,
			"status":       c.Peer.Status,
		}
	}

	res := gin.H{
		"id":                   strconv.FormatInt(c.ID, 10),
		"kind":                 c.Kind,
		"slug":                 c.Slug,
		"name":                 c.Name,
		"topic":                c.Topic,
		"dm_key":               c.DMKey,
		"created_by":           nil,
		"archived_at":          c.ArchivedAt,
		"last_message_id":      nil,
		"created_at":           c.CreatedAt,
		"updated_at":           c.UpdatedAt,
		"member_count":         c.MemberCount,
		"last_read_message_id": strconv.FormatInt(c.LastReadMessageID, 10),
		"joined":               c.Joined,
		"peer":                 peerVal,
	}
	if c.CreatedBy != nil {
		res["created_by"] = strconv.FormatInt(*c.CreatedBy, 10)
	}
	if c.LastMessageID != nil {
		res["last_message_id"] = strconv.FormatInt(*c.LastMessageID, 10)
	}
	return res
}

func dmList(s *store.Store) gin.HandlerFunc {
	return func(c *gin.Context) {
		me, _ := CurrentUser(c)
		list, err := s.ListDMs(c.Request.Context(), me.ID)
		if err != nil {
			httpx.Fail(c, 500, "internal_error", "Could not list DMs.")
			return
		}

		data := make([]gin.H, 0, len(list))
		for _, d := range list {
			data = append(data, publicDM(d))
		}
		c.JSON(200, gin.H{"data": data})
	}
}

func dmCreate(s *store.Store) gin.HandlerFunc {
	return func(c *gin.Context) {
		me, _ := CurrentUser(c)
		var in struct {
			UserID any `json:"user_id"`
		}
		if err := c.ShouldBindJSON(&in); err != nil {
			httpx.Fail(c, 400, "invalid_request", "Invalid request body.")
			return
		}

		var peerID int64
		switch v := in.UserID.(type) {
		case string:
			var err error
			peerID, err = strconv.ParseInt(v, 10, 64)
			if err != nil {
				httpx.FailField(c, 400, "invalid_request", "Invalid user ID format.", "user_id")
				return
			}
		case float64:
			peerID = int64(v)
		case int64:
			peerID = v
		default:
			httpx.FailField(c, 400, "invalid_request", "User ID is required.", "user_id")
			return
		}

		// Ensure peer exists
		_, err := s.GetUserByID(c.Request.Context(), peerID)
		if err != nil {
			if errors.Is(err, store.ErrNotFound) || errors.Is(err, sql.ErrNoRows) {
				httpx.Fail(c, 404, "user_not_found", "User not found.")
				return
			}
			httpx.Fail(c, 500, "internal_error", "Could not retrieve user.")
			return
		}

		ch, err := s.GetOrCreateDM(c.Request.Context(), me.ID, peerID)
		if err != nil {
			httpx.Fail(c, 500, "internal_error", "Could not create or retrieve DM.")
			return
		}

		c.JSON(200, gin.H{"channel": publicDM(ch)})
	}
}
