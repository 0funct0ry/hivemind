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
	body := map[string]any{"user_ids": []int64{tc.uMember.ID}}
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
	bodyCharlie := map[string]any{"user_ids": []int64{tc.uNonMember.ID}}
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

func TestDMHideAPI(t *testing.T) {
	tc := setupTestContext(t)
	defer tc.close()

	// Create a DM between Alice (admin) and Bob (member), with a message so it appears.
	body := map[string]any{"user_ids": []int64{tc.uMember.ID}}
	code, respStr := tc.request("POST", "/api/v1/dms", tc.sAdmin, body)
	if code != 200 {
		t.Fatalf("expected 200, got %d. Body: %s", code, respStr)
	}
	var resp struct {
		Channel struct {
			ID string `json:"id"`
		} `json:"channel"`
	}
	if err := json.Unmarshal([]byte(respStr), &resp); err != nil {
		t.Fatal(err)
	}
	chID := resp.Channel.ID

	msgBody := map[string]any{"body": "hello"}
	code, _ = tc.request("POST", fmt.Sprintf("/api/v1/channels/%s/messages", chID), tc.sAdmin, msgBody)
	if code != http.StatusCreated {
		t.Fatalf("expected 201 creating message, got %d", code)
	}

	// A non-participant gets 404, not 403.
	code, _ = tc.request("POST", fmt.Sprintf("/api/v1/dms/%s/hide", chID), tc.sNonMember, nil)
	if code != http.StatusNotFound {
		t.Errorf("expected 404 for non-participant, got %d", code)
	}

	// Alice hides it.
	code, _ = tc.request("POST", fmt.Sprintf("/api/v1/dms/%s/hide", chID), tc.sAdmin, nil)
	if code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", code)
	}

	// It's gone from Alice's list...
	code, listStr := tc.request("GET", "/api/v1/dms", tc.sAdmin, nil)
	if code != 200 {
		t.Fatalf("expected 200, got %d", code)
	}
	var listResp struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(listStr), &listResp); err != nil {
		t.Fatal(err)
	}
	for _, d := range listResp.Data {
		if d.ID == chID {
			t.Errorf("expected hidden DM %s to be absent from Alice's list, got %+v", chID, listResp.Data)
		}
	}

	// ...but Bob still sees it, since hiding is per-user.
	code, listStr = tc.request("GET", "/api/v1/dms", tc.sMember, nil)
	if code != 200 {
		t.Fatalf("expected 200, got %d", code)
	}
	var listResp2 struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(listStr), &listResp2); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, d := range listResp2.Data {
		if d.ID == chID {
			found = true
		}
	}
	if !found {
		t.Errorf("expected Bob to still see the DM, got %+v", listResp2.Data)
	}

	// Bob posts again — the conversation reappears for Alice too.
	code, _ = tc.request("POST", fmt.Sprintf("/api/v1/channels/%s/messages", chID), tc.sMember, msgBody)
	if code != http.StatusCreated {
		t.Fatalf("expected 201, got %d", code)
	}
	code, listStr = tc.request("GET", "/api/v1/dms", tc.sAdmin, nil)
	if code != 200 {
		t.Fatalf("expected 200, got %d", code)
	}
	var listResp3 struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(listStr), &listResp3); err != nil {
		t.Fatal(err)
	}
	found = false
	for _, d := range listResp3.Data {
		if d.ID == chID {
			found = true
		}
	}
	if !found {
		t.Errorf("expected the DM to reappear for Alice after a new message, got %+v", listResp3.Data)
	}

	// Hiding a public channel via the DM-hide route is not a thing.
	code, _ = tc.request("POST", fmt.Sprintf("/api/v1/dms/%d/hide", tc.pubCh.ID), tc.sAdmin, nil)
	if code != http.StatusNotFound {
		t.Errorf("expected 404 hiding a non-DM channel, got %d", code)
	}
}
