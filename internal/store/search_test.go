package store

import (
	"context"
	"testing"
	"time"
)

func TestSearch(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()
	s, err := Open(ctx, tmpDir)
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer s.Close()

	if err := s.Migrate(ctx); err != nil {
		t.Fatalf("Migrate failed: %v", err)
	}

	// Create users - Alice creation will auto-create the "#general" channel!
	u1, err := s.CreateUser(ctx, UserInput{
		Username:     "alice",
		Email:        "alice@example.com",
		DisplayName:  "Alice",
		PasswordHash: "password123",
		Role:         "admin",
	})
	if err != nil {
		t.Fatalf("CreateUser failed: %v", err)
	}
	u2, err := s.CreateUser(ctx, UserInput{
		Username:     "bob",
		Email:        "bob@example.com",
		DisplayName:  "Bob",
		PasswordHash: "password123",
		Role:         "member",
	})
	if err != nil {
		t.Fatalf("CreateUser failed: %v", err)
	}

	// Retrieve general channel ID
	ch1, err := s.GetChannelBySlug(ctx, "general")
	if err != nil {
		t.Fatalf("GetChannelBySlug failed: %v", err)
	}

	// Post some messages
	_, _, err = s.CreateMessage(ctx, MessageInput{
		ChannelID: ch1.ID,
		UserID:    u1.ID,
		Body:      "Welcome to general! Let's deploy the settlement fix.",
	})
	if err != nil {
		t.Fatalf("CreateMessage failed: %v", err)
	}

	_, _, err = s.CreateMessage(ctx, MessageInput{
		ChannelID: ch1.ID,
		UserID:    u2.ID,
		Body:      "Great. I will run a retry on the failing tasks.",
	})
	if err != nil {
		t.Fatalf("CreateMessage failed: %v", err)
	}

	t.Run("Basic text search", func(t *testing.T) {
		hits, err := s.Search(ctx, u1.ID, SearchQuery{Text: "settlement"})
		if err != nil {
			t.Fatalf("Search failed: %v", err)
		}
		if len(hits) != 1 {
			t.Errorf("Expected 1 hit, got %d", len(hits))
		} else {
			if hits[0].Message.Body != "Welcome to general! Let's deploy the settlement fix." {
				t.Errorf("Unexpected message body: %q", hits[0].Message.Body)
			}
			if hits[0].Message.User == nil || hits[0].Message.User.Username != "alice" {
				t.Errorf("Expected hydrated user, got %v", hits[0].Message.User)
			}
		}
	})

	t.Run("Filter by channel", func(t *testing.T) {
		hits, err := s.Search(ctx, u1.ID, SearchQuery{Text: "retry", ChannelID: &ch1.ID})
		if err != nil {
			t.Fatalf("Search failed: %v", err)
		}
		if len(hits) != 1 {
			t.Errorf("Expected 1 hit, got %d", len(hits))
		}
	})

	t.Run("Sanitize FTS operators", func(t *testing.T) {
		// Quotes balancing, stray symbols
		hits, err := s.Search(ctx, u1.ID, SearchQuery{Text: `settlement "fix` + "`"})
		if err != nil {
			t.Fatalf("Search with unbalanced quotes failed: %v", err)
		}
		if len(hits) != 1 {
			t.Errorf("Expected 1 hit for sanitized query, got %d", len(hits))
		}
	})
}

func TestSearchLeak(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()
	s, err := Open(ctx, tmpDir)
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer s.Close()

	if err := s.Migrate(ctx); err != nil {
		t.Fatalf("Migrate failed: %v", err)
	}

	// Create Alice and Bob
	u1, err := s.CreateUser(ctx, UserInput{
		Username:     "alice",
		Email:        "alice@example.com",
		DisplayName:  "Alice",
		PasswordHash: "password123",
		Role:         "admin",
	})
	if err != nil {
		t.Fatalf("CreateUser failed: %v", err)
	}
	u2, err := s.CreateUser(ctx, UserInput{
		Username:     "bob",
		Email:        "bob@example.com",
		DisplayName:  "Bob",
		PasswordHash: "password123",
		Role:         "member",
	})
	if err != nil {
		t.Fatalf("CreateUser failed: %v", err)
	}

	// Create private channel with ONLY Alice in it
	privCh, err := s.CreateChannel(ctx, "private", "secret", "Secret", "", u1.ID, []int64{u1.ID})
	if err != nil {
		t.Fatalf("CreateChannel failed: %v", err)
	}

	// Post message with a poison term
	_, _, err = s.CreateMessage(ctx, MessageInput{
		ChannelID: privCh.ID,
		UserID:    u1.ID,
		Body:      "This is a top-secret-poison-term only for alice.",
	})
	if err != nil {
		t.Fatalf("CreateMessage failed: %v", err)
	}

	// Search as Bob (non-member)
	hits, err := s.Search(ctx, u2.ID, SearchQuery{Text: "top-secret-poison-term"})
	if err != nil {
		t.Fatalf("Search failed: %v", err)
	}
	if len(hits) != 0 {
		t.Errorf("Security Leak: Bob (non-member) could search and find the private message! Got %d hits", len(hits))
	}

	// Search as Alice (member)
	hits, err = s.Search(ctx, u1.ID, SearchQuery{Text: "top-secret-poison-term"})
	if err != nil {
		t.Fatalf("Search failed: %v", err)
	}
	if len(hits) != 1 {
		t.Errorf("Expected Alice to find the message, got %d hits", len(hits))
	}
}

func TestGetChannelActivity(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()
	s, err := Open(ctx, tmpDir)
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer s.Close()

	if err := s.Migrate(ctx); err != nil {
		t.Fatalf("Migrate failed: %v", err)
	}

	u1, _ := s.CreateUser(ctx, UserInput{
		Username:     "alice",
		Email:        "alice@example.com",
		DisplayName:  "Alice",
		PasswordHash: "password123",
		Role:         "admin",
	})
	ch1, _ := s.GetChannelBySlug(ctx, "general")

	now := time.Now().UnixMilli()
	from := now - 24*time.Hour.Milliseconds()
	to := now

	// Setup clock indirection and make sure the values we return are what messages store.
	// Since we mock nowMillis, CreateMessage uses it for message creation timestamps.
	defer func() { nowMillis = func() int64 { return time.Now().UnixMilli() } }()

	nowMillis = func() int64 { return from + 2*time.Hour.Milliseconds() }
	_, _, err = s.CreateMessage(ctx, MessageInput{ChannelID: ch1.ID, UserID: u1.ID, Body: "Msg 1"})
	if err != nil {
		t.Fatalf("Failed to create Msg 1: %v", err)
	}

	nowMillis = func() int64 { return from + 5*time.Hour.Milliseconds() }
	_, _, err = s.CreateMessage(ctx, MessageInput{ChannelID: ch1.ID, UserID: u1.ID, Body: "Msg 2"})
	if err != nil {
		t.Fatalf("Failed to create Msg 2: %v", err)
	}

	activity, err := s.GetChannelActivity(ctx, u1.ID, ch1.ID, from, to, 24)
	if err != nil {
		t.Fatalf("GetChannelActivity failed: %v", err)
	}

	if activity.Counts[2] != 1 {
		t.Errorf("Expected 1 message in bucket 2, got %d", activity.Counts[2])
	}
	if activity.Counts[5] != 1 {
		t.Errorf("Expected 1 message in bucket 5, got %d", activity.Counts[5])
	}
}
