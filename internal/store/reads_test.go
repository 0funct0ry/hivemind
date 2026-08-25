package store

import (
	"context"
	"fmt"
	"math/rand"
	"testing"
)

func TestMarkReadAndUnreadSummary(t *testing.T) {
	ctx := context.Background()
	s, err := Open(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	if err := s.Migrate(ctx); err != nil {
		t.Fatal(err)
	}

	// Create users
	alice, err := s.CreateUser(ctx, UserInput{Username: "alice", Email: "alice@example.com", PasswordHash: "hash"})
	if err != nil {
		t.Fatal(err)
	}
	bob, err := s.CreateUser(ctx, UserInput{Username: "bob", Email: "bob@example.com", PasswordHash: "hash"})
	if err != nil {
		t.Fatal(err)
	}

	// Create a public channel and join alice but not bob
	chPublic, err := s.CreateChannel(ctx, "public", "general-unread-test", "General", "General topic", alice.ID, []int64{alice.ID})
	if err != nil {
		t.Fatal(err)
	}

	// Create a private channel and join both
	chPrivate, err := s.CreateChannel(ctx, "private", "secret", "Secret", "Secret topic", alice.ID, []int64{alice.ID, bob.ID})
	if err != nil {
		t.Fatal(err)
	}

	// 1. Initially, check unread summary for alice (should have 0 messages)
	summary, err := s.UnreadSummary(ctx, alice.ID)
	if err != nil {
		t.Fatal(err)
	}

	// Expecting 3 visible channels for Alice: general (auto-created), general-unread-test, secret
	if len(summary) != 3 {
		t.Fatalf("expected 3 visible channels, got %d", len(summary))
	}

	for _, item := range summary {
		if item.UnreadCount != 0 || item.MentionCount != 0 {
			t.Errorf("expected 0 unreads/mentions, got unread=%d, mention=%d for channel %d", item.UnreadCount, item.MentionCount, item.ChannelID)
		}
	}

	// 2. Post a message to general (public)
	_, _, err = s.CreateMessage(ctx, MessageInput{
		ChannelID: chPublic.ID,
		UserID:    alice.ID,
		Body:      "Message 1",
	})
	if err != nil {
		t.Fatal(err)
	}

	// 3. Post a message to secret (private)
	m2, _, err := s.CreateMessage(ctx, MessageInput{
		ChannelID: chPrivate.ID,
		UserID:    alice.ID,
		Body:      "Message 2",
	})
	if err != nil {
		t.Fatal(err)
	}

	// 4. Bob's summary:
	// - chPublic (general) is visible but Bob hasn't joined. UnreadCount must be 0, joined must be false.
	// - chPrivate (secret) is visible, Bob is a member. UnreadCount must be 1 (m2), joined must be true.
	summaryBob, err := s.UnreadSummary(ctx, bob.ID)
	if err != nil {
		t.Fatal(err)
	}

	if len(summaryBob) != 3 {
		t.Fatalf("expected 3 visible channels for Bob, got %d", len(summaryBob))
	}

	var foundPublic, foundPrivate bool
	for _, item := range summaryBob {
		if item.ChannelID == chPublic.ID {
			foundPublic = true
			if item.UnreadCount != 0 {
				t.Errorf("expected 0 unread for unjoined public channel, got %d", item.UnreadCount)
			}
			if item.Joined {
				t.Error("expected joined=false for unjoined public channel")
			}
		} else if item.ChannelID == chPrivate.ID {
			foundPrivate = true
			if item.UnreadCount != 1 {
				t.Errorf("expected 1 unread for private channel, got %d", item.UnreadCount)
			}
			if !item.Joined {
				t.Error("expected joined=true for private channel")
			}
		}
	}

	if !foundPublic || !foundPrivate {
		t.Error("did not find expected channels in summary")
	}

	// 5. Test MarkRead
	// Bob marks private channel read at m2
	err = s.MarkRead(ctx, bob.ID, chPrivate.ID, m2.ID)
	if err != nil {
		t.Fatal(err)
	}

	summaryBob2, err := s.UnreadSummary(ctx, bob.ID)
	if err != nil {
		t.Fatal(err)
	}

	for _, item := range summaryBob2 {
		if item.ChannelID == chPrivate.ID {
			if item.UnreadCount != 0 {
				t.Errorf("expected 0 unreads after marking read, got %d", item.UnreadCount)
			}
			if item.LastReadMessageID != m2.ID {
				t.Errorf("expected last_read_message_id = %d, got %d", m2.ID, item.LastReadMessageID)
			}
		}
	}

	// Ensure MarkRead doesn't move backwards
	err = s.MarkRead(ctx, bob.ID, chPrivate.ID, m2.ID-1)
	if err != nil {
		t.Fatal(err)
	}

	summaryBob3, err := s.UnreadSummary(ctx, bob.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range summaryBob3 {
		if item.ChannelID == chPrivate.ID {
			if item.LastReadMessageID != m2.ID {
				t.Errorf("expected last_read_message_id to stay %d, got %d", m2.ID, item.LastReadMessageID)
			}
		}
	}
}

func BenchmarkUnreadSummary(b *testing.B) {
	ctx := context.Background()
	s, err := Open(ctx, b.TempDir())
	if err != nil {
		b.Fatal(err)
	}
	defer s.Close()

	if err := s.Migrate(ctx); err != nil {
		b.Fatal(err)
	}

	// Create users
	u, err := s.CreateUser(ctx, UserInput{Username: "user", Email: "u@example.com", PasswordHash: "hash"})
	if err != nil {
		b.Fatal(err)
	}

	// Create 20 channels, join user to all of them
	var channelIDs []int64
	for i := 0; i < 20; i++ {
		ch, err := s.CreateChannel(ctx, "public", fmt.Sprintf("ch-%d", i), fmt.Sprintf("Channel %d", i), "", u.ID, []int64{u.ID})
		if err != nil {
			b.Fatal(err)
		}
		channelIDs = append(channelIDs, ch.ID)
	}

	// Seed 50,000 messages randomly across the 20 channels
	b.Log("Seeding 50,000 messages...")
	rng := rand.New(rand.NewSource(42))
	for i := 0; i < 50000; i++ {
		chID := channelIDs[rng.Intn(len(channelIDs))]
		_, err := s.writer.ExecContext(ctx, `
			INSERT INTO messages (channel_id, user_id, body, created_at)
			VALUES (?, ?, 'Message body', ?)`,
			chID, u.ID, nowMillis())
		if err != nil {
			b.Fatal(err)
		}
	}

	// Mark read halfway in each channel to simulate active watermarks
	for _, chID := range channelIDs {
		var mid int64
		err = s.reader.QueryRowContext(ctx, "SELECT id FROM messages WHERE channel_id = ? LIMIT 1 OFFSET 1000", chID).Scan(&mid)
		if err == nil && mid > 0 {
			_ = s.MarkRead(ctx, u.ID, chID, mid)
		}
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := s.UnreadSummary(ctx, u.ID)
		if err != nil {
			b.Fatal(err)
		}
	}
}
