package chatclient

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

var testUpgrader = websocket.Upgrader{}

func TestConnDialSendsHelloAndAutoRepliesPong(t *testing.T) {
	pongReceived := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer hm_test" {
			t.Errorf("Authorization header = %q, want %q", got, "Bearer hm_test")
		}
		ws, err := testUpgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Fatal(err)
		}
		defer ws.Close()

		_ = ws.WriteJSON(Frame{V: 1, Type: "hello", TS: 1})
		_ = ws.WriteJSON(Frame{V: 1, Type: "ping", TS: 2})

		var f Frame
		if err := ws.ReadJSON(&f); err != nil {
			return
		}
		if f.Type == "pong" {
			close(pongReceived)
		}
	}))
	defer srv.Close()

	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http") + "/"
	conn, err := Dial(context.Background(), wsURL, "hm_test", false)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	select {
	case <-pongReceived:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for client to auto-reply pong to server ping")
	}
}

func TestConnResumeReturnsGaps(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ws, err := testUpgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Fatal(err)
		}
		defer ws.Close()

		_ = ws.WriteJSON(Frame{V: 1, Type: "hello", TS: 1})

		var f Frame
		if err := ws.ReadJSON(&f); err != nil {
			return
		}
		if f.Type != "resume" {
			t.Errorf("expected resume frame, got %q", f.Type)
			return
		}
		var payload struct {
			Cursors map[string]string `json:"cursors"`
		}
		_ = json.Unmarshal(f.Payload, &payload)
		if payload.Cursors["42"] != "100" {
			t.Errorf("cursors = %#v, want {42: 100}", payload.Cursors)
		}
		gapPayload, _ := json.Marshal(ResumeOkPayload{Gaps: map[string]int64{"42": 3}})
		_ = ws.WriteJSON(Frame{V: 1, Type: "resume.ok", TS: 2, Ref: f.Ref, Payload: gapPayload})
	}))
	defer srv.Close()

	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http") + "/"
	conn, err := Dial(context.Background(), wsURL, "hm_test", false)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	gaps, err := conn.Resume(ctx, map[string]string{"42": "100"})
	if err != nil {
		t.Fatal(err)
	}
	if gaps["42"] != 3 {
		t.Fatalf("gaps = %#v, want {42: 3}", gaps)
	}
}

func TestConnForwardsOtherEvents(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ws, err := testUpgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Fatal(err)
		}
		defer ws.Close()
		_ = ws.WriteJSON(Frame{V: 1, Type: "hello", TS: 1})
		payload, _ := json.Marshal(map[string]string{"id": "99"})
		_ = ws.WriteJSON(Frame{V: 1, Type: "message.created", TS: 2, Payload: payload})
		time.Sleep(50 * time.Millisecond)
	}))
	defer srv.Close()

	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http") + "/"
	conn, err := Dial(context.Background(), wsURL, "hm_test", false)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	select {
	case ev := <-conn.Events():
		if ev.Type != "message.created" {
			t.Fatalf("event type = %q, want message.created", ev.Type)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for forwarded event")
	}
}
