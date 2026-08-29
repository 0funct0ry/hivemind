package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sort"
	"strings"
)

// Reaction is one emoji group on a message: every user who has applied that emoji, ordered by
// the group's first-applied timestamp (SPEC.md §3.2a/§4.3).
type Reaction struct {
	Emoji   string  `json:"emoji"`
	UserIDs []int64 `json:"user_ids"`
	FirstAt int64   `json:"-"`
}

// AddReaction records userID's reaction to a message. Repeating the same emoji is a no-op,
// enforced by the reactions table's composite primary key rather than an app-level check.
// Reacting to a missing or already-deleted message returns ErrNotFound, checked in the same
// transaction as the insert.
func (s *Store) AddReaction(ctx context.Context, messageID, userID int64, emoji string) error {
	now := nowMillis()
	return s.Tx(ctx, func(tx *sql.Tx) error {
		var deletedAt sql.NullInt64
		err := tx.QueryRowContext(ctx, `SELECT deleted_at FROM messages WHERE id = ?`, messageID).Scan(&deletedAt)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return ErrNotFound
			}
			return fmt.Errorf("check message for reaction: %w", err)
		}
		if deletedAt.Valid {
			return ErrNotFound
		}

		_, err = tx.ExecContext(ctx, `
			INSERT INTO reactions (message_id, user_id, emoji, created_at)
			VALUES (?, ?, ?, ?)
			ON CONFLICT DO NOTHING`,
			messageID, userID, emoji, now)
		if err != nil {
			return fmt.Errorf("add reaction: %w", err)
		}
		return nil
	})
}

// RemoveReaction removes userID's own reaction of the given emoji from a message. Removing a
// reaction that was never added, or was already removed, is a no-op, not an error.
func (s *Store) RemoveReaction(ctx context.Context, messageID, userID int64, emoji string) error {
	_, err := s.writer.ExecContext(ctx, `
		DELETE FROM reactions WHERE message_id = ? AND user_id = ? AND emoji = ?`,
		messageID, userID, emoji)
	if err != nil {
		return fmt.Errorf("remove reaction: %w", err)
	}
	return nil
}

// GetReactions returns every emoji group on a message, ordered by each group's first-applied
// timestamp.
func (s *Store) GetReactions(ctx context.Context, messageID int64) ([]Reaction, error) {
	grouped, err := s.getReactionsForMessages(ctx, []int64{messageID})
	if err != nil {
		return nil, err
	}
	return grouped[messageID], nil
}

// GetReactionsForMessages batch-loads reaction groups for a page of messages in one query, for
// hydration — never load reactions in a loop.
func (s *Store) GetReactionsForMessages(ctx context.Context, messageIDs []int64) (map[int64][]Reaction, error) {
	return s.getReactionsForMessages(ctx, messageIDs)
}

func (s *Store) getReactionsForMessages(ctx context.Context, messageIDs []int64) (map[int64][]Reaction, error) {
	result := make(map[int64][]Reaction, len(messageIDs))
	if len(messageIDs) == 0 {
		return result, nil
	}

	placeholders := make([]string, len(messageIDs))
	args := make([]any, len(messageIDs))
	for i, id := range messageIDs {
		placeholders[i] = "?"
		args[i] = id
	}

	// first_rowid breaks ties when two emoji groups on the same message are first-applied within
	// the same millisecond — rowid strictly increases with insertion order, so it recovers a
	// deterministic "which group was first" answer that a created_at tie alone cannot.
	query := fmt.Sprintf(`
		SELECT message_id, user_id, emoji, created_at,
		       MIN(created_at) OVER (PARTITION BY message_id, emoji) AS first_at,
		       MIN(rowid) OVER (PARTITION BY message_id, emoji) AS first_rowid
		FROM reactions
		WHERE message_id IN (%s)
		ORDER BY message_id, first_at, first_rowid, created_at`, strings.Join(placeholders, ","))

	rows, err := s.reader.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("get reactions query: %w", err)
	}
	defer rows.Close()

	type key struct {
		messageID int64
		emoji     string
	}
	order := make(map[int64][]string) // messageID -> emoji insertion order
	byKey := make(map[key]*Reaction)

	for rows.Next() {
		var messageID, userID, createdAt, firstAt, firstRowID int64
		var emoji string
		if err := rows.Scan(&messageID, &userID, &emoji, &createdAt, &firstAt, &firstRowID); err != nil {
			return nil, fmt.Errorf("scan reaction: %w", err)
		}
		k := key{messageID, emoji}
		r, ok := byKey[k]
		if !ok {
			r = &Reaction{Emoji: emoji, FirstAt: firstAt}
			byKey[k] = r
			order[messageID] = append(order[messageID], emoji)
		}
		r.UserIDs = append(r.UserIDs, userID)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	for messageID, emojis := range order {
		reactions := make([]Reaction, 0, len(emojis))
		for _, emoji := range emojis {
			reactions = append(reactions, *byKey[key{messageID, emoji}])
		}
		sort.SliceStable(reactions, func(i, j int) bool { return reactions[i].FirstAt < reactions[j].FirstAt })
		result[messageID] = reactions
	}

	return result, nil
}
