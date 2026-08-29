package api

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"strconv"
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

func TestMessageListAround(t *testing.T) {
	tc := setupTestContext(t)
	defer tc.close()

	var ids []string
	for i := 0; i < 6; i++ {
		code, resp := tc.request("POST", fmt.Sprintf("/api/v1/channels/%d/messages", tc.pubCh.ID), tc.sMember, map[string]any{"body": fmt.Sprintf("m%d", i)})
		if code != 201 {
			t.Fatalf("expected 201, got %d. Resp: %s", code, resp)
		}
		var posted struct {
			Message struct {
				ID string `json:"id"`
			} `json:"message"`
		}
		if err := json.Unmarshal([]byte(resp), &posted); err != nil {
			t.Fatal(err)
		}
		ids = append(ids, posted.Message.ID)
	}

	type listResp struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}

	// A member gets a symmetric window around the anchor.
	code, resp := tc.request("GET", fmt.Sprintf("/api/v1/channels/%d/messages?around=%s&limit=4", tc.pubCh.ID, ids[3]), tc.sMember, nil)
	if code != 200 {
		t.Fatalf("expected 200, got %d. Resp: %s", code, resp)
	}
	var lr listResp
	if err := json.Unmarshal([]byte(resp), &lr); err != nil {
		t.Fatal(err)
	}
	if len(lr.Data) == 0 {
		t.Fatal("expected a non-empty window around the anchor")
	}
	found := false
	for _, m := range lr.Data {
		if m.ID == ids[3] {
			found = true
		}
	}
	if !found {
		t.Errorf("expected anchor message %s to be included in the around window", ids[3])
	}

	// around takes precedence over before/after when both are supplied.
	code, resp = tc.request("GET", fmt.Sprintf("/api/v1/channels/%d/messages?around=%s&before=%s&limit=4", tc.pubCh.ID, ids[3], ids[1]), tc.sMember, nil)
	if code != 200 {
		t.Fatalf("expected 200, got %d. Resp: %s", code, resp)
	}
	var lr2 listResp
	if err := json.Unmarshal([]byte(resp), &lr2); err != nil {
		t.Fatal(err)
	}
	found = false
	for _, m := range lr2.Data {
		if m.ID == ids[3] {
			found = true
		}
	}
	if !found {
		t.Error("expected around to take precedence over before, but anchor message was not in the result")
	}

	// A non-member of a private channel gets 404, not 403, when using around.
	code, resp = tc.request("GET", fmt.Sprintf("/api/v1/channels/%d/messages?around=%s", tc.privCh.ID, ids[3]), tc.sNonMember, nil)
	if code != 404 {
		t.Fatalf("expected 404 for non-member around request, got %d. Resp: %s", code, resp)
	}

	// An empty channel with around returns 200 and an empty array, not an error.
	emptyCh, err := tc.s.CreateChannel(tc.ctx, "public", "around-empty", "Around Empty", "", tc.uMember.ID, []int64{tc.uMember.ID})
	if err != nil {
		t.Fatal(err)
	}
	code, resp = tc.request("GET", fmt.Sprintf("/api/v1/channels/%d/messages?around=%s", emptyCh.ID, ids[3]), tc.sMember, nil)
	if code != 200 {
		t.Fatalf("expected 200 for empty channel, got %d. Resp: %s", code, resp)
	}
	var lr3 listResp
	if err := json.Unmarshal([]byte(resp), &lr3); err != nil {
		t.Fatal(err)
	}
	if len(lr3.Data) != 0 {
		t.Errorf("expected empty data for empty channel, got %d messages", len(lr3.Data))
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

func TestThreadAPI(t *testing.T) {
	tc := setupTestContext(t)
	defer tc.close()

	// 1. Post root message
	code, resp := tc.request("POST", fmt.Sprintf("/api/v1/channels/%d/messages", tc.pubCh.ID), tc.sMember, map[string]any{"body": "Root thread message"})
	if code != 201 {
		t.Fatalf("expected 201, got %d", code)
	}

	var rootResp struct {
		Message struct {
			ID string `json:"id"`
		} `json:"message"`
	}
	if err := json.Unmarshal([]byte(resp), &rootResp); err != nil {
		t.Fatal(err)
	}

	// 2. Post reply
	replyBody := map[string]any{
		"body":      "Thread reply 1",
		"thread_id": rootResp.Message.ID,
	}
	code, resp = tc.request("POST", fmt.Sprintf("/api/v1/channels/%d/messages", tc.pubCh.ID), tc.sMember, replyBody)
	if code != 201 {
		t.Fatalf("expected 201, got %d", code)
	}

	var replyResp struct {
		Message struct {
			ID        string `json:"id"`
			ThreadID  string `json:"thread_id"`
			Broadcast bool   `json:"broadcast"`
		} `json:"message"`
	}
	if err := json.Unmarshal([]byte(resp), &replyResp); err != nil {
		t.Fatal(err)
	}
	if replyResp.Message.ThreadID != rootResp.Message.ID {
		t.Errorf("expected thread_id to be %s, got %s", rootResp.Message.ID, replyResp.Message.ThreadID)
	}
	if replyResp.Message.Broadcast != false {
		t.Errorf("expected broadcast to be false, got %t", replyResp.Message.Broadcast)
	}

	// 3. Post reply with broadcast (also_send_to_channel)
	replyBroadcastBody := map[string]any{
		"body":                 "Broadcast reply",
		"thread_id":            rootResp.Message.ID,
		"also_send_to_channel": true,
	}
	code, resp = tc.request("POST", fmt.Sprintf("/api/v1/channels/%d/messages", tc.pubCh.ID), tc.sMember, replyBroadcastBody)
	if code != 201 {
		t.Fatalf("expected 201, got %d", code)
	}

	var replyBroadcastResp struct {
		Message struct {
			ID        string `json:"id"`
			ThreadID  string `json:"thread_id"`
			Broadcast bool   `json:"broadcast"`
		} `json:"message"`
	}
	if err := json.Unmarshal([]byte(resp), &replyBroadcastResp); err != nil {
		t.Fatal(err)
	}
	if replyBroadcastResp.Message.Broadcast != true {
		t.Errorf("expected broadcast to be true, got %t", replyBroadcastResp.Message.Broadcast)
	}

	// 4. Test GET list replies
	code, resp = tc.request("GET", fmt.Sprintf("/api/v1/messages/%s/replies", rootResp.Message.ID), tc.sMember, nil)
	if code != 200 {
		t.Fatalf("expected 200, got %d. Body: %s", code, resp)
	}

	var listRepliesResp struct {
		Root *struct {
			ID string `json:"id"`
		} `json:"root"`
		Data []struct {
			ID        string `json:"id"`
			Broadcast bool   `json:"broadcast"`
		} `json:"data"`
		HasMore bool `json:"has_more"`
	}
	if err := json.Unmarshal([]byte(resp), &listRepliesResp); err != nil {
		t.Fatal(err)
	}

	if listRepliesResp.Root == nil || listRepliesResp.Root.ID != rootResp.Message.ID {
		t.Errorf("expected root ID to be %s, got %v", rootResp.Message.ID, listRepliesResp.Root)
	}
	if len(listRepliesResp.Data) != 2 {
		t.Errorf("expected 2 replies, got %d", len(listRepliesResp.Data))
	}

	// 5. Test depth coercion: posting reply to replyResp should coerce thread_id to rootResp
	replyToReplyBody := map[string]any{
		"body":      "Reply to reply (coercion test)",
		"thread_id": replyResp.Message.ID,
	}
	code, resp = tc.request("POST", fmt.Sprintf("/api/v1/channels/%d/messages", tc.pubCh.ID), tc.sMember, replyToReplyBody)
	if code != 201 {
		t.Fatalf("expected 201, got %d", code)
	}

	var replyToReplyResp struct {
		Message struct {
			ThreadID string `json:"thread_id"`
		} `json:"message"`
	}
	if err := json.Unmarshal([]byte(resp), &replyToReplyResp); err != nil {
		t.Fatal(err)
	}
	if replyToReplyResp.Message.ThreadID != rootResp.Message.ID {
		t.Errorf("expected coerced thread_id to be %s, got %s", rootResp.Message.ID, replyToReplyResp.Message.ThreadID)
	}

	// 6. Test channel mismatch: post a message to ch1, try to reply to it specifying ch2
	code, resp = tc.request("POST", fmt.Sprintf("/api/v1/channels/%d/messages", tc.privCh.ID), tc.sMember, replyBody)
	if code != 400 {
		t.Fatalf("expected 400, got %d. Resp: %s", code, resp)
	}
}

func TestMessageAPIMentionsAndAutocomplete(t *testing.T) {
	tc := setupTestContext(t)
	defer tc.close()

	// Autocomplete tests
	// Query users starting with "a" (alice) without channel_id
	code, resp := tc.request("GET", "/api/v1/users?q=a", tc.sMember, nil)
	if code != 200 {
		t.Fatalf("expected 200, got %d", code)
	}
	var usersResp struct {
		Data []struct {
			Username string `json:"username"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(resp), &usersResp); err != nil {
		t.Fatal(err)
	}
	if len(usersResp.Data) == 0 {
		t.Errorf("expected to find users matching 'a'")
	}

	// Post message with mention
	body := map[string]any{"body": "Hello @alice"}
	code, resp = tc.request("POST", fmt.Sprintf("/api/v1/channels/%d/messages", tc.pubCh.ID), tc.sMember, body)
	if code != 201 {
		t.Fatalf("expected 201, got %d. Body: %s", code, resp)
	}
}

func postMessage(t *testing.T, tc *testContext, channelID int64, session, body string) string {
	t.Helper()
	code, resp := tc.request("POST", fmt.Sprintf("/api/v1/channels/%d/messages", channelID), session, map[string]any{"body": body})
	if code != 201 && code != 200 {
		t.Fatalf("expected 200/201 posting message, got %d. Body: %s", code, resp)
	}
	var out struct {
		Message struct {
			ID string `json:"id"`
		} `json:"message"`
	}
	if err := json.Unmarshal([]byte(resp), &out); err != nil {
		t.Fatal(err)
	}
	return out.Message.ID
}

func TestMessageEditAPI(t *testing.T) {
	ResetMsgLimiter()
	tc := setupTestContext(t)
	defer tc.close()

	t.Run("author edit within window succeeds", func(t *testing.T) {
		id := postMessage(t, tc, tc.pubCh.ID, tc.sMember, "before edit")

		code, resp := tc.request("PATCH", "/api/v1/messages/"+id, tc.sMember, map[string]any{"body": "after edit"})
		if code != 200 {
			t.Fatalf("expected 200, got %d. Body: %s", code, resp)
		}
		var out struct {
			Message struct {
				Body     string `json:"body"`
				EditedAt *int64 `json:"edited_at"`
			} `json:"message"`
		}
		if err := json.Unmarshal([]byte(resp), &out); err != nil {
			t.Fatal(err)
		}
		if out.Message.Body != "after edit" {
			t.Errorf("expected updated body, got %q", out.Message.Body)
		}
		if out.Message.EditedAt == nil {
			t.Error("expected edited_at to be set")
		}
	})

	t.Run("non-author edit fails with 403", func(t *testing.T) {
		id := postMessage(t, tc, tc.pubCh.ID, tc.sMember, "owned by member")

		code, resp := tc.request("PATCH", "/api/v1/messages/"+id, tc.sAdmin, map[string]any{"body": "hijacked"})
		if code != 403 {
			t.Fatalf("expected 403, got %d. Body: %s", code, resp)
		}
		var out struct {
			Error struct {
				Code string `json:"code"`
			} `json:"error"`
		}
		_ = json.Unmarshal([]byte(resp), &out)
		if out.Error.Code != "not_message_owner" {
			t.Errorf("expected not_message_owner, got %q", out.Error.Code)
		}
	})

	t.Run("edit after window expired returns 409", func(t *testing.T) {
		id := postMessage(t, tc, tc.pubCh.ID, tc.sMember, "will go stale")
		idInt, _ := strconv.ParseInt(id, 10, 64)

		if err := tc.s.Tx(tc.ctx, func(tx *sql.Tx) error {
			_, err := tx.ExecContext(tc.ctx, "UPDATE messages SET created_at = ? WHERE id = ?", 0, idInt)
			return err
		}); err != nil {
			t.Fatal(err)
		}

		code, resp := tc.request("PATCH", "/api/v1/messages/"+id, tc.sMember, map[string]any{"body": "too late"})
		if code != 409 {
			t.Fatalf("expected 409, got %d. Body: %s", code, resp)
		}
		var out struct {
			Error struct {
				Code string `json:"code"`
			} `json:"error"`
		}
		_ = json.Unmarshal([]byte(resp), &out)
		if out.Error.Code != "edit_window_expired" {
			t.Errorf("expected edit_window_expired, got %q", out.Error.Code)
		}
	})

	t.Run("edit nonexistent message returns 404", func(t *testing.T) {
		code, resp := tc.request("PATCH", "/api/v1/messages/999999", tc.sMember, map[string]any{"body": "nope"})
		if code != 404 {
			t.Fatalf("expected 404, got %d. Body: %s", code, resp)
		}
	})
}

func TestMessageDeleteAPI(t *testing.T) {
	ResetMsgLimiter()
	tc := setupTestContext(t)
	defer tc.close()

	t.Run("author can delete own message", func(t *testing.T) {
		id := postMessage(t, tc, tc.pubCh.ID, tc.sMember, "delete me")

		code, resp := tc.request("DELETE", "/api/v1/messages/"+id, tc.sMember, nil)
		if code != 200 {
			t.Fatalf("expected 200, got %d. Body: %s", code, resp)
		}
		var out struct {
			Message struct {
				Body      string `json:"body"`
				DeletedAt *int64 `json:"deleted_at"`
				DeletedBy struct {
					ID     string `json:"id"`
					IsSelf bool   `json:"is_self"`
				} `json:"deleted_by"`
			} `json:"message"`
		}
		if err := json.Unmarshal([]byte(resp), &out); err != nil {
			t.Fatal(err)
		}
		if out.Message.Body != "" {
			t.Errorf("expected blanked body, got %q", out.Message.Body)
		}
		if out.Message.DeletedAt == nil {
			t.Error("expected deleted_at to be set")
		}
		if !out.Message.DeletedBy.IsSelf {
			t.Error("expected deleted_by.is_self=true")
		}
	})

	t.Run("non-author non-admin cannot delete", func(t *testing.T) {
		id := postMessage(t, tc, tc.pubCh.ID, tc.sMember, "protected")

		code, resp := tc.request("DELETE", "/api/v1/messages/"+id, tc.sNonMember, nil)
		if code != 403 {
			t.Fatalf("expected 403, got %d. Body: %s", code, resp)
		}
	})

	t.Run("admin can delete someone else's message", func(t *testing.T) {
		id := postMessage(t, tc, tc.pubCh.ID, tc.sMember, "moderated")

		code, resp := tc.request("DELETE", "/api/v1/messages/"+id, tc.sAdmin, nil)
		if code != 200 {
			t.Fatalf("expected 200, got %d. Body: %s", code, resp)
		}
		var out struct {
			Message struct {
				DeletedBy struct {
					IsSelf bool `json:"is_self"`
				} `json:"deleted_by"`
			} `json:"message"`
		}
		_ = json.Unmarshal([]byte(resp), &out)
		if out.Message.DeletedBy.IsSelf {
			t.Error("expected deleted_by.is_self=false for admin-delete")
		}
	})

	t.Run("deleting an already-deleted message is idempotent", func(t *testing.T) {
		id := postMessage(t, tc, tc.pubCh.ID, tc.sMember, "delete twice")

		code1, _ := tc.request("DELETE", "/api/v1/messages/"+id, tc.sMember, nil)
		code2, resp2 := tc.request("DELETE", "/api/v1/messages/"+id, tc.sMember, nil)
		if code1 != 200 || code2 != 200 {
			t.Fatalf("expected both deletes to return 200, got %d and %d. Body: %s", code1, code2, resp2)
		}
	})

	t.Run("reply to a just-deleted thread root returns 404 thread_locked", func(t *testing.T) {
		rootID := postMessage(t, tc, tc.pubCh.ID, tc.sMember, "will lock")

		code, resp := tc.request("DELETE", "/api/v1/messages/"+rootID, tc.sMember, nil)
		if code != 200 {
			t.Fatalf("expected 200 deleting root, got %d. Body: %s", code, resp)
		}

		code, resp = tc.request("POST", fmt.Sprintf("/api/v1/channels/%d/messages", tc.pubCh.ID), tc.sMember, map[string]any{
			"body":      "too late reply",
			"thread_id": rootID,
		})
		if code != 404 {
			t.Fatalf("expected 404, got %d. Body: %s", code, resp)
		}
		var out struct {
			Error struct {
				Code string `json:"code"`
			} `json:"error"`
		}
		_ = json.Unmarshal([]byte(resp), &out)
		if out.Error.Code != "thread_locked" {
			t.Errorf("expected thread_locked, got %q", out.Error.Code)
		}
	})
}
