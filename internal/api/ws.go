package api

import (
	"log/slog"
	"net/http"
	"net/url"
	"strings"

	"github.com/0funct0ry/hivemind/internal/config"
	"github.com/0funct0ry/hivemind/internal/realtime"
	"github.com/0funct0ry/hivemind/internal/store"
	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
)

func wsUpgrade(hub *realtime.Hub, s *store.Store, cfg config.Config) gin.HandlerFunc {
	upgrader := websocket.Upgrader{
		ReadBufferSize:  4096,
		WriteBufferSize: 4096,
		CheckOrigin: func(r *http.Request) bool {
			origin := r.Header.Get("Origin")
			if origin == "" {
				return true
			}
			if cfg.BaseURL == "" {
				return true
			}
			u, err := url.Parse(origin)
			if err != nil {
				return false
			}
			base, err := url.Parse(cfg.BaseURL)
			if err != nil {
				return false
			}
			return strings.EqualFold(u.Host, base.Host)
		},
	}

	return func(c *gin.Context) {
		u, ok := CurrentUser(c)
		if !ok {
			c.JSON(401, gin.H{"error": gin.H{"code": "unauthenticated", "message": "Authentication required."}})
			return
		}

		ws, err := upgrader.Upgrade(c.Writer, c.Request, nil)
		if err != nil {
			slog.Error("websocket upgrade failed", "error", err)
			return
		}

		conn := realtime.NewConn(u, ws)
		go conn.Run(hub)
	}
}
