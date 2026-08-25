package realtime

import (
	"context"
	"encoding/json"
	"log/slog"
	"strconv"
	"sync"
	"time"

	"github.com/0funct0ry/hivemind/internal/store"
)

// Command represents a client-initiated WebSocket command.
type Command struct {
	Conn    *Conn
	Type    string
	Payload json.RawMessage
	Ref     string
}

// Event represents an internal server event to be published to WebSocket clients.
type Event struct {
	Type        string
	Payload     any
	ChannelID   int64
	UserID      int64
	ExcludeUser int64
}

// Publisher is the interface exposed to other packages to publish events.
type Publisher interface {
	Publish(ev Event)
}

// Hub manages active WebSocket connections and channels.
type Hub struct {
	store      *store.Store
	inbox      chan Event
	register   chan *Conn
	unregister chan *Conn
	commands   chan Command
	conns      map[int64][]*Conn            // UserID -> Connections
	members    map[int64]map[int64]struct{} // ChannelID -> UserID Set
	mu         sync.RWMutex
}

// NewHub creates a new Hub.
func NewHub(s *store.Store) *Hub {
	return &Hub{
		store:      s,
		inbox:      make(chan Event, 4096),
		register:   make(chan *Conn),
		unregister: make(chan *Conn),
		commands:   make(chan Command, 1024),
		conns:      make(map[int64][]*Conn),
		members:    make(map[int64]map[int64]struct{}),
	}
}

// Publish sends an event to the Hub inbox. If inbox is full, it drops the event and logs an error.
func (h *Hub) Publish(ev Event) {
	select {
	case h.inbox <- ev:
	default:
		slog.Error("realtime hub inbox full, dropping event", "type", ev.Type)
	}
}

// Run executes the main Hub event loop.
func (h *Hub) Run(ctx context.Context) {
	for {
		select {
		case conn := <-h.register:
			h.handleRegister(ctx, conn)

		case conn := <-h.unregister:
			h.handleUnregister(conn)

		case ev := <-h.inbox:
			h.handlePublish(ctx, ev)

		case cmd := <-h.commands:
			h.handleCommand(ctx, cmd)

		case <-ctx.Done():
			// Close all active connections
			h.mu.Lock()
			for _, userConns := range h.conns {
				for _, conn := range userConns {
					conn.Close()
				}
			}
			h.mu.Unlock()
			return
		}
	}
}

func (h *Hub) handleRegister(ctx context.Context, conn *Conn) {
	h.mu.Lock()
	userConns := h.conns[conn.User.ID]

	// Enforce 8 concurrent connections per user (close oldest)
	if len(userConns) >= 8 {
		oldest := userConns[0]
		oldest.CloseWithCode(1013, "Try Again Later")
		userConns = userConns[1:]
	}

	h.conns[conn.User.ID] = append(userConns, conn)
	h.mu.Unlock()

	// Fetch visible channels for the hello payload
	channels, err := h.store.ListVisibleChannels(ctx, conn.User.ID)
	if err != nil {
		slog.Error("failed to list visible channels for hello payload", "user_id", conn.User.ID, "error", err)
	}

	// Prepare user payload matching router's publicUser schema
	userPayload := UserPayload{
		ID:          strconv.FormatInt(conn.User.ID, 10),
		Username:    conn.User.Username,
		Email:       conn.User.Email,
		DisplayName: conn.User.DisplayName,
		AvatarColor: conn.User.AvatarColor,
		Role:        conn.User.Role,
		IsBot:       conn.User.IsBot,
		Status:      conn.User.Status,
	}

	// Cast slice of ChannelDetails to any slice for JSON marshalling
	anyChannels := make([]any, len(channels))
	for i, c := range channels {
		anyChannels[i] = c
	}

	helloPayload := HelloPayload{
		User:              userPayload,
		Channels:          anyChannels,
		UnreadSummary:     map[string]any{}, // Placeholder for M11
		ServerTime:        time.Now().UnixMilli(),
		HeartbeatInterval: int(pingPeriod / time.Millisecond),
	}

	payloadBytes, _ := json.Marshal(helloPayload)
	conn.Send(Frame{
		V:       1,
		Type:    "hello",
		TS:      time.Now().UnixMilli(),
		Payload: payloadBytes,
	})
}

func (h *Hub) handleUnregister(conn *Conn) {
	h.mu.Lock()
	defer h.mu.Unlock()

	userConns := h.conns[conn.User.ID]
	for i, c := range userConns {
		if c == conn {
			h.conns[conn.User.ID] = append(userConns[:i], userConns[i+1:]...)
			break
		}
	}
	if len(h.conns[conn.User.ID]) == 0 {
		delete(h.conns, conn.User.ID)
	}
}

func (h *Hub) handlePublish(ctx context.Context, ev Event) {
	// Invalidate membership cache on member changes or channel archive/deletes
	if ev.Type == "member.joined" || ev.Type == "member.left" || ev.Type == "channel.created" || ev.Type == "channel.updated" {
		h.mu.Lock()
		if ev.ChannelID != 0 {
			delete(h.members, ev.ChannelID)
		}
		h.mu.Unlock()
	}

	payloadBytes, err := json.Marshal(ev.Payload)
	if err != nil {
		slog.Error("failed to marshal event payload", "type", ev.Type, "error", err)
		return
	}

	frame := Frame{
		V:       1,
		Type:    ev.Type,
		TS:      time.Now().UnixMilli(),
		Payload: payloadBytes,
	}

	h.mu.RLock()
	defer h.mu.RUnlock()

	// Audience Resolution
	if ev.UserID != 0 {
		// Specific user target
		if conns, ok := h.conns[ev.UserID]; ok {
			for _, conn := range conns {
				if ev.ExcludeUser != 0 && conn.User.ID == ev.ExcludeUser {
					continue
				}
				conn.Send(frame)
			}
		}
		return
	}

	if ev.ChannelID != 0 {
		// Channel members target
		h.mu.RUnlock()
		members := h.getChannelMembers(ctx, ev.ChannelID)
		h.mu.RLock()

		for memberID := range members {
			if ev.ExcludeUser != 0 && memberID == ev.ExcludeUser {
				continue
			}
			if conns, ok := h.conns[memberID]; ok {
				for _, conn := range conns {
					conn.Send(frame)
				}
			}
		}
		return
	}

	// Global broadcast (all connections)
	for _, userConns := range h.conns {
		for _, conn := range userConns {
			if ev.ExcludeUser != 0 && conn.User.ID == ev.ExcludeUser {
				continue
			}
			conn.Send(frame)
		}
	}
}

func (h *Hub) handleCommand(ctx context.Context, cmd Command) {
	switch cmd.Type {
	case "typing":
		var p TypingPayload
		if err := json.Unmarshal(cmd.Payload, &p); err != nil {
			return
		}
		chID, err := strconv.ParseInt(p.ChannelID, 10, 64)
		if err != nil {
			return
		}

		members := h.getChannelMembers(ctx, chID)
		if _, ok := members[cmd.Conn.User.ID]; !ok {
			// Sender not in channel, ignore
			return
		}

		// Fan out typing event to all other members of the channel
		typingPayload := TypingPayload{
			ChannelID: p.ChannelID,
			UserID:    strconv.FormatInt(cmd.Conn.User.ID, 10),
			ExpiresAt: time.Now().UnixMilli() + 5000,
		}

		h.Publish(Event{
			Type:        "typing",
			Payload:     typingPayload,
			ChannelID:   chID,
			ExcludeUser: cmd.Conn.User.ID,
		})

	case "resume":
		var p ResumePayload
		if err := json.Unmarshal(cmd.Payload, &p); err != nil {
			cmd.Conn.sendError("invalid_payload", "Invalid resume payload.", cmd.Ref)
			return
		}

		gaps := make(map[string]int64)
		for chIDStr, cursorStr := range p.Cursors {
			chID, err := strconv.ParseInt(chIDStr, 10, 64)
			if err != nil {
				continue
			}
			cursor, err := strconv.ParseInt(cursorStr, 10, 64)
			if err != nil {
				continue
			}

			// Validate channel membership/access
			access, err := h.store.CanAccessChannel(ctx, cmd.Conn.User.ID, chID)
			if err != nil || !access.CanRead {
				continue
			}

			count, err := h.store.CountMessagesSince(ctx, chID, cursor)
			if err != nil {
				continue
			}
			gaps[chIDStr] = count
		}

		respBytes, _ := json.Marshal(ResumeOkPayload{Gaps: gaps})
		cmd.Conn.Send(Frame{
			V:       1,
			Type:    "resume.ok",
			TS:      time.Now().UnixMilli(),
			Payload: respBytes,
			Ref:     cmd.Ref,
		})

	case "read":
		var p ReadPayload
		if err := json.Unmarshal(cmd.Payload, &p); err != nil {
			cmd.Conn.sendError("invalid_payload", "Invalid read payload.", cmd.Ref)
			return
		}
		chID, err := strconv.ParseInt(p.ChannelID, 10, 64)
		if err != nil {
			cmd.Conn.sendError("invalid_channel_id", "Invalid channel ID.", cmd.Ref)
			return
		}
		msgID, err := strconv.ParseInt(p.MessageID, 10, 64)
		if err != nil {
			cmd.Conn.sendError("invalid_message_id", "Invalid message ID.", cmd.Ref)
			return
		}

		// Verify membership/access
		access, err := h.store.CanAccessChannel(ctx, cmd.Conn.User.ID, chID)
		if err != nil || !access.CanRead {
			cmd.Conn.sendError("forbidden", "Access denied.", cmd.Ref)
			return
		}

		// Update watermark in database
		if err := h.store.MarkRead(ctx, cmd.Conn.User.ID, chID, msgID); err != nil {
			cmd.Conn.sendError("internal_error", "Failed to update read status.", cmd.Ref)
			return
		}

		// Publish read.updated to the same user's other sessions
		readPayload := map[string]string{
			"channel_id":           p.ChannelID,
			"last_read_message_id": p.MessageID,
		}
		h.Publish(Event{
			Type:        "read.updated",
			Payload:     readPayload,
			UserID:      cmd.Conn.User.ID,
			ExcludeUser: cmd.Conn.User.ID,
		})
	}
}

func (h *Hub) getChannelMembers(ctx context.Context, channelID int64) map[int64]struct{} {
	h.mu.RLock()
	cached, ok := h.members[channelID]
	h.mu.RUnlock()

	if ok {
		return cached
	}

	// Cache miss: query DB
	ids, err := h.store.GetChannelMemberIDs(ctx, channelID)
	if err != nil {
		slog.Error("failed to get channel members", "channel_id", channelID, "error", err)
		return nil
	}

	set := make(map[int64]struct{})
	for _, id := range ids {
		set[id] = struct{}{}
	}

	h.mu.Lock()
	h.members[channelID] = set
	h.mu.Unlock()

	return set
}
