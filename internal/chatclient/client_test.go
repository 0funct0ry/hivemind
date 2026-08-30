package chatclient

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func newTestServer(t *testing.T, handler http.Handler) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return srv
}

func requireBearer(t *testing.T, r *http.Request, want string) {
	t.Helper()
	if got := r.Header.Get("Authorization"); got != "Bearer "+want {
		t.Errorf("Authorization header = %q, want %q", got, "Bearer "+want)
	}
}

func TestClientListChannels(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/channels", func(w http.ResponseWriter, r *http.Request) {
		requireBearer(t, r, "hm_test")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"id":"42","kind":"public","slug":"incidents","name":"Incidents","topic":"","member_count":3,"last_message_id":"100","last_read_message_id":"90","joined":true}]}`))
	})
	srv := newTestServer(t, mux)

	c := New(srv.URL, false)
	c.SetToken("hm_test")
	got, err := c.ListChannels(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].ID != "42" || got[0].Name != "Incidents" {
		t.Fatalf("got %#v", got)
	}
}

func TestClientGetMessages(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/channels/42/messages", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("after") != "10" {
			t.Errorf("after query = %q, want %q", r.URL.Query().Get("after"), "10")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"id":"11","channel_id":"42","user_id":"7","body":"hi","created_at":1000}],"has_more":false,"next_before":"11"}`))
	})
	srv := newTestServer(t, mux)

	c := New(srv.URL, false)
	after := "10"
	page, err := c.GetMessages(context.Background(), "42", &after, nil, 50)
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Data) != 1 || page.Data[0].Body != "hi" {
		t.Fatalf("got %#v", page)
	}
}

func TestClientPostMessageIdempotentRetry(t *testing.T) {
	var seen []string
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/channels/42/messages", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			ClientMsgID string `json:"client_msg_id"`
		}
		_ = json.NewDecoder(r.Body).Decode(&in)
		seen = append(seen, in.ClientMsgID)
		w.Header().Set("Content-Type", "application/json")
		if len(seen) == 1 {
			w.WriteHeader(http.StatusCreated)
		} else {
			w.WriteHeader(http.StatusOK) // idempotent replay per SPEC.md §4.1
		}
		_, _ = w.Write([]byte(`{"message":{"id":"55","channel_id":"42","user_id":"7","body":"hello","created_at":1000,"client_msg_id":"` + in.ClientMsgID + `"}}`))
	})
	srv := newTestServer(t, mux)

	c := New(srv.URL, false)
	clientMsgID := NewClientMsgID()
	first, err := c.PostMessage(context.Background(), "42", "hello", clientMsgID, nil)
	if err != nil {
		t.Fatal(err)
	}
	second, err := c.PostMessage(context.Background(), "42", "hello", clientMsgID, nil)
	if err != nil {
		t.Fatal(err)
	}
	if first.ID != second.ID {
		t.Fatalf("retry with same client_msg_id produced a different message: %q vs %q", first.ID, second.ID)
	}
	if len(seen) != 2 || seen[0] != seen[1] {
		t.Fatalf("client_msg_id not reused verbatim across retry: %#v", seen)
	}
}

func TestClientMembers(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/channels/42/members", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"id":"7","username":"priya","display_name":"Priya","avatar_color":"#0E6E60","role":"member","status":"active"}]}`))
	})
	srv := newTestServer(t, mux)

	c := New(srv.URL, false)
	members, err := c.Members(context.Background(), "42")
	if err != nil {
		t.Fatal(err)
	}
	if len(members) != 1 || members[0].Username != "priya" {
		t.Fatalf("got %#v", members)
	}
}

func TestClientAPIErrorEnvelope(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/channels", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"error":{"code":"channel_not_found","message":"Channel not found.","field":null}}`))
	})
	srv := newTestServer(t, mux)

	c := New(srv.URL, false)
	_, err := c.ListChannels(context.Background())
	if err == nil {
		t.Fatal("expected error")
	}
	apiErr, ok := err.(*APIError)
	if !ok {
		t.Fatalf("err = %T, want *APIError", err)
	}
	if apiErr.Code != "channel_not_found" || apiErr.StatusCode != 404 {
		t.Fatalf("got %#v", apiErr)
	}
}

func TestClientLoginAndIssueToken(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/auth/login", func(w http.ResponseWriter, r *http.Request) {
		http.SetCookie(w, &http.Cookie{Name: "hm_session", Value: "sess123"})
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"user":{"id":"1","username":"alice"}}`))
	})
	mux.HandleFunc("/api/v1/tokens", func(w http.ResponseWriter, r *http.Request) {
		ck, err := r.Cookie("hm_session")
		if err != nil || ck.Value != "sess123" {
			t.Errorf("expected session cookie sess123, got %v (err=%v)", ck, err)
		}
		// The server's CSRF check (RequireAuth) rejects a cookie-authenticated POST
		// without a matching Origin/Referer — IssueToken must set one.
		if got := r.Header.Get("Origin"); got == "" {
			t.Error("expected an Origin header on the cookie-authenticated token request")
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"id":"1","name":"chat-cli","token":"hm_plaintext"}`))
	})
	srv := newTestServer(t, mux)

	c := New(srv.URL, false)
	cookie, err := c.Login(context.Background(), "alice", "password123")
	if err != nil {
		t.Fatal(err)
	}
	token, err := c.IssueToken(context.Background(), cookie, "chat-cli", 0)
	if err != nil {
		t.Fatal(err)
	}
	if token != "hm_plaintext" {
		t.Fatalf("token = %q, want %q", token, "hm_plaintext")
	}
}
