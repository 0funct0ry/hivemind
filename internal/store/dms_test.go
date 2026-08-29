package store

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
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

func TestGetOrCreateGroupDM_Concurrent(t *testing.T) {
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
	base := []int64{u1.ID, u2.ID, u3.ID}

	var wg sync.WaitGroup
	const numGoroutines = 20
	results := make([]ChannelDetails, numGoroutines)
	errs := make([]error, numGoroutines)

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			ids := append([]int64(nil), base...)
			rand.Shuffle(len(ids), func(a, b int) { ids[a], ids[b] = ids[b], ids[a] })
			ch, err := s.GetOrCreateGroupDM(ctx, ids)
			results[idx] = ch
			errs[idx] = err
		}(i)
	}
	wg.Wait()

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
		if results[i].MemberCount != 3 {
			t.Errorf("goroutine %d: expected 3 members, got %d", i, results[i].MemberCount)
		}
	}

	var count int
	if err := s.reader.QueryRowContext(ctx, "SELECT COUNT(*) FROM channels WHERE kind = 'group_dm'").Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Errorf("expected exactly 1 group DM channel, got %d", count)
	}
}

func TestGetOrCreateGroupDM_TwoIDsDelegatesToDM(t *testing.T) {
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

	ch, err := s.GetOrCreateGroupDM(ctx, []int64{u1.ID, u2.ID})
	if err != nil {
		t.Fatal(err)
	}
	if ch.Kind != "dm" {
		t.Errorf("expected 2-participant group DM to delegate to kind 'dm', got %q", ch.Kind)
	}
}

func TestGetOrCreateGroupDM_TooManyParticipants(t *testing.T) {
	ctx := context.Background()
	s, err := Open(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err := s.Migrate(ctx); err != nil {
		t.Fatal(err)
	}

	ids := make([]int64, 0, 9)
	for i := 0; i < 9; i++ {
		u, _ := s.CreateUser(ctx, UserInput{Username: fmt.Sprintf("user%d", i), Email: fmt.Sprintf("u%d@example.com", i)})
		ids = append(ids, u.ID)
	}

	_, err = s.GetOrCreateGroupDM(ctx, ids)
	if !errors.Is(err, ErrTooManyParticipants) {
		t.Errorf("expected ErrTooManyParticipants, got %v", err)
	}
}

func TestComputeGroupDMName(t *testing.T) {
	members := []User{
		{ID: 1, DisplayName: "Bruce"},
		{ID: 2, DisplayName: "Hugo"},
		{ID: 3, DisplayName: "Ann"},
		{ID: 4, DisplayName: "Zoe"},
	}
	name := computeGroupDMName(members, 0)
	want := "Ann, Bruce, Hugo, +1"
	if name != want {
		t.Errorf("computeGroupDMName() = %q, want %q", name, want)
	}
}

func TestHideConversation(t *testing.T) {
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
	if _, _, err := s.CreateMessage(ctx, MessageInput{ChannelID: dm.ID, UserID: u1.ID, Body: "hi"}); err != nil {
		t.Fatal(err)
	}

	// Sanity: it shows up for alice before hiding.
	list, err := s.ListDMs(ctx, u1.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 {
		t.Fatalf("expected 1 DM before hiding, got %d", len(list))
	}

	if err := s.HideConversation(ctx, u1.ID, dm.ID); err != nil {
		t.Fatal(err)
	}

	// Hidden for alice...
	list, err = s.ListDMs(ctx, u1.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 0 {
		t.Fatalf("expected 0 DMs for alice after hiding, got %d", len(list))
	}

	// ...but bob still sees it, since hiding is per-user.
	list, err = s.ListDMs(ctx, u2.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 {
		t.Fatalf("expected bob to still see the DM, got %d", len(list))
	}

	// A new message reinstates it for everyone who hid it.
	if _, _, err := s.CreateMessage(ctx, MessageInput{ChannelID: dm.ID, UserID: u2.ID, Body: "you there?"}); err != nil {
		t.Fatal(err)
	}
	list, err = s.ListDMs(ctx, u1.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 {
		t.Fatalf("expected the DM to reappear for alice after a new message, got %d", len(list))
	}
}

// TestHideConversation_UnhiddenByResolve_DM is a regression test: reopening a previously-
// hidden 1:1 DM via GetOrCreateDM (the New Message modal's "start a conversation" path) must
// unhide it for the caller immediately, without waiting for a new message. Before the fix, a
// hidden conversation stayed excluded from ListDMs forever once "recreated" this way, leaving
// the client navigated to a channel id it could never resolve.
func TestHideConversation_UnhiddenByResolve_DM(t *testing.T) {
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
	if _, _, err := s.CreateMessage(ctx, MessageInput{ChannelID: dm.ID, UserID: u1.ID, Body: "hi"}); err != nil {
		t.Fatal(err)
	}
	if err := s.HideConversation(ctx, u1.ID, dm.ID); err != nil {
		t.Fatal(err)
	}
	list, err := s.ListDMs(ctx, u1.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 0 {
		t.Fatalf("expected 0 DMs for alice after hiding, got %d", len(list))
	}

	// Re-resolving the same DM (no new message) must unhide it for alice immediately.
	if _, err := s.GetOrCreateDM(ctx, u1.ID, u2.ID); err != nil {
		t.Fatal(err)
	}
	list, err = s.ListDMs(ctx, u1.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 {
		t.Fatalf("expected the DM to reappear for alice after resolving it again, got %d", len(list))
	}

	// Bob never hid it, so re-resolving as alice must not disturb his hidden_at (nil).
	list, err = s.ListDMs(ctx, u2.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 {
		t.Fatalf("expected bob to still see the DM, got %d", len(list))
	}
}

// TestHideConversation_UnhiddenByResolve_GroupDM mirrors the 1:1 case above for group DMs.
func TestHideConversation_UnhiddenByResolve_GroupDM(t *testing.T) {
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
	u3, _ := s.CreateUser(ctx, UserInput{Username: "carol", Email: "carol@example.com"})

	gdm, err := s.GetOrCreateGroupDM(ctx, []int64{u1.ID, u2.ID, u3.ID})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.CreateMessage(ctx, MessageInput{ChannelID: gdm.ID, UserID: u1.ID, Body: "hi all"}); err != nil {
		t.Fatal(err)
	}
	if err := s.HideConversation(ctx, u1.ID, gdm.ID); err != nil {
		t.Fatal(err)
	}
	list, err := s.ListDMs(ctx, u1.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 0 {
		t.Fatalf("expected 0 DMs for alice after hiding the group DM, got %d", len(list))
	}

	if _, err := s.GetOrCreateGroupDM(ctx, []int64{u1.ID, u2.ID, u3.ID}); err != nil {
		t.Fatal(err)
	}
	list, err = s.ListDMs(ctx, u1.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 {
		t.Fatalf("expected the group DM to reappear for alice after resolving it again, got %d", len(list))
	}
}

func TestHideConversation_NotFoundForNonMember(t *testing.T) {
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
	u3, _ := s.CreateUser(ctx, UserInput{Username: "carol", Email: "carol@example.com"})

	dm, err := s.GetOrCreateDM(ctx, u1.ID, u2.ID)
	if err != nil {
		t.Fatal(err)
	}

	if err := s.HideConversation(ctx, u3.ID, dm.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("expected ErrNotFound for non-member, got %v", err)
	}

	pub, err := s.CreateChannel(ctx, "public", "general2", "general2", "", u1.ID, []int64{u1.ID})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.HideConversation(ctx, u1.ID, pub.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("expected ErrNotFound for a non-DM channel, got %v", err)
	}
}
