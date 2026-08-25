package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/0funct0ry/hivemind/internal/auth"
	"github.com/0funct0ry/hivemind/internal/config"
	"github.com/0funct0ry/hivemind/internal/store"
)

type testContext struct {
	ctx        context.Context
	s          *store.Store
	a          *auth.Service
	r          http.Handler
	uAdmin     store.User
	uMember    store.User
	uNonMember store.User
	sAdmin     string
	sMember    string
	sNonMember string
	pubCh      store.Channel
	privCh     store.Channel
	dmCh       store.Channel
}

func setupTestContext(t *testing.T) *testContext {
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
	uAdmin, _ := s.CreateUser(ctx, store.UserInput{Username: "adminuser", Email: "admin@example.com", PasswordHash: "hash", Role: "admin"})
	uMember, _ := s.CreateUser(ctx, store.UserInput{Username: "memberuser", Email: "member@example.com", PasswordHash: "hash"})
	uNonMember, _ := s.CreateUser(ctx, store.UserInput{Username: "nonmember", Email: "nonmember@example.com", PasswordHash: "hash"})

	// Create sessions
	sAdmin, _ := a.CreateSession(ctx, uAdmin.ID, "UA", "127.0.0.1")
	sMember, _ := a.CreateSession(ctx, uMember.ID, "UA", "127.0.0.1")
	sNonMember, _ := a.CreateSession(ctx, uNonMember.ID, "UA", "127.0.0.1")

	// Create Channels:
	// 1. Public channel (uAdmin is creator/owner, uMember joins, uNonMember does not)
	pubCh, _ := s.CreateChannel(ctx, "public", "pub-chan", "Pub", "", uAdmin.ID, []int64{uAdmin.ID, uMember.ID})
	// 2. Private channel (uMember is creator/owner, uNonMember and uAdmin are not members)
	privCh, _ := s.CreateChannel(ctx, "private", "priv-chan", "Priv", "", uMember.ID, []int64{uMember.ID})
	// 3. DM (between uAdmin and uMember; uNonMember is not participant)
	dmCh, _ := s.CreateChannel(ctx, "dm", "", "", "", uAdmin.ID, []int64{uAdmin.ID, uMember.ID})

	return &testContext{
		ctx:        ctx,
		s:          s,
		a:          a,
		r:          r,
		uAdmin:     uAdmin,
		uMember:    uMember,
		uNonMember: uNonMember,
		sAdmin:     sAdmin,
		sMember:    sMember,
		sNonMember: sNonMember,
		pubCh:      pubCh,
		privCh:     privCh,
		dmCh:       dmCh,
	}
}

func (tc *testContext) request(method, url, sessionToken string, body any) (int, string) {
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

func (tc *testContext) close() {
	tc.s.Close()
}

func TestAuthzMatrix(t *testing.T) {
	// 1. Test GET /channels/:id (Read)
	t.Run("Read Access Matrix", func(t *testing.T) {
		tc := setupTestContext(t)
		defer tc.close()

		tests := []struct {
			name       string
			channelID  int64
			session    string
			wantStatus int
		}{
			{"Public-Member", tc.pubCh.ID, tc.sMember, 200},
			{"Public-NonMember", tc.pubCh.ID, tc.sNonMember, 200},
			{"Public-Admin", tc.pubCh.ID, tc.sAdmin, 200},

			{"Private-Member", tc.privCh.ID, tc.sMember, 200},
			{"Private-NonMember", tc.privCh.ID, tc.sNonMember, 404},
			{"Private-Admin", tc.privCh.ID, tc.sAdmin, 404}, // admin is non-member, gets 404 on read

			{"DM-Member", tc.dmCh.ID, tc.sMember, 200},
			{"DM-Admin", tc.dmCh.ID, tc.sAdmin, 200},
			{"DM-NonMember", tc.dmCh.ID, tc.sNonMember, 404},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				code, _ := tc.request("GET", fmt.Sprintf("/api/v1/channels/%d", tt.channelID), tt.session, nil)
				if code != tt.wantStatus {
					t.Errorf("GET got code %d, want %d", code, tt.wantStatus)
				}
			})
		}
	})

	// 2. Test POST /channels/:id/members (Add member)
	t.Run("Add Member Access Matrix", func(t *testing.T) {
		tests := []struct {
			name       string
			channel    func(*testContext) int64
			session    func(*testContext) string
			wantStatus int
		}{
			// Public channel (members can add)
			{"Public-Member", func(tc *testContext) int64 { return tc.pubCh.ID }, func(tc *testContext) string { return tc.sMember }, 204},
			{"Public-NonMember", func(tc *testContext) int64 { return tc.pubCh.ID }, func(tc *testContext) string { return tc.sNonMember }, 403},

			// Private channel (only owner/admin can add)
			{"Private-Owner", func(tc *testContext) int64 { return tc.privCh.ID }, func(tc *testContext) string { return tc.sMember }, 204},
			{"Private-Admin", func(tc *testContext) int64 { return tc.privCh.ID }, func(tc *testContext) string { return tc.sAdmin }, 204},
			{"Private-NonMember", func(tc *testContext) int64 { return tc.privCh.ID }, func(tc *testContext) string { return tc.sNonMember }, 404},

			// DM (no one can add members)
			{"DM-Member", func(tc *testContext) int64 { return tc.dmCh.ID }, func(tc *testContext) string { return tc.sMember }, 400},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				tc := setupTestContext(t)
				defer tc.close()

				body := map[string]any{"user_ids": []int64{tc.uNonMember.ID}}
				code, _ := tc.request("POST", fmt.Sprintf("/api/v1/channels/%d/members", tt.channel(tc)), tt.session(tc), body)
				if code != tt.wantStatus {
					t.Errorf("POST got code %d, want %d", code, tt.wantStatus)
				}
			})
		}
	})
}

func TestAdminCannotReadDM(t *testing.T) {
	tc := setupTestContext(t)
	defer tc.close()

	// DM is between uMember and uNonMember
	dmCh, _ := tc.s.CreateChannel(tc.ctx, "dm", "", "", "", tc.uMember.ID, []int64{tc.uMember.ID, tc.uNonMember.ID})

	code, _ := tc.request("GET", fmt.Sprintf("/api/v1/channels/%d", dmCh.ID), tc.sAdmin, nil)
	if code != 404 {
		t.Fatalf("expected admin to get 404 for someone else's DM, got %d", code)
	}
}
