package api

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/0funct0ry/hivemind/internal/store"
)

func TestMessageAPI(t *testing.T) {
	tc := setupTestContext(t)
	defer tc.close()

	// 1. Post a valid message as a member of pubCh
	body := map[string]any{"body": "Hello world!"}
	code, resp := tc.request("POST", fmt.Sprintf("/api/v1/channels/%d/messages", tc.pubCh.ID), tc.sMember, body)
	if code != 201 {
		t.Fatalf("expected 201, got %d. Body: %s", code, resp)
	}

	var postResp struct {
		Message struct {
			ID        string `json:"id"`
			Body      string `json:"body"`
			UserID    string `json:"user_id"`
			ChannelID string `json:"channel_id"`
		} `json:"message"`
	}
	if err := json.Unmarshal([]byte(resp), &postResp); err != nil {
		t.Fatal(err)
	}
	if postResp.Message.Body != "Hello world!" {
		t.Errorf("expected body 'Hello world!', got %q", postResp.Message.Body)
	}

	// 2. GET single message
	code, resp = tc.request("GET", fmt.Sprintf("/api/v1/messages/%s", postResp.Message.ID), tc.sMember, nil)
	if code != 200 {
		t.Fatalf("expected 200, got %d", code)
	}
	
	// 3. Auto-join: Post to pubCh as uNonMember (not a member yet)
	bodyNonMember := map[string]any{"body": "Hello as guest!"}
	code, resp = tc.request("POST", fmt.Sprintf("/api/v1/channels/%d/messages", tc.pubCh.ID), tc.sNonMember, bodyNonMember)
	if code != 201 {
		t.Fatalf("expected 201 (auto-join), got %d. Resp: %s", code, resp)
	}

	// Check that uNonMember is now indeed a member of pubCh
	isMem, err := tc.s.IsMember(tc.ctx, tc.pubCh.ID, tc.uNonMember.ID)
	if err != nil || !isMem {
		t.Errorf("expected uNonMember to be auto-joined, member check: %v, err: %v", isMem, err)
	}

	// 4. Access restrictions: Post to privCh as uNonMember (not member of private channel) -> 404
	bodyPriv := map[string]any{"body": "Secret message"}
	code, resp = tc.request("POST", fmt.Sprintf("/api/v1/channels/%d/messages", tc.privCh.ID), tc.sNonMember, bodyPriv)
	if code != 404 {
		t.Fatalf("expected 404 for private channel post by non-member, got %d. Resp: %s", code, resp)
	}

	// 5. Idempotency check
	clientMsgID := "api-client-msg-123"
	bodyIdem := map[string]any{"body": "Unique", "client_msg_id": clientMsgID}
	code1, resp1 := tc.request("POST", fmt.Sprintf("/api/v1/channels/%d/messages", tc.pubCh.ID), tc.sMember, bodyIdem)
	if code1 != 201 {
		t.Fatalf("expected 201, got %d", code1)
	}

	var m1 struct {
		Message struct {
			ID string `json:"id"`
		} `json:"message"`
	}
	_ = json.Unmarshal([]byte(resp1), &m1)

	// Repeat same request
	code2, resp2 := tc.request("POST", fmt.Sprintf("/api/v1/channels/%d/messages", tc.pubCh.ID), tc.sMember, bodyIdem)
	if code2 != 200 {
		t.Fatalf("expected 200 on duplicate, got %d", code2)
	}
	var m2 struct {
		Message struct {
			ID string `json:"id"`
		} `json:"message"`
	}
	_ = json.Unmarshal([]byte(resp2), &m2)

	if m1.Message.ID != m2.Message.ID {
		t.Errorf("expected duplicate message ID to match, got %s and %s", m1.Message.ID, m2.Message.ID)
	}

	// 6. Test GET list channel messages
	// Seed a few messages
	for i := 0; i < 5; i++ {
		_, _ = tc.request("POST", fmt.Sprintf("/api/v1/channels/%d/messages", tc.pubCh.ID), tc.sMember, map[string]any{"body": fmt.Sprintf("seq %d", i)})
	}

	code, resp = tc.request("GET", fmt.Sprintf("/api/v1/channels/%d/messages?limit=3", tc.pubCh.ID), tc.sMember, nil)
	if code != 200 {
		t.Fatalf("expected 200, got %d", code)
	}

	var listResp struct {
		Data []struct {
			ID   string `json:"id"`
			Body string `json:"body"`
		} `json:"data"`
		HasMore    bool   `json:"has_more"`
		NextBefore string `json:"next_before"`
	}
	if err := json.Unmarshal([]byte(resp), &listResp); err != nil {
		t.Fatal(err)
	}

	// Total messages in pubCh: 1 (Hello world) + 1 (Hello as guest) + 1 (Unique) + 5 (seq 0..4) = 8 messages.
	// Requested limit=3, so we should get 3 messages.
	if len(listResp.Data) != 3 {
		t.Errorf("expected 3 messages, got %d", len(listResp.Data))
	}
	if !listResp.HasMore {
		t.Error("expected has_more=true")
	}
}

func TestMessageAPIRateLimit(t *testing.T) {
	tc := setupTestContext(t)
	defer tc.close()

	// Create a fresh user to avoid inheriting rate limit state from other tests
	uSpammer, err := tc.s.CreateUser(tc.ctx, store.UserInput{Username: "spammer", Email: "spammer@example.com", PasswordHash: "hash"})
	if err != nil {
		t.Fatal(err)
	}
	sSpammer, err := tc.a.CreateSession(tc.ctx, uSpammer.ID, "UA", "127.0.0.1")
	if err != nil {
		t.Fatal(err)
	}
	if err := tc.s.AddMembers(tc.ctx, tc.pubCh.ID, []int64{uSpammer.ID}); err != nil {
		t.Fatal(err)
	}

	// Rapidly post to hit rate limit of 30 msgs
	for i := 0; i < 30; i++ {
		code, _ := tc.request("POST", fmt.Sprintf("/api/v1/channels/%d/messages", tc.pubCh.ID), sSpammer, map[string]any{"body": "spam"})
		if code != 201 {
			t.Fatalf("expected 201 for post %d, got %d", i, code)
		}
	}

	// 31st post should fail with 429
	code, resp := tc.request("POST", fmt.Sprintf("/api/v1/channels/%d/messages", tc.pubCh.ID), sSpammer, map[string]any{"body": "spam-limit"})
	if code != 429 {
		t.Fatalf("expected 429 rate limit, got %d. Resp: %s", code, resp)
	}
}
