package bot

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"
)

func newTestServer(t *testing.T, scriptContent, secret string) (*Server, string) {
	t.Helper()
	dir := t.TempDir()
	writeFile(t, dir, "echo", scriptContent)
	writeFile(t, dir, "echo.secret", secret)

	routes, warnings, err := Discover(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(warnings) != 0 {
		t.Fatalf("unexpected warnings: %v", warnings)
	}
	return NewServer(routes, Options{
		Shell:               "/bin/sh",
		ScriptTimeout:       2 * time.Second,
		DefaultResponseType: ResponseTypeEphemeral,
		MaxClockSkew:        5 * time.Minute,
	}), secret
}

func signedRequest(t *testing.T, secret string, payload CommandExecPayload) *http.Request {
	t.Helper()
	body, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/hooks/echo", bytes.NewReader(body))
	req.Header.Set("X-Hivemind-Signature", sign(secret, body))
	req.Header.Set("X-Hivemind-Timestamp", strconv.FormatInt(time.Now().UnixMilli(), 10))
	return req
}

func TestServerValidSignatureExecutesScript(t *testing.T) {
	srv, secret := newTestServer(t, `echo "hi {{.Username}}: {{.ArgsJoined}}"`, "whsec_test")

	req := signedRequest(t, secret, CommandExecPayload{
		UserID: "1", Username: "priya", ChannelID: "42", Args: []string{"prod", "main"},
	})
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp CommandResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.ResponseType != ResponseTypeEphemeral {
		t.Fatalf("expected ephemeral, got %q", resp.ResponseType)
	}
	want := "hi priya: prod main"
	if resp.Text != want {
		t.Fatalf("got %q, want %q", resp.Text, want)
	}
}

func TestServerRejectsInvalidSignature(t *testing.T) {
	srv, _ := newTestServer(t, `echo hi`, "whsec_test")

	req := signedRequest(t, "wrong_secret", CommandExecPayload{UserID: "1", ChannelID: "42"})
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rec.Code)
	}
}

func TestServerInChannelJSONPassthrough(t *testing.T) {
	srv, secret := newTestServer(t, `echo '{"response_type":"in_channel","text":"{{.Username}} announced"}'`, "whsec_test")

	req := signedRequest(t, secret, CommandExecPayload{UserID: "1", Username: "bruce", ChannelID: "42"})
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	var resp CommandResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.ResponseType != ResponseTypeInChannel || resp.Text != "bruce announced" {
		t.Fatalf("unexpected response: %+v", resp)
	}
}

func TestServerNonZeroExitIsAlwaysEphemeral(t *testing.T) {
	srv, secret := newTestServer(t, `echo "should be ignored" >&2; exit 1`, "whsec_test")

	req := signedRequest(t, secret, CommandExecPayload{UserID: "1", ChannelID: "42"})
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 (failure is communicated in the JSON body), got %d", rec.Code)
	}
	var resp CommandResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.ResponseType != ResponseTypeEphemeral {
		t.Fatalf("expected ephemeral, got %q", resp.ResponseType)
	}
}

func TestServerReReadsSecretFileOnEveryRequest(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "echo", `echo hi`)
	writeFile(t, dir, "echo.secret", "whsec_original")

	routes, _, err := Discover(dir)
	if err != nil {
		t.Fatal(err)
	}
	srv := NewServer(routes, Options{Shell: "/bin/sh", ScriptTimeout: 2 * time.Second, DefaultResponseType: ResponseTypeEphemeral})

	// The original secret works right after discovery.
	req := signedRequest(t, "whsec_original", CommandExecPayload{UserID: "1"})
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 with the original secret, got %d", rec.Code)
	}

	// Rotating the secret on disk — without restarting the listener — must take effect
	// immediately, exactly like editing the script's contents already does.
	writeFile(t, dir, "echo.secret", "whsec_rotated")

	reqOld := signedRequest(t, "whsec_original", CommandExecPayload{UserID: "1"})
	recOld := httptest.NewRecorder()
	srv.Handler().ServeHTTP(recOld, reqOld)
	if recOld.Code != http.StatusUnauthorized {
		t.Fatalf("expected the stale secret to now be rejected, got %d", recOld.Code)
	}

	reqNew := signedRequest(t, "whsec_rotated", CommandExecPayload{UserID: "1"})
	recNew := httptest.NewRecorder()
	srv.Handler().ServeHTTP(recNew, reqNew)
	if recNew.Code != http.StatusOK {
		t.Fatalf("expected the rotated secret to be accepted without a restart, got %d", recNew.Code)
	}
}

func TestServerUnknownTriggerIs404(t *testing.T) {
	srv, secret := newTestServer(t, `echo hi`, "whsec_test")

	body, _ := json.Marshal(CommandExecPayload{UserID: "1"})
	req := httptest.NewRequest(http.MethodPost, "/hooks/unknown", bytes.NewReader(body))
	req.Header.Set("X-Hivemind-Signature", sign(secret, body))
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for an unregistered trigger, got %d", rec.Code)
	}
}
