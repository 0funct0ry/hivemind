package api

import (
	"encoding/json"
	"strconv"
	"strings"
	"testing"

	"github.com/0funct0ry/hivemind/internal/store"
)

func resetWebhookTestState() {
	DisableRateLimits = true
	ResetWebhookIngestLimiter()
	store.ResetWebhookFloodTracker()
}

type webhookResp struct {
	Webhook struct {
		ID        string `json:"id"`
		Status    string `json:"status"`
		MaskedTok string `json:"masked_token"`
		IngestURL string `json:"ingest_url"`
		CreatedBy string `json:"created_by"`
	} `json:"webhook"`
}

func createWebhookViaAPI(t *testing.T, tc *testContext, channelID int64, session string, extra map[string]any) (int, webhookResp, string) {
	t.Helper()
	body := map[string]any{"name": "CI", "format_preset": "generic"}
	for k, v := range extra {
		body[k] = v
	}
	code, raw := tc.request("POST", "/api/v1/channels/"+itoa(channelID)+"/webhooks", session, body)
	var out webhookResp
	_ = json.Unmarshal([]byte(raw), &out)
	return code, out, raw
}

func itoa(v int64) string {
	return strconv.FormatInt(v, 10)
}

func TestWebhookManagementAuthorization(t *testing.T) {
	resetWebhookTestState()
	tc := setupTestContext(t)
	defer tc.close()

	t.Run("owner can create on their own channel", func(t *testing.T) {
		code, out, raw := createWebhookViaAPI(t, tc, tc.pubCh.ID, tc.sAdmin, nil)
		if code != 201 {
			t.Fatalf("expected 201, got %d: %s", code, raw)
		}
		if out.Webhook.IngestURL == "" || !strings.Contains(out.Webhook.IngestURL, "/ingest/whk_") {
			t.Fatalf("expected an ingest_url with plaintext token, got %q", out.Webhook.IngestURL)
		}
	})

	t.Run("non-owner member gets 403", func(t *testing.T) {
		code, _, raw := createWebhookViaAPI(t, tc, tc.pubCh.ID, tc.sMember, nil)
		if code != 403 {
			t.Fatalf("expected 403, got %d: %s", code, raw)
		}
		assertErrorCode(t, raw, "forbidden")
	})

	t.Run("non-member of private channel gets 404", func(t *testing.T) {
		code, _, raw := createWebhookViaAPI(t, tc, tc.privCh.ID, tc.sNonMember, nil)
		if code != 404 {
			t.Fatalf("expected 404, got %d: %s", code, raw)
		}
		assertErrorCode(t, raw, "channel_not_found")
	})

	t.Run("admin can create on a private channel despite not being a member", func(t *testing.T) {
		code, _, raw := createWebhookViaAPI(t, tc, tc.privCh.ID, tc.sAdmin, nil)
		if code != 201 {
			t.Fatalf("expected 201, got %d: %s", code, raw)
		}
	})

	t.Run("private channel owner can create", func(t *testing.T) {
		code, _, raw := createWebhookViaAPI(t, tc, tc.privCh.ID, tc.sMember, nil)
		if code != 201 {
			t.Fatalf("expected 201, got %d: %s", code, raw)
		}
	})
}

func TestWebhookCRUDViaAPI(t *testing.T) {
	resetWebhookTestState()
	tc := setupTestContext(t)
	defer tc.close()

	code, created, raw := createWebhookViaAPI(t, tc, tc.pubCh.ID, tc.sAdmin, nil)
	if code != 201 {
		t.Fatalf("create failed: %d %s", code, raw)
	}
	id := created.Webhook.ID
	oldIngestURL := created.Webhook.IngestURL

	t.Run("get", func(t *testing.T) {
		code, raw := tc.request("GET", "/api/v1/webhooks/"+id, tc.sAdmin, nil)
		if code != 200 {
			t.Fatalf("expected 200, got %d: %s", code, raw)
		}
	})

	t.Run("list workspace-wide", func(t *testing.T) {
		code, raw := tc.request("GET", "/api/v1/webhooks", tc.sAdmin, nil)
		if code != 200 {
			t.Fatalf("expected 200, got %d: %s", code, raw)
		}
		var out struct {
			Data []map[string]any `json:"data"`
		}
		_ = json.Unmarshal([]byte(raw), &out)
		if len(out.Data) == 0 {
			t.Fatal("expected at least one webhook in workspace list")
		}
	})

	t.Run("list channel-scoped", func(t *testing.T) {
		code, raw := tc.request("GET", "/api/v1/channels/"+itoa(tc.pubCh.ID)+"/webhooks", tc.sAdmin, nil)
		if code != 200 {
			t.Fatalf("expected 200, got %d: %s", code, raw)
		}
	})

	t.Run("patch", func(t *testing.T) {
		code, raw := tc.request("PATCH", "/api/v1/webhooks/"+id, tc.sAdmin, map[string]any{"name": "Renamed"})
		if code != 200 {
			t.Fatalf("expected 200, got %d: %s", code, raw)
		}
		if !strings.Contains(raw, "Renamed") {
			t.Fatalf("expected renamed webhook in response, got %s", raw)
		}
	})

	t.Run("regenerate invalidates old ingest URL", func(t *testing.T) {
		code, raw := tc.request("POST", "/api/v1/webhooks/"+id+"/regenerate", tc.sAdmin, nil)
		if code != 200 {
			t.Fatalf("expected 200, got %d: %s", code, raw)
		}
		var out webhookResp
		_ = json.Unmarshal([]byte(raw), &out)
		if out.Webhook.IngestURL == oldIngestURL {
			t.Fatal("expected a new ingest URL after regenerate")
		}

		oldSuffix := oldIngestURL[strings.LastIndex(oldIngestURL, "/ingest/"):]
		code, raw = tc.request("POST", "/api/v1/webhooks/"+id+oldSuffix, "", map[string]any{"title": "should fail"})
		if code != 404 {
			t.Fatalf("old token should 404, got %d: %s", code, raw)
		}
	})

	t.Run("delete is idempotent", func(t *testing.T) {
		code, raw := tc.request("DELETE", "/api/v1/webhooks/"+id, tc.sAdmin, nil)
		if code != 200 {
			t.Fatalf("expected 200, got %d: %s", code, raw)
		}
		code, raw = tc.request("DELETE", "/api/v1/webhooks/"+id, tc.sAdmin, nil)
		if code != 200 {
			t.Fatalf("second delete should still be 200, got %d: %s", code, raw)
		}
	})
}

func TestWebhookIngest(t *testing.T) {
	resetWebhookTestState()
	tc := setupTestContext(t)
	defer tc.close()

	_, created, raw := createWebhookViaAPI(t, tc, tc.pubCh.ID, tc.sAdmin, nil)
	if created.Webhook.IngestURL == "" {
		t.Fatalf("expected ingest url, got %s", raw)
	}
	ingestPath := strings.TrimPrefix(created.Webhook.IngestURL, "http://example.com")

	t.Run("generic payload posts a card message", func(t *testing.T) {
		code, resp := tc.request("POST", ingestPath, "", map[string]any{
			"title": "Build failed", "severity": "critical",
			"fields": []map[string]any{{"label": "Env", "value": "prod"}},
		})
		if code != 200 {
			t.Fatalf("expected 200, got %d: %s", code, resp)
		}
		var out struct {
			OK        bool   `json:"ok"`
			MessageID string `json:"message_id"`
		}
		_ = json.Unmarshal([]byte(resp), &out)
		if !out.OK || out.MessageID == "" {
			t.Fatalf("unexpected response: %s", resp)
		}

		code, resp = tc.request("GET", "/api/v1/messages/"+out.MessageID, tc.sAdmin, nil)
		if code != 200 {
			t.Fatalf("expected 200 fetching message, got %d: %s", code, resp)
		}
		if !strings.Contains(resp, `"critical"`) {
			t.Fatalf("expected critical severity in card, got %s", resp)
		}
	})

	t.Run("malformed payload still posts a fallback card", func(t *testing.T) {
		code, resp := tc.request("POST", ingestPath, "", "not-json-at-all")
		if code != 200 {
			t.Fatalf("expected 200, got %d: %s", code, resp)
		}
		var out struct {
			MessageID string `json:"message_id"`
		}
		_ = json.Unmarshal([]byte(resp), &out)
		code, resp = tc.request("GET", "/api/v1/messages/"+out.MessageID, tc.sAdmin, nil)
		if code != 200 || !strings.Contains(resp, "Unrecognized webhook payload") {
			t.Fatalf("expected fallback card, got %d: %s", code, resp)
		}
	})

	t.Run("wrong token 404s", func(t *testing.T) {
		badPath := ingestPath[:strings.LastIndex(ingestPath, "/")+1] + "whk_wrongtoken"
		code, resp := tc.request("POST", badPath, "", map[string]any{"title": "x"})
		if code != 404 {
			t.Fatalf("expected 404, got %d: %s", code, resp)
		}
		assertErrorCode(t, resp, "webhook_not_found")
	})
}

func TestWebhookIngest_SlackPayload(t *testing.T) {
	resetWebhookTestState()
	tc := setupTestContext(t)
	defer tc.close()

	_, created, _ := createWebhookViaAPI(t, tc, tc.pubCh.ID, tc.sAdmin, map[string]any{"format_preset": "slack_compatible"})
	ingestPath := strings.TrimPrefix(created.Webhook.IngestURL, "http://example.com")

	code, resp := tc.request("POST", ingestPath, "", map[string]any{
		"text": "fallback", "attachments": []map[string]any{{"color": "danger", "title": "Outage"}},
	})
	if code != 200 {
		t.Fatalf("expected 200, got %d: %s", code, resp)
	}
	var out struct {
		MessageID string `json:"message_id"`
	}
	_ = json.Unmarshal([]byte(resp), &out)
	code, resp = tc.request("GET", "/api/v1/messages/"+out.MessageID, tc.sAdmin, nil)
	if code != 200 || !strings.Contains(resp, `"critical"`) {
		t.Fatalf("expected critical severity from danger color, got %d: %s", code, resp)
	}
}

func TestWebhookIngest_ArchivedChannelRedirect(t *testing.T) {
	resetWebhookTestState()
	tc := setupTestContext(t)
	defer tc.close()

	_, created, _ := createWebhookViaAPI(t, tc, tc.pubCh.ID, tc.sAdmin, nil)
	ingestPath := strings.TrimPrefix(created.Webhook.IngestURL, "http://example.com")

	code, raw := tc.request("PATCH", "/api/v1/channels/"+itoa(tc.pubCh.ID), tc.sAdmin, map[string]any{"archived": true})
	if code != 200 {
		t.Fatalf("archive failed: %d %s", code, raw)
	}

	code, resp := tc.request("POST", ingestPath, "", map[string]any{"title": "still delivers"})
	if code != 200 {
		t.Fatalf("expected 200, got %d: %s", code, resp)
	}
	var out struct {
		MessageID string `json:"message_id"`
	}
	_ = json.Unmarshal([]byte(resp), &out)
	code, resp = tc.request("GET", "/api/v1/messages/"+out.MessageID, tc.sAdmin, nil)
	if code != 200 || !strings.Contains(resp, `"redirect_notice":true`) {
		t.Fatalf("expected redirect_notice card, got %d: %s", code, resp)
	}
}

func TestWebhookIngest_FloodCollapse(t *testing.T) {
	resetWebhookTestState()
	tc := setupTestContext(t)
	defer tc.close()

	_, created, _ := createWebhookViaAPI(t, tc, tc.pubCh.ID, tc.sAdmin, nil)
	ingestPath := strings.TrimPrefix(created.Webhook.IngestURL, "http://example.com")

	seen := map[string]bool{}
	for i := 0; i < 15; i++ {
		code, resp := tc.request("POST", ingestPath, "", map[string]any{"title": "spam"})
		if code != 200 {
			t.Fatalf("expected 200 on iteration %d, got %d: %s", i, code, resp)
		}
		var out struct {
			MessageID string `json:"message_id"`
		}
		_ = json.Unmarshal([]byte(resp), &out)
		seen[out.MessageID] = true
	}
	if len(seen) >= 15 {
		t.Fatalf("expected flood collapse to produce fewer than 15 distinct messages, got %d", len(seen))
	}
}

func TestWebhookClaim(t *testing.T) {
	resetWebhookTestState()
	tc := setupTestContext(t)
	defer tc.close()

	_, created, _ := createWebhookViaAPI(t, tc, tc.pubCh.ID, tc.sMember, nil)
	// sMember isn't the pubCh owner (sAdmin is); use privCh instead, owned by sMember.
	_, created, raw := createWebhookViaAPI(t, tc, tc.privCh.ID, tc.sMember, nil)
	if created.Webhook.ID == "" {
		t.Fatalf("create failed: %s", raw)
	}

	code, raw := tc.request("POST", "/api/v1/users/"+itoa(tc.uMember.ID)+"/deactivate", tc.sAdmin, nil)
	if code != 204 {
		t.Fatalf("deactivate failed: %d %s", code, raw)
	}

	code, raw = tc.request("GET", "/api/v1/webhooks/"+created.Webhook.ID, tc.sAdmin, nil)
	if code != 200 || !strings.Contains(raw, `"orphaned"`) {
		t.Fatalf("expected orphaned status, got %d: %s", code, raw)
	}

	code, raw = tc.request("POST", "/api/v1/webhooks/"+created.Webhook.ID+"/claim", tc.sNonMember, nil)
	if code != 403 {
		t.Fatalf("non-admin claim should be forbidden, got %d: %s", code, raw)
	}

	code, raw = tc.request("POST", "/api/v1/webhooks/"+created.Webhook.ID+"/claim", tc.sAdmin, nil)
	if code != 200 || !strings.Contains(raw, `"active"`) {
		t.Fatalf("admin claim should succeed and restore active, got %d: %s", code, raw)
	}
}
