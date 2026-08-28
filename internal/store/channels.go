package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"unicode/utf8"
)

var slugPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{1,31}$`)
var ErrNotFound = errors.New("not found")

// Channel represents a public channel, private channel, or DM.
type Channel struct {
	ID            int64   `json:"id"`
	Kind          string  `json:"kind"`
	Slug          *string `json:"slug"`
	Name          string  `json:"name"`
	Topic         string  `json:"topic"`
	DMKey         *string `json:"dm_key"`
	CreatedBy     *int64  `json:"created_by"`
	ArchivedAt    *int64  `json:"archived_at"`
	LastMessageID *int64  `json:"last_message_id"`
	CreatedAt     int64   `json:"created_at"`
	UpdatedAt     int64   `json:"updated_at"`
}

// ChannelDetails extends Channel with visible info for a specific user.
type ChannelDetails struct {
	Channel
	MemberCount       int   `json:"member_count"`
	LastReadMessageID int64 `json:"last_read_message_id"`
	Joined            bool  `json:"joined"`
	Peer              *User `json:"peer,omitempty"`
}

// ChannelMember represents a user's membership in a channel.
type ChannelMember struct {
	ChannelID         int64  `json:"channel_id"`
	UserID            int64  `json:"user_id"`
	Role              string `json:"role"`
	JoinedAt          int64  `json:"joined_at"`
	LastReadMessageID int64  `json:"last_read_message_id"`
	Muted             bool   `json:"muted"`
}

// ChannelAccess carries the permissions resolved for a user on a channel.
type ChannelAccess struct {
	CanRead bool
	CanPost bool
	IsOwner bool
	Kind    string
}

// ValidateSlug validates and normalizes a channel slug.
func ValidateSlug(v string) (string, error) {
	v = strings.ToLower(strings.TrimSpace(v))
	if !slugPattern.MatchString(v) {
		return "", fmt.Errorf("slug must be 2-32 lowercase letters, numbers, or '-'")
	}
	return v, nil
}

// CreateChannel creates a channel and joins the creator as owner.
func (s *Store) CreateChannel(ctx context.Context, kind, slug, name, topic string, createdBy int64, memberIDs []int64) (Channel, error) {
	if kind != "public" && kind != "private" && kind != "dm" {
		return Channel{}, fmt.Errorf("invalid channel kind: %s", kind)
	}

	var normSlug *string
	if kind != "dm" {
		val, err := ValidateSlug(slug)
		if err != nil {
			return Channel{}, err
		}
		normSlug = &val
		if name == "" {
			name = val
		}
	} else {
		if slug != "" {
			return Channel{}, fmt.Errorf("DMs cannot have a slug")
		}
	}

	if utf8.RuneCountInString(topic) > 250 {
		return Channel{}, fmt.Errorf("topic exceeds 250 characters")
	}

	var dmKey *string
	if kind == "dm" {
		if len(memberIDs) < 1 || len(memberIDs) > 2 {
			return Channel{}, fmt.Errorf("DM requires 1 or 2 members")
		}
		var a, b int64
		if len(memberIDs) == 1 {
			a, b = memberIDs[0], memberIDs[0]
		} else {
			a, b = memberIDs[0], memberIDs[1]
		}
		key := dmKeyVal(a, b)
		dmKey = &key
	}

	now := nowMillis()
	var channel Channel

	err := s.Tx(ctx, func(tx *sql.Tx) error {
		var r sql.Result
		var err error
		if kind == "dm" {
			r, err = tx.ExecContext(ctx, `
				INSERT INTO channels(kind, slug, name, topic, dm_key, created_by, created_at, updated_at)
				VALUES (?, NULL, '', '', ?, ?, ?, ?)`,
				kind, dmKey, createdBy, now, now)
		} else {
			r, err = tx.ExecContext(ctx, `
				INSERT INTO channels(kind, slug, name, topic, dm_key, created_by, created_at, updated_at)
				VALUES (?, ?, ?, ?, NULL, ?, ?, ?)`,
				kind, normSlug, name, topic, createdBy, now, now)
		}
		if err != nil {
			return fmt.Errorf("insert channel: %w", err)
		}

		id, err := r.LastInsertId()
		if err != nil {
			return fmt.Errorf("get channel id: %w", err)
		}

		// Insert creator as owner
		_, err = tx.ExecContext(ctx, `
			INSERT INTO channel_members(channel_id, user_id, role, joined_at)
			VALUES (?, ?, 'owner', ?)`,
			id, createdBy, now)
		if err != nil {
			return fmt.Errorf("insert channel creator member: %w", err)
		}

		// Insert other members
		seen := map[int64]bool{createdBy: true}
		for _, uid := range memberIDs {
			if seen[uid] {
				continue
			}
			seen[uid] = true
			_, err = tx.ExecContext(ctx, `
				INSERT INTO channel_members(channel_id, user_id, role, joined_at)
				VALUES (?, ?, 'member', ?)`,
				id, uid, now)
			if err != nil {
				return fmt.Errorf("insert channel member %d: %w", uid, err)
			}
		}

		// Fetch the inserted channel
		channel, err = getChannelTx(ctx, tx, id)
		return err
	})

	if err != nil {
		return Channel{}, err
	}
	return channel, nil
}

func dmKeyVal(a, b int64) string {
	if a <= b {
		return fmt.Sprintf("%d:%d", a, b)
	}
	return fmt.Sprintf("%d:%d", b, a)
}

func getChannelTx(ctx context.Context, tx *sql.Tx, id int64) (Channel, error) {
	row := tx.QueryRowContext(ctx, `
		SELECT id, kind, slug, name, topic, dm_key, created_by, archived_at, last_message_id, created_at, updated_at
		FROM channels WHERE id = ?`, id)
	return scanChannel(row)
}

func scanChannel(row interface{ Scan(...any) error }) (Channel, error) {
	var c Channel
	var archivedAt, lastMessageID sql.NullInt64
	err := row.Scan(&c.ID, &c.Kind, &c.Slug, &c.Name, &c.Topic, &c.DMKey, &c.CreatedBy, &archivedAt, &lastMessageID, &c.CreatedAt, &c.UpdatedAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Channel{}, ErrNotFound
		}
		return Channel{}, err
	}
	if archivedAt.Valid {
		c.ArchivedAt = &archivedAt.Int64
	}
	if lastMessageID.Valid {
		c.LastMessageID = &lastMessageID.Int64
	}
	return c, nil
}

// GetChannel fetches a channel by ID.
func (s *Store) GetChannel(ctx context.Context, id int64) (Channel, error) {
	row := s.reader.QueryRowContext(ctx, `
		SELECT id, kind, slug, name, topic, dm_key, created_by, archived_at, last_message_id, created_at, updated_at
		FROM channels WHERE id = ?`, id)
	return scanChannel(row)
}

// GetChannelBySlug fetches a channel by slug (case-insensitive).
func (s *Store) GetChannelBySlug(ctx context.Context, slug string) (Channel, error) {
	row := s.reader.QueryRowContext(ctx, `
		SELECT id, kind, slug, name, topic, dm_key, created_by, archived_at, last_message_id, created_at, updated_at
		FROM channels WHERE slug = ? COLLATE NOCASE`, slug)
	return scanChannel(row)
}

// UpdateChannel updates a channel's name and topic.
func (s *Store) UpdateChannel(ctx context.Context, id int64, name, topic string) error {
	if utf8.RuneCountInString(topic) > 250 {
		return fmt.Errorf("topic exceeds 250 characters")
	}
	res, err := s.writer.ExecContext(ctx, `
		UPDATE channels SET name = ?, topic = ?, updated_at = ?
		WHERE id = ?`, name, topic, nowMillis(), id)
	if err != nil {
		return fmt.Errorf("update channel: %w", err)
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

// ArchiveChannel archives a channel.
func (s *Store) ArchiveChannel(ctx context.Context, id int64) error {
	res, err := s.writer.ExecContext(ctx, `
		UPDATE channels SET archived_at = ?, updated_at = ?
		WHERE id = ? AND archived_at IS NULL`, nowMillis(), nowMillis(), id)
	if err != nil {
		return fmt.Errorf("archive channel: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		// Check if it exists at all
		var exists int
		_ = s.reader.QueryRowContext(ctx, "SELECT 1 FROM channels WHERE id = ?", id).Scan(&exists)
		if exists == 0 {
			return ErrNotFound
		}
	}
	return nil
}

// UnarchiveChannel unarchives a channel.
func (s *Store) UnarchiveChannel(ctx context.Context, id int64) error {
	res, err := s.writer.ExecContext(ctx, `
		UPDATE channels SET archived_at = NULL, updated_at = ?
		WHERE id = ? AND archived_at IS NOT NULL`, nowMillis(), id)
	if err != nil {
		return fmt.Errorf("unarchive channel: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		// Check if it exists at all
		var exists int
		_ = s.reader.QueryRowContext(ctx, "SELECT 1 FROM channels WHERE id = ?", id).Scan(&exists)
		if exists == 0 {
			return ErrNotFound
		}
	}
	return nil
}

// AddMembers adds users to a channel.
func (s *Store) AddMembers(ctx context.Context, channelID int64, userIDs []int64) error {
	now := nowMillis()
	return s.Tx(ctx, func(tx *sql.Tx) error {
		for _, uid := range userIDs {
			_, err := tx.ExecContext(ctx, `
				INSERT INTO channel_members(channel_id, user_id, role, joined_at)
				VALUES (?, ?, 'member', ?)
				ON CONFLICT(channel_id, user_id) DO NOTHING`,
				channelID, uid, now)
			if err != nil {
				return err
			}
		}
		return nil
	})
}

// RemoveMember removes a user from a channel.
func (s *Store) RemoveMember(ctx context.Context, channelID int64, userID int64) error {
	return s.Tx(ctx, func(tx *sql.Tx) error {
		// Check if user is sole owner
		var role string
		err := tx.QueryRowContext(ctx, "SELECT role FROM channel_members WHERE channel_id = ? AND user_id = ?", channelID, userID).Scan(&role)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return ErrNotFound
			}
			return err
		}

		if role == "owner" {
			var ownerCount int
			err = tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM channel_members WHERE channel_id = ? AND role = 'owner'", channelID).Scan(&ownerCount)
			if err != nil {
				return err
			}
			if ownerCount <= 1 {
				return fmt.Errorf("cannot remove sole owner from channel")
			}
		}

		_, err = tx.ExecContext(ctx, "DELETE FROM channel_members WHERE channel_id = ? AND user_id = ?", channelID, userID)
		return err
	})
}

// ListMembers lists members of a channel.
func (s *Store) ListMembers(ctx context.Context, channelID int64) ([]User, error) {
	rows, err := s.reader.QueryContext(ctx, `
		SELECT u.id, u.username, u.email, u.display_name, u.password_hash, u.avatar_color, u.role, u.is_bot, u.status, u.created_at, u.updated_at
		FROM channel_members cm
		JOIN users u ON cm.user_id = u.id
		WHERE cm.channel_id = ?
		ORDER BY u.username COLLATE NOCASE`, channelID)
	if err != nil {
		return nil, err
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

// IsMember checks if a user is a member of a channel.
func (s *Store) IsMember(ctx context.Context, channelID, userID int64) (bool, error) {
	var exists int
	err := s.reader.QueryRowContext(ctx, "SELECT 1 FROM channel_members WHERE channel_id = ? AND user_id = ?", channelID, userID).Scan(&exists)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

// CanAccessChannel checks channel access permissions for a user.
func (s *Store) CanAccessChannel(ctx context.Context, userID, channelID int64) (ChannelAccess, error) {
	c, err := s.GetChannel(ctx, channelID)
	if err != nil {
		return ChannelAccess{}, err
	}

	var role string
	var joined bool
	err = s.reader.QueryRowContext(ctx, "SELECT role FROM channel_members WHERE channel_id = ? AND user_id = ?", channelID, userID).Scan(&role)
	if err == nil {
		joined = true
	} else if !errors.Is(err, sql.ErrNoRows) {
		return ChannelAccess{}, err
	}

	if joined {
		return ChannelAccess{
			CanRead: true,
			CanPost: true,
			IsOwner: role == "owner",
			Kind:    c.Kind,
		}, nil
	}

	// Non-members
	if c.Kind == "public" {
		return ChannelAccess{
			CanRead: c.ArchivedAt == nil,
			CanPost: false,
			IsOwner: false,
			Kind:    c.Kind,
		}, nil
	}

	// Private channels are invisible to non-members unless they are admin (then not ErrNotFound, but CanRead/CanPost=false)
	var userRole string
	err = s.reader.QueryRowContext(ctx, "SELECT role FROM users WHERE id = ?", userID).Scan(&userRole)
	if err != nil {
		return ChannelAccess{}, err
	}

	if c.Kind == "private" {
		if userRole == "admin" {
			return ChannelAccess{
				CanRead: false,
				CanPost: false,
				IsOwner: false,
				Kind:    c.Kind,
			}, nil
		}
		return ChannelAccess{}, ErrNotFound
	}

	// DMs are completely invisible to non-participants (even admins)
	return ChannelAccess{}, ErrNotFound
}

// ListVisibleChannels returns all channels visible to the user.
// Returns all non-archived public channels + private channels where the user is a member + the user's DM channels.
func (s *Store) ListVisibleChannels(ctx context.Context, userID int64, includeArchived ...bool) ([]ChannelDetails, error) {
	publicClause := "c.kind = 'public' AND c.archived_at IS NULL"
	if len(includeArchived) > 0 && includeArchived[0] {
		publicClause = "c.kind = 'public'"
	}
	query := `
		SELECT 
			c.id, c.kind, c.slug, c.name, c.topic, c.dm_key, c.created_by, c.archived_at, c.last_message_id, c.created_at, c.updated_at,
			(SELECT COUNT(*) FROM channel_members WHERE channel_id = c.id) AS member_count,
			COALESCE(cm.last_read_message_id, 0) AS last_read_message_id,
			CASE WHEN cm.user_id IS NOT NULL THEN 1 ELSE 0 END AS joined
		FROM channels c
		LEFT JOIN channel_members cm ON c.id = cm.channel_id AND cm.user_id = ?
		WHERE (` + publicClause + `)
		   OR (c.kind = 'private' AND cm.user_id IS NOT NULL)
		   OR (c.kind = 'dm' AND cm.user_id IS NOT NULL)
		ORDER BY 
			CASE WHEN c.kind = 'dm' THEN 1 ELSE 0 END, 
			COALESCE(c.slug, c.name) COLLATE NOCASE`

	rows, err := s.reader.QueryContext(ctx, query, userID)
	if err != nil {
		return nil, fmt.Errorf("list visible channels: %w", err)
	}
	defer rows.Close()

	var list []ChannelDetails
	for rows.Next() {
		var det ChannelDetails
		var archivedAt, lastMessageID sql.NullInt64
		var joinedInt int
		err := rows.Scan(
			&det.ID, &det.Kind, &det.Slug, &det.Name, &det.Topic, &det.DMKey, &det.CreatedBy, &archivedAt, &lastMessageID, &det.CreatedAt, &det.UpdatedAt,
			&det.MemberCount, &det.LastReadMessageID, &joinedInt,
		)
		if err != nil {
			return nil, fmt.Errorf("scan visible channel: %w", err)
		}
		if archivedAt.Valid {
			det.ArchivedAt = &archivedAt.Int64
		}
		if lastMessageID.Valid {
			det.LastMessageID = &lastMessageID.Int64
		}
		det.Joined = joinedInt != 0
		list = append(list, det)
	}
	return list, rows.Err()
}

// ListJoinableChannels returns public, non-archived channels the user has not joined.
func (s *Store) ListJoinableChannels(ctx context.Context, userID int64) ([]ChannelDetails, error) {
	rows, err := s.reader.QueryContext(ctx, `SELECT c.id,c.kind,c.slug,c.name,c.topic,c.dm_key,c.created_by,c.archived_at,c.last_message_id,c.created_at,c.updated_at,(SELECT COUNT(*) FROM channel_members WHERE channel_id=c.id),0,0 FROM channels c WHERE c.kind='public' AND c.archived_at IS NULL AND NOT EXISTS (SELECT 1 FROM channel_members m WHERE m.channel_id=c.id AND m.user_id=?) ORDER BY c.slug COLLATE NOCASE`, userID)
	if err != nil {
		return nil, fmt.Errorf("list joinable channels: %w", err)
	}
	defer rows.Close()
	var out []ChannelDetails
	for rows.Next() {
		var d ChannelDetails
		var a, l sql.NullInt64
		var joined int
		if err := rows.Scan(&d.ID, &d.Kind, &d.Slug, &d.Name, &d.Topic, &d.DMKey, &d.CreatedBy, &a, &l, &d.CreatedAt, &d.UpdatedAt, &d.MemberCount, &d.LastReadMessageID, &joined); err != nil {
			return nil, err
		}
		d.Joined = false
		if l.Valid {
			d.LastMessageID = &l.Int64
		}
		out = append(out, d)
	}
	return out, rows.Err()
}
