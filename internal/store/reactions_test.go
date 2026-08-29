package store

import (
	"context"
	"errors"
	"testing"
)

func setupReactionFixture(t *testing.T) (*Store, int64, int64, int64) {
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

	u1, err := s.CreateUser(ctx, UserInput{Username: "alice", Email: "alice@example.com", PasswordHash: "hash"})
	if err != nil {
		t.Fatal(err)
	}
	u2, err := s.CreateUser(ctx, UserInput{Username: "bob", Email: "bob@example.com", PasswordHash: "hash"})
	if err != nil {
		t.Fatal(err)
	}
	ch, err := s.CreateChannel(ctx, "public", "general-test", "General", "", u1.ID, []int64{u1.ID, u2.ID})
	if err != nil {
		t.Fatal(err)
	}
	msg, _, err := s.CreateMessage(ctx, MessageInput{ChannelID: ch.ID, UserID: u1.ID, Body: "hi"})
	if err != nil {
		t.Fatal(err)
	}
	return s, msg.ID, u1.ID, u2.ID
}

func TestReactionAddIsIdempotent(t *testing.T) {
	s, msgID, u1, _ := setupReactionFixture(t)
	ctx := context.Background()

	if err := s.AddReaction(ctx, msgID, u1, "👍"); err != nil {
		t.Fatal(err)
	}
	if err := s.AddReaction(ctx, msgID, u1, "👍"); err != nil {
		t.Fatal(err)
	}

	reactions, err := s.GetReactions(ctx, msgID)
	if err != nil {
		t.Fatal(err)
	}
	if len(reactions) != 1 || len(reactions[0].UserIDs) != 1 {
		t.Fatalf("expected one reaction with one reactor, got %+v", reactions)
	}
}

func TestReactionTwoEmojisSameUser(t *testing.T) {
	s, msgID, u1, _ := setupReactionFixture(t)
	ctx := context.Background()

	if err := s.AddReaction(ctx, msgID, u1, "👍"); err != nil {
		t.Fatal(err)
	}
	if err := s.AddReaction(ctx, msgID, u1, "🚀"); err != nil {
		t.Fatal(err)
	}

	reactions, err := s.GetReactions(ctx, msgID)
	if err != nil {
		t.Fatal(err)
	}
	if len(reactions) != 2 {
		t.Fatalf("expected two reaction groups, got %+v", reactions)
	}
}

func TestReactionRemoveNeverAddedIsNoop(t *testing.T) {
	s, msgID, u1, _ := setupReactionFixture(t)
	ctx := context.Background()

	if err := s.RemoveReaction(ctx, msgID, u1, "👍"); err != nil {
		t.Fatal(err)
	}

	reactions, err := s.GetReactions(ctx, msgID)
	if err != nil {
		t.Fatal(err)
	}
	if len(reactions) != 0 {
		t.Fatalf("expected no reactions, got %+v", reactions)
	}
}

func TestReactionAddToDeletedMessage(t *testing.T) {
	s, msgID, u1, _ := setupReactionFixture(t)
	ctx := context.Background()

	if _, err := s.DeleteMessage(ctx, msgID, u1); err != nil {
		t.Fatal(err)
	}

	err := s.AddReaction(ctx, msgID, u1, "👍")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestReactionOrderedByFirstApplied(t *testing.T) {
	s, msgID, u1, u2 := setupReactionFixture(t)
	ctx := context.Background()

	// 🚀 applied first (by u2), 👍 applied second (by u1); a later reactor on 🚀 (u1) must not
	// move the group's position.
	if err := s.AddReaction(ctx, msgID, u2, "🚀"); err != nil {
		t.Fatal(err)
	}
	if err := s.AddReaction(ctx, msgID, u1, "👍"); err != nil {
		t.Fatal(err)
	}
	if err := s.AddReaction(ctx, msgID, u1, "🚀"); err != nil {
		t.Fatal(err)
	}

	reactions, err := s.GetReactions(ctx, msgID)
	if err != nil {
		t.Fatal(err)
	}
	if len(reactions) != 2 || reactions[0].Emoji != "🚀" || reactions[1].Emoji != "👍" {
		t.Fatalf("expected 🚀 then 👍 by first-applied order, got %+v", reactions)
	}
	if len(reactions[0].UserIDs) != 2 {
		t.Fatalf("expected two reactors on 🚀, got %+v", reactions[0].UserIDs)
	}
}

func TestReactionRemoveOnlyOwnRow(t *testing.T) {
	s, msgID, u1, u2 := setupReactionFixture(t)
	ctx := context.Background()

	if err := s.AddReaction(ctx, msgID, u1, "👍"); err != nil {
		t.Fatal(err)
	}
	if err := s.AddReaction(ctx, msgID, u2, "👍"); err != nil {
		t.Fatal(err)
	}
	if err := s.RemoveReaction(ctx, msgID, u2, "👍"); err != nil {
		t.Fatal(err)
	}

	reactions, err := s.GetReactions(ctx, msgID)
	if err != nil {
		t.Fatal(err)
	}
	if len(reactions) != 1 || len(reactions[0].UserIDs) != 1 || reactions[0].UserIDs[0] != u1 {
		t.Fatalf("expected only u1's reaction to survive, got %+v", reactions)
	}
}
