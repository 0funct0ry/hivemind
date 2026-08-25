package api

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/0funct0ry/hivemind/internal/api/httpx"
	"github.com/0funct0ry/hivemind/internal/filestore"
	"github.com/0funct0ry/hivemind/internal/store"
	"github.com/gin-gonic/gin"
)

// StartOrphanSweeper starts a background worker that cleans up orphaned files/blobs every 24 hours.
func StartOrphanSweeper(ctx context.Context, fs *filestore.FileStore, logger *slog.Logger) {
	ticker := time.NewTicker(24 * time.Hour)
	go func() {
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				cutoff := time.Now().Add(-24 * time.Hour).UnixMilli()
				deleted, err := fs.CleanOrphans(ctx, cutoff)
				if err != nil {
					logger.Error("orphan file sweep failed", "error", err)
				} else if deleted > 0 {
					logger.Info("orphan file sweep completed", "deleted_blobs_count", deleted)
				}
			}
		}
	}()
}

func uploadFile(fs *filestore.FileStore) gin.HandlerFunc {
	return func(c *gin.Context) {
		me, ok := CurrentUser(c)
		if !ok {
			httpx.Fail(c, 401, "unauthenticated", "Authentication required.")
			return
		}

		fileHeader, err := c.FormFile("file")
		if err != nil {
			httpx.Fail(c, 400, "invalid_request", "Missing file parameter.")
			return
		}

		fileStream, err := fileHeader.Open()
		if err != nil {
			httpx.Fail(c, 500, "internal_error", "Failed to open upload stream.")
			return
		}
		defer fileStream.Close()

		fileRec, err := fs.Put(c.Request.Context(), fileStream, fileHeader.Filename, me.ID)
		if err != nil {
			if errors.Is(err, filestore.ErrFileTooLarge) {
				httpx.Fail(c, 413, "file_too_large", "The uploaded file exceeds the maximum allowed size.")
				return
			}
			httpx.Fail(c, 500, "internal_error", fmt.Sprintf("Failed to store file: %v", err))
			return
		}

		c.JSON(201, publicFile(*fileRec))
	}
}

func serveFile(fs *filestore.FileStore) gin.HandlerFunc {
	return func(c *gin.Context) {
		id := c.Param("id")

		rseeker, fileRec, err := fs.Open(c.Request.Context(), id)
		if err != nil {
			if errors.Is(err, store.ErrNotFound) {
				httpx.Fail(c, 404, "file_not_found", "The requested file does not exist.")
				return
			}
			httpx.Fail(c, 500, "internal_error", "Failed to open file.")
			return
		}
		defer rseeker.(io.Closer).Close()

		c.Header("X-Content-Type-Options", "nosniff")
		c.Header("Content-Security-Policy", "default-src 'none'; sandbox")
		c.Header("Cache-Control", "private, max-age=31536000, immutable")

		disposition := "attachment"
		switch fileRec.Mime {
		case "image/png", "image/jpeg", "image/gif", "image/webp", "video/mp4", "application/pdf":
			disposition = "inline"
		}
		c.Header("Content-Disposition", fmt.Sprintf("%s; filename=%q", disposition, fileRec.Name))

		http.ServeContent(c.Writer, c.Request, fileRec.Name, time.UnixMilli(fileRec.CreatedAt), rseeker)
	}
}

func publicFile(f store.File) gin.H {
	res := gin.H{
		"id":          f.ID,
		"sha256":      f.Sha256,
		"name":        f.Name,
		"mime":        f.Mime,
		"size":        f.Size,
		"uploaded_by": strconv.FormatInt(f.UploadedBy, 10),
		"created_at":  f.CreatedAt,
	}
	if f.Width != nil {
		res["width"] = *f.Width
	}
	if f.Height != nil {
		res["height"] = *f.Height
	}
	return res
}
