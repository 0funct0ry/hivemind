package store

import (
	"context"
	"database/sql"
	"errors"
	"testing"
)

func newTokenTestStore(t *testing.T) (*Store, context.Context, int64) {
	t.Helper()
	ctx := context.Background()
	s, err := Open(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	if err := s.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	u, err := s.CreateUser(ctx, UserInput{Username: "alice", Email: "alice@example.com", PasswordHash: "hash"})
	if err != nil {
		t.Fatal(err)
	}
	return s, ctx, u.ID
}

func TestListAllAPITokensSpansUsersAndScopesByPurpose(t *testing.T) {
	s, ctx, u1 := newTokenTestStore(t)
	u2, err := s.CreateUser(ctx, UserInput{Username: "bob", Email: "bob@example.com", PasswordHash: "hash"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateAPIToken(ctx, u1, "tok-a", "hash-a", 1000, 0, TokenPurposeCLISession); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateAPIToken(ctx, u2.ID, "tok-b", "hash-b", 2000, 0, TokenPurposeCLISession); err != nil {
		t.Fatal(err)
	}
	// A deliberately-created personal API key must never appear in the admin Sessions listing.
	if _, err := s.CreateAPIToken(ctx, u1, "my-bot-key", "hash-c", 3000, 0, TokenPurposeAPIKey); err != nil {
		t.Fatal(err)
	}

	all, err := s.ListAllAPITokens(ctx, TokenPurposeCLISession)
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 2 {
		t.Fatalf("len(all) = %d, want 2 (api_key token must be excluded)", len(all))
	}
	byName := map[string]APITokenWithOwner{}
	for _, tk := range all {
		byName[tk.Name] = tk
	}
	if byName["tok-a"].Username != "alice" || byName["tok-b"].Username != "bob" {
		t.Fatalf("owners not populated: %#v", byName)
	}
	if _, found := byName["my-bot-key"]; found {
		t.Fatal("ListAllAPITokens(cli_session) leaked an api_key-purposed token")
	}
}

func TestListAPITokensScopesByPurpose(t *testing.T) {
	s, ctx, uid := newTokenTestStore(t)
	if _, err := s.CreateAPIToken(ctx, uid, "my-bot-key", "hash-key", 1000, 0, TokenPurposeAPIKey); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateAPIToken(ctx, uid, "chat-cli", "hash-session", 2000, 0, TokenPurposeCLISession); err != nil {
		t.Fatal(err)
	}

	keys, err := s.ListAPITokens(ctx, uid, TokenPurposeAPIKey)
	if err != nil {
		t.Fatal(err)
	}
	if len(keys) != 1 || keys[0].Name != "my-bot-key" {
		t.Fatalf("self-service list = %#v, want only the api_key token", keys)
	}

	sessions, err := s.ListAPITokens(ctx, uid, TokenPurposeCLISession)
	if err != nil {
		t.Fatal(err)
	}
	if len(sessions) != 1 || sessions[0].Name != "chat-cli" {
		t.Fatalf("cli_session list = %#v, want only the cli_session token", sessions)
	}
}

func TestDeleteAPITokenScopedByPurpose(t *testing.T) {
	s, ctx, uid := newTokenTestStore(t)
	sessionID, err := s.CreateAPIToken(ctx, uid, "chat-cli", "hash-session", 1000, 0, TokenPurposeCLISession)
	if err != nil {
		t.Fatal(err)
	}
	// The self-service "delete my API key" path must not be able to delete a CLI session
	// token even if the caller somehow guesses its id.
	if err := s.DeleteAPIToken(ctx, uid, sessionID, TokenPurposeAPIKey); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("expected ErrNoRows deleting a cli_session token via the api_key-scoped path, got %v", err)
	}
	if err := s.DeleteAPIToken(ctx, uid, sessionID, TokenPurposeCLISession); err != nil {
		t.Fatalf("expected the correctly-scoped delete to succeed: %v", err)
	}
}

func TestDisableEnableAPIToken(t *testing.T) {
	s, ctx, uid := newTokenTestStore(t)
	id, err := s.CreateAPIToken(ctx, uid, "tok", "hash-1", 1000, 0, TokenPurposeCLISession)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.AuthenticateAPIToken(ctx, "hash-1", 1500); err != nil {
		t.Fatalf("expected token to authenticate before disable: %v", err)
	}
	if err := s.DisableAPIToken(ctx, id, 1600, TokenPurposeCLISession); err != nil {
		t.Fatal(err)
	}
	if _, err := s.AuthenticateAPIToken(ctx, "hash-1", 1700); err == nil {
		t.Fatal("expected disabled token to fail authentication")
	}
	// Disabling again is a no-op failure (already disabled), not an error we care to surface twice.
	if err := s.DisableAPIToken(ctx, id, 1800, TokenPurposeCLISession); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("expected ErrNoRows on double-disable, got %v", err)
	}
	if err := s.EnableAPIToken(ctx, id, TokenPurposeCLISession); err != nil {
		t.Fatal(err)
	}
	if _, err := s.AuthenticateAPIToken(ctx, "hash-1", 1900); err != nil {
		t.Fatalf("expected token to authenticate again after enable: %v", err)
	}
}

func TestDisableAPITokenWrongPurposeIsNoOp(t *testing.T) {
	s, ctx, uid := newTokenTestStore(t)
	id, err := s.CreateAPIToken(ctx, uid, "my-bot-key", "hash-1", 1000, 0, TokenPurposeAPIKey)
	if err != nil {
		t.Fatal(err)
	}
	// The admin Sessions "disable" action must never be able to touch a personal API key.
	if err := s.DisableAPIToken(ctx, id, 1600, TokenPurposeCLISession); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("expected ErrNoRows disabling an api_key token via the cli_session-scoped path, got %v", err)
	}
	if _, err := s.AuthenticateAPIToken(ctx, "hash-1", 1700); err != nil {
		t.Fatalf("api_key token should still authenticate: %v", err)
	}
}

func TestRotateAPIToken(t *testing.T) {
	s, ctx, uid := newTokenTestStore(t)
	id, err := s.CreateAPIToken(ctx, uid, "tok", "hash-old", 1000, 0, TokenPurposeCLISession)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.RotateAPIToken(ctx, id, "hash-new", 2000, TokenPurposeCLISession); err != nil {
		t.Fatal(err)
	}
	if _, err := s.AuthenticateAPIToken(ctx, "hash-old", 3000); err == nil {
		t.Fatal("expected old hash to stop authenticating after rotation")
	}
	if _, err := s.AuthenticateAPIToken(ctx, "hash-new", 3000); err != nil {
		t.Fatalf("expected new hash to authenticate: %v", err)
	}
}

func TestAdminDeleteAPIToken(t *testing.T) {
	s, ctx, uid := newTokenTestStore(t)
	id, err := s.CreateAPIToken(ctx, uid, "tok", "hash-1", 1000, 0, TokenPurposeCLISession)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.AdminDeleteAPIToken(ctx, id, TokenPurposeCLISession); err != nil {
		t.Fatal(err)
	}
	if err := s.AdminDeleteAPIToken(ctx, id, TokenPurposeCLISession); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("expected ErrNoRows on double-delete, got %v", err)
	}
	all, err := s.ListAllAPITokens(ctx, TokenPurposeCLISession)
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 0 {
		t.Fatalf("len(all) = %d, want 0 after delete", len(all))
	}
}
