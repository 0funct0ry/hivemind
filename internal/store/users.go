package store

import (
	"context"
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
