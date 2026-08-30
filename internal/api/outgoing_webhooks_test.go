package api

import (
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/0funct0ry/hivemind/internal/store"
)

func resetOutgoingWebhookTestState(t *testing.T) {
	t.Helper()
	DisableRateLimits = true
	t.Cleanup(store.StubOutgoingWebhookHostResolution("example.com", net.ParseIP("93.184.216.34")))
}

type outgoingWebhookResp struct {
	Webhook struct {
		ID                  string `json:"id"`
		Status              string `json:"status"`
		MaskedSecret        string `json:"masked_secret"`
		Secret              string `json:"secret"`
		KeywordFilter       string `json:"keyword_filter"`
		ConsecutiveFailures int    `json:"consecutive_failures"`
	} `json:"webhook"`
}

func createOutgoingWebhookViaAPI(t *testing.T, tc *testContext, channelID int64, session string, extra map[string]any) (int, outgoingWebhookResp, string) {
	t.Helper()
	body := map[string]any{"name": "Ops alert", "target_url": "https://example.com/hook"}
	for k, v := range extra {
		body[k] = v
	}
	code, raw := tc.request("POST", "/api/v1/channels/"+itoa(channelID)+"/outgoing-webhooks", session, body)
	var out outgoingWebhookResp
	_ = json.Unmarshal([]byte(raw), &out)
	return code, out, raw
}

func TestOutgoingWebhookManagementAuthorization(t *testing.T) {
	resetOutgoingWebhookTestState(t)
	tc := setupTestContext(t)
	defer tc.close()

	t.Run("owner can create on their own channel", func(t *testing.T) {
		code, out, raw := createOutgoingWebhookViaAPI(t, tc, tc.pubCh.ID, tc.sAdmin, nil)
		if code != 201 {
			t.Fatalf("expected 201, got %d: %s", code, raw)
		}
		if !strings.HasPrefix(out.Webhook.Secret, "whsec_") {
			t.Fatalf("expected a shown-once secret with whsec_ prefix, got %q", out.Webhook.Secret)
		}
	})

	t.Run("non-owner member gets 403", func(t *testing.T) {
		code, _, raw := createOutgoingWebhookViaAPI(t, tc, tc.pubCh.ID, tc.sMember, nil)
		if code != 403 {
			t.Fatalf("expected 403, got %d: %s", code, raw)
		}
		assertErrorCode(t, raw, "forbidden")
	})

	t.Run("non-member of private channel gets 404", func(t *testing.T) {
		code, _, raw := createOutgoingWebhookViaAPI(t, tc, tc.privCh.ID, tc.sNonMember, nil)
		if code != 404 {
			t.Fatalf("expected 404, got %d: %s", code, raw)
		}
		assertErrorCode(t, raw, "channel_not_found")
	})

	t.Run("admin can create on a private channel despite not being a member", func(t *testing.T) {
		code, _, raw := createOutgoingWebhookViaAPI(t, tc, tc.privCh.ID, tc.sAdmin, nil)
		if code != 201 {
			t.Fatalf("expected 201, got %d: %s", code, raw)
		}
	})

	t.Run("private channel owner can create", func(t *testing.T) {
		code, _, raw := createOutgoingWebhookViaAPI(t, tc, tc.privCh.ID, tc.sMember, nil)
		if code != 201 {
			t.Fatalf("expected 201, got %d: %s", code, raw)
		}
	})

	t.Run("SSRF-guarded target_url is rejected", func(t *testing.T) {
		code, _, raw := createOutgoingWebhookViaAPI(t, tc, tc.pubCh.ID, tc.sAdmin, map[string]any{"target_url": "https://127.0.0.1/hook"})
		if code != 400 {
			t.Fatalf("expected 400, got %d: %s", code, raw)
		}
	})
}

func TestOutgoingWebhookCRUDViaAPI(t *testing.T) {
	resetOutgoingWebhookTestState(t)
	tc := setupTestContext(t)
	defer tc.close()

	code, created, raw := createOutgoingWebhookViaAPI(t, tc, tc.pubCh.ID, tc.sAdmin, map[string]any{"keyword_filter": "outage"})
	if code != 201 {
		t.Fatalf("create failed: %d %s", code, raw)
	}
	id := created.Webhook.ID
	oldSecret := created.Webhook.Secret

	t.Run("get", func(t *testing.T) {
		code, raw := tc.request("GET", "/api/v1/outgoing-webhooks/"+id, tc.sAdmin, nil)
		if code != 200 {
			t.Fatalf("expected 200, got %d: %s", code, raw)
		}
		if strings.Contains(raw, oldSecret) {
			t.Fatal("GET must never return the plaintext secret again")
		}
	})

	t.Run("list workspace-wide", func(t *testing.T) {
		code, raw := tc.request("GET", "/api/v1/outgoing-webhooks", tc.sAdmin, nil)
		if code != 200 {
			t.Fatalf("expected 200, got %d: %s", code, raw)
		}
		var out struct {
			Data []map[string]any `json:"data"`
		}
		_ = json.Unmarshal([]byte(raw), &out)
		if len(out.Data) == 0 {
			t.Fatal("expected at least one outgoing webhook in workspace list")
		}
	})

	t.Run("list channel-scoped rejects non-owner", func(t *testing.T) {
		code, raw := tc.request("GET", "/api/v1/channels/"+itoa(tc.pubCh.ID)+"/outgoing-webhooks", tc.sMember, nil)
		if code != 403 {
			t.Fatalf("expected 403, got %d: %s", code, raw)
		}
	})

	t.Run("patch", func(t *testing.T) {
		code, raw := tc.request("PATCH", "/api/v1/outgoing-webhooks/"+id, tc.sAdmin, map[string]any{"name": "renamed"})
		if code != 200 {
			t.Fatalf("expected 200, got %d: %s", code, raw)
		}
		var out outgoingWebhookResp
		_ = json.Unmarshal([]byte(raw), &out)
		if out.Webhook.KeywordFilter != "outage" {
			t.Fatalf("patching name should not clear keyword_filter, got %q", out.Webhook.KeywordFilter)
		}
	})

	t.Run("regenerate secret invalidates the old one", func(t *testing.T) {
		code, raw := tc.request("POST", "/api/v1/outgoing-webhooks/"+id+"/regenerate-secret", tc.sAdmin, nil)
		if code != 200 {
			t.Fatalf("expected 200, got %d: %s", code, raw)
		}
		var out outgoingWebhookResp
		_ = json.Unmarshal([]byte(raw), &out)
		if out.Webhook.Secret == "" || out.Webhook.Secret == oldSecret {
			t.Fatalf("expected a fresh plaintext secret, got %q (old was %q)", out.Webhook.Secret, oldSecret)
		}
	})

	t.Run("delete then get 404, delete again is idempotent", func(t *testing.T) {
		code, raw := tc.request("DELETE", "/api/v1/outgoing-webhooks/"+id, tc.sAdmin, nil)
		if code != 200 {
			t.Fatalf("expected 200, got %d: %s", code, raw)
		}
		code, raw = tc.request("GET", "/api/v1/outgoing-webhooks/"+id, tc.sAdmin, nil)
		if code != 404 {
			t.Fatalf("expected 404 after delete, got %d: %s", code, raw)
		}
		code, raw = tc.request("DELETE", "/api/v1/outgoing-webhooks/"+id, tc.sAdmin, nil)
		if code != 200 {
			t.Fatalf("expected idempotent 200 on second delete, got %d: %s", code, raw)
		}
	})
}

// TestOutgoingWebhookDispatchesOnMessageCreate exercises the wiring from POST /messages through
// to a real enqueued-and-delivered HTTP call. It stores the webhook directly (bypassing the
// SSRF-guarded creation API, which is tested separately in TestOutgoingWebhookManagementAuthorization
// and at the store layer in TestOutgoingWebhookTargetURLSSRFGuard) so it can point at a plain
// httptest.Server without fighting the https-only rule.
func TestOutgoingWebhookDispatchesOnMessageCreate(t *testing.T) {
	resetOutgoingWebhookTestState(t)
	tc := setupTestContext(t)
	defer tc.close()

	received := make(chan string, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		received <- r.Header.Get("X-Hivemind-Signature")
		w.WriteHeader(200)
	}))
	defer srv.Close()

	wh, _, err := tc.s.CreateOutgoingWebhook(tc.ctx, store.OutgoingWebhookInput{
		ChannelID: tc.pubCh.ID, CreatedBy: tc.uAdmin.ID, Name: "test", TargetURL: "https://example.com/hook",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := tc.s.SetOutgoingWebhookTargetURLForTest(tc.ctx, wh.ID, srv.URL); err != nil {
		t.Fatal(err)
	}

	code, raw := tc.request("POST", "/api/v1/channels/"+itoa(tc.pubCh.ID)+"/messages", tc.sAdmin, map[string]any{"body": "hello world"})
	if code != 201 {
		t.Fatalf("message create failed: %d %s", code, raw)
	}

	select {
	case sig := <-received:
		if sig == "" || !strings.HasPrefix(sig, "sha256=") {
			t.Fatalf("X-Hivemind-Signature = %q, want sha256=... prefix", sig)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("expected the dispatch worker pool to deliver the event within 2s")
	}
}
