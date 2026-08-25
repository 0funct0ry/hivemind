package api

import (
	"strconv"
	"time"

	"github.com/0funct0ry/hivemind/internal/api/httpx"
	"github.com/0funct0ry/hivemind/internal/store"
	"github.com/gin-gonic/gin"
)

// channelActivity handles GET /channels/:id/activity
func channelActivity(s *store.Store) gin.HandlerFunc {
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

		fromStr := c.Query("from")
		toStr := c.Query("to")
		bucketsStr := c.Query("buckets")

		now := time.Now().UnixMilli()
		// Defaults: window is 24 hours back from now.
		from := now - 24*time.Hour.Milliseconds()
		to := now
		var buckets int64 = 48

		if fromStr != "" {
			if f, err := strconv.ParseInt(fromStr, 10, 64); err == nil {
				from = f
			}
		}
		if toStr != "" {
			if t, err := strconv.ParseInt(toStr, 10, 64); err == nil {
				to = t
			}
		}
		if bucketsStr != "" {
			if b, err := strconv.ParseInt(bucketsStr, 10, 64); err == nil {
				buckets = b
			}
		}

		// Clamp buckets to 12..240 and window to 90 days.
		if buckets < 12 {
			buckets = 12
		} else if buckets > 240 {
			buckets = 240
		}

		maxWindow := 90 * 24 * time.Hour.Milliseconds()
		if to-from > maxWindow {
			from = to - maxWindow
		}

		activity, err := s.GetChannelActivity(c.Request.Context(), me.ID, ch.ID, from, to, buckets)
		if err != nil {
			httpx.Fail(c, 500, "internal_error", err.Error())
			return
		}

		c.JSON(200, activity)
	}
}
