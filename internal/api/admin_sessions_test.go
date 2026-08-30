package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/0funct0ry/hivemind/internal/store"
)

// bearerRequest mirrors testContext.request but authenticates via an Authorization: Bearer
// header instead of a session cookie, for exercising admin-session rotate/disable/enable/revoke
// against the plaintext they hand out.
func (tc *testContext) bearerRequest(method, url, token string, body any) (int, string) {
	var reqBody io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		reqBody = bytes.NewReader(b)
	}
	req, _ := http.NewRequest(method, url, reqBody)
	req.Host = "example.com"
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	w := httptest.NewRecorder()
	tc.r.ServeHTTP(w, req)
	return w.Code, w.Body.String()
}

func TestAdminSessionRoutesRequireAdmin(t *testing.T) {
	tc := setupTestContext(t)
	defer tc.close()

	id, _, err := tc.a.CreateToken(tc.ctx, tc.uMember.ID, "chat-cli", 0, store.TokenPurposeCLISession)
	if err != nil {
		t.Fatal(err)
	}

	routes := []struct {
		method, path string
	}{
		{"GET", "/api/v1/admin/sessions"},
		{"POST", fmt.Sprintf("/api/v1/admin/sessions/%d/disable", id)},
		{"POST", fmt.Sprintf("/api/v1/admin/sessions/%d/enable", id)},
		{"POST", fmt.Sprintf("/api/v1/admin/sessions/%d/rotate", id)},
		{"DELETE", fmt.Sprintf("/api/v1/admin/sessions/%d", id)},
	}
	for _, r := range routes {
		code, resp := tc.request(r.method, r.path, tc.sMember, nil)
		if code != 403 {
			t.Errorf("%s %s as non-admin: code = %d, want 403. Resp: %s", r.method, r.path, code, resp)
		}
	}
}

func TestAdminSessionLifecycle(t *testing.T) {
	tc := setupTestContext(t)
	defer tc.close()

	id, plain, err := tc.a.CreateToken(tc.ctx, tc.uMember.ID, "chat-cli", 0, store.TokenPurposeCLISession)
	if err != nil {
		t.Fatal(err)
	}

	code, resp := tc.request("GET", "/api/v1/admin/sessions", tc.sAdmin, nil)
	if code != 200 {
		t.Fatalf("list sessions: code = %d, want 200. Resp: %s", code, resp)
	}
	var listOut struct {
		Data []struct {
			ID       string `json:"id"`
			Username string `json:"username"`
			Disabled bool   `json:"disabled"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(resp), &listOut); err != nil {
		t.Fatalf("unmarshal: %v. Resp: %s", err, resp)
	}
	if len(listOut.Data) != 1 || listOut.Data[0].Username != "memberuser" || listOut.Data[0].Disabled {
		t.Fatalf("unexpected list contents: %#v", listOut.Data)
	}

	// Bearer auth with the plaintext works before disabling.
	code, _ = tc.bearerRequest("GET", "/api/v1/auth/me", plain, nil)
	if code != 200 {
		t.Fatalf("bearer auth before disable: code = %d, want 200", code)
	}

	code, resp = tc.request("POST", fmt.Sprintf("/api/v1/admin/sessions/%d/disable", id), tc.sAdmin, nil)
	if code != 204 {
		t.Fatalf("disable: code = %d, want 204. Resp: %s", code, resp)
	}
	code, _ = tc.bearerRequest("GET", "/api/v1/auth/me", plain, nil)
	if code != 401 {
		t.Fatalf("bearer auth after disable: code = %d, want 401", code)
	}

	code, resp = tc.request("POST", fmt.Sprintf("/api/v1/admin/sessions/%d/enable", id), tc.sAdmin, nil)
	if code != 204 {
		t.Fatalf("enable: code = %d, want 204. Resp: %s", code, resp)
	}
	code, _ = tc.bearerRequest("GET", "/api/v1/auth/me", plain, nil)
	if code != 200 {
		t.Fatalf("bearer auth after re-enable: code = %d, want 200", code)
	}

	code, resp = tc.request("POST", fmt.Sprintf("/api/v1/admin/sessions/%d/rotate", id), tc.sAdmin, nil)
	if code != 200 {
		t.Fatalf("rotate: code = %d, want 200. Resp: %s", code, resp)
	}
	var rotateOut struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal([]byte(resp), &rotateOut); err != nil || rotateOut.Token == "" {
		t.Fatalf("rotate response missing token: %v. Resp: %s", err, resp)
	}
	code, _ = tc.bearerRequest("GET", "/api/v1/auth/me", plain, nil)
	if code != 401 {
		t.Fatalf("old plaintext after rotate: code = %d, want 401", code)
	}
	code, _ = tc.bearerRequest("GET", "/api/v1/auth/me", rotateOut.Token, nil)
	if code != 200 {
		t.Fatalf("new plaintext after rotate: code = %d, want 200", code)
	}

	code, resp = tc.request("DELETE", fmt.Sprintf("/api/v1/admin/sessions/%d", id), tc.sAdmin, nil)
	if code != 204 {
		t.Fatalf("revoke: code = %d, want 204. Resp: %s", code, resp)
	}
	code, resp = tc.request("GET", "/api/v1/admin/sessions", tc.sAdmin, nil)
	if code != 200 {
		t.Fatalf("list after revoke: code = %d, want 200. Resp: %s", code, resp)
	}
	if err := json.Unmarshal([]byte(resp), &listOut); err != nil {
		t.Fatalf("unmarshal: %v. Resp: %s", err, resp)
	}
	if len(listOut.Data) != 0 {
		t.Fatalf("expected no sessions after revoke, got %#v", listOut.Data)
	}
}

func TestAdminSessionNotFound(t *testing.T) {
	tc := setupTestContext(t)
	defer tc.close()

	routes := []struct {
		method, path string
	}{
		{"POST", "/api/v1/admin/sessions/999999/disable"},
		{"POST", "/api/v1/admin/sessions/999999/enable"},
		{"POST", "/api/v1/admin/sessions/999999/rotate"},
		{"DELETE", "/api/v1/admin/sessions/999999"},
	}
	for _, r := range routes {
		code, resp := tc.request(r.method, r.path, tc.sAdmin, nil)
		if code != 404 {
			t.Errorf("%s %s: code = %d, want 404. Resp: %s", r.method, r.path, code, resp)
		}
	}
}

// TestAdminSessionRoutesNeverTouchAPIKeys is the core regression guard for this feature: a
// personal API key a user created deliberately (via POST /tokens, self-service) must be
// invisible to and unmodifiable by every admin session-management route, even by id.
func TestAdminSessionRoutesNeverTouchAPIKeys(t *testing.T) {
	tc := setupTestContext(t)
	defer tc.close()

	id, plain, err := tc.a.CreateToken(tc.ctx, tc.uMember.ID, "my-bot-key", 0, store.TokenPurposeAPIKey)
	if err != nil {
		t.Fatal(err)
	}

	code, resp := tc.request("GET", "/api/v1/admin/sessions", tc.sAdmin, nil)
	if code != 200 {
		t.Fatalf("list sessions: code = %d, want 200. Resp: %s", code, resp)
	}
	var listOut struct {
		Data []struct{ ID string } `json:"data"`
	}
	if err := json.Unmarshal([]byte(resp), &listOut); err != nil {
		t.Fatalf("unmarshal: %v. Resp: %s", err, resp)
	}
	if len(listOut.Data) != 0 {
		t.Fatalf("admin sessions list leaked a personal API key: %#v", listOut.Data)
	}

	for _, r := range []struct{ method, path string }{
		{"POST", fmt.Sprintf("/api/v1/admin/sessions/%d/disable", id)},
		{"POST", fmt.Sprintf("/api/v1/admin/sessions/%d/rotate", id)},
		{"DELETE", fmt.Sprintf("/api/v1/admin/sessions/%d", id)},
	} {
		code, resp := tc.request(r.method, r.path, tc.sAdmin, nil)
		if code != 404 {
			t.Errorf("%s %s on an api_key token: code = %d, want 404 (must not touch it). Resp: %s", r.method, r.path, code, resp)
		}
	}

	// The key must still work after all those attempts.
	code, _ = tc.bearerRequest("GET", "/api/v1/auth/me", plain, nil)
	if code != 200 {
		t.Fatalf("api_key should still authenticate after admin session routes failed to touch it: code = %d", code)
	}
}
