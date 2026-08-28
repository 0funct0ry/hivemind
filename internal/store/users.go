package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"hash/fnv"
	"net/mail"
	"regexp"
	"strings"
)

var usernamePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{1,31}$`)

// AvatarPalette is the stable palette used to derive user avatar colors.
var AvatarPalette = [...]string{"#0E6E60", "#2563EB", "#7C3AED", "#DB2777", "#DC2626", "#EA580C", "#CA8A04", "#65A30D", "#0891B2", "#4F46E5", "#9333EA", "#BE185D"}

// UserInput contains fields accepted when creating a user.
type UserInput struct {
	Username, Email, DisplayName, PasswordHash string
	Role                                       string
	IsBot                                      bool
}

// ValidateUsername validates and normalizes a username.
func ValidateUsername(v string) (string, error) {
	v = strings.ToLower(strings.TrimSpace(v))
	if !usernamePattern.MatchString(v) {
		return "", fmt.Errorf("username must be 2-32 lowercase letters, numbers, '.', '_' or '-'")
	}
	return v, nil
}

// ValidateEmail validates and normalizes an email address.
func ValidateEmail(v string) (string, error) {
	v = strings.TrimSpace(v)
	a, err := mail.ParseAddress(v)
	if err != nil || a.Address != v || !strings.Contains(a.Address, "@") {
		return "", fmt.Errorf("email is invalid")
	}
	return strings.ToLower(v), nil
}

// AvatarColor derives a stable palette color from a username.
func AvatarColor(username string) string {
	h := fnv.New32a()
	_, _ = h.Write([]byte(username))
	return AvatarPalette[h.Sum32()%uint32(len(AvatarPalette))]
}

// CreateUser validates and inserts a user.
func (s *Store) CreateUser(ctx context.Context, in UserInput) (User, error) {
	u, err := ValidateUsername(in.Username)
	if err != nil {
		return User{}, err
	}
	e, err := ValidateEmail(in.Email)
	if err != nil {
		return User{}, err
	}
	role := in.Role
	if role == "" {
		role = "member"
	}
	now := nowMillis()
	r, err := s.writer.ExecContext(ctx, "INSERT INTO users(username,email,display_name,password_hash,avatar_color,role,is_bot,status,created_at,updated_at) VALUES (?,?,?,?,?,?,?,?,?,?)", u, e, in.DisplayName, in.PasswordHash, AvatarColor(u), role, boolInt(in.IsBot), "active", now, now)
	if err != nil {
		return User{}, fmt.Errorf("create user: %w", err)
	}
	id, err := r.LastInsertId()
	if err != nil {
		return User{}, fmt.Errorf("get created user id: %w", err)
	}

	// Auto-join/create #general
	var generalID int64
	err = s.reader.QueryRowContext(ctx, "SELECT id FROM channels WHERE slug = 'general'").Scan(&generalID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			_, err = s.CreateChannel(ctx, "public", "general", "general", "General discussion", id, []int64{id})
			if err != nil {
				return User{}, fmt.Errorf("seed general channel: %w", err)
			}
		} else {
			return User{}, fmt.Errorf("lookup general channel: %w", err)
		}
	} else {
		err = s.AddMembers(ctx, generalID, []int64{id})
		if err != nil {
			return User{}, fmt.Errorf("join general channel: %w", err)
		}
	}

	return s.GetUserByID(ctx, id)
}

func boolInt(v bool) int {
	if v {
		return 1
	}
	return 0
}

// ListUsers lists active users by username/display-name prefix.
func (s *Store) ListUsers(ctx context.Context, q string, limit int, includeDeactivated bool) ([]User, error) {
	if limit <= 0 || limit > 50 {
		limit = 50
	}
	q = strings.ToLower(q)
	status := "status='active'"
	if includeDeactivated {
		status = "1=1"
	}
	rows, err := s.reader.QueryContext(ctx, "SELECT id,username,email,display_name,password_hash,avatar_color,role,is_bot,status,created_at,updated_at FROM users WHERE "+status+" AND (username LIKE ? COLLATE NOCASE OR display_name LIKE ? COLLATE NOCASE) ORDER BY username COLLATE NOCASE LIMIT ?", q+"%", q+"%", limit)
	if err != nil {
		return nil, fmt.Errorf("list users: %w", err)
	}
	defer rows.Close()
	var out []User
	for rows.Next() {
		u, err := scanUser(rows)
		if err != nil {
			return nil, fmt.Errorf("scan user: %w", err)
		}
		u.PasswordHash = ""
		out = append(out, u)
	}
	return out, rows.Err()
}

// UpdateDisplayName updates a user's display name.
func (s *Store) UpdateDisplayName(ctx context.Context, id int64, name string) error {
	if _, err := s.writer.ExecContext(ctx, "UPDATE users SET display_name=?,updated_at=? WHERE id=?", strings.TrimSpace(name), nowMillis(), id); err != nil {
		return fmt.Errorf("update display name: %w", err)
	}
	return nil
}

// UpdateAvatar sets or clears a user's avatar file after validating the file ownership and type.
func (s *Store) UpdateAvatar(ctx context.Context, userID int64, fileID *string) error {
	if fileID != nil {
		var owner int64
		var mime string
		if err := s.reader.QueryRowContext(ctx, "SELECT uploaded_by, mime FROM files WHERE id = ?", *fileID).Scan(&owner, &mime); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return ErrNotFound
			}
			return fmt.Errorf("check avatar file: %w", err)
		}
		if owner != userID {
			return fmt.Errorf("avatar file is not owned by user")
		}
		switch mime {
		case "image/png", "image/jpeg", "image/gif", "image/webp":
		default:
			return fmt.Errorf("invalid avatar type")
		}
	}
	_, err := s.writer.ExecContext(ctx, "UPDATE users SET avatar_file_id = ?, updated_at = ? WHERE id = ?", fileID, nowMillis(), userID)
	if err != nil {
		return fmt.Errorf("update avatar: %w", err)
	}
	return nil
}

// SetPassword replaces a user's password hash.
func (s *Store) SetPassword(ctx context.Context, id int64, hash string) error {
	if _, err := s.writer.ExecContext(ctx, "UPDATE users SET password_hash=?,updated_at=? WHERE id=?", hash, nowMillis(), id); err != nil {
		return fmt.Errorf("set password: %w", err)
	}
	return nil
}

// Deactivate marks a user inactive.
func (s *Store) Deactivate(ctx context.Context, id int64) error {
	if _, err := s.writer.ExecContext(ctx, "UPDATE users SET status='deactivated',updated_at=? WHERE id=?", nowMillis(), id); err != nil {
		return fmt.Errorf("deactivate user: %w", err)
	}
	return nil
}

// Promote makes a user an administrator.
func (s *Store) Promote(ctx context.Context, id int64) error {
	if _, err := s.writer.ExecContext(ctx, "UPDATE users SET role='admin',updated_at=? WHERE id=?", nowMillis(), id); err != nil {
		return fmt.Errorf("promote user: %w", err)
	}
	return nil
}

// CountUsers returns the total number of users.
func (s *Store) CountUsers(ctx context.Context) (int64, error) {
	var n int64
	if err := s.reader.QueryRowContext(ctx, "SELECT COUNT(*) FROM users").Scan(&n); err != nil {
		return 0, fmt.Errorf("count users: %w", err)
	}
	return n, nil
}

// CountAdmins returns the number of active administrators.
func (s *Store) CountAdmins(ctx context.Context) (int64, error) {
	var n int64
	if err := s.reader.QueryRowContext(ctx, "SELECT COUNT(*) FROM users WHERE role='admin' AND status='active'").Scan(&n); err != nil {
		return 0, fmt.Errorf("count admins: %w", err)
	}
	return n, nil
}

// GetUserByUsername finds a user by case-insensitive username.
func (s *Store) GetUserByUsername(ctx context.Context, username string) (User, error) {
	return s.getUser(ctx, "WHERE username = ? COLLATE NOCASE", username)
}

// AutocompleteUsers returns active users matching a query prefix, sorted with exact-prefix matches first,
// and optionally prioritizes members of a specific channel.
func (s *Store) AutocompleteUsers(ctx context.Context, q string, channelID *int64, limit int) ([]User, error) {
	if limit <= 0 || limit > 50 {
		limit = 50
	}
	q = strings.ToLower(q)

	var query string
	var args []any

	if channelID != nil {
		query = `
			SELECT u.id, u.username, u.email, u.display_name, u.password_hash, u.avatar_color, u.role, u.is_bot, u.status, u.created_at, u.updated_at
			FROM users u
			LEFT JOIN channel_members cm ON u.id = cm.user_id AND cm.channel_id = ?
			WHERE u.status = 'active' AND (u.username LIKE ? COLLATE NOCASE OR u.display_name LIKE ? COLLATE NOCASE)
			ORDER BY 
			  (CASE WHEN u.username = ? COLLATE NOCASE OR u.display_name = ? COLLATE NOCASE THEN 1 ELSE 0 END) DESC,
			  (CASE WHEN cm.user_id IS NOT NULL THEN 1 ELSE 0 END) DESC,
			  u.username COLLATE NOCASE
			LIMIT ?`
		args = []any{*channelID, q + "%", q + "%", q, q, limit}
	} else {
		query = `
			SELECT u.id, u.username, u.email, u.display_name, u.password_hash, u.avatar_color, u.role, u.is_bot, u.status, u.created_at, u.updated_at
			FROM users u
			WHERE u.status = 'active' AND (u.username LIKE ? COLLATE NOCASE OR u.display_name LIKE ? COLLATE NOCASE)
			ORDER BY 
			  (CASE WHEN u.username = ? COLLATE NOCASE OR u.display_name = ? COLLATE NOCASE THEN 1 ELSE 0 END) DESC,
			  u.username COLLATE NOCASE
			LIMIT ?`
		args = []any{q + "%", q + "%", q, q, limit}
	}

	rows, err := s.reader.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("autocomplete users: %w", err)
	}
	defer rows.Close()

	var out []User
	for rows.Next() {
		u, err := scanUser(rows)
		if err != nil {
			return nil, fmt.Errorf("scan autocomplete user: %w", err)
		}
		u.PasswordHash = ""
		out = append(out, u)
	}
	return out, rows.Err()
}
