package store

import (
	"context"
	"testing"

	"golang.org/x/crypto/bcrypt"
)

// TestSeedPasswordMatchesDocumentedValue guards against the seeded password hash drifting
// from the password seeded users are actually documented to log in with. A stale or
// miscalculated hash here silently locks operators out of every seeded user without any
// test failure elsewhere.
func TestSeedPasswordMatchesDocumentedValue(t *testing.T) {
	ctx := context.Background()
	s, err := Open(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err := s.Migrate(ctx); err != nil {
		t.Fatal(err)
	}

	if err := s.Seed(ctx, 2, 1, 1, 0, "custom-pass"); err != nil {
		t.Fatalf("Seed failed: %v", err)
	}

	u, err := s.GetUserByLogin(ctx, "bruce")
	if err != nil {
		t.Fatalf("GetUserByLogin(bruce) failed: %v", err)
	}

	if err := bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte("custom-pass")); err != nil {
		t.Errorf("seeded user's password_hash does not validate against the given password %q: %v", "custom-pass", err)
	}

	if u.Role != "admin" {
		t.Errorf("first seeded user should be admin, got role %q", u.Role)
	}
}

// TestSeedDefaultPassword verifies the default password is applied when the caller passes
// an empty string, matching the documented default for `hivemind seed`.
func TestSeedDefaultPassword(t *testing.T) {
	ctx := context.Background()
	s, err := Open(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err := s.Migrate(ctx); err != nil {
		t.Fatal(err)
	}

	if err := s.Seed(ctx, 1, 1, 1, 0, ""); err != nil {
		t.Fatalf("Seed failed: %v", err)
	}

	u, err := s.GetUserByLogin(ctx, "bruce")
	if err != nil {
		t.Fatalf("GetUserByLogin(bruce) failed: %v", err)
	}

	if err := bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte(DefaultSeedPassword)); err != nil {
		t.Errorf("seeded user's password_hash does not validate against the default password: %v", err)
	}
}

// TestSeedUnjoinedChannels verifies unjoinedChannels creates public channels with no
// membership rows at all (not even the creator), and that seeded messages only land in
// channels that do have members.
func TestSeedUnjoinedChannels(t *testing.T) {
	ctx := context.Background()
	s, err := Open(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err := s.Migrate(ctx); err != nil {
		t.Fatal(err)
	}

	const numChannels = 5
	const unjoined = 2
	if err := s.Seed(ctx, 3, numChannels, 200, unjoined, ""); err != nil {
		t.Fatalf("Seed failed: %v", err)
	}

	rows, err := s.reader.QueryContext(ctx, `
		SELECT c.id, c.created_by, (SELECT COUNT(*) FROM channel_members WHERE channel_id = c.id)
		FROM channels c WHERE c.kind = 'public'`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()

	var withMembers, withoutMembers int
	for rows.Next() {
		var id int64
		var createdBy *int64
		var memberCount int
		if err := rows.Scan(&id, &createdBy, &memberCount); err != nil {
			t.Fatal(err)
		}
		if memberCount == 0 {
			withoutMembers++
			if createdBy != nil {
				t.Errorf("channel %d has 0 members but a non-nil created_by (%d)", id, *createdBy)
			}
		} else {
			withMembers++
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}

	if withoutMembers != unjoined {
		t.Errorf("expected %d unjoined channels, got %d", unjoined, withoutMembers)
	}
	if withMembers != numChannels-unjoined {
		t.Errorf("expected %d joined channels, got %d", numChannels-unjoined, withMembers)
	}

	// No message should ever land in a channel with zero members.
	var strandedMessages int
	if err := s.reader.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM messages m
		WHERE NOT EXISTS (SELECT 1 FROM channel_members cm WHERE cm.channel_id = m.channel_id)`,
	).Scan(&strandedMessages); err != nil {
		t.Fatal(err)
	}
	if strandedMessages != 0 {
		t.Errorf("expected 0 messages in memberless channels, got %d", strandedMessages)
	}
}
