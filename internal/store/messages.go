package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"unicode/utf8"

	"github.com/0funct0ry/hivemind/internal/mentions"
)

// Message represents a single message in a channel or thread.
type Message struct {
	ID             int64        `json:"id"`
	ChannelID      int64        `json:"channel_id"`
	UserID         int64        `json:"user_id"`
	User           *User        `json:"user,omitempty"`
	ThreadID       *int64       `json:"thread_id"`
	Body           string       `json:"body"`
	ClientMsgID    *string      `json:"client_msg_id"`
	ReplyCount     int          `json:"reply_count"`
	LastReplyID    *int64       `json:"last_reply_id"`
	HasAttachments bool         `json:"has_attachments"`
	Broadcast      bool         `json:"broadcast"`
	Attachments    []Attachment `json:"attachments"`
	EditedAt       *int64       `json:"edited_at"`
	DeletedAt      *int64       `json:"deleted_at"`
	DeletedBy      *int64       `json:"deleted_by"`
	Reactions      []Reaction   `json:"reactions"`
	CreatedAt      int64        `json:"created_at"`
}

// Attachment represents a file attached to a message.
type Attachment struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Mime   string `json:"mime"`
	Size   int64  `json:"size"`
	Width  *int   `json:"width,omitempty"`
	Height *int   `json:"height,omitempty"`
	URL    string `json:"url"`
}

// MessageInput contains the fields needed to create a new message.
type MessageInput struct {
	ChannelID   int64
	UserID      int64
	Body        string
	ThreadID    *int64
	ClientMsgID *string
	FileIDs     []string
	Broadcast   bool
	IsOnline    func(userID int64) bool
}

var (
	ErrThreadChannelMismatch = errors.New("thread_channel_mismatch")
	ErrThreadLocked          = errors.New("thread_locked")
	ErrUserDeactivated       = errors.New("user_deactivated")
	ErrAttachmentNotFound    = errors.New("attachment_not_found")
	ErrAttachmentForbidden   = errors.New("attachment_forbidden")
	ErrNotMessageOwner       = errors.New("not_message_owner")
	ErrEditWindowExpired     = errors.New("edit_window_expired")
	ErrEditNotApplied        = errors.New("edit_not_applied")
)

// editWindow is how long after posting an author may still edit a message.
const editWindow = 15 * 60 * 1000 // 15 minutes, in millis

// isUniqueConstraintError checks if an error is a SQLite unique constraint violation.
func isUniqueConstraintError(err error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(err.Error(), "UNIQUE constraint failed")
}

// CreateMessage creates a message inside one transaction and updates the channel's last message ID.
func (s *Store) CreateMessage(ctx context.Context, in MessageInput) (Message, bool, error) {
	body := strings.TrimSpace(in.Body)
	body = strings.ReplaceAll(body, "\x00", "")
	body = strings.ReplaceAll(body, "\r\n", "\n")

	runes := utf8.RuneCountInString(body)
	if len(in.FileIDs) == 0 && (runes < 1 || runes > 8000) {
		return Message{}, false, fmt.Errorf("message body must be 1 to 8000 characters")
	}
	if len(in.FileIDs) > 0 && runes > 8000 {
		return Message{}, false, fmt.Errorf("message body exceeds 8000 characters")
	}

	now := nowMillis()
	var msg Message
	var existed bool

	err := s.Tx(ctx, func(tx *sql.Tx) error {
		// DM check: block posting if peer is deactivated
		var kind string
		err := tx.QueryRowContext(ctx, "SELECT kind FROM channels WHERE id = ?", in.ChannelID).Scan(&kind)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return ErrNotFound
			}
			return fmt.Errorf("query channel kind: %w", err)
		}

		if kind == "dm" {
			var peerDeactivated bool
			err = tx.QueryRowContext(ctx, `
				SELECT EXISTS(
					SELECT 1 FROM channel_members cm
					JOIN users u ON cm.user_id = u.id
					WHERE cm.channel_id = ? AND cm.user_id != ? AND u.status = 'deactivated'
				)`, in.ChannelID, in.UserID).Scan(&peerDeactivated)
			if err != nil {
				return fmt.Errorf("check peer deactivation: %w", err)
			}
			if peerDeactivated {
				return ErrUserDeactivated
			}
		}
		// Thread replies validation
		var rootID int64
		if in.ThreadID != nil {
			var parentThreadID sql.NullInt64
			var parentChannelID int64
			var parentDeletedAt sql.NullInt64
			err := tx.QueryRowContext(ctx, `
				SELECT id, channel_id, thread_id, deleted_at
				FROM messages
				WHERE id = ?`, *in.ThreadID).Scan(&rootID, &parentChannelID, &parentThreadID, &parentDeletedAt)
			if err != nil {
				if errors.Is(err, sql.ErrNoRows) {
					return ErrNotFound
				}
				return fmt.Errorf("query parent message: %w", err)
			}
			if parentThreadID.Valid {
				rootID = parentThreadID.Int64
				err = tx.QueryRowContext(ctx, `
					SELECT channel_id, deleted_at
					FROM messages
					WHERE id = ?`, rootID).Scan(&parentChannelID, &parentDeletedAt)
				if err != nil {
					if errors.Is(err, sql.ErrNoRows) {
						return ErrNotFound
					}
					return fmt.Errorf("query root message: %w", err)
				}
			}
			if parentChannelID != in.ChannelID {
				return ErrThreadChannelMismatch
			}
			if parentDeletedAt.Valid {
				return ErrThreadLocked
			}
			in.ThreadID = &rootID
		}

		hasAttachments := 0
		if len(in.FileIDs) > 0 {
			hasAttachments = 1
		}

		broadcastVal := 0
		if in.Broadcast {
			broadcastVal = 1
		}

		res, err := tx.ExecContext(ctx, `
			INSERT INTO messages (channel_id, user_id, thread_id, body, client_msg_id, has_attachments, broadcast, created_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
			in.ChannelID, in.UserID, in.ThreadID, body, in.ClientMsgID, hasAttachments, broadcastVal, now)
		if err != nil {
			if isUniqueConstraintError(err) && in.ClientMsgID != nil {
				existed = true
				var threadIDVal, lastReplyIDVal, editedAtVal, deletedAtVal, deletedByVal sql.NullInt64
				var clientMsgIDVal sql.NullString
				err = tx.QueryRowContext(ctx, `
					SELECT id, channel_id, user_id, thread_id, body, client_msg_id, reply_count, last_reply_id, has_attachments, broadcast, edited_at, deleted_at, deleted_by, created_at
					FROM messages
					WHERE user_id = ? AND client_msg_id = ?`,
					in.UserID, *in.ClientMsgID).Scan(
					&msg.ID, &msg.ChannelID, &msg.UserID, &threadIDVal, &msg.Body, &clientMsgIDVal, &msg.ReplyCount, &lastReplyIDVal, &msg.HasAttachments, &msg.Broadcast, &editedAtVal, &deletedAtVal, &deletedByVal, &msg.CreatedAt)
				if err != nil {
					return fmt.Errorf("fetch existing message: %w", err)
				}
				if threadIDVal.Valid {
					msg.ThreadID = &threadIDVal.Int64
				}
				if clientMsgIDVal.Valid {
					msg.ClientMsgID = &clientMsgIDVal.String
				}
				if lastReplyIDVal.Valid {
					msg.LastReplyID = &lastReplyIDVal.Int64
				}
				if editedAtVal.Valid {
					msg.EditedAt = &editedAtVal.Int64
				}
				if deletedAtVal.Valid {
					msg.DeletedAt = &deletedAtVal.Int64
				}
				if deletedByVal.Valid {
					msg.DeletedBy = &deletedByVal.Int64
				}
				return nil
			}
			return fmt.Errorf("insert message: %w", err)
		}

		id, err := res.LastInsertId()
		if err != nil {
			return fmt.Errorf("last insert id: %w", err)
		}

		// Insert attachments
		for pos, fid := range in.FileIDs {
			var uploadedBy int64
			err = tx.QueryRowContext(ctx, "SELECT uploaded_by FROM files WHERE id = ?", fid).Scan(&uploadedBy)
			if err != nil {
				if errors.Is(err, sql.ErrNoRows) {
					return ErrAttachmentNotFound
				}
				return fmt.Errorf("verify attachment uploader: %w", err)
			}
			if uploadedBy != in.UserID {
				return ErrAttachmentForbidden
			}

			_, err = tx.ExecContext(ctx, `
				INSERT INTO attachments (message_id, file_id, position)
				VALUES (?, ?, ?)`,
				id, fid, pos)
			if err != nil {
				return fmt.Errorf("insert attachment: %w", err)
			}
		}

		// Update root message reply metadata if this is a reply
		if in.ThreadID != nil {
			_, err = tx.ExecContext(ctx, `
				UPDATE messages
				SET reply_count = reply_count + 1, last_reply_id = ?
				WHERE id = ?`,
				id, *in.ThreadID)
			if err != nil {
				return fmt.Errorf("update root message reply metadata: %w", err)
			}
		}

		// Update channels.last_message_id and updated_at
		_, err = tx.ExecContext(ctx, `
			UPDATE channels
			SET last_message_id = ?, updated_at = ?
			WHERE id = ?`,
			id, now, in.ChannelID)
		if err != nil {
			return fmt.Errorf("update channel metadata: %w", err)
		}

		// A DM/group DM that any member removed from their sidebar (HideConversation)
		// reappears for everyone the moment new activity arrives.
		if kind == "dm" || kind == "group_dm" {
			_, err = tx.ExecContext(ctx, `
				UPDATE channel_members SET hidden_at = NULL
				WHERE channel_id = ? AND hidden_at IS NOT NULL`,
				in.ChannelID)
			if err != nil {
				return fmt.Errorf("unhide conversation: %w", err)
			}
		}

		// Resolve mentions
		resolved := make(map[int64]string)
		var largeChannelID int64
		var largeChannelKind string
		var largeChannelSize int

		refs := mentions.Parse(body)
		for _, ref := range refs {
			switch ref.Kind {
			case "user":
				var targetID int64
				var status string
				err := tx.QueryRowContext(ctx, "SELECT id, status FROM users WHERE username = ? COLLATE NOCASE", ref.Name).Scan(&targetID, &status)
				if err == nil && status == "active" {
					var isMember bool
					err = tx.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM channel_members WHERE channel_id = ? AND user_id = ?)", in.ChannelID, targetID).Scan(&isMember)
					if err == nil && isMember && targetID != in.UserID {
						resolved[targetID] = "user"
					}
				}
			case "channel":
				rows, err := tx.QueryContext(ctx, "SELECT user_id FROM channel_members WHERE channel_id = ?", in.ChannelID)
				if err == nil {
					var members []int64
					for rows.Next() {
						var mID int64
						if err := rows.Scan(&mID); err == nil {
							members = append(members, mID)
						}
					}
					rows.Close()

					for _, mID := range members {
						if mID != in.UserID {
							if _, exists := resolved[mID]; !exists {
								resolved[mID] = "channel"
							}
						}
					}
					if len(members) > 50 {
						largeChannelID = in.ChannelID
						largeChannelKind = "channel"
						largeChannelSize = len(members)
					}
				}
			case "here":
				rows, err := tx.QueryContext(ctx, "SELECT user_id FROM channel_members WHERE channel_id = ?", in.ChannelID)
				if err == nil {
					var members []int64
					for rows.Next() {
						var mID int64
						if err := rows.Scan(&mID); err == nil {
							members = append(members, mID)
						}
					}
					rows.Close()

					for _, mID := range members {
						if mID != in.UserID {
							if in.IsOnline != nil && in.IsOnline(mID) {
								if current, exists := resolved[mID]; !exists || current == "channel" {
									resolved[mID] = "here"
								}
							}
						}
					}
					if len(members) > 50 {
						largeChannelID = in.ChannelID
						largeChannelKind = "here"
						largeChannelSize = len(members)
					}
				}
			}
		}

		if largeChannelID > 0 {
			slog.Info("Large mention fanout", "channel_id", largeChannelID, "kind", largeChannelKind, "fanout_size", largeChannelSize)
		}

		// Insert mentions
		for targetID, kind := range resolved {
			_, err = tx.ExecContext(ctx, `
				INSERT INTO mentions (message_id, user_id, channel_id, kind, created_at)
				VALUES (?, ?, ?, ?, ?)`,
				id, targetID, in.ChannelID, kind, now)
			if err != nil {
				return fmt.Errorf("insert mention: %w", err)
			}
		}

		msg.ID = id
		msg.ChannelID = in.ChannelID
		msg.UserID = in.UserID
		msg.ThreadID = in.ThreadID
		msg.Body = body
		msg.ClientMsgID = in.ClientMsgID
		msg.Broadcast = in.Broadcast
		msg.HasAttachments = len(in.FileIDs) > 0
		msg.CreatedAt = now
		return nil
	})

	if err != nil {
		return Message{}, false, err
	}

	// Hydrate authors and attachments
	messages := []Message{msg}
	if err := s.HydrateMessages(ctx, messages); err != nil {
		return Message{}, false, err
	}

	return messages[0], existed, nil
}

// GetMessage returns a single message by ID.
func (s *Store) GetMessage(ctx context.Context, id int64) (Message, error) {
	var msg Message
	var threadIDVal, lastReplyIDVal, editedAtVal, deletedAtVal, deletedByVal sql.NullInt64
	var clientMsgIDVal sql.NullString
	err := s.reader.QueryRowContext(ctx, `
		SELECT `+channelMessageColumns+`
		FROM messages
		WHERE id = ?`, id).Scan(
		&msg.ID, &msg.ChannelID, &msg.UserID, &threadIDVal, &msg.Body, &clientMsgIDVal, &msg.ReplyCount, &lastReplyIDVal, &msg.HasAttachments, &msg.Broadcast, &editedAtVal, &deletedAtVal, &deletedByVal, &msg.CreatedAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Message{}, ErrNotFound
		}
		return Message{}, fmt.Errorf("get message: %w", err)
	}
	if threadIDVal.Valid {
		msg.ThreadID = &threadIDVal.Int64
	}
	if clientMsgIDVal.Valid {
		msg.ClientMsgID = &clientMsgIDVal.String
	}
	if lastReplyIDVal.Valid {
		msg.LastReplyID = &lastReplyIDVal.Int64
	}
	if editedAtVal.Valid {
		msg.EditedAt = &editedAtVal.Int64
	}
	if deletedAtVal.Valid {
		msg.DeletedAt = &deletedAtVal.Int64
	}
	if deletedByVal.Valid {
		msg.DeletedBy = &deletedByVal.Int64
	}

	messages := []Message{msg}
	if err := s.HydrateMessages(ctx, messages); err != nil {
		return Message{}, err
	}
	return messages[0], nil
}

const channelMessageColumns = `id, channel_id, user_id, thread_id, body, client_msg_id, reply_count, last_reply_id, has_attachments, broadcast, edited_at, deleted_at, deleted_by, created_at`

// queryChannelMessages runs a channel-message query and scans the results, unhydrated.
func (s *Store) queryChannelMessages(ctx context.Context, query string, args ...any) ([]Message, error) {
	rows, err := s.reader.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list channel messages query: %w", err)
	}
	defer rows.Close()

	var messages []Message
	for rows.Next() {
		var msg Message
		var threadIDVal, lastReplyIDVal, editedAtVal, deletedAtVal, deletedByVal sql.NullInt64
		var clientMsgIDVal sql.NullString
		err := rows.Scan(
			&msg.ID, &msg.ChannelID, &msg.UserID, &threadIDVal, &msg.Body, &clientMsgIDVal, &msg.ReplyCount, &lastReplyIDVal, &msg.HasAttachments, &msg.Broadcast, &editedAtVal, &deletedAtVal, &deletedByVal, &msg.CreatedAt)
		if err != nil {
			return nil, fmt.Errorf("scan message: %w", err)
		}
		if threadIDVal.Valid {
			msg.ThreadID = &threadIDVal.Int64
		}
		if clientMsgIDVal.Valid {
			msg.ClientMsgID = &clientMsgIDVal.String
		}
		if lastReplyIDVal.Valid {
			msg.LastReplyID = &lastReplyIDVal.Int64
		}
		if editedAtVal.Valid {
			msg.EditedAt = &editedAtVal.Int64
		}
		if deletedAtVal.Valid {
			msg.DeletedAt = &deletedAtVal.Int64
		}
		if deletedByVal.Valid {
			msg.DeletedBy = &deletedByVal.Int64
		}
		messages = append(messages, msg)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}
	return messages, nil
}

// ListChannelMessages retrieves root messages in a channel using cursor-based pagination.
// When around is non-nil, before/after are ignored and the result is a symmetric window
// of up to limit/2 messages on either side of the anchor id (inclusive of the anchor).
func (s *Store) ListChannelMessages(ctx context.Context, channelID int64, before, after, around *int64, limit int) ([]Message, error) {
	if limit <= 0 {
		limit = 50
	}
	if limit > 200 {
		limit = 200
	}

	var messages []Message
	var err error

	switch {
	case around != nil:
		half := limit / 2
		if half < 1 {
			half = 1
		}
		older, err2 := s.queryChannelMessages(ctx, `
			SELECT `+channelMessageColumns+`
			FROM messages
			WHERE channel_id = ? AND (thread_id IS NULL OR broadcast = 1) AND id <= ?
			ORDER BY id DESC
			LIMIT ?`, channelID, *around, half+1)
		if err2 != nil {
			return nil, err2
		}
		for i, j := 0, len(older)-1; i < j; i, j = i+1, j-1 {
			older[i], older[j] = older[j], older[i]
		}
		newer, err2 := s.queryChannelMessages(ctx, `
			SELECT `+channelMessageColumns+`
			FROM messages
			WHERE channel_id = ? AND (thread_id IS NULL OR broadcast = 1) AND id > ?
			ORDER BY id ASC
			LIMIT ?`, channelID, *around, half)
		if err2 != nil {
			return nil, err2
		}
		messages = append(older, newer...)
	case before != nil:
		messages, err = s.queryChannelMessages(ctx, `
			SELECT `+channelMessageColumns+`
			FROM messages
			WHERE channel_id = ? AND (thread_id IS NULL OR broadcast = 1) AND id < ?
			ORDER BY id DESC
			LIMIT ?`, channelID, *before, limit)
		if err != nil {
			return nil, err
		}
		for i, j := 0, len(messages)-1; i < j; i, j = i+1, j-1 {
			messages[i], messages[j] = messages[j], messages[i]
		}
	case after != nil:
		messages, err = s.queryChannelMessages(ctx, `
			SELECT `+channelMessageColumns+`
			FROM messages
			WHERE channel_id = ? AND (thread_id IS NULL OR broadcast = 1) AND id > ?
			ORDER BY id ASC
			LIMIT ?`, channelID, *after, limit)
		if err != nil {
			return nil, err
		}
	default:
		messages, err = s.queryChannelMessages(ctx, `
			SELECT `+channelMessageColumns+`
			FROM messages
			WHERE channel_id = ? AND (thread_id IS NULL OR broadcast = 1)
			ORDER BY id DESC
			LIMIT ?`, channelID, limit)
		if err != nil {
			return nil, err
		}
		for i, j := 0, len(messages)-1; i < j; i, j = i+1, j-1 {
			messages[i], messages[j] = messages[j], messages[i]
		}
	}

	if err := s.HydrateMessages(ctx, messages); err != nil {
		return nil, err
	}

	return messages, nil
}

// ListReplies retrieves replies for a root message using cursor-based pagination.
// If after is nil, the root message is prepended to the returned list.
func (s *Store) ListReplies(ctx context.Context, rootID int64, after *int64, limit int) ([]Message, error) {
	if limit <= 0 {
		limit = 50
	}
	if limit > 200 {
		limit = 200
	}

	var messages []Message

	// If after is nil, fetch the root message first.
	if after == nil {
		rootMsg, err := s.GetMessage(ctx, rootID)
		if err != nil {
			return nil, err
		}
		messages = append(messages, rootMsg)
	}

	var query string
	var args []any
	args = append(args, rootID)

	if after != nil {
		query = `
			SELECT ` + channelMessageColumns + `
			FROM messages
			WHERE thread_id = ? AND id > ?
			ORDER BY id ASC
			LIMIT ?`
		args = append(args, *after, limit)
	} else {
		query = `
			SELECT ` + channelMessageColumns + `
			FROM messages
			WHERE thread_id = ?
			ORDER BY id ASC
			LIMIT ?`
		args = append(args, limit)
	}

	rows, err := s.reader.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list replies query: %w", err)
	}
	defer rows.Close()

	var replies []Message
	for rows.Next() {
		var msg Message
		var threadIDVal, lastReplyIDVal, editedAtVal, deletedAtVal, deletedByVal sql.NullInt64
		var clientMsgIDVal sql.NullString
		err := rows.Scan(
			&msg.ID, &msg.ChannelID, &msg.UserID, &threadIDVal, &msg.Body, &clientMsgIDVal, &msg.ReplyCount, &lastReplyIDVal, &msg.HasAttachments, &msg.Broadcast, &editedAtVal, &deletedAtVal, &deletedByVal, &msg.CreatedAt)
		if err != nil {
			return nil, fmt.Errorf("scan reply message: %w", err)
		}
		if threadIDVal.Valid {
			msg.ThreadID = &threadIDVal.Int64
		}
		if clientMsgIDVal.Valid {
			msg.ClientMsgID = &clientMsgIDVal.String
		}
		if lastReplyIDVal.Valid {
			msg.LastReplyID = &lastReplyIDVal.Int64
		}
		if editedAtVal.Valid {
			msg.EditedAt = &editedAtVal.Int64
		}
		if deletedAtVal.Valid {
			msg.DeletedAt = &deletedAtVal.Int64
		}
		if deletedByVal.Valid {
			msg.DeletedBy = &deletedByVal.Int64
		}
		replies = append(replies, msg)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	if len(replies) > 0 {
		if err := s.HydrateMessages(ctx, replies); err != nil {
			return nil, err
		}
		messages = append(messages, replies...)
	}

	return messages, nil
}

// HydrateMessages batch-loads authors and attachments for a list of messages.
func (s *Store) HydrateMessages(ctx context.Context, messages []Message) error {
	if len(messages) == 0 {
		return nil
	}

	// 1. Hydrate Users
	userIDsMap := make(map[int64]struct{})
	for _, m := range messages {
		userIDsMap[m.UserID] = struct{}{}
	}

	if len(userIDsMap) > 0 {
		var userIDs []any
		var placeholders []string
		for uid := range userIDsMap {
			userIDs = append(userIDs, uid)
			placeholders = append(placeholders, "?")
		}

		query := fmt.Sprintf(`
			SELECT u.id, u.username, u.email, u.display_name, u.password_hash, u.avatar_color, u.role, u.is_bot, u.status, u.created_at, u.updated_at, f.id, f.name
			FROM users u
			LEFT JOIN files f ON f.id = u.avatar_file_id
			WHERE u.id IN (%s)`, strings.Join(placeholders, ","))

		rows, err := s.reader.QueryContext(ctx, query, userIDs...)
		if err != nil {
			return fmt.Errorf("hydrate messages authors query: %w", err)
		}
		defer rows.Close()

		usersMap := make(map[int64]User)
		for rows.Next() {
			var u User
			var bot int
			var avatarFileID, avatarFileName sql.NullString
			if err := rows.Scan(&u.ID, &u.Username, &u.Email, &u.DisplayName, &u.PasswordHash, &u.AvatarColor, &u.Role, &bot, &u.Status, &u.CreatedAt, &u.UpdatedAt, &avatarFileID, &avatarFileName); err != nil {
				return fmt.Errorf("scan user: %w", err)
			}
			u.IsBot = bot != 0
			if avatarFileID.Valid && avatarFileName.Valid {
				u.AvatarURL = "/api/v1/files/" + avatarFileID.String + "/" + avatarFileName.String
			}
			usersMap[u.ID] = u
		}
		if err := rows.Err(); err != nil {
			return err
		}

		for i := range messages {
			if u, ok := usersMap[messages[i].UserID]; ok {
				messages[i].User = &u
			}
		}
	}

	// 2. Hydrate Attachments
	var msgIDs []any
	var msgPlaceholders []string
	msgMap := make(map[int64]*Message)
	for i := range messages {
		messages[i].Attachments = []Attachment{} // initialize to empty slice
		if messages[i].HasAttachments {
			msgIDs = append(msgIDs, messages[i].ID)
			msgPlaceholders = append(msgPlaceholders, "?")
			msgMap[messages[i].ID] = &messages[i]
		}
	}

	if len(msgIDs) > 0 {
		query := fmt.Sprintf(`
			SELECT a.message_id, f.id, f.name, f.mime, f.size, f.width, f.height
			FROM attachments a
			JOIN files f ON a.file_id = f.id
			WHERE a.message_id IN (%s)
			ORDER BY a.message_id, a.position ASC`, strings.Join(msgPlaceholders, ","))

		rows, err := s.reader.QueryContext(ctx, query, msgIDs...)
		if err != nil {
			return fmt.Errorf("hydrate attachments query: %w", err)
		}
		defer rows.Close()

		for rows.Next() {
			var mid int64
			var att Attachment
			var wVal, hVal sql.NullInt64
			err := rows.Scan(&mid, &att.ID, &att.Name, &att.Mime, &att.Size, &wVal, &hVal)
			if err != nil {
				return fmt.Errorf("scan attachment: %w", err)
			}
			if wVal.Valid {
				w := int(wVal.Int64)
				att.Width = &w
			}
			if hVal.Valid {
				h := int(hVal.Int64)
				att.Height = &h
			}
			att.URL = fmt.Sprintf("/api/v1/files/%s/%s", att.ID, att.Name)

			if m, ok := msgMap[mid]; ok {
				m.Attachments = append(m.Attachments, att)
			}
		}
		if err := rows.Err(); err != nil {
			return err
		}
	}

	// 3. Hydrate Reactions
	allIDs := make([]int64, len(messages))
	byID := make(map[int64]*Message, len(messages))
	for i := range messages {
		messages[i].Reactions = []Reaction{}
		allIDs[i] = messages[i].ID
		byID[messages[i].ID] = &messages[i]
	}
	reactionsByMessage, err := s.getReactionsForMessages(ctx, allIDs)
	if err != nil {
		return err
	}
	for id, reactions := range reactionsByMessage {
		if m, ok := byID[id]; ok {
			m.Reactions = reactions
		}
	}

	return nil
}

// UpdateMessageBody edits a message's body. The caller must be the author and the message
// must have been created within the last editWindow milliseconds; both checks are enforced in
// the UPDATE's WHERE clause rather than as an app-level read-then-check. On zero rows affected
// this returns ErrEditNotApplied, and it is up to the caller to issue a cheap follow-up read to
// distinguish "not yours" from "window closed" from "already deleted".
func (s *Store) UpdateMessageBody(ctx context.Context, id, userID int64, body string) (Message, error) {
	body = strings.TrimSpace(body)
	body = strings.ReplaceAll(body, "\x00", "")
	body = strings.ReplaceAll(body, "\r\n", "\n")

	runes := utf8.RuneCountInString(body)
	if runes < 1 || runes > 8000 {
		return Message{}, fmt.Errorf("message body must be 1 to 8000 characters")
	}

	now := nowMillis()
	cutoff := now - editWindow

	res, err := s.writer.ExecContext(ctx, `
		UPDATE messages
		SET body = ?, edited_at = ?
		WHERE id = ? AND user_id = ? AND created_at > ? AND deleted_at IS NULL`,
		body, now, id, userID, cutoff)
	if err != nil {
		return Message{}, fmt.Errorf("update message body: %w", err)
	}

	affected, err := res.RowsAffected()
	if err != nil {
		return Message{}, fmt.Errorf("update message body rows affected: %w", err)
	}
	if affected == 0 {
		return Message{}, ErrEditNotApplied
	}

	return s.GetMessage(ctx, id)
}

// DeleteMessage soft-deletes a message: sets deleted_at/deleted_by and blanks the body.
// Deleting an already-deleted message is a no-op, not an error — the caller re-reads and
// returns the (already-deleted) message either way, matching the idempotency posture
// client_msg_id already established for message creation. DeleteMessage does not check
// ownership or role; the caller has already decided the requester may delete this message.
func (s *Store) DeleteMessage(ctx context.Context, id int64, deletedBy int64) (Message, error) {
	now := nowMillis()

	_, err := s.writer.ExecContext(ctx, `
		UPDATE messages
		SET deleted_at = ?, deleted_by = ?, body = ''
		WHERE id = ? AND deleted_at IS NULL`,
		now, deletedBy, id)
	if err != nil {
		return Message{}, fmt.Errorf("delete message: %w", err)
	}

	return s.GetMessage(ctx, id)
}

// IsThreadLocked reports whether a thread's root message has been soft-deleted. Thread lock is
// derived, never stored: a thread is locked exactly when its root row has deleted_at set.
func (s *Store) IsThreadLocked(ctx context.Context, threadRootID int64) (bool, error) {
	var deletedAt sql.NullInt64
	err := s.reader.QueryRowContext(ctx, `
		SELECT deleted_at FROM messages WHERE id = ? AND thread_id IS NULL`, threadRootID).Scan(&deletedAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return false, nil
		}
		return false, fmt.Errorf("check thread locked: %w", err)
	}
	return deletedAt.Valid, nil
}

// Mention represents a row in the mentions table.
type Mention struct {
	MessageID int64
	UserID    int64
	ChannelID int64
	Kind      string
	CreatedAt int64
}

// GetMessageMentions returns all mentions associated with a message ID.
func (s *Store) GetMessageMentions(ctx context.Context, messageID int64) ([]Mention, error) {
	rows, err := s.reader.QueryContext(ctx, "SELECT message_id, user_id, channel_id, kind, created_at FROM mentions WHERE message_id = ?", messageID)
	if err != nil {
		return nil, fmt.Errorf("query message mentions: %w", err)
	}
	defer rows.Close()

	var out []Mention
	for rows.Next() {
		var m Mention
		if err := rows.Scan(&m.MessageID, &m.UserID, &m.ChannelID, &m.Kind, &m.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan message mention: %w", err)
		}
		out = append(out, m)
	}
	return out, rows.Err()
}
