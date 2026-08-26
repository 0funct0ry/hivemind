package api

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"sync"
	"time"

	"github.com/0funct0ry/hivemind/internal/auth"
	"github.com/0funct0ry/hivemind/internal/store"
	"github.com/gin-gonic/gin"
)

type bootstrap struct {
	mu      sync.Mutex
	token   string
	expires time.Time
	used    bool
}

func newBootstrap(s *store.Store) (*bootstrap, error) {
	n, err := s.CountUsers(context.Background())
	if err != nil {
		return nil, err
	}
	if n > 0 {
		return &bootstrap{}, nil
	}
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return nil, fmt.Errorf("generate setup token: %w", err)
	}
	return &bootstrap{token: base64.RawURLEncoding.EncodeToString(b), expires: time.Now().Add(15 * time.Minute)}, nil
}
func (b *bootstrap) valid(token string) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return !b.used && b.token != "" && time.Now().Before(b.expires) && token == b.token
}
func (b *bootstrap) consume(token string) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.used || b.token == "" || time.Now().After(b.expires) || token != b.token {
		return false
	}
	b.used = true
	return true
}
func (b *bootstrap) gate(s *store.Store) gin.HandlerFunc {
	return func(c *gin.Context) {
		n, err := s.CountUsers(c.Request.Context())
		if err != nil {
			c.AbortWithStatusJSON(500, gin.H{"error": gin.H{"code": "internal_error", "message": "Could not inspect workspace state."}})
			return
		}
		if n == 0 {
			c.AbortWithStatusJSON(503, gin.H{"error": gin.H{"code": "setup_required", "message": "Workspace setup is required."}})
			return
		}
		c.Next()
	}
}
func setupCreate(s *store.Store, a *auth.Service, b *bootstrap) gin.HandlerFunc {
	return func(c *gin.Context) {
		var in struct {
			Token    string `json:"token" form:"token"`
			Username string `json:"username" form:"username"`
			Email    string `json:"email" form:"email"`
			Password string `json:"password" form:"password"`
		}
		if err := c.ShouldBind(&in); err != nil {
			c.JSON(400, gin.H{"error": gin.H{"code": "invalid_request", "message": "Invalid setup request."}})
			return
		}
		if !b.valid(in.Token) {
			c.JSON(400, gin.H{"error": gin.H{"code": "invalid_setup_token", "message": "Setup token is invalid or expired."}})
			return
		}
		if err := auth.ValidatePassword(in.Password); err != nil {
			c.JSON(400, gin.H{"error": gin.H{"code": "invalid_password", "message": err.Error()}})
			return
		}
		hash, err := auth.HashPassword(in.Password)
		if err != nil {
			c.JSON(500, gin.H{"error": gin.H{"code": "internal_error", "message": "Could not hash password."}})
			return
		}
		if !b.consume(in.Token) {
			c.JSON(409, gin.H{"error": gin.H{"code": "setup_already_complete", "message": "Setup has already been completed."}})
			return
		}
		u, err := s.CreateUser(c.Request.Context(), store.UserInput{Username: in.Username, Email: in.Email, PasswordHash: hash, Role: "admin"})
		if err != nil {
			c.JSON(400, gin.H{"error": gin.H{"code": "invalid_user", "message": err.Error()}})
			return
		}
		c.JSON(201, gin.H{"user": publicUser(u)})
	}
}

func printSetupURL(addr, token string) {
	fmt.Printf("\n+--------------------------------------+\n| Hivemind setup: http://%s/setup?token=%s |\n+--------------------------------------+\n\n", addr, token)
}
