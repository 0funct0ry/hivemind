package api

import (
	"encoding/json"
	"strings"
	"testing"
)

type botResp struct {
	Bot struct {
		UserID string `json:"user_id"`
		Token  string `json:"token"`
		Status string `json:"status"`
	} `json:"bot"`
}

func createBotViaAPI(t *testing.T, tc *testContext, session string) (int, botResp, string) {
	t.Helper()
	code, raw := tc.request("POST", "/api/v1/bots", session, map[string]any{"name": "Deploy Bot", "description": "ships things"})
	var out botResp
	_ = json.Unmarshal([]byte(raw), &out)
	return code, out, raw
}

func TestBotManagementAuthorization(t *testing.T) {
	tc := setupTestContext(t)
	defer tc.close()

	t.Run("non-admin gets 403 on create", func(t *testing.T) {
		code, _, raw := createBotViaAPI(t, tc, tc.sMember)
		if code != 403 {
			t.Fatalf("expected 403, got %d: %s", code, raw)
		}
		assertErrorCode(t, raw, "forbidden")
	})

	t.Run("admin can create and gets a shown-once token", func(t *testing.T) {
		code, out, raw := createBotViaAPI(t, tc, tc.sAdmin)
		if code != 201 {
			t.Fatalf("expected 201, got %d: %s", code, raw)
		}
		if !strings.HasPrefix(out.Bot.Token, "hm_") {
			t.Fatalf("expected a shown-once token with hm_ prefix, got %q", out.Bot.Token)
		}
	})

	t.Run("non-admin gets 403 on list", func(t *testing.T) {
		code, raw := tc.request("GET", "/api/v1/bots", tc.sMember, nil)
		if code != 403 {
			t.Fatalf("expected 403, got %d: %s", code, raw)
		}
	})
}

func TestBotTokenPostsThroughUnmodifiedMessagesPath(t *testing.T) {
	ResetMsgLimiter()
	t.Cleanup(ResetMsgLimiter)
	tc := setupTestContext(t)
	defer tc.close()

	code, out, raw := createBotViaAPI(t, tc, tc.sAdmin)
	if code != 201 {
		t.Fatalf("expected 201, got %d: %s", code, raw)
	}
	token := out.Bot.Token

	code, raw = tc.bearerRequest("POST", "/api/v1/channels/"+itoa(tc.pubCh.ID)+"/messages", token, map[string]any{"body": "hello from bot"})
	if code != 201 {
		t.Fatalf("expected the bot's token to post successfully, got %d: %s", code, raw)
	}

	var msgOut struct {
		Message struct {
			User struct {
				IsBot bool `json:"is_bot"`
			} `json:"user"`
		} `json:"message"`
	}
	_ = json.Unmarshal([]byte(raw), &msgOut)
	if !msgOut.Message.User.IsBot {
		t.Fatal("expected the posted message's author to be flagged is_bot")
	}
}

func TestRegenerateBotTokenInvalidatesOldToken(t *testing.T) {
	ResetMsgLimiter()
	t.Cleanup(ResetMsgLimiter)
	tc := setupTestContext(t)
	defer tc.close()

	_, out, _ := createBotViaAPI(t, tc, tc.sAdmin)
	oldToken := out.Bot.Token

	code, raw := tc.request("POST", "/api/v1/bots/"+out.Bot.UserID+"/regenerate-token", tc.sAdmin, nil)
	if code != 200 {
		t.Fatalf("expected 200, got %d: %s", code, raw)
	}
	var regenOut botResp
	_ = json.Unmarshal([]byte(raw), &regenOut)
	newToken := regenOut.Bot.Token
	if newToken == oldToken {
		t.Fatal("expected a fresh token after regeneration")
	}

	code, raw = tc.bearerRequest("POST", "/api/v1/channels/"+itoa(tc.pubCh.ID)+"/messages", oldToken, map[string]any{"body": "should fail"})
	if code != 401 {
		t.Fatalf("expected the old token to 401 after regeneration, got %d: %s", code, raw)
	}

	code, raw = tc.bearerRequest("POST", "/api/v1/channels/"+itoa(tc.pubCh.ID)+"/messages", newToken, map[string]any{"body": "should work"})
	if code != 201 {
		t.Fatalf("expected the new token to post successfully, got %d: %s", code, raw)
	}
}

func TestBotRevokeThenDelete(t *testing.T) {
	ResetMsgLimiter()
	t.Cleanup(ResetMsgLimiter)
	tc := setupTestContext(t)
	defer tc.close()

	_, out, _ := createBotViaAPI(t, tc, tc.sAdmin)
	token := out.Bot.Token
	botID := out.Bot.UserID

	t.Run("non-admin cannot revoke or delete", func(t *testing.T) {
		code, raw := tc.request("POST", "/api/v1/bots/"+botID+"/revoke", tc.sMember, nil)
		if code != 403 {
			t.Fatalf("expected 403, got %d: %s", code, raw)
		}
		code, raw = tc.request("DELETE", "/api/v1/bots/"+botID, tc.sMember, nil)
		if code != 403 {
			t.Fatalf("expected 403, got %d: %s", code, raw)
		}
	})

	t.Run("deleting an active bot is rejected", func(t *testing.T) {
		code, raw := tc.request("DELETE", "/api/v1/bots/"+botID, tc.sAdmin, nil)
		if code != 409 {
			t.Fatalf("expected 409, got %d: %s", code, raw)
		}
		assertErrorCode(t, raw, "bot_not_revoked")
	})

	t.Run("revoke stops the token from authenticating", func(t *testing.T) {
		code, raw := tc.request("POST", "/api/v1/bots/"+botID+"/revoke", tc.sAdmin, nil)
		if code != 200 {
			t.Fatalf("expected 200, got %d: %s", code, raw)
		}
		code, raw = tc.bearerRequest("POST", "/api/v1/channels/"+itoa(tc.pubCh.ID)+"/messages", token, map[string]any{"body": "should fail"})
		if code != 401 {
			t.Fatalf("expected 401, got %d: %s", code, raw)
		}
	})

	t.Run("delete succeeds once revoked, and is idempotent", func(t *testing.T) {
		code, raw := tc.request("DELETE", "/api/v1/bots/"+botID, tc.sAdmin, nil)
		if code != 200 {
			t.Fatalf("expected 200, got %d: %s", code, raw)
		}
		code, raw = tc.request("GET", "/api/v1/bots", tc.sAdmin, nil)
		if code != 200 || strings.Contains(raw, botID) {
			t.Fatalf("expected the bot to be gone from the list, got %d: %s", code, raw)
		}
		code, raw = tc.request("DELETE", "/api/v1/bots/"+botID, tc.sAdmin, nil)
		if code != 200 {
			t.Fatalf("expected idempotent delete to still return 200, got %d: %s", code, raw)
		}
	})
}
