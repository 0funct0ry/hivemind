package httpx

import (
	"crypto/rand"
	"encoding/hex"
	"log/slog"
	"net/http"

	"github.com/gin-gonic/gin"
)

// ErrorEnvelope is the stable API error shape.
type ErrorEnvelope struct {
	Error ErrorBody `json:"error"`
}

// ErrorBody describes an API failure.
type ErrorBody struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Field   any    `json:"field"`
}

// Fail writes an error envelope.
func Fail(c *gin.Context, status int, code, msg string) {
	c.AbortWithStatusJSON(status, ErrorEnvelope{Error: ErrorBody{Code: code, Message: msg}})
}

// FailField writes an error envelope naming a field.
func FailField(c *gin.Context, status int, code, msg, field string) {
	c.AbortWithStatusJSON(status, ErrorEnvelope{Error: ErrorBody{Code: code, Message: msg, Field: field}})
}

// RequestID adds or generates a request ID.
func RequestID() gin.HandlerFunc {
	return func(c *gin.Context) {
		id := c.GetHeader("X-Request-ID")
		if id == "" {
			b := make([]byte, 12)
			_, _ = rand.Read(b)
			id = hex.EncodeToString(b)
		}
		c.Set("request_id", id)
		c.Header("X-Request-ID", id)
		c.Next()
	}
}

// Recovery logs panics and returns a generic internal error.
func Recovery(log *slog.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		defer func() {
			if v := recover(); v != nil {
				id, _ := c.Get("request_id")
				if log != nil {
					log.Error("panic recovered", "request_id", id, "panic", v)
				}
				Fail(c, http.StatusInternalServerError, "internal_error", "An internal error occurred.")
			}
		}()
		c.Next()
	}
}
