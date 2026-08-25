package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
)

// SearchQuery represents a parsed full-text search query with filters.
type SearchQuery struct {
	Text         string
	ChannelID    *int64
	FromUserID   *int64
	Has          *string // "link" | "file" | "image"
	Before       *int64
	After        *int64
	Limit        int
}

// Hit represents a single search result.
type Hit struct {
	Message Message `json:"message"`
	Channel Channel `json:"channel"`
	Snippet string  `json:"snippet"`
}

// sanitizeFTS sanitizes a search string for safe usage in SQLite FTS5.
// It balances quotes, drops trailing unmatched quotes, and escapes special FTS5 operators
// if they are not part of an intended structure.
func sanitizeFTS(q string) string {
	q = strings.TrimSpace(q)
	if q == "" {
		return ""
	}

	// Balance double quotes. If there's an odd number of double quotes, drop the last one.
	quoteCount := strings.Count(q, "\"")
	if quoteCount%2 != 0 {
		lastIdx := strings.LastIndex(q, "\"")
		if lastIdx != -1 {
			q = q[:lastIdx] + q[lastIdx+1:]
		}
	}

	// Tokenize/clean string.
	// We want to escape bare "-", "^", ":" if they aren't part of allowed operators or phrases.
	// SQLite FTS5 allows AND, OR, NOT, NEAR/n, and phrase quotes.
	// For simplicity and safety, we can wrap terms in double quotes or escape special characters.
	var sb strings.Builder
	inQuote := false
	runes := []rune(q)

	for i := 0; i < len(runes); i++ {
		r := runes[i]
		if r == '"' {
			inQuote = !inQuote
			sb.WriteRune(r)
			continue
		}

		if inQuote {
			sb.WriteRune(r)
			continue
		}

		// If not inside a quote, sanitize special FTS characters.
		switch r {
		case '-', '^', ':', '*':
			// Escape by wrapping in double quotes or escaping depending on context.
			if r == '*' {
				// Only allow '*' if preceded by a letter/number (prefix search).
				if i > 0 && isAlphaNumeric(runes[i-1]) {
					sb.WriteRune(r)
				} else {
					sb.WriteString(` "*" `)
				}
			} else {
				sb.WriteByte(' ')
				sb.WriteRune('"')
				sb.WriteRune(r)
				sb.WriteRune('"')
				sb.WriteByte(' ')
			}
		case '`', '~', '@', '#', '$', '%', '&', '(', ')', '_', '=', '+', '[', ']', '{', '}', ';', '\'', '<', '>', ',', '?', '/', '|', '\\':
			// For all other special characters that might cause syntax errors in FTS5, strip them or escape them
			sb.WriteByte(' ')
			sb.WriteRune('"')
			sb.WriteRune(r)
			sb.WriteRune('"')
			sb.WriteByte(' ')
		default:
			sb.WriteRune(r)
		}
	}

	return strings.TrimSpace(sb.String())
}

func isAlphaNumeric(r rune) bool {
	return (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9')
}

// Search queries the FTS5 virtual table joined with channel_members to enforce authorization.
func (s *Store) Search(ctx context.Context, userID int64, q SearchQuery) ([]Hit, error) {
	sanitized := sanitizeFTS(q.Text)
	if sanitized == "" && q.ChannelID == nil && q.FromUserID == nil && q.Has == nil {
		return nil, errors.New("empty_search_query")
	}

	if q.Limit <= 0 {
		q.Limit = 50
	} else if q.Limit > 200 {
		q.Limit = 200
	}

	// Base query joining messages_fts -> messages -> channel_members.
	// This ensures a user can never search messages in a channel they are not a member of.
	var args []any

	selectFields := `
		m.id, m.channel_id, m.user_id, m.thread_id, m.body, m.client_msg_id, m.reply_count,
		m.last_reply_id, m.has_attachments, m.broadcast, m.edited_at, m.deleted_at, m.created_at,
		c.kind, c.slug, c.name, c.topic, c.dm_key, c.created_by, c.archived_at, c.last_message_id,
		c.created_at, c.updated_at`

	var queryStr string
	if sanitized != "" {
		selectFields += `, snippet(messages_fts, 0, '<mark>', '</mark>', '…', 18) AS snippet`
		queryStr = `
			SELECT ` + selectFields + `
			FROM messages_fts f
			JOIN messages m ON m.id = f.rowid
			JOIN channels c ON c.id = m.channel_id
			JOIN channel_members cm ON cm.channel_id = m.channel_id AND cm.user_id = ?
			WHERE f.body MATCH ? AND m.deleted_at IS NULL`
		args = append(args, userID, sanitized)
	} else {
		selectFields += `, '' AS snippet`
		queryStr = `
			SELECT ` + selectFields + `
			FROM messages m
			JOIN channels c ON c.id = m.channel_id
			JOIN channel_members cm ON cm.channel_id = m.channel_id AND cm.user_id = ?
			WHERE m.deleted_at IS NULL`
		args = append(args, userID)
	}

	// Add filters
	if q.ChannelID != nil {
		queryStr += " AND m.channel_id = ?"
		args = append(args, *q.ChannelID)
	}
	if q.FromUserID != nil {
		queryStr += " AND m.user_id = ?"
		args = append(args, *q.FromUserID)
	}
	if q.Has != nil {
		switch *q.Has {
		case "file":
			queryStr += " AND m.has_attachments = 1"
		case "image":
			queryStr += ` AND EXISTS (
				SELECT 1 FROM attachments att
				JOIN files fil ON fil.id = att.file_id
				WHERE att.message_id = m.id AND fil.mime LIKE 'image/%'
			)`
		case "link":
			queryStr += " AND (m.body LIKE '%http://%' OR m.body LIKE '%https://%')"
		}
	}
	if q.Before != nil {
		queryStr += " AND m.id < ?"
		args = append(args, *q.Before)
	}
	if q.After != nil {
		queryStr += " AND m.id > ?"
		args = append(args, *q.After)
	}

	// Order by m.id DESC (recency is preferred over FTS rank)
	queryStr += " ORDER BY m.id DESC LIMIT ?"
	args = append(args, q.Limit)

	rows, err := s.reader.QueryContext(ctx, queryStr, args...)
	if err != nil {
		errStr := err.Error()
		if strings.Contains(errStr, "fts5") || strings.Contains(errStr, "syntax error") {
			return nil, fmt.Errorf("invalid_search_query: %w", err)
		}
		return nil, fmt.Errorf("search messages: %w", err)
	}
	defer rows.Close()

	var hits []Hit

	for rows.Next() {
		var hit Hit
		var threadIDVal, lastReplyIDVal, editedAtVal, deletedAtVal, archivedAt, lastMessageID sql.NullInt64
		var clientMsgIDVal sql.NullString
		var channelSlug, dmKey sql.NullString

		scanArgs := []any{
			&hit.Message.ID, &hit.Message.ChannelID, &hit.Message.UserID, &threadIDVal, &hit.Message.Body, &clientMsgIDVal, &hit.Message.ReplyCount,
			&lastReplyIDVal, &hit.Message.HasAttachments, &hit.Message.Broadcast, &editedAtVal, &deletedAtVal, &hit.Message.CreatedAt,
			&hit.Channel.Kind, &channelSlug, &hit.Channel.Name, &hit.Channel.Topic, &dmKey, &hit.Channel.CreatedBy, &archivedAt, &lastMessageID,
			&hit.Channel.CreatedAt, &hit.Channel.UpdatedAt, &hit.Snippet,
		}

		if err := rows.Scan(scanArgs...); err != nil {
			return nil, fmt.Errorf("scan search hit: %w", err)
		}

		hit.Channel.ID = hit.Message.ChannelID
		if threadIDVal.Valid {
			hit.Message.ThreadID = &threadIDVal.Int64
		}
		if clientMsgIDVal.Valid {
			hit.Message.ClientMsgID = &clientMsgIDVal.String
		}
		if lastReplyIDVal.Valid {
			hit.Message.LastReplyID = &lastReplyIDVal.Int64
		}
		if editedAtVal.Valid {
			hit.Message.EditedAt = &editedAtVal.Int64
		}
		if deletedAtVal.Valid {
			hit.Message.DeletedAt = &deletedAtVal.Int64
		}
		if channelSlug.Valid {
			hit.Channel.Slug = &channelSlug.String
		}
		if dmKey.Valid {
			hit.Channel.DMKey = &dmKey.String
		}
		if archivedAt.Valid {
			hit.Channel.ArchivedAt = &archivedAt.Int64
		}
		if lastMessageID.Valid {
			hit.Channel.LastMessageID = &lastMessageID.Int64
		}

		hits = append(hits, hit)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("search rows iteration: %w", err)
	}

	if len(hits) == 0 {
		return hits, nil
	}

	// Hydrate authors (users) for the messages
	userIDsMap := make(map[int64]bool)
	for _, h := range hits {
		userIDsMap[h.Message.UserID] = true
	}
	if len(userIDsMap) > 0 {
		uids := make([]int64, 0, len(userIDsMap))
		for uid := range userIDsMap {
			uids = append(uids, uid)
		}

		placeholders := make([]string, len(uids))
		queryArgs := make([]any, len(uids))
		for i, uid := range uids {
			placeholders[i] = "?"
			queryArgs[i] = uid
		}
		userQuery := fmt.Sprintf(`
			SELECT id, username, display_name, password_hash, avatar_color, role, is_bot, status, created_at, updated_at
			FROM users WHERE id IN (%s)`, strings.Join(placeholders, ","))

		userRows, err := s.reader.QueryContext(ctx, userQuery, queryArgs...)
		if err != nil {
			return nil, fmt.Errorf("hydrate message authors: %w", err)
		}
		defer userRows.Close()

		usersMap := make(map[int64]*User)
		for userRows.Next() {
			var u User
			if err := userRows.Scan(&u.ID, &u.Username, &u.DisplayName, &u.PasswordHash, &u.AvatarColor, &u.Role, &u.IsBot, &u.Status, &u.CreatedAt, &u.UpdatedAt); err != nil {
				return nil, fmt.Errorf("scan user: %w", err)
			}
			usersMap[u.ID] = &u
		}

		for i := range hits {
			hits[i].Message.User = usersMap[hits[i].Message.UserID]
		}
	}

	// Hydrate attachments for the hits that have them
	hasAttachmentsMsgs := make([]int64, 0)
	for _, h := range hits {
		if h.Message.HasAttachments {
			hasAttachmentsMsgs = append(hasAttachmentsMsgs, h.Message.ID)
		}
	}

	if len(hasAttachmentsMsgs) > 0 {
		placeholders := make([]string, len(hasAttachmentsMsgs))
		queryArgs := make([]any, len(hasAttachmentsMsgs))
		for i, id := range hasAttachmentsMsgs {
			placeholders[i] = "?"
			queryArgs[i] = id
		}

		attachQuery := fmt.Sprintf(`
			SELECT a.message_id, f.id, f.name, f.mime, f.size, f.width, f.height
			FROM attachments a
			JOIN files f ON f.id = a.file_id
			WHERE a.message_id IN (%s)
			ORDER BY a.message_id, a.position`, strings.Join(placeholders, ","))

		attachRows, err := s.reader.QueryContext(ctx, attachQuery, queryArgs...)
		if err != nil {
			return nil, fmt.Errorf("hydrate attachments: %w", err)
		}
		defer attachRows.Close()

		attachmentsMap := make(map[int64][]Attachment)
		for attachRows.Next() {
			var msgID int64
			var att Attachment
			var w, h sql.NullInt64
			if err := attachRows.Scan(&msgID, &att.ID, &att.Name, &att.Mime, &att.Size, &w, &h); err != nil {
				return nil, fmt.Errorf("scan attachment: %w", err)
			}
			if w.Valid {
				widthVal := int(w.Int64)
				att.Width = &widthVal
			}
			if h.Valid {
				heightVal := int(h.Int64)
				att.Height = &heightVal
			}
			att.URL = fmt.Sprintf("/api/v1/files/%s/%s", att.ID, att.Name)
			attachmentsMap[msgID] = append(attachmentsMap[msgID], att)
		}

		for i := range hits {
			if hits[i].Message.HasAttachments {
				hits[i].Message.Attachments = attachmentsMap[hits[i].Message.ID]
			}
		}
	}

	return hits, nil
}

// ActivityCounts contains aggregated message activity count per bucket.
type ActivityCounts struct {
	From           int64          `json:"from"`
	To             int64          `json:"to"`
	BucketMS       int64          `json:"bucket_ms"`
	Counts         []int64        `json:"counts"`
	Mentions       []MentionHit   `json:"mentions"`
	UnreadBoundary *UnreadHit     `json:"unread_boundary,omitempty"`
	Max            int64          `json:"max"`
}

// MentionHit represents a mention event bucket.
type MentionHit struct {
	Bucket    int64 `json:"bucket"`
	MessageID int64 `json:"message_id"`
}

// UnreadHit represents the unread boundary bucket.
type UnreadHit struct {
	Bucket    int64 `json:"bucket"`
	MessageID int64 `json:"message_id"`
}

// GetChannelActivity calculates message activity bucketed over a time window.
func (s *Store) GetChannelActivity(ctx context.Context, userID, channelID, from, to, buckets int64) (ActivityCounts, error) {
	if buckets <= 0 {
		buckets = 48
	}
	bucketMS := (to - from) / buckets
	if bucketMS <= 0 {
		bucketMS = 1
	}

	counts := make([]int64, buckets)

	query := `
		SELECT CAST((created_at - ?) / ? AS INTEGER) AS b, COUNT(*)
		FROM messages
		WHERE channel_id = ? AND created_at >= ? AND created_at < ? AND deleted_at IS NULL
		GROUP BY b`

	rows, err := s.reader.QueryContext(ctx, query, from, bucketMS, channelID, from, to)
	if err != nil {
		return ActivityCounts{}, fmt.Errorf("query activity: %w", err)
	}
	defer rows.Close()

	var maxVal int64
	for rows.Next() {
		var bucketIndex, count int64
		if err := rows.Scan(&bucketIndex, &count); err != nil {
			return ActivityCounts{}, fmt.Errorf("scan activity row: %w", err)
		}
		if bucketIndex >= 0 && bucketIndex < buckets {
			counts[bucketIndex] = count
			if count > maxVal {
				maxVal = count
			}
		}
	}
	if err := rows.Err(); err != nil {
		return ActivityCounts{}, fmt.Errorf("activity rows iteration: %w", err)
	}

	// Fetch mentions for this user in this channel within window
	mentionQuery := `
		SELECT m.id, m.created_at
		FROM mentions men
		JOIN messages m ON m.id = men.message_id
		WHERE men.user_id = ? AND men.channel_id = ? AND m.created_at >= ? AND m.created_at < ? AND m.deleted_at IS NULL
		ORDER BY m.id ASC`

	menRows, err := s.reader.QueryContext(ctx, mentionQuery, userID, channelID, from, to)
	if err != nil {
		return ActivityCounts{}, fmt.Errorf("query activity mentions: %w", err)
	}
	defer menRows.Close()

	var mentions []MentionHit
	for menRows.Next() {
		var msgID, createdAt int64
		if err := menRows.Scan(&msgID, &createdAt); err != nil {
			return ActivityCounts{}, fmt.Errorf("scan activity mention: %w", err)
		}
		bucketIndex := (createdAt - from) / bucketMS
		if bucketIndex >= 0 && bucketIndex < buckets {
			mentions = append(mentions, MentionHit{
				Bucket:    bucketIndex,
				MessageID: msgID,
			})
		}
	}

	// Fetch unread boundary
	var lastReadMsgID int64
	err = s.reader.QueryRowContext(ctx, `
		SELECT last_read_message_id
		FROM channel_members
		WHERE channel_id = ? AND user_id = ?`, channelID, userID).Scan(&lastReadMsgID)
	
	var unreadBoundary *UnreadHit
	if err == nil && lastReadMsgID > 0 {
		var nextUnreadMsgID, nextUnreadCreatedAt int64
		err = s.reader.QueryRowContext(ctx, `
			SELECT id, created_at FROM messages
			WHERE channel_id = ? AND id > ? AND created_at >= ? AND created_at < ? AND deleted_at IS NULL
			ORDER BY id ASC LIMIT 1`, channelID, lastReadMsgID, from, to).Scan(&nextUnreadMsgID, &nextUnreadCreatedAt)
		if err == nil {
			bucketIndex := (nextUnreadCreatedAt - from) / bucketMS
			if bucketIndex >= 0 && bucketIndex < buckets {
				unreadBoundary = &UnreadHit{
					Bucket:    bucketIndex,
					MessageID: nextUnreadMsgID,
				}
			}
		}
	}

	return ActivityCounts{
		From:           from,
		To:             to,
		BucketMS:       bucketMS,
		Counts:         counts,
		Mentions:       mentions,
		UnreadBoundary: unreadBoundary,
		Max:            maxVal,
	}, nil
}
