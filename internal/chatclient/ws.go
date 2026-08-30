package chatclient

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"math/rand"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

// Frame mirrors internal/realtime.Frame — the {v, type, ts, payload, ref} envelope used on
// both directions of the WebSocket (SPEC.md §5.1).
type Frame struct {
	V       int             `json:"v"`
	Type    string          `json:"type"`
	TS      int64           `json:"ts"`
	Payload json.RawMessage `json:"payload"`
	Ref     string          `json:"ref,omitempty"`
}

// Event is a server-pushed frame surfaced to the caller. "ping" frames are intercepted and
// answered internally — they never appear here.
type Event struct {
	Type    string
	TS      int64
	Payload json.RawMessage
}

// Conn is a single WebSocket connection to the server, implementing the client half of
// SPEC.md §5.2's lifecycle: it reads hello on connect, auto-replies to the server's
// JSON-level ping/pong heartbeat (NOT a WS control frame — see conn.go's writeLoop on the
// server side), and exposes a synchronous Resume call plus a channel of further events.
type Conn struct {
	ws *websocket.Conn

	mu      sync.Mutex
	pending map[string]chan ResumeOkPayload
	refSeq  int64

	events chan Event
	done   chan struct{}
}

// ResumeOkPayload mirrors internal/realtime.ResumeOkPayload.
type ResumeOkPayload struct {
	Gaps map[string]int64 `json:"gaps"`
}

// Dial upgrades to the WebSocket endpoint using a bearer token and reads the initial hello
// frame, discarding it (the caller re-derives channel state over REST) — its only purpose
// here is to confirm the handshake completed.
func Dial(ctx context.Context, wsURL, token string, insecure bool) (*Conn, error) {
	dialer := *websocket.DefaultDialer
	if insecure {
		dialer.TLSClientConfig = &tls.Config{InsecureSkipVerify: true} //nolint:gosec // explicit --insecure opt-in
	}
	header := http.Header{}
	header.Set("Authorization", "Bearer "+token)
	ws, _, err := dialer.DialContext(ctx, wsURL, header)
	if err != nil {
		return nil, fmt.Errorf("dial websocket: %w", err)
	}
	c := &Conn{
		ws:      ws,
		pending: make(map[string]chan ResumeOkPayload),
		events:  make(chan Event, 64),
		done:    make(chan struct{}),
	}
	var hello Frame
	if err := ws.ReadJSON(&hello); err != nil {
		ws.Close()
		return nil, fmt.Errorf("read hello: %w", err)
	}
	go c.readLoop()
	return c, nil
}

func (c *Conn) readLoop() {
	defer close(c.done)
	defer close(c.events)
	for {
		var f Frame
		if err := c.ws.ReadJSON(&f); err != nil {
			return
		}
		switch f.Type {
		case "ping":
			_ = c.ws.WriteJSON(Frame{V: 1, Type: "pong", TS: time.Now().UnixMilli()})
		case "resume.ok":
			var payload ResumeOkPayload
			_ = json.Unmarshal(f.Payload, &payload)
			c.mu.Lock()
			ch, ok := c.pending[f.Ref]
			if ok {
				delete(c.pending, f.Ref)
			}
			c.mu.Unlock()
			if ok {
				ch <- payload
			}
		default:
			c.events <- Event{Type: f.Type, TS: f.TS, Payload: f.Payload}
		}
	}
}

// Events returns the channel of server-pushed events (excluding ping/pong/resume.ok, which
// are handled internally). It closes when the connection drops.
func (c *Conn) Events() <-chan Event { return c.events }

// Done closes when the underlying connection's read loop exits (server closed it, or a
// network error occurred).
func (c *Conn) Done() <-chan struct{} { return c.done }

// Resume sends a resume command with the given per-channel cursors and blocks for the
// matching resume.ok, returning the reported gap size per channel (SPEC.md §5.2/§5.4).
func (c *Conn) Resume(ctx context.Context, cursors map[string]string) (map[string]int64, error) {
	c.mu.Lock()
	c.refSeq++
	ref := strconv.FormatInt(c.refSeq, 10)
	ch := make(chan ResumeOkPayload, 1)
	c.pending[ref] = ch
	c.mu.Unlock()

	payload, err := json.Marshal(map[string]any{"cursors": cursors})
	if err != nil {
		return nil, fmt.Errorf("encode resume: %w", err)
	}
	if err := c.ws.WriteJSON(Frame{V: 1, Type: "resume", TS: time.Now().UnixMilli(), Payload: payload, Ref: ref}); err != nil {
		return nil, fmt.Errorf("send resume: %w", err)
	}
	select {
	case res := <-ch:
		return res.Gaps, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-c.done:
		return nil, fmt.Errorf("connection closed while waiting for resume.ok")
	}
}

// Close closes the underlying connection.
func (c *Conn) Close() error { return c.ws.Close() }

// ManagedConn wraps Conn with automatic reconnect (exponential backoff 1s..30s + jitter,
// SPEC.md §5.2) and re-resume against the last-known cursors after every reconnect. Callers
// read from Events() exactly as with a bare Conn; a synthetic "_reconnected" event marks each
// successful reconnect so chattui knows to trigger a backfill.
type ManagedConn struct {
	wsURL, token string
	insecure     bool

	mu      sync.Mutex
	cursors map[string]string

	events chan Event
}

// NewManagedConn constructs a ManagedConn. Call Run to start connecting.
func NewManagedConn(wsURL, token string, insecure bool) *ManagedConn {
	return &ManagedConn{wsURL: wsURL, token: token, insecure: insecure, cursors: map[string]string{}, events: make(chan Event, 64)}
}

// Events returns the merged event stream across reconnects, including synthetic
// "_reconnected" events.
func (m *ManagedConn) Events() <-chan Event { return m.events }

// SetCursor records the highest-seen message id for a channel, used to resume after a
// reconnect.
func (m *ManagedConn) SetCursor(channelID, messageID string) {
	m.mu.Lock()
	m.cursors[channelID] = messageID
	m.mu.Unlock()
}

func (m *ManagedConn) snapshotCursors() map[string]string {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make(map[string]string, len(m.cursors))
	for k, v := range m.cursors {
		out[k] = v
	}
	return out
}

// Run connects and reconnects until ctx is cancelled, forwarding events and emitting a
// synthetic {Type: "_reconnected", Payload: {"gaps": {...}}} event after each successful
// resume so the caller can backfill.
func (m *ManagedConn) Run(ctx context.Context) {
	defer close(m.events)
	backoff := time.Second
	const maxBackoff = 30 * time.Second
	for {
		if ctx.Err() != nil {
			return
		}
		if m.token == "" {
			// Logged out (SPEC.md §7.7's .logout) — nothing to authenticate with. Idle
			// rather than hammering the server with unauthenticated dial attempts; the
			// caller builds a fresh ManagedConn once .login sets a new token.
			if !sleepOrDone(ctx, time.Second) {
				return
			}
			continue
		}
		conn, err := Dial(ctx, m.wsURL, m.token, m.insecure)
		if err != nil {
			if !sleepOrDone(ctx, backoff) {
				return
			}
			backoff = nextBackoff(backoff, maxBackoff)
			continue
		}
		backoff = time.Second

		cursors := m.snapshotCursors()
		if len(cursors) > 0 {
			gaps, err := conn.Resume(ctx, cursors)
			if err == nil {
				payload, _ := json.Marshal(map[string]any{"gaps": gaps})
				select {
				case m.events <- Event{Type: "_reconnected", TS: time.Now().UnixMilli(), Payload: payload}:
				case <-ctx.Done():
					conn.Close()
					return
				}
			}
		}

		m.pump(ctx, conn)
		conn.Close()
		if ctx.Err() != nil {
			return
		}
		if !sleepOrDone(ctx, backoff) {
			return
		}
		backoff = nextBackoff(backoff, maxBackoff)
	}
}

func (m *ManagedConn) pump(ctx context.Context, conn *Conn) {
	for {
		select {
		case ev, ok := <-conn.Events():
			if !ok {
				return
			}
			select {
			case m.events <- ev:
			case <-ctx.Done():
				return
			}
		case <-ctx.Done():
			return
		}
	}
}

func nextBackoff(cur, max time.Duration) time.Duration {
	next := cur * 2
	if next > max {
		next = max
	}
	jitter := time.Duration(rand.Int63n(int64(next) / 5))
	return next + jitter
}

func sleepOrDone(ctx context.Context, d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-t.C:
		return true
	case <-ctx.Done():
		return false
	}
}

// WSURLFromBase converts an http(s) base URL into the ws(s) URL for the /ws endpoint.
func WSURLFromBase(baseURL string) string {
	scheme := "ws"
	rest := baseURL
	switch {
	case strings.HasPrefix(baseURL, "https://"):
		scheme = "wss"
		rest = strings.TrimPrefix(baseURL, "https://")
	case strings.HasPrefix(baseURL, "http://"):
		rest = strings.TrimPrefix(baseURL, "http://")
	}
	return scheme + "://" + rest + "/api/v1/ws"
}
