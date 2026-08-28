package api

import (
	"database/sql"
	"errors"
	"strconv"
	"strings"

	"github.com/0funct0ry/hivemind/internal/api/httpx"
	"github.com/0funct0ry/hivemind/internal/realtime"
	"github.com/0funct0ry/hivemind/internal/store"
	"github.com/gin-gonic/gin"
)

func resolveChannel(c *gin.Context, s *store.Store, meID int64) (store.Channel, store.ChannelAccess, bool) {
	idParam := c.Param("id")
	var ch store.Channel
	var err error
	if strings.HasPrefix(idParam, "slug:") {
		slug := strings.TrimPrefix(idParam, "slug:")
		ch, err = s.GetChannelBySlug(c.Request.Context(), slug)
	} else {
		id, parseErr := strconv.ParseInt(idParam, 10, 64)
		if parseErr != nil {
			httpx.Fail(c, 404, "channel_not_found", "Channel not found.")
			return store.Channel{}, store.ChannelAccess{}, false
		}
		ch, err = s.GetChannel(c.Request.Context(), id)
	}

	if err != nil {
		if errors.Is(err, store.ErrNotFound) || errors.Is(err, sql.ErrNoRows) {
			httpx.Fail(c, 404, "channel_not_found", "Channel not found.")
			return store.Channel{}, store.ChannelAccess{}, false
		}
		httpx.Fail(c, 500, "internal_error", "Could not fetch channel.")
		return store.Channel{}, store.ChannelAccess{}, false
	}

	access, err := s.CanAccessChannel(c.Request.Context(), meID, ch.ID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) || errors.Is(err, sql.ErrNoRows) {
			httpx.Fail(c, 404, "channel_not_found", "Channel not found.")
			return store.Channel{}, store.ChannelAccess{}, false
		}
		httpx.Fail(c, 500, "internal_error", "Could not check channel access.")
		return store.Channel{}, store.ChannelAccess{}, false
	}

	return ch, access, true
}

// publicChannel renders a bare store.Channel with string-serialized ids, matching the
// rest of the API's id-as-string convention. store.Channel's own json tags leave id
// fields as raw int64/*int64, which would otherwise leak numeric ids to callers that
// (correctly, per every other endpoint) expect strings. Timestamps (created_at,
// updated_at, archived_at) are left as JSON numbers — they're unix millis, not ids.
func publicChannel(ch store.Channel) gin.H {
	res := gin.H{
		"id":              strconv.FormatInt(ch.ID, 10),
		"kind":            ch.Kind,
		"slug":            ch.Slug,
		"name":            ch.Name,
		"topic":           ch.Topic,
		"dm_key":          ch.DMKey,
		"created_by":      nil,
		"archived_at":     ch.ArchivedAt,
		"last_message_id": nil,
		"created_at":      ch.CreatedAt,
		"updated_at":      ch.UpdatedAt,
	}
	if ch.CreatedBy != nil {
		res["created_by"] = strconv.FormatInt(*ch.CreatedBy, 10)
	}
	if ch.LastMessageID != nil {
		res["last_message_id"] = strconv.FormatInt(*ch.LastMessageID, 10)
	}
	return res
}

func publicChannelDetails(ch store.ChannelDetails) gin.H {
	res := gin.H{
		"id":                   strconv.FormatInt(ch.ID, 10),
		"kind":                 ch.Kind,
		"slug":                 ch.Slug,
		"name":                 ch.Name,
		"topic":                ch.Topic,
		"member_count":         ch.MemberCount,
		"last_message_id":      nil,
		"last_read_message_id": strconv.FormatInt(ch.LastReadMessageID, 10),
		"joined":               ch.Joined,
	}
	if ch.LastMessageID != nil {
		res["last_message_id"] = strconv.FormatInt(*ch.LastMessageID, 10)
	}
	return res
}

func channelList(s *store.Store) gin.HandlerFunc {
	return func(c *gin.Context) {
		me, _ := CurrentUser(c)
		var list []store.ChannelDetails
		var err error
		if c.Query("joinable") == "true" {
			list, err = s.ListJoinableChannels(c.Request.Context(), me.ID)
		} else {
			includeArchived := c.Query("include_archived") == "true" && me.Role == "admin"
			list, err = s.ListVisibleChannels(c.Request.Context(), me.ID, includeArchived)
		}
		if err != nil {
			httpx.Fail(c, 500, "internal_error", "Could not list channels.")
			return
		}
		data := make([]gin.H, 0, len(list))
		for _, ch := range list {
			data = append(data, publicChannelDetails(ch))
		}
		c.JSON(200, gin.H{"data": data})
	}
}

func channelCreate(s *store.Store) gin.HandlerFunc {
	return func(c *gin.Context) {
		me, _ := CurrentUser(c)
		var in struct {
			Kind      string  `json:"kind"`
			Slug      string  `json:"slug"`
			Name      string  `json:"name"`
			Topic     string  `json:"topic"`
			MemberIDs []int64 `json:"member_ids"`
		}
		if err := c.ShouldBindJSON(&in); err != nil {
			httpx.Fail(c, 400, "invalid_request", "Invalid request body.")
			return
		}
		ch, err := s.CreateChannel(c.Request.Context(), in.Kind, in.Slug, in.Name, in.Topic, me.ID, in.MemberIDs)
		if err != nil {
			httpx.Fail(c, 400, "invalid_channel", err.Error())
			return
		}
		c.JSON(201, gin.H{"channel": publicChannel(ch)})
	}
}

func channelGet(s *store.Store) gin.HandlerFunc {
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
		c.JSON(200, gin.H{"channel": publicChannel(ch)})
	}
}

func channelUpdate(s *store.Store) gin.HandlerFunc {
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
			httpx.Fail(c, 403, "forbidden", "Only owner or admin can update the channel.")
			return
		}
		var in struct {
			Name     *string `json:"name"`
			Topic    *string `json:"topic"`
			Archived *bool   `json:"archived"`
		}
		if err := c.ShouldBindJSON(&in); err != nil {
			httpx.Fail(c, 400, "invalid_request", "Invalid request body.")
			return
		}
		if in.Archived != nil {
			if ch.Kind == "dm" {
				httpx.Fail(c, 400, "channel_not_archivable", "DM channels cannot be archived.")
				return
			}
			var err error
			if *in.Archived {
				err = s.ArchiveChannel(c.Request.Context(), ch.ID)
			} else {
				err = s.UnarchiveChannel(c.Request.Context(), ch.ID)
			}
			if err != nil {
				httpx.Fail(c, 400, "invalid_channel", err.Error())
				return
			}
		}
		name, topic := ch.Name, ch.Topic
		if in.Name != nil {
			name = *in.Name
		}
		if in.Topic != nil {
			topic = *in.Topic
		}
		if err := s.UpdateChannel(c.Request.Context(), ch.ID, name, topic); err != nil {
			httpx.Fail(c, 400, "invalid_channel", err.Error())
			return
		}
		updated, err := s.GetChannel(c.Request.Context(), ch.ID)
		if err != nil {
			httpx.Fail(c, 500, "internal_error", "Could not get updated channel.")
			return
		}
		c.JSON(200, gin.H{"channel": publicChannel(updated)})
	}
}

func channelJoin(s *store.Store) gin.HandlerFunc {
	return func(c *gin.Context) {
		me, _ := CurrentUser(c)
		ch, _, ok := resolveChannel(c, s, me.ID)
		if !ok {
			return
		}
		if ch.ArchivedAt != nil {
			httpx.Fail(c, 400, "channel_archived", "This channel is archived.")
			return
		}
		if ch.Kind != "public" {
			httpx.Fail(c, 404, "channel_not_found", "Channel not found.")
			return
		}
		if err := s.AddMembers(c.Request.Context(), ch.ID, []int64{me.ID}); err != nil {
			httpx.Fail(c, 500, "internal_error", "Could not join channel.")
			return
		}
		c.Status(204)
	}
}

func channelLeave(s *store.Store) gin.HandlerFunc {
	return func(c *gin.Context) {
		me, _ := CurrentUser(c)
		ch, _, ok := resolveChannel(c, s, me.ID)
		if !ok {
			return
		}
		isMem, err := s.IsMember(c.Request.Context(), ch.ID, me.ID)
		if err != nil {
			httpx.Fail(c, 500, "internal_error", "Could not check membership.")
			return
		}
		if !isMem {
			httpx.Fail(c, 400, "not_member", "You are not a member of this channel.")
			return
		}
		if err := s.RemoveMember(c.Request.Context(), ch.ID, me.ID); err != nil {
			httpx.Fail(c, 400, "cannot_leave", err.Error())
			return
		}
		c.Status(204)
	}
}

func channelMembersList(s *store.Store, h *realtime.Hub) gin.HandlerFunc {
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
		members, err := s.ListMembers(c.Request.Context(), ch.ID)
		if err != nil {
			httpx.Fail(c, 500, "internal_error", "Could not list members.")
			return
		}
		data := make([]gin.H, 0, len(members))
		for _, m := range members {
			data = append(data, publicUserOnline(m, h))
		}
		c.JSON(200, gin.H{"data": data})
	}
}

func channelAddMembers(s *store.Store) gin.HandlerFunc {
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
		if ch.Kind == "dm" {
			httpx.Fail(c, 400, "invalid_operation", "Cannot add members to a DM.")
			return
		}

		if ch.Kind == "public" {
			isMem, err := s.IsMember(c.Request.Context(), ch.ID, me.ID)
			if err != nil || !isMem {
				httpx.Fail(c, 403, "forbidden", "Must be a member to add others to a public channel.")
				return
			}
		} else if ch.Kind == "private" {
			if !access.IsOwner && me.Role != "admin" {
				httpx.Fail(c, 403, "forbidden", "Only owner or admin can add members to a private channel.")
				return
			}
		}

		var in struct {
			UserIDs []int64 `json:"user_ids"`
		}
		if err := c.ShouldBindJSON(&in); err != nil {
			httpx.Fail(c, 400, "invalid_request", "Invalid request body.")
			return
		}
		if len(in.UserIDs) == 0 {
			c.Status(204)
			return
		}
		if err := s.AddMembers(c.Request.Context(), ch.ID, in.UserIDs); err != nil {
			httpx.Fail(c, 500, "internal_error", "Could not add members.")
			return
		}
		c.Status(204)
	}
}

func channelRemoveMember(s *store.Store) gin.HandlerFunc {
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
		if ch.Kind == "dm" {
			httpx.Fail(c, 400, "invalid_operation", "Cannot remove members from a DM.")
			return
		}
		if !access.IsOwner && me.Role != "admin" {
			httpx.Fail(c, 403, "forbidden", "Only owner or admin can remove members.")
			return
		}
		uid, err := strconv.ParseInt(c.Param("uid"), 10, 64)
		if err != nil {
			httpx.Fail(c, 400, "invalid_request", "Invalid user ID.")
			return
		}
		if err := s.RemoveMember(c.Request.Context(), ch.ID, uid); err != nil {
			httpx.Fail(c, 400, "cannot_remove", err.Error())
			return
		}
		c.Status(204)
	}
}

// Client contract: the unread divider position is decided by
// the client from last_read_message_id at channel-open time and frozen until the channel is
// left. The server never tells the client where to draw the line mid-session.
func channelRead(s *store.Store, h *realtime.Hub) gin.HandlerFunc {
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

		var in struct {
			MessageID string `json:"message_id"`
		}
		if err := c.ShouldBindJSON(&in); err != nil {
			httpx.Fail(c, 400, "invalid_request", "Invalid request body.")
			return
		}

		msgID, err := strconv.ParseInt(in.MessageID, 10, 64)
		if err != nil {
			httpx.Fail(c, 400, "invalid_message_id", "Invalid message ID.")
			return
		}

		if err := s.MarkRead(c.Request.Context(), me.ID, ch.ID, msgID); err != nil {
			httpx.Fail(c, 500, "internal_error", "Could not mark read.")
			return
		}

		// Fan out read.updated to other connections of the same user
		h.Publish(realtime.Event{
			Type: "read.updated",
			Payload: map[string]string{
				"channel_id":           strconv.FormatInt(ch.ID, 10),
				"last_read_message_id": in.MessageID,
			},
			UserID:      me.ID,
			ExcludeUser: me.ID,
		})

		c.Status(204)
	}
}

func unreadSummary(s *store.Store) gin.HandlerFunc {
	return func(c *gin.Context) {
		me, _ := CurrentUser(c)
		summary, err := s.UnreadSummary(c.Request.Context(), me.ID)
		if err != nil {
			httpx.Fail(c, 500, "internal_error", "Could not get unread summary.")
			return
		}
		channels := make([]gin.H, 0, len(summary))
		var totalUnread, totalMentions int
		for _, item := range summary {
			channels = append(channels, gin.H{
				"channel_id":   strconv.FormatInt(item.ChannelID, 10),
				"unread_count": item.UnreadCount,
				"has_mention":  item.MentionCount > 0,
			})
			totalUnread += item.UnreadCount
			totalMentions += item.MentionCount
		}
		c.JSON(200, gin.H{
			"channels":       channels,
			"total_unread":   totalUnread,
			"total_mentions": totalMentions,
		})
	}
}
