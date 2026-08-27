package realtime_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/0funct0ry/hivemind/internal/api"
	"github.com/0funct0ry/hivemind/internal/auth"
	"github.com/0funct0ry/hivemind/internal/config"
	"github.com/0funct0ry/hivemind/internal/realtime"
	"github.com/0funct0ry/hivemind/internal/store"
	"github.com/gorilla/websocket"
)

func TestRealtimeHub(t *testing.T) {
	api.DisableRateLimits = true
	defer func() { api.DisableRateLimits = false }()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	s, err := store.Open(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	if err := s.Migrate(ctx); err != nil {
		t.Fatal(err)
	}

	a := auth.New(s, 24*time.Hour)
	cfg := config.Config{
		WorkspaceName: "Test Workspace",
		BaseURL:       "",
	}
	r := api.NewRouter(s, a, cfg)

	// Create three users
	u1, _ := s.CreateUser(ctx, store.UserInput{Username: "userone", Email: "u1@example.com", PasswordHash: "hash"})
	u2, _ := s.CreateUser(ctx, store.UserInput{Username: "usertwo", Email: "u2@example.com", PasswordHash: "hash"})
	u3, _ := s.CreateUser(ctx, store.UserInput{Username: "userthree", Email: "u3@example.com", PasswordHash: "hash"})

	// Create sessions
	s1, _ := a.CreateSession(ctx, u1.ID, "UA", "127.0.0.1")
	s2, _ := a.CreateSession(ctx, u2.ID, "UA", "127.0.0.1")
	s3, _ := a.CreateSession(ctx, u3.ID, "UA", "127.0.0.1")

	// Create a private channel where u1 and u2 are members, but u3 is not.
	ch, err := s.CreateChannel(ctx, "private", "test-chan", "Test Channel", "Topic", u1.ID, []int64{u1.ID, u2.ID})
	if err != nil {
		t.Fatal(err)
	}

	// Start test HTTP server
	srv := httptest.NewServer(r)
	defer srv.Close()

	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http") + "/api/v1/ws"

	dialWS := func(sessionID string) *websocket.Conn {
		headers := http.Header{}
		headers.Set("Cookie", "hm_session="+sessionID)
		conn, _, err := websocket.DefaultDialer.Dial(wsURL, headers)
		if err != nil {
			t.Fatalf("failed to dial websocket: %v", err)
		}
		return conn
	}

	// 1. Hello Event verification
	ws1 := dialWS(s1)
	defer ws1.Close()

	var helloFrame realtime.Frame
	if err := ws1.ReadJSON(&helloFrame); err != nil {
		t.Fatal(err)
	}
	if helloFrame.Type != "hello" {
		t.Errorf("expected hello frame, got %s", helloFrame.Type)
	}

	// Connect u2 and u3
	ws2 := dialWS(s2)
	defer ws2.Close()
	var f2 realtime.Frame
	_ = ws2.ReadJSON(&f2) // read hello

	ws3 := dialWS(s3)
	defer ws3.Close()
	var f3 realtime.Frame
	_ = ws3.ReadJSON(&f3) // read hello

	// 2. Event Routing (a) member receives, (b) non-member receives nothing
	// Post a message in the channel as u1
	postMessage := func(sessionID string, channelID int64, body string) {
		client := &http.Client{}
		reqBody, _ := json.Marshal(map[string]string{"body": body})
		req, _ := http.NewRequest("POST", srv.URL+"/api/v1/channels/"+strconv.FormatInt(channelID, 10)+"/messages", bytes.NewBuffer(reqBody))
		req.Header.Set("Cookie", "hm_session="+sessionID)
		req.Header.Set("Origin", srv.URL) // Pass CSRF check
		req.Header.Set("Content-Type", "application/json")
		resp, err := client.Do(req)
		if err != nil {
			t.Fatalf("failed to post message: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusCreated {
			bodyBytes, _ := io.ReadAll(resp.Body)
			t.Fatalf("expected 201 Created, got %d. Body: %s", resp.StatusCode, string(bodyBytes))
		}
	}

	postMessage(s1, ch.ID, "Hello members!")

	readFrameWithTimeout := func(ws *websocket.Conn, timeout time.Duration) (realtime.Frame, error) {
		_ = ws.SetReadDeadline(time.Now().Add(timeout))
		var f realtime.Frame
		err := ws.ReadJSON(&f)
		return f, err
	}

	f, err := readFrameWithTimeout(ws1, time.Second)
	if err != nil {
		t.Errorf("ws1 failed to read frame: %v", err)
	} else if f.Type != "message.created" {
		t.Errorf("ws1 expected message.created, got %s", f.Type)
	}

	f, err = readFrameWithTimeout(ws2, time.Second)
	if err != nil {
		t.Errorf("ws2 failed to read frame: %v", err)
	} else if f.Type != "message.created" {
		t.Errorf("ws2 expected message.created, got %s", f.Type)
	}

	// ws3 (non-member) should not receive anything
	_, err = readFrameWithTimeout(ws3, 100*time.Millisecond)
	if err == nil {
		t.Errorf("ws3 (non-member) received frame unexpectedly")
	}

	// 3. Ordering under concurrent posts (c)
	const concurrentPosts = 100
	var wg sync.WaitGroup
	for i := 0; i < concurrentPosts; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			postMessage(s1, ch.ID, fmt.Sprintf("msg %d", idx))
		}(i)
	}
	wg.Wait()

	// Read 100 messages from ws1 and verify that their IDs are strictly ascending
	var lastID int64
	for i := 0; i < concurrentPosts; i++ {
		frame, err := readFrameWithTimeout(ws1, 2*time.Second)
		if err != nil {
			t.Fatalf("failed to read frame %d: %v", i, err)
		}
		if frame.Type != "message.created" {
			t.Fatalf("expected message.created, got %s", frame.Type)
		}

		var payload map[string]any
		if err := json.Unmarshal(frame.Payload, &payload); err != nil {
			t.Fatal(err)
		}
		idStr, ok := payload["id"].(string)
		if !ok {
			t.Fatalf("no id string in payload")
		}
		id, err := strconv.ParseInt(idStr, 10, 64)
		if err != nil {
			t.Fatal(err)
		}
		if lastID != 0 && id <= lastID {
			t.Errorf("out of order message: ID %d <= last ID %d", id, lastID)
		}
		lastID = id
	}
}

func TestSlowClientEviction(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	s, err := store.Open(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	if err := s.Migrate(ctx); err != nil {
		t.Fatal(err)
	}

	a := auth.New(s, 24*time.Hour)
	r := api.NewRouter(s, a, config.Config{})

	u1, _ := s.CreateUser(ctx, store.UserInput{Username: "userone", Email: "u1@example.com", PasswordHash: "hash"})
	s1, _ := a.CreateSession(ctx, u1.ID, "UA", "127.0.0.1")

	ch, err := s.CreateChannel(ctx, "private", "test-chan", "Test Channel", "Topic", u1.ID, []int64{u1.ID})
	if err != nil {
		t.Fatal(err)
	}

	srv := httptest.NewServer(r)
	defer srv.Close()

	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http") + "/api/v1/ws"

	dialWS := func(sessionID string) *websocket.Conn {
		headers := http.Header{}
		headers.Set("Cookie", "hm_session="+sessionID)
		conn, _, err := websocket.DefaultDialer.Dial(wsURL, headers)
		if err != nil {
			t.Fatalf("failed to dial websocket: %v", err)
		}
		return conn
	}

	// Enable Write Delay to simulate slow client
	realtime.TestWriteDelayMs.Store(10)
	defer func() { realtime.TestWriteDelayMs.Store(0) }()

	wsSlow := dialWS(s1)
	defer wsSlow.Close()
	var f realtime.Frame
	_ = wsSlow.ReadJSON(&f) // read hello

	// Generate more than 256 messages to force buffer overflow
	for i := 0; i < 300; i++ {
		api.TestHub.Publish(realtime.Event{
			Type:      "message.created",
			ChannelID: ch.ID,
			Payload:   map[string]string{"body": fmt.Sprintf("overflow msg %d", i)},
		})
	}

	// Check that wsSlow gets closed
	closedChan := make(chan bool, 1)
	go func() {
		for {
			_, _, err := wsSlow.ReadMessage()
			if err != nil {
				closedChan <- true
				return
			}
		}
	}()

	select {
	case <-closedChan:
		// Evicted successfully!
	case <-time.After(5 * time.Second):
		t.Errorf("expected slow client to be evicted, but it was not closed")
	}

	// Reset write delay for normal client test
	realtime.TestWriteDelayMs.Store(0)

	// Dial another normal client for the same user
	wsNormal := dialWS(s1)
	defer wsNormal.Close()
	_ = wsNormal.ReadJSON(&f) // read hello

	// Send a message and verify normal client receives it
	api.TestHub.Publish(realtime.Event{
		Type:      "message.created",
		ChannelID: ch.ID,
		Payload:   map[string]string{"body": "Normal client message"},
	})

	_ = wsNormal.SetReadDeadline(time.Now().Add(time.Second))
	if err := wsNormal.ReadJSON(&f); err != nil {
		t.Errorf("normal client got error or timed out: %v", err)
	} else if f.Type != "message.created" {
		t.Errorf("expected message.created, got %s", f.Type)
	}
}

func TestResumeCommand(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	s, err := store.Open(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	if err := s.Migrate(ctx); err != nil {
		t.Fatal(err)
	}

	a := auth.New(s, 24*time.Hour)
	r := api.NewRouter(s, a, config.Config{})

	u1, err := s.CreateUser(ctx, store.UserInput{Username: "userone", Email: "u1@example.com", PasswordHash: "hash"})
	if err != nil {
		t.Fatal(err)
	}
	s1, err := a.CreateSession(ctx, u1.ID, "UA", "127.0.0.1")
	if err != nil {
		t.Fatal(err)
	}

	ch, err := s.CreateChannel(ctx, "public", "resume-chan", "Resume Channel", "Topic", u1.ID, []int64{u1.ID})
	if err != nil {
		t.Fatal(err)
	}

	// Create some messages
	m1, _, err := s.CreateMessage(ctx, store.MessageInput{ChannelID: ch.ID, UserID: u1.ID, Body: "msg 1"})
	if err != nil {
		t.Fatal(err)
	}
	_, _, err = s.CreateMessage(ctx, store.MessageInput{ChannelID: ch.ID, UserID: u1.ID, Body: "msg 2"})
	if err != nil {
		t.Fatal(err)
	}
	_, _, err = s.CreateMessage(ctx, store.MessageInput{ChannelID: ch.ID, UserID: u1.ID, Body: "msg 3"})
	if err != nil {
		t.Fatal(err)
	}

	srv := httptest.NewServer(r)
	defer srv.Close()

	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http") + "/api/v1/ws"
	u, _ := url.Parse(wsURL)
	headers := http.Header{}
	headers.Set("Cookie", "hm_session="+s1)
	ws, _, err := websocket.DefaultDialer.Dial(u.String(), headers)
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()

	// Read hello
	var helloFrame realtime.Frame
	_ = ws.ReadJSON(&helloFrame)

	// Send resume command
	resumePayload := realtime.ResumePayload{
		Cursors: map[string]string{
			strconv.FormatInt(ch.ID, 10): strconv.FormatInt(m1.ID, 10),
		},
	}
	pBytes, _ := json.Marshal(resumePayload)
	ws.WriteJSON(realtime.Frame{
		V:       1,
		Type:    "resume",
		TS:      time.Now().UnixMilli(),
		Payload: pBytes,
		Ref:     "test-ref",
	})

	var respFrame realtime.Frame
	ws.SetReadDeadline(time.Now().Add(2 * time.Second))
	if err := ws.ReadJSON(&respFrame); err != nil {
		t.Fatal(err)
	}

	if respFrame.Type != "resume.ok" {
		t.Fatalf("expected resume.ok, got %s", respFrame.Type)
	}
	if respFrame.Ref != "test-ref" {
		t.Errorf("expected ref test-ref, got %s", respFrame.Ref)
	}

	var okPayload realtime.ResumeOkPayload
	json.Unmarshal(respFrame.Payload, &okPayload)

	gapCount := okPayload.Gaps[strconv.FormatInt(ch.ID, 10)]
	if gapCount != 2 {
		t.Errorf("expected gap count of 2, got %d", gapCount)
	}
}

func TestPresenceAPI(t *testing.T) {
	api.DisableRateLimits = true
	defer func() { api.DisableRateLimits = false }()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	s, err := store.Open(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err := s.Migrate(ctx); err != nil {
		t.Fatal(err)
	}

	a := auth.New(s, 24*time.Hour)
	cfg := config.Config{WorkspaceName: "Test Workspace"}
	r := api.NewRouter(s, a, cfg)

	u1, _ := s.CreateUser(ctx, store.UserInput{Username: "presenceone", Email: "p1@example.com", PasswordHash: "hash"})
	u2, _ := s.CreateUser(ctx, store.UserInput{Username: "presencetwo", Email: "p2@example.com", PasswordHash: "hash"})
	s1, _ := a.CreateSession(ctx, u1.ID, "UA", "127.0.0.1")
	s2, _ := a.CreateSession(ctx, u2.ID, "UA", "127.0.0.1")

	srv := httptest.NewServer(r)
	defer srv.Close()

	getPresence := func(sessionID string) map[string]bool {
		req, _ := http.NewRequest("GET", srv.URL+"/api/v1/presence", nil)
		req.Header.Set("Cookie", "hm_session="+sessionID)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		body, _ := io.ReadAll(resp.Body)
		var out struct {
			Online []string `json:"online"`
		}
		if err := json.Unmarshal(body, &out); err != nil {
			t.Fatalf("unmarshal presence response %q: %v", body, err)
		}
		set := map[string]bool{}
		for _, id := range out.Online {
			set[id] = true
		}
		return set
	}

	// No one connected yet.
	online := getPresence(s1)
	if online[strconv.FormatInt(u1.ID, 10)] {
		t.Fatalf("expected u1 not online before connecting, got %v", online)
	}

	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http") + "/api/v1/ws"
	headers := http.Header{}
	headers.Set("Cookie", "hm_session="+s1)
	ws, _, err := websocket.DefaultDialer.Dial(wsURL, headers)
	if err != nil {
		t.Fatal(err)
	}
	var hello realtime.Frame
	_ = ws.ReadJSON(&hello)

	// Give the hub's register command a moment to be processed.
	time.Sleep(50 * time.Millisecond)

	online = getPresence(s2)
	if !online[strconv.FormatInt(u1.ID, 10)] {
		t.Fatalf("expected u1 online after connecting, got %v", online)
	}
	if online[strconv.FormatInt(u2.ID, 10)] {
		t.Fatalf("expected u2 not online, got %v", online)
	}

	ws.Close()
	time.Sleep(50 * time.Millisecond)

	online = getPresence(s2)
	if online[strconv.FormatInt(u1.ID, 10)] {
		t.Fatalf("expected u1 offline after disconnecting, got %v", online)
	}
}
