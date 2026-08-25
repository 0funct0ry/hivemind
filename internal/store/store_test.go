package store

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"testing"
)

func TestOpenMigrateDowngradeAndStats(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	s, err := Open(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err := s.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	if err := s.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	if err := s.Tx(ctx, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, "INSERT INTO users(username,email,password_hash,created_at,updated_at) VALUES (?, ?, ?, ?, ?)", "alice", "alice@example.com", "hash", nowMillis(), nowMillis())
		return err
	}); err != nil {
		t.Fatal(err)
	}
	stats, err := s.Stats(ctx)
	if err != nil || stats.Users != 1 || stats.DBSizeBytes <= 0 {
		t.Fatalf("stats = %#v, err=%v", stats, err)
	}
	if err := s.MigrateTo(ctx, 0); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "hivemind.db")); err != nil {
		t.Fatal(err)
	}
	if err := s.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
}

func TestTxRollbackAndPanic(t *testing.T) {
	ctx := context.Background()
	s, err := Open(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err := s.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	if err := s.Tx(ctx, func(tx *sql.Tx) error { return os.ErrInvalid }); err == nil {
		t.Fatal("expected rollback error")
	}
	defer func() {
		if recover() == nil {
			t.Fatal("expected transaction panic to propagate")
		}
	}()
	_ = s.Tx(ctx, func(*sql.Tx) error { panic("boom") })
}
