package store

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"io/fs"
	"path"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

//go:embed migrations/*.sql
var migrationFiles embed.FS

type migration struct {
	version int
	name    string
	up      string
	down    string
}

var migrationName = regexp.MustCompile(`^([0-9]{4})_[a-z0-9_]+\.sql$`)

// Migrate applies all pending embedded migrations.
func Migrate(ctx context.Context, db *sql.DB) error {
	migrations, err := loadMigrations(migrationFiles)
	if err != nil {
		return err
	}
	if len(migrations) == 0 {
		return nil
	}
	return MigrateTo(ctx, db, migrations[len(migrations)-1].version)
}

// MigrateTo migrates db to target, where zero means the schema is fully down.
func MigrateTo(ctx context.Context, db *sql.DB, target int) error {
	if target < 0 {
		return fmt.Errorf("migrate: target version must be non-negative")
	}
	migrations, err := loadMigrations(migrationFiles)
	if err != nil {
		return err
	}
	if target == 0 {
		// Zero is the explicit down target, not the latest version.
	} else if !hasVersion(migrations, target) {
		return fmt.Errorf("migrate: target version %d is not embedded", target)
	}
	current, err := appliedVersions(ctx, db)
	if err != nil {
		return err
	}
	for version := range current {
		if !hasVersion(migrations, version) {
			return fmt.Errorf("migrate: database has version %04d but this binary has no migration; use a newer binary", version)
		}
	}
	for i := len(migrations) - 1; i >= 0; i-- {
		m := migrations[i]
		if current[m.version] && m.version > target {
			if err := applyDown(ctx, db, m); err != nil {
				return err
			}
		}
	}
	for _, m := range migrations {
		if m.version > target || current[m.version] {
			continue
		}
		if err := applyUp(ctx, db, m); err != nil {
			return err
		}
	}
	return nil
}

func loadMigrations(files fs.FS) ([]migration, error) {
	entries, err := fs.ReadDir(files, "migrations")
	if err != nil {
		return nil, fmt.Errorf("load migrations: %w", err)
	}
	var result []migration
	seen := make(map[int]bool)
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		match := migrationName.FindStringSubmatch(entry.Name())
		if match == nil {
			return nil, fmt.Errorf("load migrations: invalid filename %q (want NNNN_name.sql)", entry.Name())
		}
		version, _ := strconv.Atoi(match[1])
		if seen[version] {
			return nil, fmt.Errorf("load migrations: duplicate version %04d", version)
		}
		seen[version] = true
		data, err := fs.ReadFile(files, path.Join("migrations", entry.Name()))
		if err != nil {
			return nil, fmt.Errorf("read migration %s: %w", entry.Name(), err)
		}
		up, down := splitMigration(string(data))
		result = append(result, migration{version: version, name: entry.Name(), up: strings.TrimSpace(up), down: strings.TrimSpace(down)})
	}
	sort.Slice(result, func(i, j int) bool { return result[i].version < result[j].version })
	return result, nil
}

func splitMigration(sqlText string) (string, string) {
	const marker = "-- +down"
	if i := strings.Index(sqlText, marker); i >= 0 {
		return sqlText[:i], sqlText[i+len(marker):]
	}
	return sqlText, ""
}

func hasVersion(migrations []migration, version int) bool {
	for _, m := range migrations {
		if m.version == version {
			return true
		}
	}
	return false
}

func appliedVersions(ctx context.Context, db *sql.DB) (map[int]bool, error) {
	rows, err := db.QueryContext(ctx, "SELECT version FROM schema_migrations ORDER BY version")
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "no such table") {
			return map[int]bool{}, nil
		}
		return nil, fmt.Errorf("read schema migrations: %w", err)
	}
	defer rows.Close()
	versions := make(map[int]bool)
	for rows.Next() {
		var version int
		if err := rows.Scan(&version); err != nil {
			return nil, fmt.Errorf("scan schema migration: %w", err)
		}
		versions[version] = true
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read schema migrations: %w", err)
	}
	return versions, nil
}

func applyUp(ctx context.Context, db *sql.DB, m migration) error {
	return withMigrationTx(ctx, db, m, false)
}

func applyDown(ctx context.Context, db *sql.DB, m migration) error {
	if m.down == "" {
		return fmt.Errorf("migrate: migration %04d (%s) has no down section", m.version, m.name)
	}
	return withMigrationTx(ctx, db, m, true)
}

func withMigrationTx(ctx context.Context, db *sql.DB, m migration, down bool) (err error) {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("migration %04d begin: %w", m.version, err)
	}
	defer func() {
		if err != nil {
			if rollbackErr := tx.Rollback(); rollbackErr != nil {
				err = fmt.Errorf("%w (rollback: %v)", err, rollbackErr)
			}
			return
		}
		if commitErr := tx.Commit(); commitErr != nil {
			err = fmt.Errorf("migration %04d commit: %w", m.version, commitErr)
		}
	}()
	if down {
		if _, err := tx.ExecContext(ctx, "DELETE FROM schema_migrations WHERE version = ?", m.version); err != nil {
			return fmt.Errorf("migration %04d remove record: %w", m.version, err)
		}
		if _, err := tx.ExecContext(ctx, m.down); err != nil {
			return fmt.Errorf("migration %04d down: %w", m.version, err)
		}
	} else {
		if _, err := tx.ExecContext(ctx, m.up); err != nil {
			return fmt.Errorf("migration %04d up: %w", m.version, err)
		}
		if _, err := tx.ExecContext(ctx, "INSERT INTO schema_migrations(version, applied_at) VALUES (?, ?)", m.version, nowMillis()); err != nil {
			return fmt.Errorf("migration %04d record: %w", m.version, err)
		}
	}
	return nil
}
