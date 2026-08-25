package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

// GetOrCreateDM gets or creates a DM channel between two users.
// It inserts a new channel with a unique dm_key and, on constraint violation, selects the existing channel.
func (s *Store) GetOrCreateDM(ctx context.Context, a, b int64) (ChannelDetails, error) {
	key := dmKeyVal(a, b)
	now := nowMillis()
	var channel Channel

	err := s.Tx(ctx, func(tx *sql.Tx) error {
		r, err := tx.ExecContext(ctx, `
			INSERT INTO channels(kind, slug, name, topic, dm_key, created_by, created_at, updated_at)
			VALUES ('dm', NULL, '', '', ?, NULL, ?, ?)`,
			key, now, now)

		if err != nil {
			// If insertion failed, retrieve the existing channel by dm_key.
			var existID int64
			errSelect := tx.QueryRowContext(ctx, "SELECT id FROM channels WHERE dm_key = ?", key).Scan(&existID)
			if errSelect != nil {
				return fmt.Errorf("insert dm channel failed: %w", err)
			}
			channel, err = getChannelTx(ctx, tx, existID)
			return err
		}

		id, err := r.LastInsertId()
		if err != nil {
			return err
		}

		// Insert memberships
		_, err = tx.ExecContext(ctx, `
			INSERT INTO channel_members(channel_id, user_id, role, joined_at)
			VALUES (?, ?, 'member', ?)`,
			id, a, now)
		if err != nil {
			return err
		}

		if a != b {
			_, err = tx.ExecContext(ctx, `
				INSERT INTO channel_members(channel_id, user_id, role, joined_at)
				VALUES (?, ?, 'member', ?)`,
				id, b, now)
			if err != nil {
				return err
			}
		}

		channel, err = getChannelTx(ctx, tx, id)
		return err
	})

	if err != nil {
		return ChannelDetails{}, err
	}

	// Fetch peer details
	peerID := b
	if a == b {
		peerID = a
	}
	peerUser, err := s.GetUserByID(ctx, peerID)
	if err != nil {
		return ChannelDetails{}, fmt.Errorf("get peer user: %w", err)
	}
	peerUser.PasswordHash = ""

	// Fetch last_read_message_id for user a (the caller)
	var lastRead int64
	err = s.reader.QueryRowContext(ctx, `
		SELECT last_read_message_id 
		FROM channel_members 
		WHERE channel_id = ? AND user_id = ?`,
		channel.ID, a).Scan(&lastRead)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return ChannelDetails{}, fmt.Errorf("get last read: %w", err)
	}

	memberCount := 2
	if a == b {
		memberCount = 1
	}

	return ChannelDetails{
		Channel:           channel,
		MemberCount:       memberCount,
		LastReadMessageID: lastRead,
		Joined:            true,
		Peer:              &peerUser,
	}, nil
}

// ListDMs returns the DM channels the user belongs to, with peer user objects inlined.
// It only includes DMs with at least one message OR created in the last hour.
func (s *Store) ListDMs(ctx context.Context, userID int64) ([]ChannelDetails, error) {
	cutoffTime := nowMillis() - (60 * 60 * 1000) // 1 hour ago in milliseconds

	query := `
		SELECT 
			c.id, c.kind, c.slug, c.name, c.topic, c.dm_key, c.created_by, c.archived_at, c.last_message_id, c.created_at, c.updated_at,
			(SELECT COUNT(*) FROM channel_members WHERE channel_id = c.id) AS member_count,
			cm.last_read_message_id,
			u.id, u.username, u.email, u.display_name, u.password_hash, u.avatar_color, u.role, u.is_bot, u.status, u.created_at, u.updated_at
		FROM channels c
		JOIN channel_members cm ON c.id = cm.channel_id AND cm.user_id = ?
		JOIN users u ON u.id = COALESCE(
			(SELECT user_id FROM channel_members WHERE channel_id = c.id AND user_id != ? LIMIT 1),
			?
		)
		WHERE c.kind = 'dm'
		  AND (c.last_message_id IS NOT NULL OR c.created_at > ?)
		ORDER BY c.last_message_id DESC, c.created_at DESC`

	rows, err := s.reader.QueryContext(ctx, query, userID, userID, userID, cutoffTime)
	if err != nil {
		return nil, fmt.Errorf("list dms: %w", err)
	}
	defer rows.Close()

	var list []ChannelDetails
	for rows.Next() {
		var det ChannelDetails
		var peer User
		var archivedAt, lastMessageID sql.NullInt64
		var bot int
		err := rows.Scan(
			&det.ID, &det.Kind, &det.Slug, &det.Name, &det.Topic, &det.DMKey, &det.CreatedBy, &archivedAt, &lastMessageID, &det.CreatedAt, &det.UpdatedAt,
			&det.MemberCount, &det.LastReadMessageID,
			&peer.ID, &peer.Username, &peer.Email, &peer.DisplayName, &peer.PasswordHash, &peer.AvatarColor, &peer.Role, &bot, &peer.Status, &peer.CreatedAt, &peer.UpdatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("scan dm: %w", err)
		}
		if archivedAt.Valid {
			det.ArchivedAt = &archivedAt.Int64
		}
		if lastMessageID.Valid {
			det.LastMessageID = &lastMessageID.Int64
		}
		peer.IsBot = bot != 0
		peer.PasswordHash = "" // Redact password hash
		det.Joined = true
		det.Peer = &peer
		list = append(list, det)
	}
	return list, rows.Err()
}
