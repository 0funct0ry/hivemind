package store

import (
	"context"
	"database/sql"
	"errors"
	"strings"
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

func TestQueryPlan(t *testing.T) {
	ctx := context.Background()
	s, err := Open(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err := s.Migrate(ctx); err != nil {
		t.Fatal(err)
	}

	query := `EXPLAIN QUERY PLAN
		SELECT id, channel_id, user_id, thread_id, body, client_msg_id, reply_count, last_reply_id, has_attachments, broadcast, edited_at, deleted_at, created_at
		FROM messages
		WHERE channel_id = ? AND (thread_id IS NULL OR broadcast = 1)
		ORDER BY id DESC`
	rows, err := s.reader.QueryContext(ctx, query, 1)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()

	var selectid, order, from int
	var detail string
	var usedIndex bool
	for rows.Next() {
		if err := rows.Scan(&selectid, &order, &from, &detail); err != nil {
			t.Fatal(err)
		}
		t.Logf("QUERY PLAN: %s", detail)
		if strings.Contains(detail, "USING INDEX idx_msg_channel_root") || strings.Contains(detail, "USING COVERING INDEX idx_msg_channel_root") {
			usedIndex = true
		}
	}
	if !usedIndex {
		t.Error("expected query plan to use idx_msg_channel_root")
	}
}

func TestThreads(t *testing.T) {
	ctx := context.Background()
	s, err := Open(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err := s.Migrate(ctx); err != nil {
		t.Fatal(err)
	}

	u1, err := s.CreateUser(ctx, UserInput{Username: "alice", Email: "alice@example.com", PasswordHash: "hash"})
	if err != nil {
		t.Fatal(err)
	}
	ch1, err := s.CreateChannel(ctx, "public", "general-threads", "General", "General talk", u1.ID, []int64{u1.ID})
	if err != nil {
		t.Fatal(err)
	}
	ch2, err := s.CreateChannel(ctx, "public", "random", "Random", "Random talk", u1.ID, []int64{u1.ID})
	if err != nil {
		t.Fatal(err)
	}

	root, _, err := s.CreateMessage(ctx, MessageInput{
		ChannelID: ch1.ID,
		UserID:    u1.ID,
		Body:      "Root thread message",
	})
	if err != nil {
		t.Fatal(err)
	}

	reply1, _, err := s.CreateMessage(ctx, MessageInput{
		ChannelID: ch1.ID,
		UserID:    u1.ID,
		Body:      "Reply 1",
		ThreadID:  &root.ID,
	})
	if err != nil {
		t.Fatal(err)
	}

	if reply1.ThreadID == nil || *reply1.ThreadID != root.ID {
		t.Fatalf("expected thread_id to be %d, got %v", root.ID, reply1.ThreadID)
	}

	rootGot, _ := s.GetMessage(ctx, root.ID)
	if rootGot.ReplyCount != 1 || rootGot.LastReplyID == nil || *rootGot.LastReplyID != reply1.ID {
		t.Errorf("expected root reply_count=1, last_reply_id=%d, got count=%d, last=%v", reply1.ID, rootGot.ReplyCount, rootGot.LastReplyID)
	}

	reply2, _, err := s.CreateMessage(ctx, MessageInput{
		ChannelID: ch1.ID,
		UserID:    u1.ID,
		Body:      "Reply 2 (depth 2 coercion test)",
		ThreadID:  &reply1.ID,
	})
	if err != nil {
		t.Fatal(err)
	}
	if reply2.ThreadID == nil || *reply2.ThreadID != root.ID {
		t.Errorf("expected reply2 thread_id coerced to root ID %d, got %v", root.ID, reply2.ThreadID)
	}

	rootGot2, _ := s.GetMessage(ctx, root.ID)
	if rootGot2.ReplyCount != 2 || rootGot2.LastReplyID == nil || *rootGot2.LastReplyID != reply2.ID {
		t.Errorf("expected root reply_count=2, last_reply_id=%d, got count=%d, last=%v", reply2.ID, rootGot2.ReplyCount, rootGot2.LastReplyID)
	}

	_, _, err = s.CreateMessage(ctx, MessageInput{
		ChannelID: ch2.ID,
		UserID:    u1.ID,
		Body:      "Reply in wrong channel",
		ThreadID:  &root.ID,
	})
	if !errors.Is(err, ErrThreadChannelMismatch) {
		t.Errorf("expected ErrThreadChannelMismatch, got %v", err)
	}

	err = s.Tx(ctx, func(tx *sql.Tx) error {
		now := nowMillis()
		_, err := tx.ExecContext(ctx, "UPDATE messages SET deleted_at = ? WHERE id = ?", now, root.ID)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}

	_, _, err = s.CreateMessage(ctx, MessageInput{
		ChannelID: ch1.ID,
		UserID:    u1.ID,
		Body:      "Reply to deleted root",
		ThreadID:  &root.ID,
	})
	if !errors.Is(err, ErrThreadDeleted) {
		t.Errorf("expected ErrThreadDeleted, got %v", err)
	}

	err = s.Tx(ctx, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, "UPDATE messages SET deleted_at = NULL WHERE id = ?", root.ID)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}

	repliesList, err := s.ListReplies(ctx, root.ID, nil, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(repliesList) != 3 {
		t.Fatalf("expected 3 items in ListReplies, got %d", len(repliesList))
	}
	if repliesList[0].ID != root.ID || repliesList[1].ID != reply1.ID || repliesList[2].ID != reply2.ID {
		t.Errorf("unexpected list order: %v, %v, %v", repliesList[0].ID, repliesList[1].ID, repliesList[2].ID)
	}

	afterID := reply1.ID
	repliesListAfter, err := s.ListReplies(ctx, root.ID, &afterID, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(repliesListAfter) != 1 || repliesListAfter[0].ID != reply2.ID {
		t.Errorf("expected only reply2, got %d items, first ID %d", len(repliesListAfter), repliesListAfter[0].ID)
	}

	broadcastReply, _, err := s.CreateMessage(ctx, MessageInput{
		ChannelID: ch1.ID,
		UserID:    u1.ID,
		Body:      "Broadcast reply",
		ThreadID:  &root.ID,
		Broadcast: true,
	})
	if err != nil {
		t.Fatal(err)
	}

	channelMsgs, err := s.ListChannelMessages(ctx, ch1.ID, nil, nil, 10)
	if err != nil {
		t.Fatal(err)
	}

	hasRoot := false
	hasBroadcast := false
	hasReply1 := false
	for _, m := range channelMsgs {
		if m.ID == root.ID {
			hasRoot = true
		}
		if m.ID == broadcastReply.ID {
			hasBroadcast = true
		}
		if m.ID == reply1.ID {
			hasReply1 = true
		}
	}
	if !hasRoot || !hasBroadcast || hasReply1 {
		t.Errorf("unexpected channel messages list (want root and broadcast, no reply1): root=%v, broadcast=%v, reply1=%v", hasRoot, hasBroadcast, hasReply1)
	}
}
