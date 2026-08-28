package api

import (
	"database/sql"
	"errors"
	"strconv"

	"github.com/0funct0ry/hivemind/internal/api/httpx"
	"github.com/0funct0ry/hivemind/internal/realtime"
	"github.com/0funct0ry/hivemind/internal/store"
	"github.com/gin-gonic/gin"
)

const maxDMRequestParticipants = 8

func publicDM(c store.ChannelDetails, h *realtime.Hub) gin.H {
	var peerVal any = nil
	if c.Peer != nil {
		peerVal = publicUserOnline(*c.Peer, h)
	}

	members := make([]gin.H, 0, len(c.Members))
	for _, m := range c.Members {
		members = append(members, publicUserOnline(m, h))
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
		"members":              members,
	}
	if c.CreatedBy != nil {
		res["created_by"] = strconv.FormatInt(*c.CreatedBy, 10)
	}
	if c.LastMessageID != nil {
		res["last_message_id"] = strconv.FormatInt(*c.LastMessageID, 10)
	}
	return res
}

func dmList(s *store.Store, h *realtime.Hub) gin.HandlerFunc {
	return func(c *gin.Context) {
		me, _ := CurrentUser(c)

		if c.Query("recent") != "" {
			partners, err := s.RecentDMPartners(c.Request.Context(), me.ID, 20)
			if err != nil {
				httpx.Fail(c, 500, "internal_error", "Could not list recent DM partners.")
				return
			}
			data := make([]gin.H, 0, len(partners))
			for _, u := range partners {
				data = append(data, publicUserOnline(u, h))
			}
			c.JSON(200, gin.H{"data": data})
			return
		}

		list, err := s.ListDMs(c.Request.Context(), me.ID)
		if err != nil {
			httpx.Fail(c, 500, "internal_error", "Could not list DMs.")
			return
		}

		data := make([]gin.H, 0, len(list))
		for _, d := range list {
			data = append(data, publicDM(d, h))
		}
		c.JSON(200, gin.H{"data": data})
	}
}

func dmCreate(s *store.Store, h *realtime.Hub) gin.HandlerFunc {
	return func(c *gin.Context) {
		me, _ := CurrentUser(c)
		var in struct {
			UserIDs []any `json:"user_ids"`
		}
		if err := c.ShouldBindJSON(&in); err != nil {
			httpx.Fail(c, 400, "invalid_request", "Invalid request body.")
			return
		}

		if len(in.UserIDs) == 0 {
			httpx.FailField(c, 400, "invalid_participants", "At least one recipient is required.", "user_ids")
			return
		}
		if len(in.UserIDs) > maxDMRequestParticipants {
			httpx.FailField(c, 400, "too_many_participants", "A conversation can have at most 8 participants.", "user_ids")
			return
		}

		targetIDs := make([]int64, 0, len(in.UserIDs))
		for _, v := range in.UserIDs {
			id, err := coerceUserID(v)
			if err != nil {
				httpx.FailField(c, 400, "invalid_participants", "Invalid user ID format.", "user_ids")
				return
			}
			targetIDs = append(targetIDs, id)
		}
		if len(store.DedupeInt64(targetIDs)) != len(targetIDs) {
			httpx.FailField(c, 400, "invalid_participants", "Duplicate recipients are not allowed.", "user_ids")
			return
		}

		// The caller is always a participant, whether or not they included their own ID.
		ids := append([]int64{me.ID}, targetIDs...)
		unique := store.DedupeInt64(ids)
		if len(unique) > maxDMRequestParticipants {
			httpx.FailField(c, 400, "too_many_participants", "A conversation can have at most 8 participants.", "user_ids")
			return
		}

		for _, id := range unique {
			if id == me.ID {
				continue
			}
			if _, err := s.GetUserByID(c.Request.Context(), id); err != nil {
				if errors.Is(err, store.ErrNotFound) || errors.Is(err, sql.ErrNoRows) {
					httpx.Fail(c, 404, "user_not_found", "User not found.")
					return
				}
				httpx.Fail(c, 500, "internal_error", "Could not retrieve user.")
				return
			}
		}

		var ch store.ChannelDetails
		var err error
		if len(unique) <= 2 {
			a, b := unique[0], unique[0]
			if len(unique) == 2 {
				b = unique[1]
			}
			ch, err = s.GetOrCreateDM(c.Request.Context(), a, b)
		} else {
			ch, err = s.GetOrCreateGroupDM(c.Request.Context(), unique)
		}
		if err != nil {
			if errors.Is(err, store.ErrTooManyParticipants) {
				httpx.FailField(c, 400, "too_many_participants", "A conversation can have at most 8 participants.", "user_ids")
				return
			}
			httpx.Fail(c, 500, "internal_error", "Could not create or retrieve DM.")
			return
		}

		c.JSON(200, gin.H{"channel": publicDM(ch, h)})
	}
}

// dmHide removes a DM/group DM from the caller's own sidebar (HideConversation) without
// touching membership or messages — it reappears for the caller the moment new activity
// arrives. A channel id that isn't a dm/group_dm, or that the caller isn't a participant
// of, is reported as 404 like every other DM resource — never 403, so existence isn't
// confirmed to non-participants.
func dmHide(s *store.Store) gin.HandlerFunc {
	return func(c *gin.Context) {
		me, _ := CurrentUser(c)
		ch, _, ok := resolveChannel(c, s, me.ID)
		if !ok {
			return
		}
		if ch.Kind != "dm" && ch.Kind != "group_dm" {
			httpx.Fail(c, 404, "channel_not_found", "Channel not found.")
			return
		}
		if err := s.HideConversation(c.Request.Context(), me.ID, ch.ID); err != nil {
			if errors.Is(err, store.ErrNotFound) {
				httpx.Fail(c, 404, "channel_not_found", "Channel not found.")
				return
			}
			httpx.Fail(c, 500, "internal_error", "Could not remove conversation.")
			return
		}
		c.Status(204)
	}
}

// coerceUserID accepts the same string/number JSON shapes the pre-M18 single-peer endpoint did.
func coerceUserID(v any) (int64, error) {
	switch t := v.(type) {
	case string:
		return strconv.ParseInt(t, 10, 64)
	case float64:
		return int64(t), nil
	case int64:
		return t, nil
	default:
		return 0, errors.New("unsupported user id type")
	}
}
