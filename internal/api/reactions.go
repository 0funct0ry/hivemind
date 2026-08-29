package api

import (
	"errors"
	"strconv"

	"github.com/0funct0ry/hivemind/internal/api/httpx"
	"github.com/0funct0ry/hivemind/internal/realtime"
	"github.com/0funct0ry/hivemind/internal/store"
	"github.com/gin-gonic/gin"
)

// isSingleEmojiGrapheme approximates "one visible emoji character" without a full Unicode
// grapheme-segmentation dependency (none is in the approved module list, SPEC.md §2.5). A plain
// utf8.RuneCountInString == 1 check is wrong for the majority of real emoji — "❤️", one of the
// six fixed quick-reactions itself, is two runes (U+2764 + the U+FE0F variation selector). This
// walks the rune sequence and only counts a rune as a new grapheme when it isn't a variation
// selector, skin-tone modifier, ZWJ, the rune immediately following a ZWJ (family/profession
// sequences), or the second half of a two-rune regional-indicator flag pair.
func isSingleEmojiGrapheme(s string) bool {
	if s == "" {
		return false
	}
	runes := []rune(s)
	count := 0
	joined := false
	prevRegional := false
	for _, r := range runes {
		switch {
		case r == 0xFE0E || r == 0xFE0F: // variation selectors
			continue
		case r >= 0x1F3FB && r <= 0x1F3FF: // skin tone modifiers
			continue
		case r == 0x200D: // zero-width joiner: the next rune joins this grapheme
			joined = true
			continue
		case joined:
			joined = false
			continue
		case r >= 0x1F1E6 && r <= 0x1F1FF: // regional indicator (flag half)
			if prevRegional {
				prevRegional = false
				continue
			}
			prevRegional = true
			count++
		default:
			prevRegional = false
			count++
		}
	}
	return count == 1
}

// publicReactions returns the current message's reactions array in the shape §4.3 documents,
// for reaction endpoints to return without going through publicMessage's full payload.
func publicReactions(reactions []store.Reaction) []gin.H {
	out := make([]gin.H, 0, len(reactions))
	for _, r := range reactions {
		userIDs := make([]string, 0, len(r.UserIDs))
		for _, uid := range r.UserIDs {
			userIDs = append(userIDs, strconv.FormatInt(uid, 10))
		}
		out = append(out, gin.H{"emoji": r.Emoji, "user_ids": userIDs})
	}
	return out
}

func publishReactionChanged(pub realtime.Publisher, msg store.Message, emoji string, userID int64, action string) {
	var threadIDVal any = nil
	if msg.ThreadID != nil {
		threadIDVal = strconv.FormatInt(*msg.ThreadID, 10)
	}
	pub.Publish(realtime.Event{
		Type: "reaction.changed",
		Payload: gin.H{
			"message_id": strconv.FormatInt(msg.ID, 10),
			"channel_id": strconv.FormatInt(msg.ChannelID, 10),
			"thread_id":  threadIDVal,
			"emoji":      emoji,
			"user_id":    strconv.FormatInt(userID, 10),
			"action":     action,
		},
		ChannelID: msg.ChannelID,
	})
}

func reactionAdd(s *store.Store, pub realtime.Publisher) gin.HandlerFunc {
	return func(c *gin.Context) {
		me, _ := CurrentUser(c)

		msg, ok := loadMessageForMutation(c, s, me.ID)
		if !ok {
			return
		}

		var in struct {
			Emoji string `json:"emoji"`
		}
		if err := c.ShouldBindJSON(&in); err != nil {
			httpx.Fail(c, 400, "invalid_emoji", "Invalid request body.")
			return
		}
		if !isSingleEmojiGrapheme(in.Emoji) {
			httpx.Fail(c, 400, "invalid_emoji", "Emoji must be a single character.")
			return
		}

		if err := s.AddReaction(c.Request.Context(), msg.ID, me.ID, in.Emoji); err != nil {
			if errors.Is(err, store.ErrNotFound) {
				httpx.Fail(c, 404, "message_not_found", "Message not found.")
				return
			}
			httpx.Fail(c, 500, "internal_error", "Could not add reaction.")
			return
		}

		reactions, err := s.GetReactions(c.Request.Context(), msg.ID)
		if err != nil {
			httpx.Fail(c, 500, "internal_error", "Could not fetch reactions.")
			return
		}

		c.JSON(200, gin.H{"reactions": publicReactions(reactions)})
		publishReactionChanged(pub, msg, in.Emoji, me.ID, "added")
	}
}

func reactionRemove(s *store.Store, pub realtime.Publisher) gin.HandlerFunc {
	return func(c *gin.Context) {
		me, _ := CurrentUser(c)

		msg, ok := loadMessageForMutation(c, s, me.ID)
		if !ok {
			return
		}

		emoji := c.Param("emoji")

		if err := s.RemoveReaction(c.Request.Context(), msg.ID, me.ID, emoji); err != nil {
			httpx.Fail(c, 500, "internal_error", "Could not remove reaction.")
			return
		}

		reactions, err := s.GetReactions(c.Request.Context(), msg.ID)
		if err != nil {
			httpx.Fail(c, 500, "internal_error", "Could not fetch reactions.")
			return
		}

		c.JSON(200, gin.H{"reactions": publicReactions(reactions)})
		publishReactionChanged(pub, msg, emoji, me.ID, "removed")
	}
}
