// Package chatclient is a pure REST+WebSocket client for the hivemind server, used by
// `hivemind chat` (internal/chattui) to authenticate, read, send, and receive realtime
// fanout without ever touching the store directly.
package chatclient

// Channel is the subset of publicChannelDetails (internal/api/channels.go) the CLI needs.
// All ids are strings, matching the wire convention (SPEC.md §4, "IDs serialize as strings").
type Channel struct {
	ID                string  `json:"id"`
	Kind              string  `json:"kind"`
	Slug              *string `json:"slug"`
	Name              string  `json:"name"`
	Topic             string  `json:"topic"`
	MemberCount       int     `json:"member_count"`
	LastMessageID     *string `json:"last_message_id"`
	LastReadMessageID string  `json:"last_read_message_id"`
	Joined            bool    `json:"joined"`
}

// User mirrors publicUser (internal/api/router.go).
type User struct {
	ID          string `json:"id"`
	Username    string `json:"username"`
	Email       string `json:"email"`
	DisplayName string `json:"display_name"`
	AvatarColor string `json:"avatar_color"`
	AvatarURL   string `json:"avatar_url"`
	Role        string `json:"role"`
	IsBot       bool   `json:"is_bot"`
	Status      string `json:"status"`
	CreatedAt   int64  `json:"created_at"`
	Online      bool   `json:"online"`
}

// Message mirrors publicMessage (internal/api/messages.go) — only the fields the CLI
// renders or acts on.
type Message struct {
	ID          string  `json:"id"`
	ChannelID   string  `json:"channel_id"`
	UserID      string  `json:"user_id"`
	User        *User   `json:"user"`
	Body        string  `json:"body"`
	ThreadID    *string `json:"thread_id"`
	ReplyCount  int     `json:"reply_count"`
	LastReplyID *string `json:"last_reply_id"`
	DeletedAt   *int64  `json:"deleted_at"`
	EditedAt    *int64  `json:"edited_at"`
	CreatedAt   int64   `json:"created_at"`
	ClientMsgID string  `json:"client_msg_id"`
}

// DM mirrors publicDM (internal/api/dms.go) — a channel of kind "dm" or "group_dm" with its
// peer (1:1) or members (group) inlined, since a bare Channel carries no name for these.
type DM struct {
	ID                string  `json:"id"`
	Kind              string  `json:"kind"`
	Name              *string `json:"name"`
	Peer              *User   `json:"peer"`
	Members           []User  `json:"members"`
	MemberCount       int     `json:"member_count"`
	LastMessageID     *string `json:"last_message_id"`
	LastReadMessageID string  `json:"last_read_message_id"`
	Joined            bool    `json:"joined"`
}

// MessagePage is the response shape of GET /channels/:id/messages.
type MessagePage struct {
	Data       []Message `json:"data"`
	HasMore    bool      `json:"has_more"`
	NextBefore string    `json:"next_before"`
}

// apiError mirrors the {"error":{...}} envelope every non-2xx response uses (SPEC.md §4.1).
type apiError struct {
	Error struct {
		Code    string `json:"code"`
		Message string `json:"message"`
		Field   any    `json:"field"`
	} `json:"error"`
}

// APIError is a typed, decoded server error response.
type APIError struct {
	StatusCode int
	Code       string
	Message    string
	Field      any
}

func (e *APIError) Error() string { return e.Message }
