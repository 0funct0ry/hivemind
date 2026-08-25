package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/0funct0ry/hivemind/internal/auth"
	"github.com/0funct0ry/hivemind/internal/config"
	"github.com/0funct0ry/hivemind/internal/store"
)

func TestAPISearch(t *testing.T) {
	ctx := context.Background()
	s, err := store.Open(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err := s.Migrate(ctx); err != nil {
		t.Fatal(err)
	}

	a := auth.New(s, 30*24*time.Hour)
	cfg := config.Config{WorkspaceName: "Test", MaxUploadSize: 25 * 1024 * 1024}
	r := NewRouter(s, a, cfg)

	u1, _ := s.CreateUser(ctx, store.UserInput{Username: "alice", Email: "alice@example.com", PasswordHash: "hash"})

	sAlice, _ := a.CreateSession(ctx, u1.ID, "UA", "127.0.0.1")

	ch1, _ := s.GetChannelBySlug(ctx, "general")

	_, _, _ = s.CreateMessage(ctx, store.MessageInput{
		ChannelID: ch1.ID,
		UserID:    u1.ID,
		Body:      "This contains the magic search query token.",
	})

	t.Run("Valid Search", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/v1/search?q=magic", nil)
		req.AddCookie(&http.Cookie{Name: "hm_session", Value: sAlice})
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if w.Code != 200 {
			t.Errorf("Expected status 200, got %d. Body: %s", w.Code, w.Body.String())
		}

		var res struct {
			Data []store.Hit `json:"data"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &res); err != nil {
			t.Fatal(err)
		}
		if len(res.Data) != 1 {
			t.Errorf("Expected 1 hit, got %d", len(res.Data))
		}
	})

	t.Run("Empty search query fail", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/v1/search", nil)
		req.AddCookie(&http.Cookie{Name: "hm_session", Value: sAlice})
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if w.Code != 400 {
			t.Errorf("Expected status 400, got %d", w.Code)
		}
	})
}

func TestAPIChannelActivity(t *testing.T) {
	ctx := context.Background()
	s, err := store.Open(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err := s.Migrate(ctx); err != nil {
		t.Fatal(err)
	}

	a := auth.New(s, 30*24*time.Hour)
	cfg := config.Config{WorkspaceName: "Test", MaxUploadSize: 25 * 1024 * 1024}
	r := NewRouter(s, a, cfg)

	u1, _ := s.CreateUser(ctx, store.UserInput{Username: "alice", Email: "alice@example.com", PasswordHash: "hash"})
	sAlice, _ := a.CreateSession(ctx, u1.ID, "UA", "127.0.0.1")

	ch1, _ := s.GetChannelBySlug(ctx, "general")

	t.Run("Get Activity", func(t *testing.T) {
		req := httptest.NewRequest("GET", fmt.Sprintf("/api/v1/channels/%d/activity?buckets=12", ch1.ID), nil)
		req.AddCookie(&http.Cookie{Name: "hm_session", Value: sAlice})
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if w.Code != 200 {
			t.Errorf("Expected status 200, got %d. Body: %s", w.Code, w.Body.String())
		}
	})
}
