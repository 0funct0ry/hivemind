package realtime

import (
	"encoding/json"
	"log/slog"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/0funct0ry/hivemind/internal/store"
	"github.com/gorilla/websocket"
)

const (
	writeWait      = 10 * time.Second
	pongWait       = 60 * time.Second
	pingPeriod     = 25 * time.Second
	maxMessageSize = 4096
)

// Conn represents an active WebSocket connection.
type Conn struct {
	User       store.User
	ws         *websocket.Conn
	send       chan Frame
	done       chan struct{}
	closeOnce  sync.Once
	lastTyping map[int64]time.Time
	mu         sync.Mutex
}

// NewConn creates a new Conn.
func NewConn(u store.User, ws *websocket.Conn) *Conn {
	return &Conn{
		User:       u,
		ws:         ws,
		send:       make(chan Frame, 256),
		done:       make(chan struct{}),
		lastTyping: make(map[int64]time.Time),
	}
}

// Send queues a frame to be written. Returns false if the send buffer is full.
func (c *Conn) Send(f Frame) bool {
	select {
	case c.send <- f:
		return true
	default:
		slog.Warn("send buffer overflow, closing connection", "user_id", c.User.ID)
		c.CloseWithCode(1013, "Try Again Later")
		return false
	}
}

// Close closes the connection.
func (c *Conn) Close() {
	c.closeOnce.Do(func() {
		_ = c.ws.Close()
		close(c.done)
	})
}

// CloseWithCode closes the connection with a specific WS close code.
func (c *Conn) CloseWithCode(code int, reason string) {
	c.closeOnce.Do(func() {
		msg := websocket.FormatCloseMessage(code, reason)
		_ = c.ws.WriteControl(websocket.CloseMessage, msg, time.Now().Add(writeWait))
		_ = c.ws.Close()
		close(c.done)
	})
}

// Run starts the read and write loops.
func (c *Conn) Run(hub *Hub) {
	// Register connection
	hub.register <- c

	// Start writer in background
	go c.writeLoop()

	// Run reader in foreground (blocks until connection closes)
	c.readLoop(hub)

	// Unregister connection when done
	hub.unregister <- c
	c.Close()
}

func (c *Conn) readLoop(hub *Hub) {
	c.ws.SetReadLimit(maxMessageSize)
	_ = c.ws.SetReadDeadline(time.Now().Add(pongWait))

	c.ws.SetPongHandler(func(string) error {
		_ = c.ws.SetReadDeadline(time.Now().Add(pongWait))
		return nil
	})

	for {
		_, payload, err := c.ws.ReadMessage()
		if err != nil {
			break
		}

		_ = c.ws.SetReadDeadline(time.Now().Add(pongWait))

		var f Frame
		if err := json.Unmarshal(payload, &f); err != nil {
			c.sendError("invalid_frame", "Failed to parse frame.", "")
			continue
		}

		c.handleFrame(hub, f)
	}
}

// TestWriteDelayMs is used to simulate a slow consumer during testing.
var TestWriteDelayMs atomic.Int64

func (c *Conn) writeLoop() {
	ticker := time.NewTicker(pingPeriod)
	defer ticker.Stop()

	for {
		select {
		case f, ok := <-c.send:
			if delay := TestWriteDelayMs.Load(); delay > 0 {
				time.Sleep(time.Duration(delay) * time.Millisecond)
			}
			_ = c.ws.SetWriteDeadline(time.Now().Add(writeWait))
			if !ok {
				_ = c.ws.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}

			w, err := c.ws.NextWriter(websocket.TextMessage)
			if err != nil {
				return
			}
			if err := json.NewEncoder(w).Encode(f); err != nil {
				return
			}
			if err := w.Close(); err != nil {
				return
			}

		case <-ticker.C:
			_ = c.ws.SetWriteDeadline(time.Now().Add(writeWait))
			pingFrame := Frame{
				V:    1,
				Type: "ping",
				TS:   time.Now().UnixMilli(),
			}
			w, err := c.ws.NextWriter(websocket.TextMessage)
			if err != nil {
				return
			}
			if err := json.NewEncoder(w).Encode(pingFrame); err != nil {
				return
			}
			if err := w.Close(); err != nil {
				return
			}

		case <-c.done:
			return
		}
	}
}

func (c *Conn) handleFrame(hub *Hub, f Frame) {
	switch f.Type {
	case "pong":
		_ = c.ws.SetReadDeadline(time.Now().Add(pongWait))

	case "typing":
		var p TypingPayload
		if err := json.Unmarshal(f.Payload, &p); err != nil {
			c.sendError("invalid_payload", "Invalid typing payload.", f.Ref)
			return
		}
		chID, err := strconv.ParseInt(p.ChannelID, 10, 64)
		if err != nil {
			c.sendError("invalid_channel_id", "Invalid channel ID.", f.Ref)
			return
		}

		c.mu.Lock()
		last, ok := c.lastTyping[chID]
		now := time.Now()
		if !ok || now.Sub(last) >= 3*time.Second {
			c.lastTyping[chID] = now
			c.mu.Unlock()

			hub.commands <- Command{
				Conn:    c,
				Type:    "typing",
				Payload: f.Payload,
				Ref:     f.Ref,
			}
		} else {
			c.mu.Unlock()
		}

	case "resume":
		hub.commands <- Command{
			Conn:    c,
			Type:    "resume",
			Payload: f.Payload,
			Ref:     f.Ref,
		}

	case "read":
		hub.commands <- Command{
			Conn:    c,
			Type:    "read",
			Payload: f.Payload,
			Ref:     f.Ref,
		}

	default:
		c.sendError("unknown_command", "Unknown command type.", f.Ref)
	}
}

func (c *Conn) sendError(code, msg, ref string) {
	payload, _ := json.Marshal(map[string]string{
		"code":    code,
		"message": msg,
	})
	c.Send(Frame{
		V:       1,
		Type:    "error",
		TS:      time.Now().UnixMilli(),
		Payload: payload,
		Ref:     ref,
	})
}
