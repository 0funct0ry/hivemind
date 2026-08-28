package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

// File represents a record in the files table.
type File struct {
	ID         string `json:"id"`
	Sha256     string `json:"sha256"`
	Name       string `json:"name"`
	Mime       string `json:"mime"`
	Size       int64  `json:"size"`
	Width      *int   `json:"width,omitempty"`
	Height     *int   `json:"height,omitempty"`
	UploadedBy int64  `json:"uploaded_by"`
	CreatedAt  int64  `json:"created_at"`
}

// GetFile retrieves a file record by its ID.
func (s *Store) GetFile(ctx context.Context, id string) (File, error) {
	var f File
	var wVal, hVal sql.NullInt64
	err := s.reader.QueryRowContext(ctx, `
		SELECT id, sha256, name, mime, size, width, height, uploaded_by, created_at
		FROM files
		WHERE id = ?`, id).Scan(
		&f.ID, &f.Sha256, &f.Name, &f.Mime, &f.Size, &wVal, &hVal, &f.UploadedBy, &f.CreatedAt,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return File{}, ErrNotFound
		}
		return File{}, fmt.Errorf("query file: %w", err)
	}
	if wVal.Valid {
		w := int(wVal.Int64)
		f.Width = &w
	}
	if hVal.Valid {
		h := int(hVal.Int64)
		f.Height = &h
	}
	return f, nil
}

// InsertFile inserts a new file record into the database.
func (s *Store) InsertFile(ctx context.Context, f File) error {
	var wVal, hVal sql.NullInt64
	if f.Width != nil {
		wVal = sql.NullInt64{Int64: int64(*f.Width), Valid: true}
	}
	if f.Height != nil {
		hVal = sql.NullInt64{Int64: int64(*f.Height), Valid: true}
	}
	_, err := s.writer.ExecContext(ctx, `
		INSERT INTO files (id, sha256, name, mime, size, width, height, uploaded_by, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		f.ID, f.Sha256, f.Name, f.Mime, f.Size, wVal, hVal, f.UploadedBy, f.CreatedAt,
	)
	if err != nil {
		return fmt.Errorf("insert file: %w", err)
	}
	return nil
}

// ExistsSha256 checks if any file record with the given sha256 exists.
func (s *Store) ExistsSha256(ctx context.Context, sha256 string) (bool, error) {
	var exists bool
	err := s.reader.QueryRowContext(ctx, `
		SELECT EXISTS(SELECT 1 FROM files WHERE sha256 = ?)`, sha256).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("check sha256 existence: %w", err)
	}
	return exists, nil
}

// CleanOrphanFiles deletes file records that are not referenced in the attachments table and were created more than 24 hours ago.
// It returns the list of sha256 hashes of deleted files that are no longer referenced by any remaining records.
func (s *Store) CleanOrphanFiles(ctx context.Context, cutoffTime int64) ([]string, error) {
	var orphans []struct {
		id     string
		sha256 string
	}
	err := s.Tx(ctx, func(tx *sql.Tx) error {
		rows, err := tx.QueryContext(ctx, `
			SELECT id, sha256 FROM files
			WHERE created_at < ? AND id NOT IN (SELECT file_id FROM attachments)
			AND id NOT IN (SELECT avatar_file_id FROM users WHERE avatar_file_id IS NOT NULL)`, cutoffTime)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var o struct {
				id     string
				sha256 string
			}
			if err := rows.Scan(&o.id, &o.sha256); err != nil {
				return err
			}
			orphans = append(orphans, o)
		}
		if err := rows.Err(); err != nil {
			return err
		}

		if len(orphans) == 0 {
			return nil
		}

		// Delete the orphaned file records from DB
		for _, o := range orphans {
			_, err = tx.ExecContext(ctx, "DELETE FROM files WHERE id = ?", o.id)
			if err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("clean orphan files transaction: %w", err)
	}

	if len(orphans) == 0 {
		return nil, nil
	}

	// Now check which sha256s are completely unreferenced in the files table
	var shasToDelete []string
	seen := make(map[string]bool)
	for _, o := range orphans {
		if seen[o.sha256] {
			continue
		}
		seen[o.sha256] = true

		var count int
		err := s.reader.QueryRowContext(ctx, "SELECT COUNT(*) FROM files WHERE sha256 = ?", o.sha256).Scan(&count)
		if err != nil {
			return nil, fmt.Errorf("check remaining files for sha256: %w", err)
		}
		if count == 0 {
			shasToDelete = append(shasToDelete, o.sha256)
		}
	}

	return shasToDelete, nil
}
