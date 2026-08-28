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
	cfg := config.Config{WorkspaceName: "Test", MaxUploadSize: 25 * 1024 * 1024}
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

// TestChannelIDsSerializeAsStrings guards against store.Channel's int64 id fields
// leaking as bare JSON numbers through channelCreate/channelGet/channelUpdate — every
// other channel-returning endpoint (channelList, channelJoin, ...) goes through
// publicChannelDetails, which string-encodes ids; these three must match.
func TestChannelIDsSerializeAsStrings(t *testing.T) {
	tc := setupTestContext(t)
	defer tc.close()

	type channelIDResp struct {
		Channel struct {
			ID        string `json:"id"`
			CreatedBy string `json:"created_by"`
		} `json:"channel"`
	}

	// channelCreate
	code, resp := tc.request("POST", "/api/v1/channels", tc.sMember, map[string]any{
		"kind": "public", "slug": "id-string-check", "name": "ID String Check",
	})
	if code != 201 {
		t.Fatalf("expected 201 creating channel, got %d. Resp: %s", code, resp)
	}
	var created channelIDResp
	if err := json.Unmarshal([]byte(resp), &created); err != nil {
		t.Fatalf("channelCreate response: channel.id did not unmarshal as a string: %v. Resp: %s", err, resp)
	}
	if created.Channel.ID == "" {
		t.Error("channelCreate: expected a non-empty channel.id")
	}
	if created.Channel.CreatedBy == "" {
		t.Error("channelCreate: expected a non-empty channel.created_by")
	}

	// channelGet
	code, resp = tc.request("GET", "/api/v1/channels/"+created.Channel.ID, tc.sMember, nil)
	if code != 200 {
		t.Fatalf("expected 200 getting channel, got %d. Resp: %s", code, resp)
	}
	var got channelIDResp
	if err := json.Unmarshal([]byte(resp), &got); err != nil {
		t.Fatalf("channelGet response: channel.id did not unmarshal as a string: %v. Resp: %s", err, resp)
	}
	if got.Channel.ID != created.Channel.ID {
		t.Errorf("channelGet: expected channel.id %q, got %q", created.Channel.ID, got.Channel.ID)
	}

	// channelUpdate
	code, resp = tc.request("PATCH", "/api/v1/channels/"+created.Channel.ID, tc.sMember, map[string]any{
		"name": "Renamed", "topic": "New topic",
	})
	if code != 200 {
		t.Fatalf("expected 200 updating channel, got %d. Resp: %s", code, resp)
	}
	var updated channelIDResp
	if err := json.Unmarshal([]byte(resp), &updated); err != nil {
		t.Fatalf("channelUpdate response: channel.id did not unmarshal as a string: %v. Resp: %s", err, resp)
	}
	if updated.Channel.ID != created.Channel.ID {
		t.Errorf("channelUpdate: expected channel.id %q, got %q", created.Channel.ID, updated.Channel.ID)
	}
}

// TestArchiveChannelAuthzMatrix covers the M17.3 archive-lifecycle additions to the M6
// authorization matrix: only the owner or a workspace admin may archive a channel, archived
// public channels stay readable but reject new posts and re-joins.
func TestArchiveChannelAuthzMatrix(t *testing.T) {
	t.Run("non-owner member cannot archive", func(t *testing.T) {
		tc := setupTestContext(t)
		defer tc.close()

		code, resp := tc.request("PATCH", fmt.Sprintf("/api/v1/channels/%d", tc.pubCh.ID), tc.sMember, map[string]any{"archived": true})
		if code != 403 {
			t.Fatalf("expected 403 for non-owner archive, got %d. Resp: %s", code, resp)
		}
	})

	t.Run("admin archives a channel it doesn't own", func(t *testing.T) {
		tc := setupTestContext(t)
		defer tc.close()

		// privCh is owned by uMember; uAdmin is not a member or owner.
		code, resp := tc.request("PATCH", fmt.Sprintf("/api/v1/channels/%d", tc.privCh.ID), tc.sAdmin, map[string]any{"archived": true})
		if code != 200 {
			t.Fatalf("expected 200 for admin archive, got %d. Resp: %s", code, resp)
		}
	})

	t.Run("owner archives, member reads but cannot post or rejoin", func(t *testing.T) {
		tc := setupTestContext(t)
		defer tc.close()

		// pubCh is owned by uAdmin, and uMember is a member.
		code, resp := tc.request("PATCH", fmt.Sprintf("/api/v1/channels/%d", tc.pubCh.ID), tc.sAdmin, map[string]any{"archived": true})
		if code != 200 {
			t.Fatalf("expected 200 for owner archive, got %d. Resp: %s", code, resp)
		}

		code, resp = tc.request("GET", fmt.Sprintf("/api/v1/channels/%d", tc.pubCh.ID), tc.sMember, nil)
		if code != 200 {
			t.Fatalf("expected member to still read archived channel, got %d. Resp: %s", code, resp)
		}

		code, resp = tc.request("POST", fmt.Sprintf("/api/v1/channels/%d/messages", tc.pubCh.ID), tc.sMember, map[string]any{"body": "hello"})
		if code != 400 {
			t.Fatalf("expected 400 posting to archived channel, got %d. Resp: %s", code, resp)
		}

		code, resp = tc.request("POST", fmt.Sprintf("/api/v1/channels/%d/leave", tc.pubCh.ID), tc.sMember, nil)
		if code != 204 {
			t.Fatalf("expected 204 leaving archived channel, got %d. Resp: %s", code, resp)
		}

		code, resp = tc.request("POST", fmt.Sprintf("/api/v1/channels/%d/join", tc.pubCh.ID), tc.sMember, nil)
		if code != 400 {
			t.Fatalf("expected 400 re-joining archived channel, got %d. Resp: %s", code, resp)
		}
	})

	t.Run("archiving a DM is rejected", func(t *testing.T) {
		tc := setupTestContext(t)
		defer tc.close()

		code, resp := tc.request("PATCH", fmt.Sprintf("/api/v1/channels/%d", tc.dmCh.ID), tc.sAdmin, map[string]any{"archived": true})
		if code != 400 {
			t.Fatalf("expected 400 archiving a DM, got %d. Resp: %s", code, resp)
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
