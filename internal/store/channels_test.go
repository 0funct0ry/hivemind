package store

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestChannelCRUDAndMembers(t *testing.T) {
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

	// 1. Create Channel
	ch1, err := s.CreateChannel(ctx, "public", "discussion", "Discussion", "Discussion topic", u1.ID, []int64{u2.ID})
	if err != nil {
		t.Fatal(err)
	}
	if ch1.Name != "Discussion" || *ch1.Slug != "discussion" || ch1.Kind != "public" {
		t.Errorf("unexpected channel properties: %+v", ch1)
	}

	// Creator should be member (role = owner), u2 should be member (role = member)
	mem1, err := s.IsMember(ctx, ch1.ID, u1.ID)
	if err != nil || !mem1 {
		t.Errorf("u1 should be a member, err: %v", err)
	}
	mem2, err := s.IsMember(ctx, ch1.ID, u2.ID)
	if err != nil || !mem2 {
		t.Errorf("u2 should be a member, err: %v", err)
	}

	// 2. Fetch Channel
	chGot, err := s.GetChannel(ctx, ch1.ID)
	if err != nil {
		t.Fatal(err)
	}
	if chGot.Name != ch1.Name {
		t.Errorf("got name %q, want %q", chGot.Name, ch1.Name)
	}

	chGotSlug, err := s.GetChannelBySlug(ctx, "DISCUSSION")
	if err != nil {
		t.Fatal(err)
	}
	if chGotSlug.ID != ch1.ID {
		t.Errorf("got ID %d, want %d", chGotSlug.ID, ch1.ID)
	}

	// 3. Update Channel
	err = s.UpdateChannel(ctx, ch1.ID, "General Discussion", "New Topic")
	if err != nil {
		t.Fatal(err)
	}
	chUpdated, _ := s.GetChannel(ctx, ch1.ID)
	if chUpdated.Name != "General Discussion" || chUpdated.Topic != "New Topic" {
		t.Errorf("update failed: %+v", chUpdated)
	}

	// 4. Archive & Unarchive
	err = s.ArchiveChannel(ctx, ch1.ID)
	if err != nil {
		t.Fatal(err)
	}
	chArchived, _ := s.GetChannel(ctx, ch1.ID)
	if chArchived.ArchivedAt == nil {
		t.Error("expected channel to be archived")
	}

	err = s.UnarchiveChannel(ctx, ch1.ID)
	if err != nil {
		t.Fatal(err)
	}
	chUnarchived, _ := s.GetChannel(ctx, ch1.ID)
	if chUnarchived.ArchivedAt != nil {
		t.Error("expected channel to be unarchived")
	}

	// 5. Remove member sole owner rule
	err = s.RemoveMember(ctx, ch1.ID, u1.ID)
	if err == nil || !strings.Contains(err.Error(), "sole owner") {
		t.Errorf("expected sole owner removal to fail, got err: %v", err)
	}

	// Add u2 as owner first
	_, err = s.writer.ExecContext(ctx, "UPDATE channel_members SET role = 'owner' WHERE channel_id = ? AND user_id = ?", ch1.ID, u2.ID)
	if err != nil {
		t.Fatal(err)
	}

	err = s.RemoveMember(ctx, ch1.ID, u1.ID)
	if err != nil {
		t.Errorf("expected removal to succeed now that u2 is owner, got: %v", err)
	}

	isMem, err := s.IsMember(ctx, ch1.ID, u1.ID)
	if err != nil || isMem {
		t.Errorf("u1 should not be member, isMem: %v, err: %v", isMem, err)
	}
}

func TestListVisibleChannelsAndAccess(t *testing.T) {
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
	u3, _ := s.CreateUser(ctx, UserInput{Username: "admin", Email: "admin@example.com", Role: "admin"})

	// Public channel
	pub, _ := s.CreateChannel(ctx, "public", "pub", "Public", "", u1.ID, []int64{})
	// Private channel (u1 is creator/owner, u2 is not member)
	priv, _ := s.CreateChannel(ctx, "private", "priv", "Private", "", u1.ID, []int64{})
	// DM between u1 and u2
	dm, _ := s.CreateChannel(ctx, "dm", "", "", "", u1.ID, []int64{u1.ID, u2.ID})

	// Access Checks for u1 (member of all)
	aPub1, _ := s.CanAccessChannel(ctx, u1.ID, pub.ID)
	if !aPub1.CanRead || !aPub1.CanPost {
		t.Error("u1 should read/post in pub")
	}
	aPriv1, _ := s.CanAccessChannel(ctx, u1.ID, priv.ID)
	if !aPriv1.CanRead || !aPriv1.CanPost || !aPriv1.IsOwner {
		t.Error("u1 should be owner of priv")
	}
	aDm1, _ := s.CanAccessChannel(ctx, u1.ID, dm.ID)
	if !aDm1.CanRead || !aDm1.CanPost {
		t.Error("u1 should access DM")
	}

	// Access Checks for u2 (not in priv, in DM)
	aPub2, _ := s.CanAccessChannel(ctx, u2.ID, pub.ID)
	if !aPub2.CanRead || aPub2.CanPost {
		t.Error("u2 should read pub but not post (not member yet)")
	}
	_, err = s.CanAccessChannel(ctx, u2.ID, priv.ID)
	if err == nil || !errors.Is(err, ErrNotFound) {
		t.Errorf("u2 should get not found on priv, got: %v", err)
	}

	// Access Checks for u3 (admin, not member)
	aPrivAdmin, _ := s.CanAccessChannel(ctx, u3.ID, priv.ID)
	if aPrivAdmin.CanRead || aPrivAdmin.CanPost {
		t.Error("admin should not read/post in private channel they aren't member of")
	}
	// But it did not return ErrNotFound, meaning they can archive/administer it.
	_, err = s.CanAccessChannel(ctx, u3.ID, dm.ID)
	if err == nil || !errors.Is(err, ErrNotFound) {
		t.Errorf("admin should get not found on DM, got: %v", err)
	}

	// List Visible Channels for u2
	list2, err := s.ListVisibleChannels(ctx, u2.ID)
	if err != nil {
		t.Fatal(err)
	}
	// Should see public (pub) and DM, but NOT private (priv)
	hasPub := false
	hasPriv := false
	hasDM := false
	for _, c := range list2 {
		switch c.Kind {
		case "public":
			if c.ID == pub.ID {
				hasPub = true
			}
		case "private":
			if c.ID == priv.ID {
				hasPriv = true
			}
		case "dm":
			if c.ID == dm.ID {
				hasDM = true
			}
		}
	}
	if !hasPub {
		t.Error("u2 should see public channel")
	}
	if hasPriv {
		t.Error("u2 should NOT see private channel")
	}
	if !hasDM {
		t.Error("u2 should see DM channel")
	}
}
