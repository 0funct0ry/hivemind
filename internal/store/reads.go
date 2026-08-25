package store

import (
	"context"
	"fmt"
)

// UnreadItem represents the badge state (unread and mention counts) for a user in a specific channel.
type UnreadItem struct {
	ChannelID         int64 `json:"channel_id"`
	UnreadCount       int   `json:"unread_count"`
	MentionCount      int   `json:"mention_count"`
	LastMessageID     int64 `json:"last_message_id"`
	LastReadMessageID int64 `json:"last_read_message_id"`
	Joined            bool  `json:"joined"`
}

// MarkRead updates the last read message ID for a user in a channel.
// It sets last_read_message_id to the MAX of its current value and the new messageID.
func (s *Store) MarkRead(ctx context.Context, userID, channelID, messageID int64) error {
	_, err := s.writer.ExecContext(ctx, `
		UPDATE channel_members 
		SET last_read_message_id = MAX(last_read_message_id, ?) 
		WHERE channel_id = ? AND user_id = ?`,
		messageID, channelID, userID)
	if err != nil {
		return fmt.Errorf("mark read: %w", err)
	}
	return nil
}

// UnreadSummary returns the unread and mention badge state for all visible channels of a user.
func (s *Store) UnreadSummary(ctx context.Context, userID int64) ([]UnreadItem, error) {
	query := `
		SELECT 
			c.id,
			COALESCE(c.last_message_id, 0) AS last_message_id,
			COALESCE(cm.last_read_message_id, 0) AS last_read_message_id,
			CASE WHEN cm.user_id IS NOT NULL THEN 1 ELSE 0 END AS joined,
			CASE 
				WHEN cm.user_id IS NULL THEN 0
				ELSE (
					SELECT COUNT(*) 
					FROM messages m 
					WHERE m.channel_id = c.id 
					  AND m.id > COALESCE(cm.last_read_message_id, 0) 
					  AND (m.thread_id IS NULL OR m.broadcast = 1)
				)
			END AS unread_count,
			CASE 
				WHEN cm.user_id IS NULL THEN 0
				ELSE (
					SELECT COUNT(*) 
					FROM mentions mn 
					WHERE mn.user_id = ? 
					  AND mn.channel_id = c.id 
					  AND mn.message_id > COALESCE(cm.last_read_message_id, 0)
				)
			END AS mention_count
		FROM channels c
		LEFT JOIN channel_members cm ON c.id = cm.channel_id AND cm.user_id = ?
		WHERE (c.kind = 'public' AND c.archived_at IS NULL)
		   OR (c.kind = 'private' AND cm.user_id IS NOT NULL)
		   OR (c.kind = 'dm' AND cm.user_id IS NOT NULL)`

	rows, err := s.reader.QueryContext(ctx, query, userID, userID)
	if err != nil {
		return nil, fmt.Errorf("unread summary query: %w", err)
	}
	defer rows.Close()

	var summary []UnreadItem
	for rows.Next() {
		var item UnreadItem
		var joinedInt int
		err := rows.Scan(
			&item.ChannelID,
			&item.LastMessageID,
			&item.LastReadMessageID,
			&joinedInt,
			&item.UnreadCount,
			&item.MentionCount,
		)
		if err != nil {
			return nil, fmt.Errorf("scan unread item: %w", err)
		}
		item.Joined = joinedInt != 0
		summary = append(summary, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("unread summary rows: %w", err)
	}

	return summary, nil
}
