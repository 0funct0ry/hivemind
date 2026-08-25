package filestore

import (
	"bytes"
	"context"
	"database/sql"
	"image"
	"image/color"
	"image/png"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/0funct0ry/hivemind/internal/store"
)

func createTestStore(t *testing.T) (*store.Store, func()) {
	ctx := context.Background()
	dir := t.TempDir()
	s, err := store.Open(ctx, dir)
	if err != nil {
		t.Fatalf("failed to open store: %v", err)
	}
	if err := s.Migrate(ctx); err != nil {
		s.Close()
		t.Fatalf("failed to migrate store: %v", err)
	}
	if err := s.Tx(ctx, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `
			INSERT INTO users(id, username, email, password_hash, created_at, updated_at)
			VALUES (1, "testuser", "test@example.com", "hash", 0, 0)`)
		return err
	}); err != nil {
		s.Close()
		t.Fatalf("failed to seed test user: %v", err)
	}
	return s, func() {
		s.Close()
	}
}

func generatePNG(width, height int) []byte {
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			img.Set(x, y, color.RGBA{R: 255, G: 0, B: 0, A: 255})
		}
	}
	var buf bytes.Buffer
	_ = png.Encode(&buf, img)
	return buf.Bytes()
}

func TestFileStorePutAndGet(t *testing.T) {
	s, cleanup := createTestStore(t)
	defer cleanup()

	dir := t.TempDir()
	fs := New(dir, 1024*1024, s)
	ctx := context.Background()

	// 1. Basic put and open
	content := []byte("hello world")
	fileRec, err := fs.Put(ctx, bytes.NewReader(content), "hello.txt", 1)
	if err != nil {
		t.Fatalf("Put failed: %v", err)
	}

	if fileRec.Name != "hello.txt" {
		t.Errorf("expected name hello.txt, got %s", fileRec.Name)
	}
	if fileRec.Size != int64(len(content)) {
		t.Errorf("expected size %d, got %d", len(content), fileRec.Size)
	}

	rseeker, readRec, err := fs.Open(ctx, fileRec.ID)
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer rseeker.(io.Closer).Close()

	if readRec.ID != fileRec.ID {
		t.Errorf("ID mismatch: %s vs %s", readRec.ID, fileRec.ID)
	}

	gotBytes, err := io.ReadAll(rseeker)
	if err != nil {
		t.Fatalf("read all failed: %v", err)
	}
	if !bytes.Equal(gotBytes, content) {
		t.Errorf("content mismatch: %s vs %s", string(gotBytes), string(content))
	}
}

func TestFileStoreSanitizeFilename(t *testing.T) {
	s, cleanup := createTestStore(t)
	defer cleanup()

	dir := t.TempDir()
	fs := New(dir, 1024*1024, s)

	tests := []struct {
		input    string
		expected string
	}{
		{"../../etc/passwd", "etcpasswd"},
		{"..\\..\\etc\\passwd", "etcpasswd"},
		{".file.txt", "file.txt"},
		{"a\x00b.txt", "ab.txt"},
		{"", "file"},
		{strings.Repeat("a", 250) + ".txt", strings.Repeat("a", 200)},
	}

	for _, tc := range tests {
		got := fs.SanitizeFilename(tc.input)
		if got != tc.expected {
			t.Errorf("SanitizeFilename(%q) = %q, expected %q", tc.input, got, tc.expected)
		}
	}
}

func TestFileStoreMaxUploadSize(t *testing.T) {
	s, cleanup := createTestStore(t)
	defer cleanup()

	dir := t.TempDir()
	fs := New(dir, 5, s) // 5 bytes limit
	ctx := context.Background()

	_, err := fs.Put(ctx, bytes.NewReader([]byte("12345")), "ok.txt", 1)
	if err != nil {
		t.Fatalf("Put failed for valid size: %v", err)
	}

	_, err = fs.Put(ctx, bytes.NewReader([]byte("123456")), "too_large.txt", 1)
	if err == nil || !strings.Contains(err.Error(), "file too large") {
		t.Fatalf("expected file too large error, got %v", err)
	}
}

func TestFileStoreImageDimensions(t *testing.T) {
	s, cleanup := createTestStore(t)
	defer cleanup()

	dir := t.TempDir()
	fs := New(dir, 1024*1024, s)
	ctx := context.Background()

	imgBytes := generatePNG(120, 80)
	fileRec, err := fs.Put(ctx, bytes.NewReader(imgBytes), "test.png", 1)
	if err != nil {
		t.Fatalf("Put image failed: %v", err)
	}

	if fileRec.Width == nil || *fileRec.Width != 120 {
		t.Errorf("expected width 120, got %v", fileRec.Width)
	}
	if fileRec.Height == nil || *fileRec.Height != 80 {
		t.Errorf("expected height 80, got %v", fileRec.Height)
	}
}

func TestFileStoreCleanOrphans(t *testing.T) {
	s, cleanup := createTestStore(t)
	defer cleanup()

	dir := t.TempDir()
	fs := New(dir, 1024*1024, s)
	ctx := context.Background()

	// Put two files
	f1, err := fs.Put(ctx, bytes.NewReader([]byte("orphan")), "orphan.txt", 1)
	if err != nil {
		t.Fatal(err)
	}
	_, err = fs.Put(ctx, bytes.NewReader([]byte("attached")), "attached.txt", 1)
	if err != nil {
		t.Fatal(err)
	} // Age f1 so it is swept
	err = s.Tx(ctx, func(tx *sql.Tx) error {
		_, err = tx.ExecContext(ctx, "UPDATE files SET created_at = ? WHERE id = ?", s.NowMillis()-25*3600*1000, f1.ID)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}

	// Now clean orphans older than 24 hours
	deleted, err := fs.CleanOrphans(ctx, s.NowMillis()-24*3600*1000)
	if err != nil {
		t.Fatal(err)
	}

	if deleted != 1 {
		t.Errorf("expected 1 file deleted, got %d", deleted)
	}

	// f1 should be deleted from DB and disk
	_, err = s.GetFile(ctx, f1.ID)
	if err == nil {
		t.Error("expected f1 to be deleted from DB")
	}

	uploadsDir := filepath.Join(dir, "uploads")
	f1Path := filepath.Join(uploadsDir, f1.Sha256[0:2], f1.Sha256[2:4], f1.Sha256)
	if _, err := os.Stat(f1Path); !os.IsNotExist(err) {
		t.Error("expected physical file for f1 to be deleted")
	}
}
