package api

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/0funct0ry/hivemind/internal/auth"
	"github.com/0funct0ry/hivemind/internal/config"
	"github.com/0funct0ry/hivemind/internal/store"
)

type readsTestCtx struct {
	ctx    context.Context
	s      *store.Store
	a      *auth.Service
	r      http.Handler
	u1     store.User
	u2     store.User
	s1     string
	s2     string
	pubCh  store.Channel
	privCh store.Channel
}

func setupReadsTestContext(t *testing.T) *readsTestCtx {
	ctx := context.Background()
	s, err := store.Open(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Migrate(ctx); err != nil {
		s.Close()
		t.Fatal(err)
	}

	a := auth.New(s, 30*24*time.Hour)
	cfg := config.Config{WorkspaceName: "Test"}
	r := NewRouter(s, a, cfg)

	// Create users
	u1, _ := s.CreateUser(ctx, store.UserInput{Username: "userone", Email: "one@example.com", PasswordHash: "hash"})
	u2, _ := s.CreateUser(ctx, store.UserInput{Username: "usertwo", Email: "two@example.com", PasswordHash: "hash"})

	// Create sessions
	s1, _ := a.CreateSession(ctx, u1.ID, "UA", "127.0.0.1")
	s2, _ := a.CreateSession(ctx, u2.ID, "UA", "127.0.0.1")

	// Create Channels:
	// 1. Public channel with u1 only
	pubCh, _ := s.CreateChannel(ctx, "public", "pub-unread", "Pub", "", u1.ID, []int64{u1.ID})
	// 2. Private channel with both u1 and u2
	privCh, _ := s.CreateChannel(ctx, "private", "priv-unread", "Priv", "", u1.ID, []int64{u1.ID, u2.ID})

	return &readsTestCtx{
		ctx:    ctx,
		s:      s,
		a:      a,
		r:      r,
		u1:     u1,
		u2:     u2,
		s1:     s1,
		s2:     s2,
		pubCh:  pubCh,
		privCh: privCh,
	}
}

func (tc *readsTestCtx) request(method, url, sessionToken string, body any) (int, string) {
	var reqBody io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		reqBody = bytes.NewReader(b)
	}
	req, _ := http.NewRequest(method, url, reqBody)
	req.Host = "example.com"
	req.Header.Set("Origin", "http://example.com")
	if sessionToken != "" {
		req.AddCookie(&http.Cookie{Name: "hm_session", Value: sessionToken})
	}
	w := httptest.NewRecorder()
	tc.r.ServeHTTP(w, req)
	return w.Code, w.Body.String()
}

func (tc *readsTestCtx) close() {
	tc.s.Close()
}

func TestUnreadsAndMarkReadAPI(t *testing.T) {
	tc := setupReadsTestContext(t)
	defer tc.close()

	// 1. Create a message in the private channel as u1
	m1, _, err := tc.s.CreateMessage(tc.ctx, store.MessageInput{
		ChannelID: tc.privCh.ID,
		UserID:    tc.u1.ID,
		Body:      "Message in private",
	})
	if err != nil {
		t.Fatal(err)
	}

	// 2. Get unreads for u2
	code, body := tc.request("GET", "/api/v1/unreads", tc.s2, nil)
	if code != 200 {
		t.Fatalf("expected 200, got %d: %s", code, body)
	}

	var resp struct {
		Data []struct {
			ChannelID    int64 `json:"channel_id"`
			UnreadCount  int   `json:"unread_count"`
			MentionCount int   `json:"mention_count"`
			Joined       bool  `json:"joined"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(body), &resp); err != nil {
		t.Fatal(err)
	}

	// We expect 3 channels: general (auto-seeded, joined by both), pub-unread (public, visible, not joined by u2), priv-unread (private, joined by both)
	if len(resp.Data) != 3 {
		t.Fatalf("expected 3 channels in unread summary, got %d: %+v", len(resp.Data), resp.Data)
	}

	var foundPriv bool
	for _, item := range resp.Data {
		if item.ChannelID == tc.privCh.ID {
			foundPriv = true
			if item.UnreadCount != 1 {
				t.Errorf("expected 1 unread in private channel, got %d", item.UnreadCount)
			}
			if !item.Joined {
				t.Error("expected joined=true for private channel")
			}
		} else if item.ChannelID == tc.pubCh.ID {
			if item.UnreadCount != 0 {
				t.Errorf("expected 0 unreads for unjoined public channel, got %d", item.UnreadCount)
			}
			if item.Joined {
				t.Error("expected joined=false for unjoined public channel")
			}
		}
	}
	if !foundPriv {
		t.Error("did not find private channel in unreads")
	}

	// 3. Mark read for u2
	readBody := map[string]string{
		"message_id": strconv.FormatInt(m1.ID, 10),
	}
	code, body = tc.request("POST", "/api/v1/channels/"+strconv.FormatInt(tc.privCh.ID, 10)+"/read", tc.s2, readBody)
	if code != 204 {
		t.Fatalf("expected 204, got %d: %s", code, body)
	}

	// 4. Verify unreads is now 0
	code, body = tc.request("GET", "/api/v1/unreads", tc.s2, nil)
	if code != 200 {
		t.Fatalf("expected 200, got %d: %s", code, body)
	}
	if err := json.Unmarshal([]byte(body), &resp); err != nil {
		t.Fatal(err)
	}

	for _, item := range resp.Data {
		if item.ChannelID == tc.privCh.ID {
			if item.UnreadCount != 0 {
				t.Errorf("expected 0 unread in private channel after marking read, got %d", item.UnreadCount)
			}
		}
	}
}
