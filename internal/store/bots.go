package store

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base32"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"strings"
)

// Bot status values (SPEC.md §3.2d/§4.12).
const (
	BotStatusActive  = "active"
	BotStatusRevoked = "revoked"
)

const botTokenName = "bot-token"

// ErrBotNotRevoked is returned by DeleteBot when the bot's token could still authenticate —
// deletion requires revoking first so a deleted-but-still-listed-nowhere bot can never keep
// posting with a token no admin can see or manage anymore.
var ErrBotNotRevoked = errors.New("bot must be revoked before it can be deleted")

// ErrBotInUse is returned by DeleteBot when one or more slash_commands still reference the bot
// (SPEC.md §3.2d's bot_id FK) — deleting it would either violate the FK or silently orphan the
// command's "identity a public response posts as."
var ErrBotInUse = errors.New("bot is still in use by one or more slash commands")

// Bot is a persisted bot's metadata plus its owning user's display fields.
type Bot struct {
	UserID      int64
	CreatedBy   int64
	Description string
	Status      string
	CreatedAt   int64
	UpdatedAt   int64

	Username    string
	DisplayName string
	AvatarColor string
}

// BotInput contains the fields accepted when creating a bot.
type BotInput struct {
	Name        string
	Description string
}

// generateBotToken returns a fresh "hm_<random>" plaintext bearer token, the same shape
// RequireAuth already expects of any bearer credential (internal/api/router.go checks the
// "hm_" prefix before ever reaching AuthenticateAPIToken).
func generateBotToken() (string, error) {
	b := make([]byte, 20)
	if _, err := io.ReadFull(rand.Reader, b); err != nil {
		return "", fmt.Errorf("generate bot token: %w", err)
	}
	return "hm_" + base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(b), nil
}

// hashBotToken returns the hex-encoded sha256 digest of a plaintext bot token — the same
// hashing api_tokens.hash already uses (internal/auth.Service.AuthenticateToken).
func hashBotToken(plaintext string) string {
	sum := sha256.Sum256([]byte(plaintext))
	return hex.EncodeToString(sum[:])
}

func scanBot(row interface{ Scan(...any) error }) (Bot, error) {
	var b Bot
	err := row.Scan(&b.UserID, &b.CreatedBy, &b.Description, &b.Status, &b.CreatedAt, &b.UpdatedAt,
		&b.Username, &b.DisplayName, &b.AvatarColor)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Bot{}, ErrNotFound
		}
		return Bot{}, err
	}
	return b, nil
}

const botColumns = `b.user_id, b.created_by, b.description, b.status, b.created_at, b.updated_at, u.username, u.display_name, u.avatar_color`

// CreateBot creates the bot's dedicated is_bot=1 user, its bots row, and mints a fresh
// "hm_<random>" bearer token stored in api_tokens with purpose=bot_token — all in one
// transaction, mirroring CreateWebhook's bot-user-creation pattern (SPEC.md §3.3/§4.12). The
// plaintext token is returned once and never persisted.
func (s *Store) CreateBot(ctx context.Context, in BotInput, createdBy int64) (Bot, string, error) {
	if strings.TrimSpace(in.Name) == "" {
		return Bot{}, "", fmt.Errorf("name is required")
	}

	plaintext, err := generateBotToken()
	if err != nil {
		return Bot{}, "", err
	}
	hash := hashBotToken(plaintext)
	now := nowMillis()

	var bot Bot
	err = s.Tx(ctx, func(tx *sql.Tx) error {
		botUsername := fmt.Sprintf("bot-%d-%s", now, strings.ToLower(strings.ReplaceAll(in.Name, " ", "-")))
		res, err := tx.ExecContext(ctx, `
			INSERT INTO users(username, email, display_name, password_hash, avatar_color, role, is_bot, status, created_at, updated_at)
			VALUES (?, ?, ?, '', ?, 'member', 1, 'active', ?, ?)`,
			botUsername, botUsername+"@bots.invalid", in.Name, AvatarColor(botUsername), now, now)
		if err != nil {
			return fmt.Errorf("create bot user: %w", err)
		}
		botUserID, err := res.LastInsertId()
		if err != nil {
			return fmt.Errorf("get bot user id: %w", err)
		}

		if _, err := tx.ExecContext(ctx, `
			INSERT INTO bots(user_id, created_by, description, status, created_at, updated_at)
			VALUES (?, ?, ?, 'active', ?, ?)`,
			botUserID, createdBy, in.Description, now, now); err != nil {
			return fmt.Errorf("insert bot: %w", err)
		}

		if _, err := tx.ExecContext(ctx, `
			INSERT INTO api_tokens(user_id, name, hash, created_at, expires_at, purpose)
			VALUES (?, ?, ?, ?, NULL, ?)`,
			botUserID, botTokenName, hash, now, TokenPurposeBotToken); err != nil {
			return fmt.Errorf("create bot token: %w", err)
		}

		bot, err = getBotTx(ctx, tx, botUserID)
		return err
	})
	if err != nil {
		return Bot{}, "", err
	}
	return bot, plaintext, nil
}

func getBotTx(ctx context.Context, tx *sql.Tx, userID int64) (Bot, error) {
	row := tx.QueryRowContext(ctx, "SELECT "+botColumns+" FROM bots b JOIN users u ON u.id = b.user_id WHERE b.user_id = ?", userID)
	return scanBot(row)
}

// GetBotByUserID fetches a single bot by its user id.
func (s *Store) GetBotByUserID(ctx context.Context, userID int64) (Bot, error) {
	row := s.reader.QueryRowContext(ctx, "SELECT "+botColumns+" FROM bots b JOIN users u ON u.id = b.user_id WHERE b.user_id = ?", userID)
	bot, err := scanBot(row)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return Bot{}, ErrNotFound
		}
		return Bot{}, fmt.Errorf("get bot: %w", err)
	}
	return bot, nil
}

// ListBots lists every bot in the workspace (admin-only call site — enforced by the API layer).
func (s *Store) ListBots(ctx context.Context) ([]Bot, error) {
	rows, err := s.reader.QueryContext(ctx, "SELECT "+botColumns+" FROM bots b JOIN users u ON u.id = b.user_id ORDER BY b.user_id")
	if err != nil {
		return nil, fmt.Errorf("list bots: %w", err)
	}
	defer rows.Close()
	var out []Bot
	for rows.Next() {
		b, err := scanBot(rows)
		if err != nil {
			return nil, fmt.Errorf("scan bot: %w", err)
		}
		out = append(out, b)
	}
	return out, rows.Err()
}

// RegenerateBotToken mints a fresh token for a bot, replacing its api_tokens row's hash in
// place — the previous plaintext stops authenticating immediately, same shape as
// RegenerateWebhookToken.
func (s *Store) RegenerateBotToken(ctx context.Context, botUserID int64) (string, error) {
	if _, err := s.GetBotByUserID(ctx, botUserID); err != nil {
		return "", err
	}
	plaintext, err := generateBotToken()
	if err != nil {
		return "", err
	}
	hash := hashBotToken(plaintext)
	now := nowMillis()

	res, err := s.writer.ExecContext(ctx, `
		UPDATE api_tokens SET hash = ?, last_used_at = NULL
		WHERE user_id = ? AND purpose = ?`, hash, botUserID, TokenPurposeBotToken)
	if err != nil {
		return "", fmt.Errorf("regenerate bot token: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return "", fmt.Errorf("regenerate bot token rows affected: %w", err)
	}
	if n == 0 {
		// The bot user existed but its token row was lost somehow — mint a fresh one rather
		// than leaving the bot permanently unauthenticatable.
		if _, err := s.writer.ExecContext(ctx, `
			INSERT INTO api_tokens(user_id, name, hash, created_at, expires_at, purpose)
			VALUES (?, ?, ?, ?, NULL, ?)`, botUserID, botTokenName, hash, now, TokenPurposeBotToken); err != nil {
			return "", fmt.Errorf("recreate bot token: %w", err)
		}
	}
	return plaintext, nil
}

// RevokeBot marks a bot revoked and deactivates its underlying user (same posture as
// DeactivateAndOrphanWebhooks — soft, not a hard delete, so messages already authored by the
// bot keep rendering).
func (s *Store) RevokeBot(ctx context.Context, botUserID int64) error {
	if _, err := s.GetBotByUserID(ctx, botUserID); err != nil {
		return err
	}
	now := nowMillis()
	return s.Tx(ctx, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, "UPDATE bots SET status = 'revoked', updated_at = ? WHERE user_id = ?", now, botUserID); err != nil {
			return fmt.Errorf("revoke bot: %w", err)
		}
		if _, err := tx.ExecContext(ctx, "UPDATE users SET status = 'deactivated', updated_at = ? WHERE id = ?", now, botUserID); err != nil {
			return fmt.Errorf("deactivate bot user: %w", err)
		}
		return nil
	})
}

// DeleteBot removes a bot's registration entirely (idempotent — deleting an already-gone bot is
// a no-op success, matching DeleteWebhook's posture). Unlike RevokeBot, this is a hard delete of
// the bots row, so it requires the bot to already be status='revoked': deleting an active bot's
// row would leave its api_tokens row (and the underlying user) untouched and still able to
// authenticate, with no UI left to see or manage it. The underlying is_bot=1 user row is
// deliberately left alone — its past messages must keep rendering.
func (s *Store) DeleteBot(ctx context.Context, botUserID int64) error {
	bot, err := s.GetBotByUserID(ctx, botUserID)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return nil
		}
		return err
	}
	if bot.Status != BotStatusRevoked {
		return ErrBotNotRevoked
	}

	_, err = s.writer.ExecContext(ctx, "DELETE FROM bots WHERE user_id = ?", botUserID)
	if err != nil {
		if isForeignKeyConstraintError(err) {
			return ErrBotInUse
		}
		return fmt.Errorf("delete bot: %w", err)
	}
	return nil
}
