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

	if err := s.Seed(ctx, 2, 1, 1, "custom-pass"); err != nil {
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

	if err := s.Seed(ctx, 1, 1, 1, ""); err != nil {
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
