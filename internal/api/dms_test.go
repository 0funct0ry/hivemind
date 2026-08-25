package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
)

func TestDMAPI(t *testing.T) {
	tc := setupTestContext(t)
	defer tc.close()

	// 1. Create DM with Bob (uMember) from Alice (uAdmin)
	body := map[string]any{"user_id": tc.uMember.ID}
	code, respStr := tc.request("POST", "/api/v1/dms", tc.sAdmin, body)
	if code != 200 {
		t.Fatalf("expected 200, got %d. Body: %s", code, respStr)
	}

	var resp struct {
		Channel struct {
			ID   string `json:"id"`
			Kind string `json:"kind"`
			Peer struct {
				ID       string `json:"id"`
				Username string `json:"username"`
			} `json:"peer"`
		} `json:"channel"`
	}
	if err := json.Unmarshal([]byte(respStr), &resp); err != nil {
		t.Fatal(err)
	}

	if resp.Channel.Kind != "dm" {
		t.Errorf("expected channel kind to be 'dm', got %q", resp.Channel.Kind)
	}
	if resp.Channel.Peer.Username != "memberuser" {
		t.Errorf("expected peer username 'memberuser', got %q", resp.Channel.Peer.Username)
	}

	// Create a second DM with Charlie (uNonMember)
	bodyCharlie := map[string]any{"user_id": tc.uNonMember.ID}
	code, respCharlieStr := tc.request("POST", "/api/v1/dms", tc.sAdmin, bodyCharlie)
	if code != 200 {
		t.Fatalf("expected 200 for Charlie DM, got %d. Body: %s", code, respCharlieStr)
	}

	// 2. List DMs for Alice
	// Note: Both DMs we just created/retrieved have no messages yet. But they were opened in the last hour, so they should appear in the list.
	code, listStr := tc.request("GET", "/api/v1/dms", tc.sAdmin, nil)
	if code != 200 {
		t.Fatalf("expected 200, got %d. Body: %s", code, listStr)
	}

	var listResp struct {
		Data []struct {
			ID   string `json:"id"`
			Kind string `json:"kind"`
			Peer struct {
				ID       string `json:"id"`
				Username string `json:"username"`
			} `json:"peer"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(listStr), &listResp); err != nil {
		t.Fatal(err)
	}

	if len(listResp.Data) != 2 { // wait, 2 DMs: the one seeded in setupTestContext (dmCh) plus the new one we just created
		t.Errorf("expected 2 DMs in list, got %d. Body: %s", len(listResp.Data), listStr)
	}

	// 3. Deactivate Bob and assert posting to DM returns 400 user_deactivated
	// Deactivate Bob
	err := tc.s.Deactivate(tc.ctx, tc.uMember.ID)
	if err != nil {
		t.Fatal(err)
	}

	// Try to post message to the DM channel we created
	msgBody := map[string]any{"body": "Hello Bob"}
	code, msgResp := tc.request("POST", fmt.Sprintf("/api/v1/channels/%s/messages", resp.Channel.ID), tc.sAdmin, msgBody)
	if code != http.StatusBadRequest {
		t.Errorf("expected 400 Bad Request, got %d. Body: %s", code, msgResp)
	}

	var errResp struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal([]byte(msgResp), &errResp); err != nil {
		t.Fatal(err)
	}
	if errResp.Error.Code != "user_deactivated" {
		t.Errorf("expected error code 'user_deactivated', got %q", errResp.Error.Code)
	}
}
