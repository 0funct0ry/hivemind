package api

import (
	"fmt"
	"sync"
	"testing"
)

// TestChannelCreateHTTPRace exercises POST /api/v1/channels through the full HTTP
// handler chain repeatedly and concurrently. store.CreateChannel (and several other
// store/auth calls reached from internal/api) were passed the raw *gin.Context instead
// of c.Request.Context() — since *gin.Context implements context.Context by forwarding
// to the request context, this "worked" functionally, but handed database/sql's
// context-tracking goroutines (spawned by BeginTx/QueryContext) a reference to a
// gin.Context object that gin's sync.Pool recycles for a *different* request as soon as
// ServeHTTP returns. Under concurrent traffic this races: a leftover goroutine from
// request A reads fields on the pooled *gin.Context while gin resets it for request B.
// This test hammers the endpoint concurrently under `-race` to catch that class of bug.
func TestChannelCreateHTTPRace(t *testing.T) {
	tc := setupTestContext(t)
	defer tc.close()

	const n = 20
	var wg sync.WaitGroup
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func(i int) {
			defer wg.Done()
			body := map[string]any{
				"kind": "public",
				"slug": fmt.Sprintf("race-chan-%d", i),
				"name": fmt.Sprintf("Race Chan %d", i),
			}
			code, resp := tc.request("POST", "/api/v1/channels", tc.sMember, body)
			if code != 201 {
				t.Errorf("expected 201 creating channel %d, got %d. Resp: %s", i, code, resp)
			}
		}(i)
	}
	wg.Wait()
}

// TestChannelCreateHTTPSequentialRace reproduces the originally reported race: rapid
// *sequential* requests through the pooled gin.Context, immediately followed by another
// request, so a leftover background goroutine from request N can still be touching the
// recycled *gin.Context when request N+1 starts mutating it.
func TestChannelCreateHTTPSequentialRace(t *testing.T) {
	tc := setupTestContext(t)
	defer tc.close()

	for i := 0; i < 50; i++ {
		body := map[string]any{
			"kind": "public",
			"slug": fmt.Sprintf("seq-chan-%d", i),
			"name": fmt.Sprintf("Seq Chan %d", i),
		}
		code, resp := tc.request("POST", "/api/v1/channels", tc.sMember, body)
		if code != 201 {
			t.Fatalf("expected 201 creating channel %d, got %d. Resp: %s", i, code, resp)
		}
		code, resp = tc.request("GET", "/api/v1/channels", tc.sMember, nil)
		if code != 200 {
			t.Fatalf("expected 200 listing channels, got %d. Resp: %s", code, resp)
		}
	}
}
