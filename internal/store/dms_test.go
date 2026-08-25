package store

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

func TestDMGetOrCreateAndConcurrency(t *testing.T) {
	ctx := context.Background()
	s, err := Open(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	if err := s.Migrate(ctx); err != nil {
		t.Fatal(err)
	}

	u1, _ := s.CreateUser(ctx, UserInput{Username: "alice", Email: "alice@example.com"})
	u2, _ := s.CreateUser(ctx, UserInput{Username: "bob", Email: "bob@example.com"})

	// Concurrency test: 20 goroutines calling GetOrCreateDM concurrently
	var wg sync.WaitGroup
	const numGoroutines = 20
	results := make([]ChannelDetails, numGoroutines)
	errs := make([]error, numGoroutines)

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			ch, err := s.GetOrCreateDM(ctx, u1.ID, u2.ID)
			results[idx] = ch
			errs[idx] = err
		}(i)
	}
	wg.Wait()

	// Verify all succeeded and returned the same channel ID
	var targetID int64
	for i := 0; i < numGoroutines; i++ {
		if errs[i] != nil {
			t.Fatalf("goroutine %d failed: %v", i, errs[i])
		}
		if i == 0 {
			targetID = results[i].ID
		} else if results[i].ID != targetID {
			t.Errorf("goroutine %d returned channel ID %d, want %d", i, results[i].ID, targetID)
		}
	}

	// Verify exactly one channel was created in the DB
	var count int
	err = s.reader.QueryRowContext(ctx, "SELECT COUNT(*) FROM channels WHERE kind = 'dm'").Scan(&count)
	if err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Errorf("expected exactly 1 DM channel, got %d", count)
	}
}

func TestListDMsAndFiltering(t *testing.T) {
	ctx := context.Background()
	s, err := Open(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	if err := s.Migrate(ctx); err != nil {
		t.Fatal(err)
	}

	u1, _ := s.CreateUser(ctx, UserInput{Username: "alice", Email: "alice@example.com"})
	u2, _ := s.CreateUser(ctx, UserInput{Username: "bob", Email: "bob@example.com"})
	u3, _ := s.CreateUser(ctx, UserInput{Username: "charlie", Email: "charlie@example.com"})

	// 1. Create DM between u1 and u2 (recently opened, no messages yet)
	dm1, err := s.GetOrCreateDM(ctx, u1.ID, u2.ID)
	if err != nil {
		t.Fatal(err)
	}

	// 2. Create DM between u1 and u3, freeze time back 2 hours, then open it so it's older than 1 hour with no messages
	originalClock := nowMillis
	defer func() { nowMillis = originalClock }()

	now := time.Now().UnixMilli()
	nowMillis = func() int64 { return now - 2*3600*1000 } // 2 hours ago
	dm2, err := s.GetOrCreateDM(ctx, u1.ID, u3.ID)
	if err != nil {
		t.Fatal(err)
	}

	// Restore clock to now
	nowMillis = func() int64 { return now }

	// List DMs for u1: should see dm1 (recent) but NOT dm2 (old, no messages)
	list, err := s.ListDMs(ctx, u1.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 || list[0].ID != dm1.ID {
		t.Errorf("expected only recent DM %d, got %v", dm1.ID, list)
	}
	if list[0].Peer == nil || list[0].Peer.ID != u2.ID {
		t.Errorf("expected peer to be bob (ID %d), got %+v", u2.ID, list[0].Peer)
	}

	// Now send a message to dm2 (the old DM)
	_, _, err = s.CreateMessage(ctx, MessageInput{
		ChannelID: dm2.ID,
		UserID:    u1.ID,
		Body:      "hello charlie",
	})
	if err != nil {
		t.Fatal(err)
	}

	// List DMs for u1: now both should show up! dm2 should be first since it has a message (last_message_id DESC)
	list, err = s.ListDMs(ctx, u1.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 2 {
		t.Errorf("expected both DMs, got %d", len(list))
	}
	if list[0].ID != dm2.ID {
		t.Errorf("expected dm2 first due to message, got %d", list[0].ID)
	}
}

func TestDeactivatedPeerCheck(t *testing.T) {
	ctx := context.Background()
	s, err := Open(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	if err := s.Migrate(ctx); err != nil {
		t.Fatal(err)
	}

	u1, _ := s.CreateUser(ctx, UserInput{Username: "alice", Email: "alice@example.com"})
	u2, _ := s.CreateUser(ctx, UserInput{Username: "bob", Email: "bob@example.com"})

	dm, err := s.GetOrCreateDM(ctx, u1.ID, u2.ID)
	if err != nil {
		t.Fatal(err)
	}

	// Deactivate bob
	if err := s.Deactivate(ctx, u2.ID); err != nil {
		t.Fatal(err)
	}

	// Try to post message to the DM from alice
	_, _, err = s.CreateMessage(ctx, MessageInput{
		ChannelID: dm.ID,
		UserID:    u1.ID,
		Body:      "cannot send this",
	})
	if !errors.Is(err, ErrUserDeactivated) {
		t.Errorf("expected ErrUserDeactivated, got: %v", err)
	}
}
