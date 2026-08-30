package api

import (
	"encoding/json"
	"fmt"
	"testing"
)

// TestTokenCreateDefaultsToAPIKey verifies the self-service POST /tokens endpoint (available
// to any user, not just admins) defaults a plain request with no "purpose" field to an api_key
// — the personal-key use case — and that it shows up in the caller's own GET /tokens list.
func TestTokenCreateDefaultsToAPIKey(t *testing.T) {
	tc := setupTestContext(t)
	defer tc.close()

	code, resp := tc.request("POST", "/api/v1/tokens", tc.sMember, map[string]any{"name": "my-bot-key"})
	if code != 201 {
		t.Fatalf("create token: code = %d, want 201. Resp: %s", code, resp)
	}
	var createOut struct {
		ID    string `json:"id"`
		Token string `json:"token"`
	}
	if err := json.Unmarshal([]byte(resp), &createOut); err != nil || createOut.Token == "" {
		t.Fatalf("create response missing token: %v. Resp: %s", err, resp)
	}

	code, resp = tc.request("GET", "/api/v1/tokens", tc.sMember, nil)
	if code != 200 {
		t.Fatalf("list tokens: code = %d, want 200. Resp: %s", code, resp)
	}
	var listOut struct {
		Data []struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(resp), &listOut); err != nil {
		t.Fatalf("unmarshal: %v. Resp: %s", err, resp)
	}
	if len(listOut.Data) != 1 || listOut.Data[0].Name != "my-bot-key" {
		t.Fatalf("self-service list = %#v, want the just-created api_key", listOut.Data)
	}
}

// TestTokenCreateCLISessionExcludedFromSelfServiceList mirrors what hivemind chat's login flow
// does (creates a token with purpose=cli_session) and verifies that token never shows up in
// the self-service "my API keys" list — it belongs only in the admin Sessions view.
func TestTokenCreateCLISessionExcludedFromSelfServiceList(t *testing.T) {
	tc := setupTestContext(t)
	defer tc.close()

	code, resp := tc.request("POST", "/api/v1/tokens", tc.sMember, map[string]any{"name": "chat-cli", "purpose": "cli_session"})
	if code != 201 {
		t.Fatalf("create session token: code = %d, want 201. Resp: %s", code, resp)
	}

	code, resp = tc.request("GET", "/api/v1/tokens", tc.sMember, nil)
	if code != 200 {
		t.Fatalf("list tokens: code = %d, want 200. Resp: %s", code, resp)
	}
	var listOut struct {
		Data []struct{ Name string } `json:"data"`
	}
	if err := json.Unmarshal([]byte(resp), &listOut); err != nil {
		t.Fatalf("unmarshal: %v. Resp: %s", err, resp)
	}
	if len(listOut.Data) != 0 {
		t.Fatalf("self-service API-keys list leaked a cli_session token: %#v", listOut.Data)
	}

	code, resp = tc.request("GET", "/api/v1/admin/sessions", tc.sAdmin, nil)
	if code != 200 {
		t.Fatalf("admin sessions list: code = %d, want 200. Resp: %s", code, resp)
	}
	var sessOut struct {
		Data []struct{ Name string } `json:"data"`
	}
	if err := json.Unmarshal([]byte(resp), &sessOut); err != nil {
		t.Fatalf("unmarshal: %v. Resp: %s", err, resp)
	}
	if len(sessOut.Data) != 1 || sessOut.Data[0].Name != "chat-cli" {
		t.Fatalf("admin sessions list = %#v, want the cli_session token", sessOut.Data)
	}
}

func TestTokenCreateInvalidPurposeRejected(t *testing.T) {
	tc := setupTestContext(t)
	defer tc.close()

	code, resp := tc.request("POST", "/api/v1/tokens", tc.sMember, map[string]any{"name": "x", "purpose": "root"})
	if code != 400 {
		t.Fatalf("create with invalid purpose: code = %d, want 400. Resp: %s", code, resp)
	}
}

// TestTokenDeleteCannotRevokeOwnCLISession is available to any authenticated user (not just
// admins) is the point of this endpoint, but it must stay scoped to api_key tokens — a member
// deleting their own chat-cli session via the personal API-keys UI would silently log their CLI
// out, which isn't what "revoke my API key" should ever do.
func TestTokenDeleteCannotRevokeOwnCLISession(t *testing.T) {
	tc := setupTestContext(t)
	defer tc.close()

	id, _, err := tc.a.CreateToken(tc.ctx, tc.uMember.ID, "chat-cli", 0, "cli_session")
	if err != nil {
		t.Fatal(err)
	}

	code, resp := tc.request("DELETE", fmt.Sprintf("/api/v1/tokens/%d", id), tc.sMember, nil)
	if code != 404 {
		t.Fatalf("delete own cli_session via self-service route: code = %d, want 404. Resp: %s", code, resp)
	}
}

func TestTokenDeleteOwnAPIKey(t *testing.T) {
	tc := setupTestContext(t)
	defer tc.close()

	id, _, err := tc.a.CreateToken(tc.ctx, tc.uMember.ID, "my-bot-key", 0, "api_key")
	if err != nil {
		t.Fatal(err)
	}

	code, resp := tc.request("DELETE", fmt.Sprintf("/api/v1/tokens/%d", id), tc.sMember, nil)
	if code != 204 {
		t.Fatalf("delete own api_key: code = %d, want 204. Resp: %s", code, resp)
	}
}
