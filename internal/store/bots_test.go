package store

import (
	"context"
	"errors"
	"net"
	"testing"
)

func newBotTestStore(t *testing.T) *Store {
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
	return s
}

func TestCreateBotMintsWorkingToken(t *testing.T) {
	ctx := context.Background()
	s := newBotTestStore(t)
	admin, err := s.CreateUser(ctx, UserInput{Username: "admin", Email: "admin@example.com", PasswordHash: "hash"})
	if err != nil {
		t.Fatal(err)
	}

	bot, token, err := s.CreateBot(ctx, BotInput{Name: "Deploy Bot", Description: "ships things"}, admin.ID)
	if err != nil {
		t.Fatal(err)
	}
	if token == "" {
		t.Fatal("expected a non-empty plaintext token")
	}
	if bot.Status != BotStatusActive {
		t.Fatalf("expected status active, got %q", bot.Status)
	}

	u, err := s.GetUserByID(ctx, bot.UserID)
	if err != nil {
		t.Fatal(err)
	}
	if !u.IsBot {
		t.Fatal("expected the created user to have is_bot=1")
	}

	authed, err := s.AuthenticateAPIToken(ctx, hashBotToken(token), nowMillis())
	if err != nil {
		t.Fatalf("bot token did not authenticate: %v", err)
	}
	if authed.ID != bot.UserID {
		t.Fatalf("authenticated as wrong user: got %d want %d", authed.ID, bot.UserID)
	}
}

func TestRegenerateBotTokenInvalidatesOld(t *testing.T) {
	ctx := context.Background()
	s := newBotTestStore(t)
	admin, err := s.CreateUser(ctx, UserInput{Username: "admin", Email: "admin@example.com", PasswordHash: "hash"})
	if err != nil {
		t.Fatal(err)
	}
	bot, oldToken, err := s.CreateBot(ctx, BotInput{Name: "Bot"}, admin.ID)
	if err != nil {
		t.Fatal(err)
	}

	newToken, err := s.RegenerateBotToken(ctx, bot.UserID)
	if err != nil {
		t.Fatal(err)
	}
	if newToken == oldToken {
		t.Fatal("expected a different token after regeneration")
	}

	if _, err := s.AuthenticateAPIToken(ctx, hashBotToken(oldToken), nowMillis()); err == nil {
		t.Fatal("expected old token to no longer authenticate")
	}
	if _, err := s.AuthenticateAPIToken(ctx, hashBotToken(newToken), nowMillis()); err != nil {
		t.Fatalf("expected new token to authenticate: %v", err)
	}
}

func TestRevokeBotKeepsPastMessagesAttributable(t *testing.T) {
	ctx := context.Background()
	s := newBotTestStore(t)
	admin, err := s.CreateUser(ctx, UserInput{Username: "admin", Email: "admin@example.com", PasswordHash: "hash"})
	if err != nil {
		t.Fatal(err)
	}
	bot, token, err := s.CreateBot(ctx, BotInput{Name: "Bot"}, admin.ID)
	if err != nil {
		t.Fatal(err)
	}

	ch, err := s.CreateChannel(ctx, "public", "bot-channel", "bot-channel", "", admin.ID, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.AddMembers(ctx, ch.ID, []int64{bot.UserID}); err != nil {
		t.Fatal(err)
	}
	msg, _, err := s.CreateMessage(ctx, MessageInput{ChannelID: ch.ID, UserID: bot.UserID, Body: "hello from bot"})
	if err != nil {
		t.Fatal(err)
	}

	if err := s.RevokeBot(ctx, bot.UserID); err != nil {
		t.Fatal(err)
	}

	got, err := s.GetBotByUserID(ctx, bot.UserID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != BotStatusRevoked {
		t.Fatalf("expected revoked status, got %q", got.Status)
	}

	// The old token must stop authenticating (bot user is deactivated).
	if _, err := s.AuthenticateAPIToken(ctx, hashBotToken(token), nowMillis()); err == nil {
		t.Fatal("expected revoked bot's token to no longer authenticate")
	}

	// The message it posted before revocation must still be readable and attributed to it.
	stillThere, err := s.GetMessage(ctx, msg.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stillThere.UserID != bot.UserID {
		t.Fatalf("expected message to remain attributed to the bot user, got %d", stillThere.UserID)
	}
}

func TestDeleteBotRequiresRevokeFirst(t *testing.T) {
	ctx := context.Background()
	s := newBotTestStore(t)
	admin, err := s.CreateUser(ctx, UserInput{Username: "admin", Email: "admin@example.com", PasswordHash: "hash"})
	if err != nil {
		t.Fatal(err)
	}
	bot, _, err := s.CreateBot(ctx, BotInput{Name: "Bot"}, admin.ID)
	if err != nil {
		t.Fatal(err)
	}

	if err := s.DeleteBot(ctx, bot.UserID); !errors.Is(err, ErrBotNotRevoked) {
		t.Fatalf("expected ErrBotNotRevoked deleting an active bot, got %v", err)
	}

	if err := s.RevokeBot(ctx, bot.UserID); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteBot(ctx, bot.UserID); err != nil {
		t.Fatalf("expected delete to succeed after revoke, got %v", err)
	}
	if _, err := s.GetBotByUserID(ctx, bot.UserID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected the bot to be gone, got %v", err)
	}

	// Idempotent: deleting an already-gone bot is a no-op success.
	if err := s.DeleteBot(ctx, bot.UserID); err != nil {
		t.Fatalf("expected idempotent delete to succeed, got %v", err)
	}
}

func TestDeleteBotRejectsWhenSlashCommandStillReferencesIt(t *testing.T) {
	ctx := context.Background()
	s := newBotTestStore(t)
	admin, err := s.CreateUser(ctx, UserInput{Username: "admin", Email: "admin@example.com", PasswordHash: "hash"})
	if err != nil {
		t.Fatal(err)
	}
	bot, _, err := s.CreateBot(ctx, BotInput{Name: "Bot"}, admin.ID)
	if err != nil {
		t.Fatal(err)
	}
	withPublicHostResolution(t, "example.com", net.ParseIP("93.184.216.34"))
	if _, _, err := s.CreateSlashCommand(ctx, SlashCommandInput{
		Trigger: "/status", BotID: bot.UserID, Description: "d", WebhookURL: "https://example.com/hook", CreatedBy: admin.ID,
	}); err != nil {
		t.Fatal(err)
	}

	if err := s.RevokeBot(ctx, bot.UserID); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteBot(ctx, bot.UserID); !errors.Is(err, ErrBotInUse) {
		t.Fatalf("expected ErrBotInUse, got %v", err)
	}
}

func TestListBots(t *testing.T) {
	ctx := context.Background()
	s := newBotTestStore(t)
	admin, err := s.CreateUser(ctx, UserInput{Username: "admin", Email: "admin@example.com", PasswordHash: "hash"})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.CreateBot(ctx, BotInput{Name: "Bot One"}, admin.ID); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.CreateBot(ctx, BotInput{Name: "Bot Two"}, admin.ID); err != nil {
		t.Fatal(err)
	}
	list, err := s.ListBots(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 2 {
		t.Fatalf("expected 2 bots, got %d", len(list))
	}
}
