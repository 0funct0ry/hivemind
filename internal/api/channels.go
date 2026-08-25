package api

import (
	"database/sql"
	"errors"
	"strconv"
	"strings"

	"github.com/0funct0ry/hivemind/internal/api/httpx"
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

func channelList(s *store.Store) gin.HandlerFunc {
	return func(c *gin.Context) {
		me, _ := CurrentUser(c)
		list, err := s.ListVisibleChannels(c, me.ID)
		if err != nil {
			httpx.Fail(c, 500, "internal_error", "Could not list channels.")
			return
		}
		c.JSON(200, gin.H{"data": list})
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
		ch, err := s.CreateChannel(c, in.Kind, in.Slug, in.Name, in.Topic, me.ID, in.MemberIDs)
		if err != nil {
			httpx.Fail(c, 400, "invalid_channel", err.Error())
			return
		}
		c.JSON(201, gin.H{"channel": ch})
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
		c.JSON(200, gin.H{"channel": ch})
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
			Name  string `json:"name"`
			Topic string `json:"topic"`
		}
		if err := c.ShouldBindJSON(&in); err != nil {
			httpx.Fail(c, 400, "invalid_request", "Invalid request body.")
			return
		}
		if err := s.UpdateChannel(c, ch.ID, in.Name, in.Topic); err != nil {
			httpx.Fail(c, 400, "invalid_channel", err.Error())
			return
		}
		updated, err := s.GetChannel(c, ch.ID)
		if err != nil {
			httpx.Fail(c, 500, "internal_error", "Could not get updated channel.")
			return
		}
		c.JSON(200, gin.H{"channel": updated})
	}
}

func channelJoin(s *store.Store) gin.HandlerFunc {
	return func(c *gin.Context) {
		me, _ := CurrentUser(c)
		ch, _, ok := resolveChannel(c, s, me.ID)
		if !ok {
			return
		}
		if ch.Kind != "public" {
			httpx.Fail(c, 404, "channel_not_found", "Channel not found.")
			return
		}
		if err := s.AddMembers(c, ch.ID, []int64{me.ID}); err != nil {
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
		isMem, err := s.IsMember(c, ch.ID, me.ID)
		if err != nil {
			httpx.Fail(c, 500, "internal_error", "Could not check membership.")
			return
		}
		if !isMem {
			httpx.Fail(c, 400, "not_member", "You are not a member of this channel.")
			return
		}
		if err := s.RemoveMember(c, ch.ID, me.ID); err != nil {
			httpx.Fail(c, 400, "cannot_leave", err.Error())
			return
		}
		c.Status(204)
	}
}

func channelMembersList(s *store.Store) gin.HandlerFunc {
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
		members, err := s.ListMembers(c, ch.ID)
		if err != nil {
			httpx.Fail(c, 500, "internal_error", "Could not list members.")
			return
		}
		data := make([]gin.H, 0, len(members))
		for _, m := range members {
			data = append(data, publicUser(m))
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
			isMem, err := s.IsMember(c, ch.ID, me.ID)
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
		if err := s.AddMembers(c, ch.ID, in.UserIDs); err != nil {
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
		if err := s.RemoveMember(c, ch.ID, uid); err != nil {
			httpx.Fail(c, 400, "cannot_remove", err.Error())
			return
		}
		c.Status(204)
	}
}
