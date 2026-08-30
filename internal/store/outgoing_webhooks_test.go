package store

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func newOutgoingWebhookTestStore(t *testing.T) *Store {
	t.Helper()
	ctx := context.Background()
	s, err := Open(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	if err := s.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	return s
}

// withPublicHostResolution stubs DNS resolution so a plain httptest.Server (bound to
// 127.0.0.1) can pass the SSRF guard in tests without touching real DNS or real public hosts.
func withPublicHostResolution(t *testing.T, host string, ip net.IP) {
	t.Helper()
	t.Cleanup(StubOutgoingWebhookHostResolution(host, ip))
}

func newOutgoingTestChannel(t *testing.T, s *Store) (User, Channel) {
	t.Helper()
	ctx := context.Background()
	owner, err := s.CreateUser(ctx, UserInput{Username: "owner", Email: "owner@example.com", PasswordHash: "hash"})
	if err != nil {
		t.Fatal(err)
	}
	ch, err := s.CreateChannel(ctx, "public", "incidents", "incidents", "", owner.ID, nil)
	if err != nil {
		t.Fatal(err)
	}
	return owner, ch
}

func TestOutgoingWebhookCreateListGetUpdateRegenerateDelete(t *testing.T) {
	ctx := context.Background()
	s := newOutgoingWebhookTestStore(t)
	owner, ch := newOutgoingTestChannel(t, s)

	withPublicHostResolution(t, "example.com", net.ParseIP("93.184.216.34"))

	wh, secret, err := s.CreateOutgoingWebhook(ctx, OutgoingWebhookInput{
		ChannelID: ch.ID, CreatedBy: owner.ID, Name: "Ops alert", TargetURL: "https://example.com/hook",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(secret, "whsec_") {
		t.Fatalf("secret = %q, want whsec_ prefix", secret)
	}
	if wh.SecretLast4 != secret[len(secret)-4:] {
		t.Fatalf("SecretLast4 = %q, want last 4 of %q", wh.SecretLast4, secret)
	}
	if wh.Status != OutgoingWebhookStatusActive {
		t.Fatalf("Status = %q, want active", wh.Status)
	}
	if wh.Secret != secret {
		t.Fatalf("stored Secret = %q, want %q (dispatch must be able to recompute the HMAC)", wh.Secret, secret)
	}

	chList, err := s.ListOutgoingWebhooksForChannel(ctx, ch.ID)
	if err != nil || len(chList) != 1 {
		t.Fatalf("ListOutgoingWebhooksForChannel = %v, %v", chList, err)
	}
	userList, err := s.ListOutgoingWebhooksForUser(ctx, owner.ID, false)
	if err != nil || len(userList) != 1 {
		t.Fatalf("ListOutgoingWebhooksForUser = %v, %v", userList, err)
	}

	got, err := s.GetOutgoingWebhookByID(ctx, wh.ID)
	if err != nil || got.ID != wh.ID {
		t.Fatalf("GetOutgoingWebhookByID = %v, %v", got, err)
	}

	newName := "Ops alert (renamed)"
	updated, err := s.UpdateOutgoingWebhook(ctx, wh.ID, OutgoingWebhookPatch{Name: &newName})
	if err != nil {
		t.Fatal(err)
	}
	if updated.Name != newName {
		t.Fatalf("Name = %q, want %q", updated.Name, newName)
	}

	regenerated, newSecret, err := s.RegenerateOutgoingWebhookSecret(ctx, wh.ID)
	if err != nil {
		t.Fatal(err)
	}
	if newSecret == secret {
		t.Fatal("expected regenerate to produce a different secret")
	}
	if regenerated.Secret != newSecret {
		t.Fatalf("stored Secret after regenerate = %q, want %q", regenerated.Secret, newSecret)
	}

	if err := s.DeleteOutgoingWebhook(ctx, wh.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GetOutgoingWebhookByID(ctx, wh.ID); err != ErrNotFound {
		t.Fatalf("GetOutgoingWebhookByID after delete = %v, want ErrNotFound", err)
	}
	// Idempotent delete.
	if err := s.DeleteOutgoingWebhook(ctx, wh.ID); err != nil {
		t.Fatalf("second delete should be a no-op, got %v", err)
	}
}

func TestOutgoingWebhookTargetURLSSRFGuard(t *testing.T) {
	ctx := context.Background()
	s := newOutgoingWebhookTestStore(t)
	owner, ch := newOutgoingTestChannel(t, s)

	cases := []struct {
		name string
		url  string
		ip   net.IP
	}{
		{"loopback", "https://internal.test/hook", net.ParseIP("127.0.0.1")},
		{"private-range", "https://internal.test/hook", net.ParseIP("10.0.0.5")},
		{"link-local", "https://internal.test/hook", net.ParseIP("169.254.1.1")},
		{"http-scheme-rejected", "http://example.com/hook", net.ParseIP("93.184.216.34")},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.ip != nil {
				withPublicHostResolution(t, "internal.test", tc.ip)
			}
			if _, _, err := s.CreateOutgoingWebhook(ctx, OutgoingWebhookInput{
				ChannelID: ch.ID, CreatedBy: owner.ID, Name: "bad", TargetURL: tc.url,
			}); err == nil {
				t.Fatalf("expected CreateOutgoingWebhook to reject %s", tc.url)
			}
		})
	}
}

func TestMatchOutgoingWebhooksForMessage(t *testing.T) {
	ctx := context.Background()
	s := newOutgoingWebhookTestStore(t)
	owner, ch := newOutgoingTestChannel(t, s)
	withPublicHostResolution(t, "example.com", net.ParseIP("93.184.216.34"))

	unfiltered, _, err := s.CreateOutgoingWebhook(ctx, OutgoingWebhookInput{
		ChannelID: ch.ID, CreatedBy: owner.ID, Name: "all", TargetURL: "https://example.com/a",
	})
	if err != nil {
		t.Fatal(err)
	}
	filtered, _, err := s.CreateOutgoingWebhook(ctx, OutgoingWebhookInput{
		ChannelID: ch.ID, CreatedBy: owner.ID, Name: "outage-only", TargetURL: "https://example.com/b", KeywordFilter: "outage",
	})
	if err != nil {
		t.Fatal(err)
	}
	matches, err := s.MatchOutgoingWebhooksForMessage(ctx, ch.ID, "everything is fine")
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != 1 || matches[0].ID != unfiltered.ID {
		t.Fatalf("non-matching keyword: got %d matches, want 1 (unfiltered only)", len(matches))
	}

	matches, err = s.MatchOutgoingWebhooksForMessage(ctx, ch.ID, "we have an OUTAGE in prod")
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != 2 {
		t.Fatalf("matching keyword (case-insensitive): got %d matches, want 2", len(matches))
	}

	disabledName := OutgoingWebhookStatusDisabled
	disabled, err := s.UpdateOutgoingWebhook(ctx, filtered.ID, OutgoingWebhookPatch{Status: &disabledName})
	if err != nil {
		t.Fatal(err)
	}
	if disabled.Status != OutgoingWebhookStatusDisabled {
		t.Fatalf("Status = %q, want disabled", disabled.Status)
	}
	matches, err = s.MatchOutgoingWebhooksForMessage(ctx, ch.ID, "we have an outage in prod")
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != 1 || matches[0].ID != unfiltered.ID {
		t.Fatalf("disabled webhook should not match: got %d matches", len(matches))
	}
}

func TestDispatchOutgoingWebhookSignsAndRecordsDeliveries(t *testing.T) {
	ctx := context.Background()
	s := newOutgoingWebhookTestStore(t)
	owner, ch := newOutgoingTestChannel(t, s)
	withPublicHostResolution(t, "example.com", net.ParseIP("93.184.216.34"))

	var gotSignature, gotTimestamp, gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotSignature = r.Header.Get("X-Hivemind-Signature")
		gotTimestamp = r.Header.Get("X-Hivemind-Timestamp")
		buf := make([]byte, r.ContentLength)
		_, _ = r.Body.Read(buf)
		gotBody = string(buf)
		w.WriteHeader(200)
	}))
	defer srv.Close()

	wh, secret, err := s.CreateOutgoingWebhook(ctx, OutgoingWebhookInput{
		ChannelID: ch.ID, CreatedBy: owner.ID, Name: "test", TargetURL: "https://example.com/hook",
	})
	if err != nil {
		t.Fatal(err)
	}
	wh.TargetURL = srv.URL // dispatch itself doesn't re-run the SSRF guard against a live test server

	event := OutgoingEvent{
		MessageID: 1,
		ChannelID: ch.ID,
		Channel:   OutgoingEventChannel{ID: "1", Name: "incidents"},
		Message:   map[string]any{"id": "1", "body": "hello"},
	}
	if err := s.DispatchOutgoingWebhook(ctx, wh, event); err != nil {
		t.Fatalf("DispatchOutgoingWebhook: %v", err)
	}

	if gotSignature == "" || !strings.HasPrefix(gotSignature, "sha256=") {
		t.Fatalf("X-Hivemind-Signature = %q, want sha256=... prefix", gotSignature)
	}
	expected := signOutgoingWebhookBody(secret, []byte(gotBody))
	if gotSignature != expected {
		t.Fatalf("signature mismatch: got %q, want %q (recomputed with the shown-once secret)", gotSignature, expected)
	}
	if gotTimestamp == "" {
		t.Fatal("expected X-Hivemind-Timestamp header")
	}

	deliveries, err := s.ListDeliveries(ctx, wh.ID, nil, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(deliveries) != 1 {
		t.Fatalf("expected exactly 1 delivery row on first-attempt success, got %d", len(deliveries))
	}
	if deliveries[0].ResponseStatus == nil || *deliveries[0].ResponseStatus != 200 {
		t.Fatalf("ResponseStatus = %v, want 200", deliveries[0].ResponseStatus)
	}

	refreshed, err := s.GetOutgoingWebhookByID(ctx, wh.ID)
	if err != nil {
		t.Fatal(err)
	}
	if refreshed.ConsecutiveFailures != 0 {
		t.Fatalf("ConsecutiveFailures = %d, want 0 after success", refreshed.ConsecutiveFailures)
	}
	if refreshed.LastSuccessAt == nil {
		t.Fatal("expected LastSuccessAt to be set after a successful delivery")
	}
}

func TestDispatchOutgoingWebhookRetriesThenRecordsFailures(t *testing.T) {
	origDelays := outgoingWebhookRetryDelays
	outgoingWebhookRetryDelays = []time.Duration{0, 0, 0} // skip real backoff sleeps in tests
	defer func() { outgoingWebhookRetryDelays = origDelays }()

	ctx := context.Background()
	s := newOutgoingWebhookTestStore(t)
	owner, ch := newOutgoingTestChannel(t, s)
	withPublicHostResolution(t, "example.com", net.ParseIP("93.184.216.34"))

	var attempts int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&attempts, 1)
		w.WriteHeader(500)
	}))
	defer srv.Close()

	wh, _, err := s.CreateOutgoingWebhook(ctx, OutgoingWebhookInput{
		ChannelID: ch.ID, CreatedBy: owner.ID, Name: "flaky", TargetURL: "https://example.com/hook",
	})
	if err != nil {
		t.Fatal(err)
	}
	wh.TargetURL = srv.URL

	event := OutgoingEvent{MessageID: 1, ChannelID: ch.ID, Channel: OutgoingEventChannel{ID: "1", Name: "incidents"}, Message: map[string]any{"id": "1"}}
	if err := s.DispatchOutgoingWebhook(ctx, wh, event); err == nil {
		t.Fatal("expected DispatchOutgoingWebhook to return an error after exhausting retries")
	}

	if got := atomic.LoadInt32(&attempts); got != 3 {
		t.Fatalf("attempts = %d, want 3", got)
	}

	deliveries, err := s.ListDeliveries(ctx, wh.ID, nil, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(deliveries) != 3 {
		t.Fatalf("expected 3 delivery rows (one per attempt), got %d", len(deliveries))
	}

	refreshed, err := s.GetOutgoingWebhookByID(ctx, wh.ID)
	if err != nil {
		t.Fatal(err)
	}
	if refreshed.ConsecutiveFailures != 1 {
		t.Fatalf("ConsecutiveFailures = %d, want 1 (one failed message = one increment, regardless of attempt count)", refreshed.ConsecutiveFailures)
	}
	if refreshed.Status != OutgoingWebhookStatusActive {
		t.Fatalf("Status = %q, want still active before the 20-failure threshold", refreshed.Status)
	}
}

func TestOutgoingWebhookFlipsUnhealthyAfterThreshold(t *testing.T) {
	origDelays := outgoingWebhookRetryDelays
	outgoingWebhookRetryDelays = []time.Duration{0, 0, 0}
	defer func() { outgoingWebhookRetryDelays = origDelays }()

	ctx := context.Background()
	s := newOutgoingWebhookTestStore(t)
	owner, ch := newOutgoingTestChannel(t, s)
	withPublicHostResolution(t, "example.com", net.ParseIP("93.184.216.34"))

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(500)
	}))
	defer srv.Close()

	wh, _, err := s.CreateOutgoingWebhook(ctx, OutgoingWebhookInput{
		ChannelID: ch.ID, CreatedBy: owner.ID, Name: "always-down", TargetURL: "https://example.com/hook",
	})
	if err != nil {
		t.Fatal(err)
	}
	wh.TargetURL = srv.URL

	event := OutgoingEvent{MessageID: 1, ChannelID: ch.ID, Channel: OutgoingEventChannel{ID: "1", Name: "incidents"}, Message: map[string]any{"id": "1"}}
	for i := 0; i < outgoingWebhookUnhealthyThreshold; i++ {
		_ = s.DispatchOutgoingWebhook(ctx, wh, event)
	}

	refreshed, err := s.GetOutgoingWebhookByID(ctx, wh.ID)
	if err != nil {
		t.Fatal(err)
	}
	if refreshed.Status != OutgoingWebhookStatusUnhealthy {
		t.Fatalf("Status = %q, want unhealthy after %d consecutive failures", refreshed.Status, outgoingWebhookUnhealthyThreshold)
	}

	// Matching stops dispatching once unhealthy: MatchOutgoingWebhooksForMessage only selects
	// status='active' rows, per SPEC.md §4.11.
	matches, err := s.MatchOutgoingWebhooksForMessage(ctx, ch.ID, "anything")
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != 0 {
		t.Fatalf("expected 0 matches for an unhealthy webhook, got %d", len(matches))
	}

	// PATCH status=active is the re-enable action and must reset the failure count.
	active := OutgoingWebhookStatusActive
	reenabled, err := s.UpdateOutgoingWebhook(ctx, wh.ID, OutgoingWebhookPatch{Status: &active})
	if err != nil {
		t.Fatal(err)
	}
	if reenabled.Status != OutgoingWebhookStatusActive || reenabled.ConsecutiveFailures != 0 {
		t.Fatalf("reenabled = %+v, want status=active, consecutive_failures=0", reenabled)
	}
}

func TestDeliveryPruneKeepsOnlyLast50(t *testing.T) {
	ctx := context.Background()
	s := newOutgoingWebhookTestStore(t)
	owner, ch := newOutgoingTestChannel(t, s)
	withPublicHostResolution(t, "example.com", net.ParseIP("93.184.216.34"))

	wh, _, err := s.CreateOutgoingWebhook(ctx, OutgoingWebhookInput{
		ChannelID: ch.ID, CreatedBy: owner.ID, Name: "prune-me", TargetURL: "https://example.com/hook",
	})
	if err != nil {
		t.Fatal(err)
	}

	for i := 0; i < 55; i++ {
		if err := s.RecordDelivery(ctx, wh.ID, DeliveryAttempt{MessageID: int64(i), AttemptNumber: 1, RequestBody: "{}"}); err != nil {
			t.Fatal(err)
		}
	}

	deliveries, err := s.ListDeliveries(ctx, wh.ID, nil, 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(deliveries) != 50 {
		t.Fatalf("expected pruning to cap at 50 rows, got %d", len(deliveries))
	}
	if deliveries[0].MessageID == nil || *deliveries[0].MessageID != 54 {
		t.Fatalf("expected most recent delivery first, got message_id=%v", deliveries[0].MessageID)
	}
}
