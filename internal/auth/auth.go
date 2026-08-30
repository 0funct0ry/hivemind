package auth

import (
	"compress/gzip"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"embed"
	"encoding/base32"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"io"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/0funct0ry/hivemind/internal/store"
	"golang.org/x/crypto/bcrypt"
)

//go:embed common_passwords.txt.gz
var commonFS embed.FS

const bcryptCost = 12

var dummyHash = mustDummy()

func mustDummy() string {
	h, err := bcrypt.GenerateFromPassword([]byte("hivemind-invalid-password"), bcryptCost)
	if err != nil {
		panic(err)
	}
	return string(h)
}

// DummyHash returns a valid cost-12 hash for unknown-user checks.
func DummyHash() string { return dummyHash }

var commonOnce sync.Once
var common map[string]struct{}

func loadCommon() {
	commonOnce.Do(func() {
		common = map[string]struct{}{}
		f, err := commonFS.Open("common_passwords.txt.gz")
		if err != nil {
			return
		}
		defer f.Close()
		gz, err := gzip.NewReader(f)
		if err != nil {
			return
		}
		defer gz.Close()
		b, _ := io.ReadAll(gz)
		for _, p := range strings.Split(string(b), "\n") {
			p = strings.TrimSuffix(p, "\r")
			if p != "" {
				common[p] = struct{}{}
			}
		}
	})
}

// HashPassword hashes a password using bcrypt cost 12.
func HashPassword(password string) (string, error) {
	h, err := bcrypt.GenerateFromPassword([]byte(password), bcryptCost)
	if err != nil {
		return "", fmt.Errorf("hash password: %w", err)
	}
	return string(h), nil
}

// CheckPassword verifies a bcrypt password hash.
func CheckPassword(hash, password string) bool {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) == nil
}

// ValidatePassword validates length and common-password policy.
func ValidatePassword(password string) error {
	if utf8.RuneCountInString(password) < 10 {
		return fmt.Errorf("password must be at least 10 characters")
	}
	loadCommon()
	if _, ok := common[password]; ok {
		return fmt.Errorf("password is too common")
	}
	return nil
}

// Service provides authentication operations.
type Service struct {
	Store      *store.Store
	SessionTTL time.Duration
	Now        func() time.Time
	Random     io.Reader
}

// New constructs an authentication service.
func New(s *store.Store, ttl time.Duration) *Service {
	return &Service{Store: s, SessionTTL: ttl, Now: time.Now, Random: rand.Reader}
}
func (s *Service) now() int64 { return s.Now().UnixMilli() }

// CreateSession creates and persists a browser session.
func (s *Service) CreateSession(ctx context.Context, userID int64, ua, ip string) (string, error) {
	b := make([]byte, 32)
	if _, err := io.ReadFull(s.Random, b); err != nil {
		return "", fmt.Errorf("random session ID: %w", err)
	}
	id := base64.RawURLEncoding.EncodeToString(b)
	now := s.now()
	if err := s.Store.CreateSession(ctx, store.Session{ID: id, UserID: userID, UserAgent: ua, IP: ip, CreatedAt: now, ExpiresAt: now + s.SessionTTL.Milliseconds(), LastSeenAt: now}); err != nil {
		return "", err
	}
	return id, nil
}

// AuthenticateSession validates a session cookie.
func (s *Service) AuthenticateSession(ctx context.Context, id string) (store.User, store.Session, error) {
	return s.Store.AuthenticateSession(ctx, id, s.now(), s.SessionTTL.Milliseconds())
}

// AuthenticateToken validates an API token plaintext.
func (s *Service) AuthenticateToken(ctx context.Context, plaintext string) (store.User, error) {
	sum := sha256.Sum256([]byte(plaintext))
	return s.Store.AuthenticateAPIToken(ctx, hex.EncodeToString(sum[:]), s.now())
}

// newTokenSecret generates a fresh "hm_<random>" plaintext token secret and its SHA-256 hash,
// shared by CreateToken and RotateToken so the two never drift on format.
func (s *Service) newTokenSecret() (secret, hash string, err error) {
	b := make([]byte, 20)
	if _, err := io.ReadFull(s.Random, b); err != nil {
		return "", "", fmt.Errorf("random API token: %w", err)
	}
	secret = "hm_" + base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(b)
	sum := sha256.Sum256([]byte(secret))
	return secret, hex.EncodeToString(sum[:]), nil
}

// CreateToken creates an API token of the given purpose (store.TokenPurposeAPIKey for a
// self-service personal key, store.TokenPurposeCLISession for hivemind chat's auto-minted
// login token) and returns its plaintext once.
func (s *Service) CreateToken(ctx context.Context, userID int64, name string, expires time.Duration, purpose string) (int64, string, error) {
	secret, hash, err := s.newTokenSecret()
	if err != nil {
		return 0, "", err
	}
	expiresAt := int64(0)
	if expires > 0 {
		expiresAt = s.now() + expires.Milliseconds()
	}
	id, err := s.Store.CreateAPIToken(ctx, userID, name, hash, s.now(), expiresAt, purpose)
	if err != nil {
		return 0, "", err
	}
	return id, secret, nil
}

// RotateToken generates a fresh secret for an existing token id of the given purpose and
// returns the new plaintext once, for admin use — the previous plaintext stops authenticating
// immediately.
func (s *Service) RotateToken(ctx context.Context, id int64, purpose string) (string, error) {
	secret, hash, err := s.newTokenSecret()
	if err != nil {
		return "", err
	}
	if err := s.Store.RotateAPIToken(ctx, id, hash, s.now(), purpose); err != nil {
		return "", err
	}
	return secret, nil
}

// ChangePassword validates and changes a password, revoking all sessions.
func (s *Service) ChangePassword(ctx context.Context, u store.User, current, next string) error {
	if !CheckPassword(u.PasswordHash, current) {
		return sql.ErrNoRows
	}
	if err := ValidatePassword(next); err != nil {
		return err
	}
	h, err := HashPassword(next)
	if err != nil {
		return err
	}
	if err := s.Store.Tx(ctx, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, "UPDATE users SET password_hash=?,updated_at=? WHERE id=?", h, s.now(), u.ID); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx, "DELETE FROM sessions WHERE user_id=?", u.ID)
		return err
	}); err != nil {
		return fmt.Errorf("change password: %w", err)
	}
	return nil
}

// SweepExpiredSessions removes expired sessions.
func (s *Service) SweepExpiredSessions(ctx context.Context) error {
	return s.Store.SweepSessions(ctx, s.now())
}
