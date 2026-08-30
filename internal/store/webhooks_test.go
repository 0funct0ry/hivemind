package store

import (
	"context"
	"database/sql"
	"strings"
	"testing"
	"time"
)

func newWebhookTestStore(t *testing.T) *Store {
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
	ResetWebhookFloodTracker()
	return s
}

func TestWebhookCreateListGetPatchRegenerateDelete(t *testing.T) {
	ctx := context.Background()
	s := newWebhookTestStore(t)

	owner, err := s.CreateUser(ctx, UserInput{Username: "owner", Email: "owner@example.com", PasswordHash: "hash"})
	if err != nil {
		t.Fatal(err)
	}
	ch, err := s.CreateChannel(ctx, "public", "deploys", "deploys", "", owner.ID, nil)
	if err != nil {
		t.Fatal(err)
	}

	wh, plain, err := s.CreateWebhook(ctx, WebhookInput{
		ChannelID: ch.ID, CreatedBy: owner.ID, Name: "Production CI", FormatPreset: "generic",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(plain, "whk_") {
		t.Fatalf("plaintext token = %q, want whk_ prefix", plain)
	}
	if wh.SecretLast4 != plain[len(plain)-4:] {
		t.Fatalf("SecretLast4 = %q, want last 4 of %q", wh.SecretLast4, plain)
	}
	if wh.Status != "active" {
		t.Fatalf("Status = %q, want active", wh.Status)
	}
	if wh.BotUserID == 0 {
		t.Fatal("expected a bot user id")
	}
	botUser, err := s.GetUserByID(ctx, wh.BotUserID)
	if err != nil {
		t.Fatal(err)
	}
	if !botUser.IsBot {
		t.Fatal("expected webhook's owning user to be is_bot=1")
	}

	// List for channel and for user.
	chList, err := s.ListWebhooksForChannel(ctx, ch.ID)
	if err != nil || len(chList) != 1 {
		t.Fatalf("ListWebhooksForChannel = %v, %v", chList, err)
	}
	userList, err := s.ListWebhooksForUser(ctx, owner.ID, false)
	if err != nil || len(userList) != 1 {
		t.Fatalf("ListWebhooksForUser = %v, %v", userList, err)
	}

	// Get
	got, err := s.GetWebhookByID(ctx, wh.ID)
	if err != nil || got.ID != wh.ID {
		t.Fatalf("GetWebhookByID = %v, %v", got, err)
	}

	// Patch
	newName := "Renamed CI"
	patched, err := s.UpdateWebhook(ctx, wh.ID, WebhookPatch{Name: &newName})
	if err != nil || patched.Name != newName {
		t.Fatalf("UpdateWebhook = %v, %v", patched, err)
	}

	// Regenerate
	regenerated, newPlain, err := s.RegenerateWebhookToken(ctx, wh.ID)
	if err != nil {
		t.Fatal(err)
	}
	if newPlain == plain {
		t.Fatal("regenerated token should differ from the original")
	}
	if regenerated.SecretLast4 != newPlain[len(newPlain)-4:] {
		t.Fatal("SecretLast4 not updated after regenerate")
	}
	// Old token must no longer authenticate.
	if _, err := s.AuthenticateWebhook(ctx, wh.ID, plain); err != ErrWebhookNotFound {
		t.Fatalf("old token AuthenticateWebhook err = %v, want ErrWebhookNotFound", err)
	}
	// New token authenticates.
	if authed, err := s.AuthenticateWebhook(ctx, wh.ID, newPlain); err != nil || authed.ID != wh.ID {
		t.Fatalf("new token AuthenticateWebhook = %v, %v", authed, err)
	}

	// Delete, idempotently.
	if err := s.DeleteWebhook(ctx, wh.ID); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteWebhook(ctx, wh.ID); err != nil {
		t.Fatalf("second delete should be a no-op, got %v", err)
	}
	if _, err := s.GetWebhookByID(ctx, wh.ID); err != ErrNotFound {
		t.Fatalf("GetWebhookByID after delete = %v, want ErrNotFound", err)
	}
}

func TestAuthenticateWebhook_WrongTokenOrDisabled(t *testing.T) {
	ctx := context.Background()
	s := newWebhookTestStore(t)

	owner, err := s.CreateUser(ctx, UserInput{Username: "owner", Email: "owner@example.com", PasswordHash: "hash"})
	if err != nil {
		t.Fatal(err)
	}
	ch, err := s.CreateChannel(ctx, "public", "deploys", "deploys", "", owner.ID, nil)
	if err != nil {
		t.Fatal(err)
	}
	wh, plain, err := s.CreateWebhook(ctx, WebhookInput{ChannelID: ch.ID, CreatedBy: owner.ID, Name: "CI"})
	if err != nil {
		t.Fatal(err)
	}

	if _, err := s.AuthenticateWebhook(ctx, wh.ID, "whk_wrongtoken"); err != ErrWebhookNotFound {
		t.Fatalf("wrong token err = %v, want ErrWebhookNotFound", err)
	}
	if _, err := s.AuthenticateWebhook(ctx, "does-not-exist", plain); err != ErrWebhookNotFound {
		t.Fatalf("wrong id err = %v, want ErrWebhookNotFound", err)
	}

	disabledStatus := WebhookStatusDisabled
	if _, err := s.UpdateWebhook(ctx, wh.ID, WebhookPatch{Status: &disabledStatus}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.AuthenticateWebhook(ctx, wh.ID, plain); err != ErrWebhookNotFound {
		t.Fatalf("disabled webhook err = %v, want ErrWebhookNotFound", err)
	}
}

func TestCreateWebhookMessage_ArchivedChannelRedirect(t *testing.T) {
	ctx := context.Background()
	s := newWebhookTestStore(t)

	admin, err := s.CreateUser(ctx, UserInput{Username: "admin", Email: "admin@example.com", PasswordHash: "hash", Role: "admin"})
	if err != nil {
		t.Fatal(err)
	}
	ch, err := s.CreateChannel(ctx, "public", "deploys", "deploys", "", admin.ID, nil)
	if err != nil {
		t.Fatal(err)
	}
	wh, _, err := s.CreateWebhook(ctx, WebhookInput{ChannelID: ch.ID, CreatedBy: admin.ID, Name: "CI", DefaultSeverity: "neutral"})
	if err != nil {
		t.Fatal(err)
	}

	if err := s.ArchiveChannel(ctx, ch.ID); err != nil {
		t.Fatal(err)
	}

	msg, err := s.CreateWebhookMessage(ctx, WebhookMessageInput{
		Webhook: wh, Title: "Build failed", Severity: "critical", Body: "details",
	})
	if err != nil {
		t.Fatal(err)
	}
	if msg.ChannelID == ch.ID {
		t.Fatal("expected message redirected away from archived channel")
	}
	redirected, err := s.GetChannel(ctx, msg.ChannelID)
	if err != nil {
		t.Fatal(err)
	}
	if redirected.Slug == nil || *redirected.Slug != "webhook-fallback" {
		t.Fatalf("expected redirect to #webhook-fallback, got %v", redirected.Slug)
	}
	if msg.Card == nil || !strings.Contains(*msg.Card, `"redirect_notice":true`) {
		t.Fatalf("expected redirect_notice:true in card, got %v", msg.Card)
	}
}

func TestCreateWebhookMessage_CriticalNotifiesChannel(t *testing.T) {
	ctx := context.Background()
	s := newWebhookTestStore(t)

	owner, err := s.CreateUser(ctx, UserInput{Username: "owner", Email: "owner@example.com", PasswordHash: "hash"})
	if err != nil {
		t.Fatal(err)
	}
	member, err := s.CreateUser(ctx, UserInput{Username: "member", Email: "member@example.com", PasswordHash: "hash"})
	if err != nil {
		t.Fatal(err)
	}
	ch, err := s.CreateChannel(ctx, "public", "deploys", "deploys", "", owner.ID, []int64{member.ID})
	if err != nil {
		t.Fatal(err)
	}
	wh, _, err := s.CreateWebhook(ctx, WebhookInput{
		ChannelID: ch.ID, CreatedBy: owner.ID, Name: "Pager", NotifyChannelOnCritical: true,
	})
	if err != nil {
		t.Fatal(err)
	}

	msg, err := s.CreateWebhookMessage(ctx, WebhookMessageInput{Webhook: wh, Title: "Outage", Severity: "critical"})
	if err != nil {
		t.Fatal(err)
	}

	mentions, err := s.GetMessageMentions(ctx, msg.ID)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, m := range mentions {
		if m.UserID == member.ID && m.Kind == "channel" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected an @channel mention for member, got %v", mentions)
	}
}

func TestWebhookFloodCollapse(t *testing.T) {
	ctx := context.Background()
	s := newWebhookTestStore(t)

	owner, err := s.CreateUser(ctx, UserInput{Username: "owner", Email: "owner@example.com", PasswordHash: "hash"})
	if err != nil {
		t.Fatal(err)
	}
	ch, err := s.CreateChannel(ctx, "public", "deploys", "deploys", "", owner.ID, nil)
	if err != nil {
		t.Fatal(err)
	}
	wh, _, err := s.CreateWebhook(ctx, WebhookInput{ChannelID: ch.ID, CreatedBy: owner.ID, Name: "CI"})
	if err != nil {
		t.Fatal(err)
	}

	now := time.Now()
	var lastMsgID int64
	messageCount := 0
	for i := 0; i < 15; i++ {
		collapse, summaryID := s.CheckWebhookFlood(wh.ID, now)
		if !collapse {
			msg, err := s.CreateWebhookMessage(ctx, WebhookMessageInput{Webhook: wh, Title: "spam"})
			if err != nil {
				t.Fatal(err)
			}
			lastMsgID = msg.ID
			messageCount++
			if err := s.NoteWebhookFloodSummary(ctx, wh.ID, msg.ID); err != nil {
				t.Fatal(err)
			}
		} else {
			if summaryID == nil || *summaryID != lastMsgID {
				t.Fatalf("summary id = %v, want %d", summaryID, lastMsgID)
			}
			if _, err := s.UpdateWebhookFloodSummary(ctx, wh.ID, WebhookCard{Title: "spam (collapsed)"}); err != nil {
				t.Fatal(err)
			}
		}
	}

	if messageCount != webhookFloodThreshold {
		t.Fatalf("messageCount = %d, want %d fresh messages before collapse", messageCount, webhookFloodThreshold)
	}

	final, err := s.GetMessage(ctx, lastMsgID)
	if err != nil {
		t.Fatal(err)
	}
	if final.Card == nil || !strings.Contains(*final.Card, "collapsed") {
		t.Fatalf("expected the summary message's card to reflect the collapse, got %v", final.Card)
	}
}

func TestOrphanWebhooksForUserAndClaim(t *testing.T) {
	ctx := context.Background()
	s := newWebhookTestStore(t)

	owner, err := s.CreateUser(ctx, UserInput{Username: "owner", Email: "owner@example.com", PasswordHash: "hash"})
	if err != nil {
		t.Fatal(err)
	}
	admin, err := s.CreateUser(ctx, UserInput{Username: "admin", Email: "admin@example.com", PasswordHash: "hash", Role: "admin"})
	if err != nil {
		t.Fatal(err)
	}
	ch, err := s.CreateChannel(ctx, "public", "deploys", "deploys", "", owner.ID, nil)
	if err != nil {
		t.Fatal(err)
	}
	active, _, err := s.CreateWebhook(ctx, WebhookInput{ChannelID: ch.ID, CreatedBy: owner.ID, Name: "Active one"})
	if err != nil {
		t.Fatal(err)
	}
	disabled, _, err := s.CreateWebhook(ctx, WebhookInput{ChannelID: ch.ID, CreatedBy: owner.ID, Name: "Disabled one"})
	if err != nil {
		t.Fatal(err)
	}
	disabledStatus := WebhookStatusDisabled
	if _, err := s.UpdateWebhook(ctx, disabled.ID, WebhookPatch{Status: &disabledStatus}); err != nil {
		t.Fatal(err)
	}

	if err := s.Tx(ctx, func(tx *sql.Tx) error {
		return s.OrphanWebhooksForUser(ctx, tx, owner.ID)
	}); err != nil {
		t.Fatal(err)
	}

	orphaned, err := s.GetWebhookByID(ctx, active.ID)
	if err != nil {
		t.Fatal(err)
	}
	if orphaned.Status != WebhookStatusOrphaned {
		t.Fatalf("Status = %q, want orphaned", orphaned.Status)
	}
	stillDisabled, err := s.GetWebhookByID(ctx, disabled.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stillDisabled.Status != WebhookStatusDisabled {
		t.Fatalf("disabled webhook Status = %q, want unchanged disabled", stillDisabled.Status)
	}

	claimed, err := s.ClaimWebhook(ctx, active.ID, admin.ID)
	if err != nil {
		t.Fatal(err)
	}
	if claimed.Status != WebhookStatusActive || claimed.CreatedBy != admin.ID {
		t.Fatalf("claimed = %+v", claimed)
	}

	if _, err := s.ClaimWebhook(ctx, disabled.ID, admin.ID); err == nil {
		t.Fatal("expected error claiming a non-orphaned webhook")
	}
}

func TestListWebhooksForUser_NonOwnerNonAdminSeesNothing(t *testing.T) {
	ctx := context.Background()
	s := newWebhookTestStore(t)

	owner, err := s.CreateUser(ctx, UserInput{Username: "owner", Email: "owner@example.com", PasswordHash: "hash"})
	if err != nil {
		t.Fatal(err)
	}
	other, err := s.CreateUser(ctx, UserInput{Username: "other", Email: "other@example.com", PasswordHash: "hash"})
	if err != nil {
		t.Fatal(err)
	}
	ch, err := s.CreateChannel(ctx, "public", "deploys", "deploys", "", owner.ID, []int64{other.ID})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.CreateWebhook(ctx, WebhookInput{ChannelID: ch.ID, CreatedBy: owner.ID, Name: "CI"}); err != nil {
		t.Fatal(err)
	}

	list, err := s.ListWebhooksForUser(ctx, other.ID, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 0 {
		t.Fatalf("non-owner, non-admin member should see zero manageable webhooks, got %d", len(list))
	}
}
