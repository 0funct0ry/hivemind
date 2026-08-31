package store

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func newSlashCommandTestFixture(t *testing.T) (*Store, User, Bot) {
	t.Helper()
	ctx := context.Background()
	s := newBotTestStore(t)
	admin, err := s.CreateUser(ctx, UserInput{Username: "admin", Email: "admin@example.com", PasswordHash: "hash"})
	if err != nil {
		t.Fatal(err)
	}
	bot, _, err := s.CreateBot(ctx, BotInput{Name: "Ops Bot"}, admin.ID)
	if err != nil {
		t.Fatal(err)
	}
	got, err := s.GetBotByUserID(ctx, bot.UserID)
	if err != nil {
		t.Fatal(err)
	}
	return s, admin, got
}

func TestCreateSlashCommandValidatesTriggerAndUniqueness(t *testing.T) {
	ctx := context.Background()
	s, admin, bot := newSlashCommandTestFixture(t)
	withPublicHostResolution(t, "example.com", net.ParseIP("93.184.216.34"))

	if _, _, err := s.CreateSlashCommand(ctx, SlashCommandInput{
		Trigger: "deploy", BotID: bot.UserID, Description: "d", WebhookURL: "https://example.com/hook", CreatedBy: admin.ID,
	}); err == nil {
		t.Fatal("expected an error for a trigger missing the leading slash")
	}
	if _, _, err := s.CreateSlashCommand(ctx, SlashCommandInput{
		Trigger: "/dep loy", BotID: bot.UserID, Description: "d", WebhookURL: "https://example.com/hook", CreatedBy: admin.ID,
	}); err == nil {
		t.Fatal("expected an error for a trigger containing whitespace")
	}

	if _, _, err := s.CreateSlashCommand(ctx, SlashCommandInput{
		Trigger: "/deploy", BotID: bot.UserID, Description: "d", WebhookURL: "https://example.com/hook", CreatedBy: admin.ID,
	}); err != nil {
		t.Fatal(err)
	}

	if _, _, err := s.CreateSlashCommand(ctx, SlashCommandInput{
		Trigger: "/DEPLOY", BotID: bot.UserID, Description: "d2", WebhookURL: "https://example.com/hook", CreatedBy: admin.ID,
	}); !errors.Is(err, ErrTriggerTaken) {
		t.Fatalf("expected ErrTriggerTaken for a case-insensitive collision, got %v", err)
	}
}

func TestCreateSlashCommandRejectsSSRFTarget(t *testing.T) {
	ctx := context.Background()
	s, admin, bot := newSlashCommandTestFixture(t)

	if _, _, err := s.CreateSlashCommand(ctx, SlashCommandInput{
		Trigger: "/deploy", BotID: bot.UserID, Description: "d", WebhookURL: "https://127.0.0.1/hook", CreatedBy: admin.ID,
	}); err == nil {
		t.Fatal("expected the SSRF guard to reject a loopback target")
	}
	if _, _, err := s.CreateSlashCommand(ctx, SlashCommandInput{
		Trigger: "/deploy", BotID: bot.UserID, Description: "d", WebhookURL: "http://example.com/hook", CreatedBy: admin.ID,
	}); err == nil {
		t.Fatal("expected the SSRF guard to reject a non-https target")
	}
}

func TestExecuteSlashCommandSuccessNonSuccessAndTimeout(t *testing.T) {
	ctx := context.Background()
	s, admin, bot := newSlashCommandTestFixture(t)

	okSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"response_type": "ephemeral", "text": "ok"})
	}))
	defer okSrv.Close()
	errSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(500)
	}))
	defer errSrv.Close()
	slowSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(200 * time.Millisecond)
		w.WriteHeader(200)
	}))
	defer slowSrv.Close()

	origClient := slashCommandHTTPClient
	slashCommandHTTPClient = &http.Client{Timeout: 50 * time.Millisecond}
	t.Cleanup(func() { slashCommandHTTPClient = origClient })

	mk := func(url string) SlashCommand {
		cmd, err := s.GetSlashCommandByID(ctx, mustCreateCmd(t, ctx, s, admin, bot, url))
		if err != nil {
			t.Fatal(err)
		}
		return cmd
	}

	ok := mk(okSrv.URL)
	res, err := s.ExecuteSlashCommand(ctx, ok, CommandExecRequest{UserID: admin.ID, Username: admin.Username, ChannelID: 1})
	if err != nil {
		t.Fatal(err)
	}
	if res.Failed || res.ResponseType != "ephemeral" || res.Text != "ok" {
		t.Fatalf("unexpected result: %+v", res)
	}

	bad := mk(errSrv.URL)
	res, err = s.ExecuteSlashCommand(ctx, bad, CommandExecRequest{UserID: admin.ID, Username: admin.Username, ChannelID: 1})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed || res.FailureKind != "error" {
		t.Fatalf("expected a non-2xx failure, got %+v", res)
	}

	slow := mk(slowSrv.URL)
	res, err = s.ExecuteSlashCommand(ctx, slow, CommandExecRequest{UserID: admin.ID, Username: admin.Username, ChannelID: 1})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed || res.FailureKind != "timeout" {
		t.Fatalf("expected a timeout failure, got %+v", res)
	}
}

// mustCreateCmd is a small helper so ExecuteSlashCommand tests can each point a fresh command at
// a distinct httptest.Server without fighting the global trigger-uniqueness constraint.
var mustCreateCmdCounter int

func mustCreateCmd(t *testing.T, ctx context.Context, s *Store, admin User, bot Bot, url string) string {
	t.Helper()
	mustCreateCmdCounter++
	withPublicHostResolution(t, "example.com", net.ParseIP("93.184.216.34"))
	trigger := "/cmd" + string(rune('a'+mustCreateCmdCounter))
	cmd, _, err := s.CreateSlashCommand(ctx, SlashCommandInput{
		Trigger: trigger, BotID: bot.UserID, Description: "d", WebhookURL: "https://example.com/hook", CreatedBy: admin.ID,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SetSlashCommandWebhookURLForTest(ctx, cmd.ID, url); err != nil {
		t.Fatal(err)
	}
	return cmd.ID
}
