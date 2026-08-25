package store

import (
	"context"
	"testing"
)

func TestMessageStoreCRUD(t *testing.T) {
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
	u1, err := s.CreateUser(ctx, UserInput{Username: "alice", Email: "alice@example.com", PasswordHash: "hash"})
	if err != nil {
		t.Fatal(err)
	}
	u2, err := s.CreateUser(ctx, UserInput{Username: "bob", Email: "bob@example.com", PasswordHash: "hash"})
	if err != nil {
		t.Fatal(err)
	}

	// Create channel
	ch, err := s.CreateChannel(ctx, "public", "general-test", "General", "General talk", u1.ID, []int64{u1.ID, u2.ID})
	if err != nil {
		t.Fatal(err)
	}

	// 1. Create a message
	m1, existed, err := s.CreateMessage(ctx, MessageInput{
		ChannelID: ch.ID,
		UserID:    u1.ID,
		Body:      "Hello world!",
	})
	if err != nil {
		t.Fatal(err)
	}
	if existed {
		t.Error("expected existed=false")
	}
	if m1.Body != "Hello world!" {
		t.Errorf("expected body 'Hello world!', got %q", m1.Body)
	}
	if m1.User == nil || m1.User.Username != "alice" {
		t.Errorf("expected hydrated user 'alice', got %+v", m1.User)
	}

	// Check channel last_message_id is updated
	chGot, _ := s.GetChannel(ctx, ch.ID)
	if chGot.LastMessageID == nil || *chGot.LastMessageID != m1.ID {
		t.Errorf("expected channel last_message_id to be %d, got %v", m1.ID, chGot.LastMessageID)
	}

	// 2. Test Idempotency with client_msg_id
	clientMsgID := "unique-id-123"
	m2, existed2, err := s.CreateMessage(ctx, MessageInput{
		ChannelID:   ch.ID,
		UserID:      u1.ID,
		Body:        "First post",
		ClientMsgID: &clientMsgID,
	})
	if err != nil {
		t.Fatal(err)
	}
	if existed2 {
		t.Error("expected first message with client_msg_id to have existed=false")
	}

	m3, existed3, err := s.CreateMessage(ctx, MessageInput{
		ChannelID:   ch.ID,
		UserID:      u1.ID,
		Body:        "First post duplicate", // Should return original body/message
		ClientMsgID: &clientMsgID,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !existed3 {
		t.Error("expected duplicate client_msg_id to have existed=true")
	}
	if m3.ID != m2.ID {
		t.Errorf("expected duplicate to return message ID %d, got %d", m2.ID, m3.ID)
	}
	if m3.Body != "First post" {
		t.Errorf("expected duplicate to return original body 'First post', got %q", m3.Body)
	}

	// 3. Test Retrieve Single Message
	mGot, err := s.GetMessage(ctx, m1.ID)
	if err != nil {
		t.Fatal(err)
	}
	if mGot.Body != m1.Body {
		t.Errorf("expected body %q, got %q", m1.Body, mGot.Body)
	}

	// 4. Test ListChannelMessages cursor pagination
	// Create several messages
	// m1 (Hello world!), m2 (First post) are already there.
	var msgs []Message
	for i := 0; i < 5; i++ {
		m, _, err := s.CreateMessage(ctx, MessageInput{
			ChannelID: ch.ID,
			UserID:    u2.ID,
			Body:      "Message sequential",
		})
		if err != nil {
			t.Fatal(err)
		}
		msgs = append(msgs, m)
	}

	// List messages (no cursors) - should return all messages in ASC order (by list reversal)
	list, err := s.ListChannelMessages(ctx, ch.ID, nil, nil, 10)
	if err != nil {
		t.Fatal(err)
	}
	// Total messages: m1, m2 (which is m3 too), and 5 sequential = 7 messages.
	if len(list) != 7 {
		t.Errorf("expected 7 messages, got %d", len(list))
	}
	// The oldest should be list[0] (which is m1), the newest should be list[6]
	if list[0].ID != m1.ID {
		t.Errorf("expected first element in list to be oldest message (ID %d), got ID %d", m1.ID, list[0].ID)
	}

	// List with `before` cursor
	// Let's get messages before the last created message (msgs[4].ID)
	beforeCursor := msgs[4].ID
	listBefore, err := s.ListChannelMessages(ctx, ch.ID, &beforeCursor, nil, 3)
	if err != nil {
		t.Fatal(err)
	}
	// Should return 3 messages before msgs[4] (i.e., msgs[3], msgs[2], msgs[1]) in ascending order:
	// msgs[1], msgs[2], msgs[3]
	if len(listBefore) != 3 {
		t.Errorf("expected 3 messages before %d, got %d", beforeCursor, len(listBefore))
	}
	if listBefore[2].ID != msgs[3].ID {
		t.Errorf("expected last message in listBefore to be msgs[3] (ID %d), got ID %d", msgs[3].ID, listBefore[2].ID)
	}

	// List with `after` cursor
	afterCursor := msgs[1].ID
	listAfter, err := s.ListChannelMessages(ctx, ch.ID, nil, &afterCursor, 2)
	if err != nil {
		t.Fatal(err)
	}
	// Should return 2 messages after msgs[1]: msgs[2], msgs[3] in ascending order.
	if len(listAfter) != 2 {
		t.Errorf("expected 2 messages after %d, got %d", afterCursor, len(listAfter))
	}
	if listAfter[0].ID != msgs[2].ID || listAfter[1].ID != msgs[3].ID {
		t.Errorf("expected msgs[2] and msgs[3], got IDs %d and %d", listAfter[0].ID, listAfter[1].ID)
	}
}
