package store

import (
	"context"
	"fmt"
)

// CountMessagesSince counts root messages in a channel with an ID greater than the given message ID.
func (s *Store) CountMessagesSince(ctx context.Context, channelID, messageID int64) (int64, error) {
	var count int64
	query := `SELECT COUNT(*) FROM messages WHERE channel_id = ? AND id > ? AND thread_id IS NULL`
	err := s.reader.QueryRowContext(ctx, query, channelID, messageID).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("count messages since: %w", err)
	}
	return count, nil
}

// GetChannelMemberIDs returns the IDs of all members of a channel.
func (s *Store) GetChannelMemberIDs(ctx context.Context, channelID int64) ([]int64, error) {
	rows, err := s.reader.QueryContext(ctx, "SELECT user_id FROM channel_members WHERE channel_id = ?", channelID)
	if err != nil {
		return nil, fmt.Errorf("get channel member ids: %w", err)
	}
	defer rows.Close()

	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("scan channel member id: %w", err)
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}
