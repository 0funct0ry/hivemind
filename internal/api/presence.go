package api

import (
	"strconv"

	"github.com/0funct0ry/hivemind/internal/realtime"
	"github.com/gin-gonic/gin"
)

// presence reports which users currently hold at least one open WebSocket connection,
// derived from the existing Hub connection registry — no separate presence subsystem.
func presence(h *realtime.Hub) gin.HandlerFunc {
	return func(c *gin.Context) {
		ids := h.OnlineUserIDs()
		online := make([]string, 0, len(ids))
		for _, id := range ids {
			online = append(online, strconv.FormatInt(id, 10))
		}
		c.JSON(200, gin.H{"online": online})
	}
}
