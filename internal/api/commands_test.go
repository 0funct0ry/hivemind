package api

import (
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/0funct0ry/hivemind/internal/store"
)

func resetCommandTestState(t *testing.T) {
	t.Helper()
	ResetMsgLimiter()
	t.Cleanup(ResetMsgLimiter)
	DisableRateLimits = true
	t.Cleanup(func() { DisableRateLimits = false })
	t.Cleanup(store.StubOutgoingWebhookHostResolution("example.com", net.ParseIP("93.184.216.34")))
}

// createSlashCommandFixture creates a bot and a slash command pointed at srv, returning the
// command's id and its trigger.
func createSlashCommandFixture(t *testing.T, tc *testContext, session string, srv *httptest.Server, adminOnly bool) (string, string) {
	t.Helper()
	code, botOut, raw := createBotViaAPI(t, tc, session)
	if code != 201 {
		t.Fatalf("bot create failed: %d %s", code, raw)
	}

	code, raw = tc.request("POST", "/api/v1/slash-commands", session, map[string]any{
		"trigger":     "/status",
		"bot_id":      botOut.Bot.UserID,
		"description": "check status",
		"syntax_hint": "",
		"webhook_url": "https://example.com/hook",
		"admin_only":  adminOnly,
	})
	if code != 201 {
		t.Fatalf("slash command create failed: %d %s", code, raw)
	}
	var out struct {
		Command struct {
			ID      string `json:"id"`
			Trigger string `json:"trigger"`
		} `json:"command"`
	}
	_ = json.Unmarshal([]byte(raw), &out)

	if err := tc.s.SetSlashCommandWebhookURLForTest(tc.ctx, out.Command.ID, srv.URL); err != nil {
		t.Fatal(err)
	}
	return out.Command.ID, out.Command.Trigger
}

func TestSlashCommandRegistrationRequiresAdmin(t *testing.T) {
	resetCommandTestState(t)
	tc := setupTestContext(t)
	defer tc.close()

	_, botOut, _ := createBotViaAPI(t, tc, tc.sAdmin)

	code, raw := tc.request("POST", "/api/v1/slash-commands", tc.sMember, map[string]any{
		"trigger": "/status", "bot_id": botOut.Bot.UserID, "description": "d", "webhook_url": "https://example.com/hook",
	})
	if code != 403 {
		t.Fatalf("expected 403, got %d: %s", code, raw)
	}
}

func TestSlashCommandExecuteEphemeralAndInChannel(t *testing.T) {
	resetCommandTestState(t)
	tc := setupTestContext(t)
	defer tc.close()

	var respType string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sig := r.Header.Get("X-Hivemind-Signature")
		if sig == "" {
			t.Error("expected a signature header on every execution")
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"response_type": respType, "text": "all systems go"})
	}))
	defer srv.Close()

	_, trigger := createSlashCommandFixture(t, tc, tc.sAdmin, srv, false)

	t.Run("ephemeral response is not persisted", func(t *testing.T) {
		respType = "ephemeral"
		code, raw := tc.request("POST", "/api/v1/commands/execute", tc.sMember, map[string]any{
			"channel_id": itoa(tc.pubCh.ID), "trigger": trigger, "args": []string{},
		})
		if code != 200 {
			t.Fatalf("expected 200, got %d: %s", code, raw)
		}
		var out struct {
			ResponseType string `json:"response_type"`
			Text         string `json:"text"`
		}
		_ = json.Unmarshal([]byte(raw), &out)
		if out.ResponseType != "ephemeral" || out.Text != "all systems go" {
			t.Fatalf("unexpected response: %s", raw)
		}

		list, err := tc.s.ListChannelMessages(tc.ctx, tc.pubCh.ID, nil, nil, nil, 10)
		if err != nil {
			t.Fatal(err)
		}
		for _, m := range list {
			if m.Body == "all systems go" {
				t.Fatal("ephemeral response must never be written to messages")
			}
		}
	})

	t.Run("in_channel response posts a real message", func(t *testing.T) {
		respType = "in_channel"
		code, raw := tc.request("POST", "/api/v1/commands/execute", tc.sMember, map[string]any{
			"channel_id": itoa(tc.pubCh.ID), "trigger": trigger, "args": []string{},
		})
		if code != 200 {
			t.Fatalf("expected 200, got %d: %s", code, raw)
		}
		var out struct {
			ResponseType string `json:"response_type"`
		}
		_ = json.Unmarshal([]byte(raw), &out)
		if out.ResponseType != "in_channel" {
			t.Fatalf("unexpected response: %s", raw)
		}

		list, err := tc.s.ListChannelMessages(tc.ctx, tc.pubCh.ID, nil, nil, nil, 10)
		if err != nil {
			t.Fatal(err)
		}
		found := false
		for _, m := range list {
			if m.Body == "all systems go" {
				found = true
			}
		}
		if !found {
			t.Fatal("expected the in_channel response to post a real message")
		}
	})
}

func TestSlashCommandAdminOnlyDeniesWithoutHittingWebhook(t *testing.T) {
	resetCommandTestState(t)
	tc := setupTestContext(t)
	defer tc.close()

	hit := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hit = true
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"response_type": "ephemeral", "text": "should never be seen"})
	}))
	defer srv.Close()

	_, trigger := createSlashCommandFixture(t, tc, tc.sAdmin, srv, true)

	code, raw := tc.request("POST", "/api/v1/commands/execute", tc.sMember, map[string]any{
		"channel_id": itoa(tc.pubCh.ID), "trigger": trigger, "args": []string{},
	})
	if code != 200 {
		t.Fatalf("expected 200 (ephemeral access-denied card), got %d: %s", code, raw)
	}
	var out struct {
		ResponseType string `json:"response_type"`
	}
	_ = json.Unmarshal([]byte(raw), &out)
	if out.ResponseType != "ephemeral" {
		t.Fatalf("expected an ephemeral access-denied card, got %s", raw)
	}
	if hit {
		t.Fatal("admin_only command must not reach the remote webhook for a non-admin caller")
	}
}

func TestSlashCommandExecuteNonMemberOfPrivateChannelGets404(t *testing.T) {
	resetCommandTestState(t)
	tc := setupTestContext(t)
	defer tc.close()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"response_type": "ephemeral", "text": "hi"})
	}))
	defer srv.Close()

	_, trigger := createSlashCommandFixture(t, tc, tc.sAdmin, srv, false)

	code, raw := tc.request("POST", "/api/v1/commands/execute", tc.sNonMember, map[string]any{
		"channel_id": itoa(tc.privCh.ID), "trigger": trigger, "args": []string{},
	})
	if code != 404 {
		t.Fatalf("expected 404 for a non-member of a private channel, got %d: %s", code, raw)
	}
	assertErrorCode(t, raw, "channel_not_found")
}

func TestSlashCommandExecuteTimeoutRendersWarning(t *testing.T) {
	resetCommandTestState(t)
	tc := setupTestContext(t)
	defer tc.close()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(200 * time.Millisecond)
		w.WriteHeader(200)
	}))
	defer srv.Close()

	orig := store.SetSlashCommandHTTPClientTimeoutForTest(50 * time.Millisecond)
	defer orig()

	_, trigger := createSlashCommandFixture(t, tc, tc.sAdmin, srv, false)

	code, raw := tc.request("POST", "/api/v1/commands/execute", tc.sMember, map[string]any{
		"channel_id": itoa(tc.pubCh.ID), "trigger": trigger, "args": []string{},
	})
	if code != 200 {
		t.Fatalf("expected 200, got %d: %s", code, raw)
	}
	var out struct {
		ResponseType string `json:"response_type"`
	}
	_ = json.Unmarshal([]byte(raw), &out)
	if out.ResponseType != "ephemeral" {
		t.Fatalf("expected an ephemeral timeout card, got %s", raw)
	}
}

func TestSlashCommandExecuteUnknownTriggerIs404(t *testing.T) {
	resetCommandTestState(t)
	tc := setupTestContext(t)
	defer tc.close()

	code, raw := tc.request("POST", "/api/v1/commands/execute", tc.sMember, map[string]any{
		"channel_id": itoa(tc.pubCh.ID), "trigger": "/unknowncmd", "args": []string{},
	})
	if code != 404 {
		t.Fatalf("expected 404, got %d: %s", code, raw)
	}
	assertErrorCode(t, raw, "command_not_found")
}
