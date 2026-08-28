package realtime

import (
	"encoding/json"
)

// Frame represents the standard WebSocket message envelope.
type Frame struct {
	V       int             `json:"v"`
	Type    string          `json:"type"`
	TS      int64           `json:"ts"`
	Payload json.RawMessage `json:"payload"`
	Ref     string          `json:"ref,omitempty"`
}

// UserPayload represents the JSON payload structure for user details.
type UserPayload struct {
	ID          string `json:"id"`
	Username    string `json:"username"`
	Email       string `json:"email"`
	DisplayName string `json:"display_name"`
	AvatarColor string `json:"avatar_color"`
	Role        string `json:"role"`
	IsBot       bool   `json:"is_bot"`
	Status      string `json:"status"`
}

// HelloPayload is sent to the client immediately after connection.
type HelloPayload struct {
	User              UserPayload `json:"user"`
	Channels          []any       `json:"channels"`
	UnreadSummary     any         `json:"unread_summary"`
	ServerTime        int64       `json:"server_time"`
	HeartbeatInterval int         `json:"heartbeat_ms"`
}

// ResumePayload represents the incoming resume command.
type ResumePayload struct {
	Cursors map[string]string `json:"cursors"`
}

// ResumeOkPayload represents the response to a successful resume command.
type ResumeOkPayload struct {
	Gaps map[string]int64 `json:"gaps"`
}

// TypingPayload represents the typing notification sent to/from clients.
type TypingPayload struct {
	ChannelID string `json:"channel_id"`
	UserID    string `json:"user_id,omitempty"`
	ExpiresAt int64  `json:"expires_at,omitempty"`
}

// ReadPayload represents the incoming read command.
type ReadPayload struct {
	ChannelID string `json:"channel_id"`
	MessageID string `json:"message_id"`
}

// PresencePayload announces a user's online/offline transition to everyone who shares a
// channel with them.
type PresencePayload struct {
	UserID string `json:"user_id"`
	Online bool   `json:"online"`
}
