package filestore

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base32"
	"encoding/hex"
	"errors"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"github.com/0funct0ry/hivemind/internal/store"
	"github.com/gabriel-vasile/mimetype"
)

var (
	ErrFileTooLarge = errors.New("file too large")
)

// FileStore handles storing files content-addressed on disk and indexing them in SQLite.
type FileStore struct {
	dataDir       string
	maxUploadSize int64
	store         *store.Store
}

// New constructs a new FileStore.
func New(dataDir string, maxUploadSize int64, s *store.Store) *FileStore {
	return &FileStore{
		dataDir:       dataDir,
		maxUploadSize: maxUploadSize,
		store:         s,
	}
}

// Put streams from r to a temp file while computing sha256, enforces maxUploadSize,
// sniffs MIME type, decodes image dimensions (png, jpeg, gif), and stores the file
// in uploads/<sha[0:2]>/<sha[2:4]>/<sha>. If the blob already exists, it is deduplicated.
func (fs *FileStore) Put(ctx context.Context, r io.Reader, name string, uploader int64) (*store.File, error) {
	uploadsDir := filepath.Join(fs.dataDir, "uploads")
	tmpDir := filepath.Join(uploadsDir, "tmp")
	if err := os.MkdirAll(tmpDir, 0o750); err != nil {
		return nil, fmt.Errorf("create uploads tmp dir: %w", err)
	}

	tmpFile, err := os.CreateTemp(tmpDir, "upload-*")
	if err != nil {
		return nil, fmt.Errorf("create temp file: %w", err)
	}
	tempPath := tmpFile.Name()
	defer func() {
		if tempPath != "" {
			_ = tmpFile.Close()
			_ = os.Remove(tempPath)
		}
	}()

	sanitizedName := fs.SanitizeFilename(name)
	hasher := sha256.New()
	limitReader := io.LimitReader(r, fs.maxUploadSize+1)

	var totalWritten int64
	var first512 []byte
	buf := make([]byte, 32*1024)

	for {
		n, err := limitReader.Read(buf)
		if n > 0 {
			chunk := buf[:n]
			totalWritten += int64(n)
			if totalWritten > fs.maxUploadSize {
				return nil, ErrFileTooLarge
			}

			if len(first512) < 512 {
				needed := 512 - len(first512)
				if needed > len(chunk) {
					needed = len(chunk)
				}
				first512 = append(first512, chunk[:needed]...)
			}

			if _, werr := tmpFile.Write(chunk); werr != nil {
				return nil, fmt.Errorf("write to temp file: %w", werr)
			}
			if _, herr := hasher.Write(chunk); herr != nil {
				return nil, fmt.Errorf("write to hash: %w", herr)
			}
		}
		if err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return nil, fmt.Errorf("read upload stream: %w", err)
		}
	}

	if err := tmpFile.Sync(); err != nil {
		return nil, fmt.Errorf("sync temp file: %w", err)
	}
	_ = tmpFile.Close()

	mimeType := "application/octet-stream"
	if len(first512) > 0 {
		m := mimetype.Detect(first512)
		mimeType = m.String()
	}

	shaSum := hex.EncodeToString(hasher.Sum(nil))

	var width, height *int
	if strings.HasPrefix(mimeType, "image/png") || strings.HasPrefix(mimeType, "image/jpeg") || strings.HasPrefix(mimeType, "image/gif") {
		if f, oerr := os.Open(tempPath); oerr == nil {
			if cfg, _, derr := image.DecodeConfig(f); derr == nil {
				w := cfg.Width
				h := cfg.Height
				width = &w
				height = &h
			}
			_ = f.Close()
		}
	}

	shardedDir := filepath.Join(uploadsDir, shaSum[0:2], shaSum[2:4])
	destPath := filepath.Join(shardedDir, shaSum)

	existsDisk := false
	if _, err := os.Stat(destPath); err == nil {
		existsDisk = true
	}

	if existsDisk {
		_ = os.Remove(tempPath)
		tempPath = ""
	} else {
		if err := os.MkdirAll(shardedDir, 0o750); err != nil {
			return nil, fmt.Errorf("create sharded dir: %w", err)
		}
		if err := os.Rename(tempPath, destPath); err != nil {
			return nil, fmt.Errorf("rename temp file to dest: %w", err)
		}
		tempPath = ""
	}

	id, err := generateBase32ID()
	if err != nil {
		return nil, fmt.Errorf("generate file ID: %w", err)
	}

	now := fs.store.NowMillis()
	f := store.File{
		ID:         id,
		Sha256:     shaSum,
		Name:       sanitizedName,
		Mime:       mimeType,
		Size:       totalWritten,
		Width:      width,
		Height:     height,
		UploadedBy: uploader,
		CreatedAt:  now,
	}

	if err := fs.store.InsertFile(ctx, f); err != nil {
		return nil, err
	}

	return &f, nil
}

// Open returns a ReadSeeker for the content of the file and the file record.
func (fs *FileStore) Open(ctx context.Context, id string) (io.ReadSeeker, *store.File, error) {
	f, err := fs.store.GetFile(ctx, id)
	if err != nil {
		return nil, nil, err
	}

	uploadsDir := filepath.Join(fs.dataDir, "uploads")
	filePath := filepath.Join(uploadsDir, f.Sha256[0:2], f.Sha256[2:4], f.Sha256)
	file, err := os.Open(filePath)
	if err != nil {
		return nil, nil, fmt.Errorf("open physical file: %w", err)
	}

	return file, &f, nil
}

// CleanOrphans deletes orphaned files from the DB and their CAS filesystem blobs.
func (fs *FileStore) CleanOrphans(ctx context.Context, cutoffTime int64) (int, error) {
	shas, err := fs.store.CleanOrphanFiles(ctx, cutoffTime)
	if err != nil {
		return 0, err
	}

	uploadsDir := filepath.Join(fs.dataDir, "uploads")
	deletedCount := 0
	for _, sha := range shas {
		filePath := filepath.Join(uploadsDir, sha[0:2], sha[2:4], sha)
		if err := os.Remove(filePath); err == nil {
			deletedCount++
		}
	}
	return deletedCount, nil
}

// SanitizeFilename strips path separators, control characters, and leading dots; caps at 200 bytes; empty name becomes "file".
func (fs *FileStore) SanitizeFilename(name string) string {
	name = strings.ReplaceAll(name, "/", "")
	name = strings.ReplaceAll(name, "\\", "")

	var sb strings.Builder
	for _, r := range name {
		if r < 32 || r == 127 {
			continue
		}
		sb.WriteRune(r)
	}
	res := sb.String()
	res = strings.TrimLeft(res, ".")

	if len(res) > 200 {
		res = truncateBytes(res, 200)
	}
	if res == "" {
		res = "file"
	}
	return res
}

func truncateBytes(s string, limit int) string {
	if len(s) <= limit {
		return s
	}
	res := s[:limit]
	for len(res) > 0 && !utf8.ValidString(res) {
		res = res[:len(res)-1]
	}
	return res
}

func generateBase32ID() (string, error) {
	b := make([]byte, 10)
	if _, err := io.ReadFull(rand.Reader, b); err != nil {
		return "", err
	}
	return base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(b), nil
}
