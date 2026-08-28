package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sort"
)

// ErrTooManyParticipants is returned when a DM is requested with more than the allowed
// maximum number of participants (8, matching the realtime hub's per-user connection cap).
var ErrTooManyParticipants = errors.New("too many participants")

const maxDMParticipants = 8

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

// GetOrCreateGroupDM gets or creates a group DM channel among 3+ users, deduping on the same
// dm_key trick GetOrCreateDM uses for 1:1 DMs. Two concurrent requests with the same
// participant set (in any order) must converge on exactly one channel row.
func (s *Store) GetOrCreateGroupDM(ctx context.Context, userIDs []int64) (ChannelDetails, error) {
	unique := DedupeInt64(userIDs)
	switch {
	case len(unique) == 0:
		return ChannelDetails{}, fmt.Errorf("group dm requires at least one participant")
	case len(unique) == 1:
		return s.GetOrCreateDM(ctx, unique[0], unique[0])
	case len(unique) == 2:
		return s.GetOrCreateDM(ctx, unique[0], unique[1])
	case len(unique) > maxDMParticipants:
		return ChannelDetails{}, ErrTooManyParticipants
	}

	key := dmKeyValN(unique)
	now := nowMillis()
	var channel Channel

	err := s.Tx(ctx, func(tx *sql.Tx) error {
		r, err := tx.ExecContext(ctx, `
			INSERT INTO channels(kind, slug, name, topic, dm_key, created_by, created_at, updated_at)
			VALUES ('group_dm', NULL, '', '', ?, NULL, ?, ?)`,
			key, now, now)

		if err != nil {
			var existID int64
			errSelect := tx.QueryRowContext(ctx, "SELECT id FROM channels WHERE dm_key = ?", key).Scan(&existID)
			if errSelect != nil {
				return fmt.Errorf("insert group dm channel failed: %w", err)
			}
			channel, err = getChannelTx(ctx, tx, existID)
			return err
		}

		id, err := r.LastInsertId()
		if err != nil {
			return err
		}

		for _, uid := range unique {
			_, err = tx.ExecContext(ctx, `
				INSERT INTO channel_members(channel_id, user_id, role, joined_at)
				VALUES (?, ?, 'member', ?)`,
				id, uid, now)
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

	members, err := s.ListMembers(ctx, channel.ID)
	if err != nil {
		return ChannelDetails{}, fmt.Errorf("list group dm members: %w", err)
	}
	caller := unique[0]
	channel.Name = computeGroupDMName(members, caller)
	var lastRead int64
	err = s.reader.QueryRowContext(ctx, `
		SELECT last_read_message_id
		FROM channel_members
		WHERE channel_id = ? AND user_id = ?`,
		channel.ID, caller).Scan(&lastRead)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return ChannelDetails{}, fmt.Errorf("get last read: %w", err)
	}

	return ChannelDetails{
		Channel:           channel,
		MemberCount:       len(members),
		LastReadMessageID: lastRead,
		Joined:            true,
		Members:           members,
	}, nil
}

// DedupeInt64 returns the unique values of ids in their original relative order. Exported
// so callers (e.g. the API layer, validating participant lists before they reach the store)
// share this exact dedup logic instead of reimplementing it.
func DedupeInt64(ids []int64) []int64 {
	seen := make(map[int64]struct{}, len(ids))
	out := make([]int64, 0, len(ids))
	for _, id := range ids {
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	return out
}

// computeGroupDMName joins member display names for a group DM's synthesized name
// (e.g. "Bruce, Hugo, +2"), never stored in channels.name. excludeUserID, if nonzero,
// omits that member (the caller) from the name the same way chat products name a
// group conversation from the other participants' perspective; pass 0 to include everyone.
func computeGroupDMName(members []User, excludeUserID int64) string {
	names := make([]string, 0, len(members))
	for _, m := range members {
		if excludeUserID != 0 && m.ID == excludeUserID {
			continue
		}
		name := m.DisplayName
		if name == "" {
			name = m.Username
		}
		names = append(names, name)
	}
	sort.Strings(names)
	const shown = 3
	if len(names) <= shown {
		return joinNames(names)
	}
	return joinNames(names[:shown]) + fmt.Sprintf(", +%d", len(names)-shown)
}

func joinNames(names []string) string {
	out := ""
	for i, n := range names {
		if i > 0 {
			out += ", "
		}
		out += n
	}
	return out
}

// ListDMs returns the DM and group DM channels the user belongs to, with peer/member user
// objects inlined. It only includes conversations with at least one message OR created in
// the last hour, so opening a DM and not sending doesn't clutter the list forever. A
// conversation the caller has hidden via HideConversation is excluded until its next
// message arrives (CreateMessage clears hidden_at for every member at that point).
func (s *Store) ListDMs(ctx context.Context, userID int64) ([]ChannelDetails, error) {
	cutoffTime := nowMillis() - (60 * 60 * 1000) // 1 hour ago in milliseconds

	query := `
		SELECT
			c.id, c.kind, c.slug, c.name, c.topic, c.dm_key, c.created_by, c.archived_at, c.last_message_id, c.created_at, c.updated_at,
			(SELECT COUNT(*) FROM channel_members WHERE channel_id = c.id) AS member_count,
			cm.last_read_message_id
		FROM channels c
		JOIN channel_members cm ON c.id = cm.channel_id AND cm.user_id = ?
		WHERE c.kind IN ('dm', 'group_dm')
		  AND cm.hidden_at IS NULL
		  AND (c.last_message_id IS NOT NULL OR c.created_at > ?)
		ORDER BY c.last_message_id DESC, c.created_at DESC`

	rows, err := s.reader.QueryContext(ctx, query, userID, cutoffTime)
	if err != nil {
		return nil, fmt.Errorf("list dms: %w", err)
	}

	var list []ChannelDetails
	var groupChannelIDs []int64
	for rows.Next() {
		var det ChannelDetails
		var archivedAt, lastMessageID sql.NullInt64
		err := rows.Scan(
			&det.ID, &det.Kind, &det.Slug, &det.Name, &det.Topic, &det.DMKey, &det.CreatedBy, &archivedAt, &lastMessageID, &det.CreatedAt, &det.UpdatedAt,
			&det.MemberCount, &det.LastReadMessageID,
		)
		if err != nil {
			rows.Close()
			return nil, fmt.Errorf("scan dm: %w", err)
		}
		if archivedAt.Valid {
			det.ArchivedAt = &archivedAt.Int64
		}
		if lastMessageID.Valid {
			det.LastMessageID = &lastMessageID.Int64
		}
		det.Joined = true
		if det.Kind == "group_dm" {
			groupChannelIDs = append(groupChannelIDs, det.ID)
		}
		list = append(list, det)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, fmt.Errorf("list dms rows: %w", err)
	}
	rows.Close()

	if len(list) == 0 {
		return list, nil
	}

	// Batch-hydrate group DM members with one extra query — no N+1.
	membersByChannel := make(map[int64][]User, len(groupChannelIDs))
	if len(groupChannelIDs) > 0 {
		placeholders := make([]string, len(groupChannelIDs))
		args := make([]any, len(groupChannelIDs))
		for i, id := range groupChannelIDs {
			placeholders[i] = "?"
			args[i] = id
		}
		q := fmt.Sprintf(`
			SELECT cm.channel_id, u.id, u.username, u.email, u.display_name, u.password_hash, u.avatar_color, u.role, u.is_bot, u.status, u.created_at, u.updated_at
			FROM channel_members cm
			JOIN users u ON u.id = cm.user_id
			WHERE cm.channel_id IN (%s)
			ORDER BY u.username COLLATE NOCASE`, joinPlaceholders(placeholders))
		mrows, err := s.reader.QueryContext(ctx, q, args...)
		if err != nil {
			return nil, fmt.Errorf("hydrate group dm members: %w", err)
		}
		for mrows.Next() {
			var chID int64
			u, err := scanUserWithChannel(mrows, &chID)
			if err != nil {
				mrows.Close()
				return nil, err
			}
			u.PasswordHash = ""
			membersByChannel[chID] = append(membersByChannel[chID], u)
		}
		if err := mrows.Err(); err != nil {
			mrows.Close()
			return nil, err
		}
		mrows.Close()
	}

	// Fetch every member of every 1:1 DM in one extra query, then pick the non-caller
	// member per channel (or the sole member, for a self-DM) — no N+1.
	dmMembersByChannel := make(map[int64][]User)
	var dmChannelIDs []int64
	for _, d := range list {
		if d.Kind == "dm" {
			dmChannelIDs = append(dmChannelIDs, d.ID)
		}
	}
	if len(dmChannelIDs) > 0 {
		placeholders := make([]string, len(dmChannelIDs))
		args := make([]any, len(dmChannelIDs))
		for i, id := range dmChannelIDs {
			placeholders[i] = "?"
			args[i] = id
		}
		q := fmt.Sprintf(`
			SELECT cm.channel_id, u.id, u.username, u.email, u.display_name, u.password_hash, u.avatar_color, u.role, u.is_bot, u.status, u.created_at, u.updated_at
			FROM channel_members cm
			JOIN users u ON u.id = cm.user_id
			WHERE cm.channel_id IN (%s)`, joinPlaceholders(placeholders))
		prows, err := s.reader.QueryContext(ctx, q, args...)
		if err != nil {
			return nil, fmt.Errorf("hydrate dm peers: %w", err)
		}
		for prows.Next() {
			var chID int64
			u, err := scanUserWithChannel(prows, &chID)
			if err != nil {
				prows.Close()
				return nil, err
			}
			u.PasswordHash = ""
			dmMembersByChannel[chID] = append(dmMembersByChannel[chID], u)
		}
		if err := prows.Err(); err != nil {
			prows.Close()
			return nil, err
		}
		prows.Close()
	}

	for i := range list {
		switch list[i].Kind {
		case "group_dm":
			members := membersByChannel[list[i].ID]
			list[i].Members = members
			list[i].Name = computeGroupDMName(members, userID)
		case "dm":
			members := dmMembersByChannel[list[i].ID]
			var peer *User
			for j := range members {
				if members[j].ID != userID {
					peer = &members[j]
					break
				}
			}
			if peer == nil && len(members) > 0 {
				peer = &members[0] // self-DM: the only member is both caller and peer
			}
			list[i].Peer = peer
		}
	}

	return list, nil
}

// RecentDMPartners returns users the caller already has a 1:1 DM with, ordered by that DM's
// last_message_id descending. Backs the New Message modal's "Suggested / Recent" section.
func (s *Store) RecentDMPartners(ctx context.Context, userID int64, limit int) ([]User, error) {
	if limit <= 0 || limit > 50 {
		limit = 20
	}
	rows, err := s.reader.QueryContext(ctx, `
		SELECT u.id, u.username, u.email, u.display_name, u.password_hash, u.avatar_color, u.role, u.is_bot, u.status, u.created_at, u.updated_at
		FROM channels c
		JOIN channel_members mine ON mine.channel_id = c.id AND mine.user_id = ?
		JOIN channel_members other ON other.channel_id = c.id AND other.user_id != ?
		JOIN users u ON u.id = other.user_id
		WHERE c.kind = 'dm'
		ORDER BY c.last_message_id DESC
		LIMIT ?`, userID, userID, limit)
	if err != nil {
		return nil, fmt.Errorf("recent dm partners: %w", err)
	}
	defer rows.Close()

	var users []User
	for rows.Next() {
		u, err := scanUser(rows)
		if err != nil {
			return nil, err
		}
		u.PasswordHash = ""
		users = append(users, u)
	}
	return users, rows.Err()
}

// HideConversation removes a DM or group DM from the caller's sidebar without touching
// membership or any message data: it stamps that user's channel_members row with
// hidden_at. The conversation reappears automatically the next time a message is posted
// to it (CreateMessage clears hidden_at for every member), and it never affects the other
// participants' view. Returns ErrNotFound if the caller isn't a member of a dm/group_dm
// channel with that id.
func (s *Store) HideConversation(ctx context.Context, userID, channelID int64) error {
	var kind string
	err := s.reader.QueryRowContext(ctx, "SELECT kind FROM channels WHERE id = ?", channelID).Scan(&kind)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		return fmt.Errorf("get channel kind: %w", err)
	}
	if kind != "dm" && kind != "group_dm" {
		return ErrNotFound
	}

	res, err := s.writer.ExecContext(ctx, `
		UPDATE channel_members SET hidden_at = ?
		WHERE channel_id = ? AND user_id = ?`,
		nowMillis(), channelID, userID)
	if err != nil {
		return fmt.Errorf("hide conversation: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

func joinPlaceholders(ph []string) string {
	out := ""
	for i, p := range ph {
		if i > 0 {
			out += ","
		}
		out += p
	}
	return out
}

// scanUserWithChannel scans a row shaped (channel_id, <user columns...>) into a channel id
// and a User, matching the column order used by the batch-hydration queries above.
func scanUserWithChannel(rows *sql.Rows, chID *int64) (User, error) {
	var u User
	var bot int
	err := rows.Scan(chID, &u.ID, &u.Username, &u.Email, &u.DisplayName, &u.PasswordHash, &u.AvatarColor, &u.Role, &bot, &u.Status, &u.CreatedAt, &u.UpdatedAt)
	if err != nil {
		return User{}, fmt.Errorf("scan user with channel: %w", err)
	}
	u.IsBot = bot != 0
	return u, nil
}
