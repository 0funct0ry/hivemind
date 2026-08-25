package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"time"

	_ "modernc.org/sqlite"
)

const sqliteDriver = "sqlite"

// Store owns the writer and read-only SQLite pools for the application.
type Store struct {
	writer *sql.DB
	reader *sql.DB
	path   string
}

// Stats contains database row counts and on-disk sizes.
type Stats struct {
	Users        int64
	Channels     int64
	Messages     int64
	DBSizeBytes  int64
	WALSizeBytes int64
}

// nowMillis is indirected so store tests can use deterministic timestamps.
var nowMillis = func() int64 { return time.Now().UnixMilli() }

const sqlitePragmas = "_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)&_pragma=synchronous(NORMAL)"

// Open opens the writer and read-only pools over dataDir/hivemind.db.
func Open(ctx context.Context, dataDir string) (*Store, error) {
	if dataDir == "" {
		return nil, fmt.Errorf("open store: data directory is empty")
	}
	if err := os.MkdirAll(dataDir, 0o750); err != nil {
		return nil, fmt.Errorf("create data directory %s: %w", dataDir, err)
	}
	path := filepath.Join(dataDir, "hivemind.db")
	writer, err := sql.Open(sqliteDriver, path+"?"+sqlitePragmas)
	if err != nil {
		return nil, fmt.Errorf("open SQLite writer: %w", err)
	}
	writer.SetMaxOpenConns(1)
	if err := writer.PingContext(ctx); err != nil {
		_ = writer.Close()
		return nil, fmt.Errorf("ping SQLite writer: %w", err)
	}
	if err := verifyFTS5(ctx, writer); err != nil {
		_ = writer.Close()
		return nil, err
	}

	reader, err := sql.Open(sqliteDriver, path+"?"+sqlitePragmas+"&mode=ro")
	if err != nil {
		_ = writer.Close()
		return nil, fmt.Errorf("open SQLite reader: %w", err)
	}
	reader.SetMaxOpenConns(runtime.NumCPU() * 2)
	if err := reader.PingContext(ctx); err != nil {
		_ = reader.Close()
		_ = writer.Close()
		return nil, fmt.Errorf("ping SQLite reader: %w", err)
	}
	return &Store{writer: writer, reader: reader, path: path}, nil
}

func verifyFTS5(ctx context.Context, db *sql.DB) error {
	if _, err := db.ExecContext(ctx, "CREATE VIRTUAL TABLE temp.hivemind_fts_probe USING fts5(body)"); err != nil {
		return fmt.Errorf("SQLite FTS5 is unavailable; rebuild modernc.org/sqlite with FTS5 enabled: %w", err)
	}
	if _, err := db.ExecContext(ctx, "DROP TABLE temp.hivemind_fts_probe"); err != nil {
		return fmt.Errorf("remove FTS5 probe: %w", err)
	}
	return nil
}

// Close checkpoints the WAL and closes both database pools.
func (s *Store) Close() error {
	if s == nil {
		return nil
	}
	var errs []error
	if s.reader != nil {
		if err := s.reader.Close(); err != nil {
			errs = append(errs, fmt.Errorf("close reader: %w", err))
		}
	}
	if s.writer != nil {
		if _, err := s.writer.Exec("PRAGMA wal_checkpoint(TRUNCATE)"); err != nil {
			errs = append(errs, fmt.Errorf("checkpoint WAL: %w", err))
		}
		if err := s.writer.Close(); err != nil {
			errs = append(errs, fmt.Errorf("close writer: %w", err))
		}
	}
	return errors.Join(errs...)
}

// Tx executes fn in a transaction on the single writer connection.
func (s *Store) Tx(ctx context.Context, fn func(*sql.Tx) error) (err error) {
	tx, err := s.writer.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin store transaction: %w", err)
	}
	defer func() {
		if recovered := recover(); recovered != nil {
			_ = tx.Rollback()
			panic(recovered)
		}
		if err != nil {
			if rollbackErr := tx.Rollback(); rollbackErr != nil && !errors.Is(rollbackErr, sql.ErrTxDone) {
				err = fmt.Errorf("%w (rollback: %v)", err, rollbackErr)
			}
			return
		}
		if commitErr := tx.Commit(); commitErr != nil {
			err = fmt.Errorf("commit store transaction: %w", commitErr)
		}
	}()
	if fn == nil {
		return fmt.Errorf("store transaction: callback is nil")
	}
	err = fn(tx)
	if err != nil {
		err = fmt.Errorf("store transaction callback: %w", err)
	}
	return err
}

// Stats returns row counts and the sizes of the database and WAL files.
func (s *Store) Stats(ctx context.Context) (Stats, error) {
	var out Stats
	queries := []struct {
		name string
		dst  *int64
	}{
		{"users", &out.Users}, {"channels", &out.Channels}, {"messages", &out.Messages},
	}
	for _, q := range queries {
		if err := s.reader.QueryRowContext(ctx, "SELECT COUNT(*) FROM "+q.name).Scan(q.dst); err != nil {
			return Stats{}, fmt.Errorf("count %s: %w", q.name, err)
		}
	}
	info, err := os.Stat(s.path)
	if err != nil {
		return Stats{}, fmt.Errorf("stat database: %w", err)
	}
	out.DBSizeBytes = info.Size()
	if info, err := os.Stat(s.path + "-wal"); err == nil {
		out.WALSizeBytes = info.Size()
	} else if !errors.Is(err, os.ErrNotExist) {
		return Stats{}, fmt.Errorf("stat WAL: %w", err)
	}
	return out, nil
}

// Migrate applies all embedded migrations to the store.
func (s *Store) Migrate(ctx context.Context) error { return Migrate(ctx, s.writer) }

// MigrateTo migrates the store to an exact embedded migration version.
func (s *Store) MigrateTo(ctx context.Context, target int) error {
	return MigrateTo(ctx, s.writer, target)
}
