package store

import (
	"context"
	"database/sql"
	"fmt"
)

// User is the persisted identity used by authentication and API responses.
type User struct {
	ID                                                                    int64
	Username, Email, DisplayName, PasswordHash, AvatarColor, Role, Status string
	AvatarURL                                                             string
	IsBot                                                                 bool
	CreatedAt, UpdatedAt                                                  int64
}

// Session is a browser login session.
type Session struct {
	ID                                       string
	UserID, CreatedAt, ExpiresAt, LastSeenAt int64
	UserAgent, IP                            string
}

// Token purposes distinguish a deliberately-created personal API key (self-service, for
// bots/scripts/integrations) from a token `hivemind chat` mints automatically after login so
// it doesn't have to re-prompt for a password — the CLI equivalent of a browser session
// cookie, not something the user consciously "created." The two live in the same table but
// are surfaced through entirely different UIs: API keys are self-service (any user, their own
// tokens only); sessions are admin-only, workspace-wide oversight.
const (
	TokenPurposeAPIKey     = "api_key"
	TokenPurposeCLISession = "cli_session"
)

// APIToken is an API token without its plaintext secret.
type APIToken struct {
	ID, UserID, CreatedAt, ExpiresAt, LastUsedAt int64
	Name, Purpose                                string
	DisabledAt                                   *int64
}

// APITokenWithOwner is an APIToken plus the identity of the user it belongs to, used by
// admin-facing listings that span every user rather than the caller's own tokens.
type APITokenWithOwner struct {
	APIToken
	Username, DisplayName string
}

func scanUser(s interface{ Scan(...any) error }) (User, error) {
	var u User
	var bot int
	if err := s.Scan(&u.ID, &u.Username, &u.Email, &u.DisplayName, &u.PasswordHash, &u.AvatarColor, &u.Role, &bot, &u.Status, &u.CreatedAt, &u.UpdatedAt); err != nil {
		return User{}, err
	}
	u.IsBot = bot != 0
	return u, nil
}

// GetUserByLogin finds an active user by case-insensitive username or email.
func (s *Store) GetUserByLogin(ctx context.Context, login string) (User, error) {
	return s.getUser(ctx, "WHERE (username = ? COLLATE NOCASE OR email = ? COLLATE NOCASE)", login, login)
}

// GetUserByID returns a user by ID.
func (s *Store) GetUserByID(ctx context.Context, id int64) (User, error) {
	return s.getUser(ctx, "WHERE id = ?", id)
}
func (s *Store) getUser(ctx context.Context, clause string, args ...any) (User, error) {
	q := "SELECT id,username,email,display_name,password_hash,avatar_color,role,is_bot,status,created_at,updated_at FROM users " + clause
	u, err := scanUser(s.reader.QueryRowContext(ctx, q, args...))
	if err != nil {
		return User{}, fmt.Errorf("get user: %w", err)
	}
	if err := s.hydrateAvatarURL(ctx, &u); err != nil {
		return User{}, fmt.Errorf("hydrate user avatar: %w", err)
	}
	return u, nil
}

func (s *Store) hydrateAvatarURL(ctx context.Context, u *User) error {
	var id, name string
	err := s.reader.QueryRowContext(ctx, `SELECT id, name FROM files WHERE id = (SELECT avatar_file_id FROM users WHERE id = ?)`, u.ID).Scan(&id, &name)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil
		}
		return err
	}
	u.AvatarURL = "/api/v1/files/" + id + "/" + name
	return nil
}

// CreateSession persists a new browser session.
func (s *Store) CreateSession(ctx context.Context, v Session) error {
	_, err := s.writer.ExecContext(ctx, "INSERT INTO sessions(id,user_id,user_agent,ip,created_at,expires_at,last_seen_at) VALUES (?,?,?,?,?,?,?)", v.ID, v.UserID, v.UserAgent, v.IP, v.CreatedAt, v.ExpiresAt, v.LastSeenAt)
	if err != nil {
		return fmt.Errorf("create session: %w", err)
	}
	return nil
}

// AuthenticateSession returns the session's active user and refreshes it when stale.
func (s *Store) AuthenticateSession(ctx context.Context, id string, now, ttl int64) (User, Session, error) {
	var v Session
	var u User
	row := s.reader.QueryRowContext(ctx, "SELECT s.id,s.user_id,s.created_at,s.expires_at,s.last_seen_at,s.user_agent,s.ip,u.id,u.username,u.email,u.display_name,u.password_hash,u.avatar_color,u.role,u.is_bot,u.status,u.created_at,u.updated_at FROM sessions s JOIN users u ON u.id=s.user_id WHERE s.id=? AND s.expires_at>? AND u.status='active'", id, now)
	var bot int
	if err := row.Scan(&v.ID, &v.UserID, &v.CreatedAt, &v.ExpiresAt, &v.LastSeenAt, &v.UserAgent, &v.IP, &u.ID, &u.Username, &u.Email, &u.DisplayName, &u.PasswordHash, &u.AvatarColor, &u.Role, &bot, &u.Status, &u.CreatedAt, &u.UpdatedAt); err != nil {
		return User{}, Session{}, fmt.Errorf("authenticate session: %w", err)
	}
	u.IsBot = bot != 0
	if now-v.LastSeenAt > 3600000 {
		v.LastSeenAt = now
		v.ExpiresAt = now + ttl
		if _, err := s.writer.ExecContext(ctx, "UPDATE sessions SET last_seen_at=?, expires_at=? WHERE id=?", v.LastSeenAt, v.ExpiresAt, id); err != nil {
			return User{}, Session{}, fmt.Errorf("refresh session: %w", err)
		}
	}
	if err := s.hydrateAvatarURL(ctx, &u); err != nil {
		return User{}, Session{}, fmt.Errorf("hydrate session user avatar: %w", err)
	}
	return u, v, nil
}

// DeleteSession removes one session.
func (s *Store) DeleteSession(ctx context.Context, id string) error {
	if _, err := s.writer.ExecContext(ctx, "DELETE FROM sessions WHERE id=?", id); err != nil {
		return fmt.Errorf("delete session: %w", err)
	}
	return nil
}

// DeleteUserSessions removes every session belonging to a user.
func (s *Store) DeleteUserSessions(ctx context.Context, userID int64) error {
	if _, err := s.writer.ExecContext(ctx, "DELETE FROM sessions WHERE user_id=?", userID); err != nil {
		return fmt.Errorf("delete user sessions: %w", err)
	}
	return nil
}

// SweepSessions removes expired sessions.
func (s *Store) SweepSessions(ctx context.Context, now int64) error {
	if _, err := s.writer.ExecContext(ctx, "DELETE FROM sessions WHERE expires_at<=?", now); err != nil {
		return fmt.Errorf("sweep sessions: %w", err)
	}
	return nil
}

// CreateAPIToken stores a token digest and returns its numeric ID.
func (s *Store) CreateAPIToken(ctx context.Context, userID int64, name, hash string, created, expires int64, purpose string) (int64, error) {
	r, err := s.writer.ExecContext(ctx, "INSERT INTO api_tokens(user_id,name,hash,created_at,expires_at,purpose) VALUES (?,?,?,?,?,?)", userID, name, hash, created, nilIfZero(expires), purpose)
	if err != nil {
		return 0, fmt.Errorf("create API token: %w", err)
	}
	id, err := r.LastInsertId()
	return id, err
}
func nilIfZero(v int64) any {
	if v == 0 {
		return nil
	}
	return v
}

// AuthenticateAPIToken returns its active owner and marks usage at most once per minute.
func (s *Store) AuthenticateAPIToken(ctx context.Context, hash string, now int64) (User, error) {
	var id int64
	var u User
	var bot int
	row := s.reader.QueryRowContext(ctx, "SELECT u.id,u.username,u.email,u.display_name,u.password_hash,u.avatar_color,u.role,u.is_bot,u.status,u.created_at,u.updated_at,t.id FROM api_tokens t JOIN users u ON u.id=t.user_id WHERE t.hash=? AND (t.expires_at IS NULL OR t.expires_at>?) AND t.disabled_at IS NULL AND u.status='active'", hash, now)
	if err := row.Scan(&u.ID, &u.Username, &u.Email, &u.DisplayName, &u.PasswordHash, &u.AvatarColor, &u.Role, &bot, &u.Status, &u.CreatedAt, &u.UpdatedAt, &id); err != nil {
		return User{}, fmt.Errorf("authenticate API token: %w", err)
	}
	u.IsBot = bot != 0
	if _, err := s.writer.ExecContext(ctx, "UPDATE api_tokens SET last_used_at=? WHERE id=? AND (last_used_at IS NULL OR last_used_at<=?)", now, id, now-60000); err != nil {
		return User{}, fmt.Errorf("update API token usage: %w", err)
	}
	if err := s.hydrateAvatarURL(ctx, &u); err != nil {
		return User{}, fmt.Errorf("hydrate token user avatar: %w", err)
	}
	return u, nil
}

// ListAPITokens lists a user's non-secret token metadata for the given purpose — the
// self-service API-keys UI always passes TokenPurposeAPIKey, so a user's own auto-minted CLI
// session token(s) never show up alongside their deliberately-created keys.
func (s *Store) ListAPITokens(ctx context.Context, userID int64, purpose string) ([]APIToken, error) {
	rows, err := s.reader.QueryContext(ctx, "SELECT id,user_id,name,created_at,COALESCE(expires_at,0),COALESCE(last_used_at,0),purpose FROM api_tokens WHERE user_id=? AND purpose=? ORDER BY id DESC", userID, purpose)
	if err != nil {
		return nil, fmt.Errorf("list API tokens: %w", err)
	}
	defer rows.Close()
	var out []APIToken
	for rows.Next() {
		var t APIToken
		if err := rows.Scan(&t.ID, &t.UserID, &t.Name, &t.CreatedAt, &t.ExpiresAt, &t.LastUsedAt, &t.Purpose); err != nil {
			return nil, fmt.Errorf("scan API token: %w", err)
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// DeleteAPIToken deletes a token only when owned by the user and matching purpose — scoping by
// purpose means the self-service "revoke my API key" endpoint can never delete the CLI session
// token it also owns, even by a malformed id guess.
func (s *Store) DeleteAPIToken(ctx context.Context, userID, id int64, purpose string) error {
	r, err := s.writer.ExecContext(ctx, "DELETE FROM api_tokens WHERE id=? AND user_id=? AND purpose=?", id, userID, purpose)
	if err != nil {
		return fmt.Errorf("delete API token: %w", err)
	}
	n, err := r.RowsAffected()
	if err != nil || n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

// ListAllAPITokens lists every API token of the given purpose across every user, for admin
// management — unlike ListAPITokens, it is not scoped to a single owner. The admin Sessions
// page always passes TokenPurposeCLISession, so it never surfaces (or lets an admin act on) a
// user's personal API keys.
func (s *Store) ListAllAPITokens(ctx context.Context, purpose string) ([]APITokenWithOwner, error) {
	rows, err := s.reader.QueryContext(ctx, "SELECT t.id,t.user_id,t.name,t.created_at,COALESCE(t.expires_at,0),COALESCE(t.last_used_at,0),t.disabled_at,u.username,u.display_name FROM api_tokens t JOIN users u ON u.id=t.user_id WHERE t.purpose=? ORDER BY t.created_at DESC", purpose)
	if err != nil {
		return nil, fmt.Errorf("list all API tokens: %w", err)
	}
	defer rows.Close()
	var out []APITokenWithOwner
	for rows.Next() {
		var t APITokenWithOwner
		if err := rows.Scan(&t.ID, &t.UserID, &t.Name, &t.CreatedAt, &t.ExpiresAt, &t.LastUsedAt, &t.DisabledAt, &t.Username, &t.DisplayName); err != nil {
			return nil, fmt.Errorf("scan API token: %w", err)
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// DisableAPIToken soft-disables a token of the given purpose regardless of owner, for admin
// use. Authentication against a disabled token fails immediately even though the row (and its
// hash) still exists.
func (s *Store) DisableAPIToken(ctx context.Context, id, now int64, purpose string) error {
	return execAffecting(ctx, s.writer, "disable API token", "UPDATE api_tokens SET disabled_at=? WHERE id=? AND purpose=? AND disabled_at IS NULL", now, id, purpose)
}

// EnableAPIToken clears a token's disabled state, for admin use, scoped to purpose as above.
func (s *Store) EnableAPIToken(ctx context.Context, id int64, purpose string) error {
	return execAffecting(ctx, s.writer, "enable API token", "UPDATE api_tokens SET disabled_at=NULL WHERE id=? AND purpose=? AND disabled_at IS NOT NULL", id, purpose)
}

// RotateAPIToken replaces a token's secret hash in place, invalidating the previous plaintext
// immediately, clearing last_used_at and any disabled state, for admin use, scoped to purpose
// as above.
func (s *Store) RotateAPIToken(ctx context.Context, id int64, newHash string, now int64, purpose string) error {
	return execAffecting(ctx, s.writer, "rotate API token", "UPDATE api_tokens SET hash=?, created_at=?, last_used_at=NULL, disabled_at=NULL WHERE id=? AND purpose=?", newHash, now, id, purpose)
}

// AdminDeleteAPIToken deletes a token of the given purpose regardless of owner, for admin use.
func (s *Store) AdminDeleteAPIToken(ctx context.Context, id int64, purpose string) error {
	return execAffecting(ctx, s.writer, "admin delete API token", "DELETE FROM api_tokens WHERE id=? AND purpose=?", id, purpose)
}

// execAffecting runs a write query and reports sql.ErrNoRows when it touches nothing, the
// shared pattern the admin token-management methods above use to signal "not found".
func execAffecting(ctx context.Context, w interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
}, verb, query string, args ...any) error {
	r, err := w.ExecContext(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("%s: %w", verb, err)
	}
	n, err := r.RowsAffected()
	if err != nil || n == 0 {
		return sql.ErrNoRows
	}
	return nil
}
