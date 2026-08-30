package chattui

import (
	"context"
	"strings"
	"testing"

	"github.com/0funct0ry/hivemind/internal/chatclient"
)

func TestHelpListsEveryCommand(t *testing.T) {
	app := NewApp(chatclient.New("http://example.invalid", false), chatclient.User{})
	out := app.Help()
	for _, c := range Commands {
		if !strings.Contains(out, c.Usage) {
			t.Errorf("Help() output missing usage for %q:\n%s", c.Name, out)
		}
		if !strings.Contains(out, c.Help) {
			t.Errorf("Help() output missing help text for %q:\n%s", c.Name, out)
		}
	}
}

func TestDispatchHelpCommand(t *testing.T) {
	app := NewApp(chatclient.New("http://example.invalid", false), chatclient.User{})
	out, err := app.Dispatch(context.Background(), ".help")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, ".quit") {
		t.Errorf(".help output missing .quit usage:\n%s", out)
	}
}

// TestDispatchRecognizesEveryRegisteredCommand guards against Commands and Dispatch's switch
// drifting apart — every name in Commands must be handled, not fall through to "unknown
// command".
func TestDispatchRecognizesEveryRegisteredCommand(t *testing.T) {
	app := NewApp(chatclient.New("http://example.invalid", false), chatclient.User{})
	for _, c := range Commands {
		out, err := app.Dispatch(context.Background(), c.Name)
		if c.Name == ".quit" {
			if err != ErrQuit {
				t.Errorf("Dispatch(%q) error = %v, want ErrQuit", c.Name, err)
			}
			continue
		}
		if strings.Contains(out, "unknown command") {
			t.Errorf("Dispatch(%q) treated as unknown: %q", c.Name, out)
		}
	}
}

func TestLeaveClosesViewWithoutServerCall(t *testing.T) {
	app := NewApp(chatclient.New("http://example.invalid", false), chatclient.User{})
	if out, err := app.Leave(); err != nil || !strings.Contains(out, "no conversation is open") {
		t.Fatalf("Leave() with no view = (%q, %v), want a no-op message", out, err)
	}

	app.view = &viewState{channel: chatclient.Channel{ID: "42"}, displayName: "#general"}
	out, err := app.Leave()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "#general") {
		t.Errorf("Leave() message missing channel name: %q", out)
	}
	if app.view != nil {
		t.Fatal("Leave() did not clear the view")
	}
	if app.HeaderLine() != "" {
		t.Errorf("HeaderLine() after Leave() = %q, want empty", app.HeaderLine())
	}
}

func TestDispatchUnknownCommandMentionsHelp(t *testing.T) {
	app := NewApp(chatclient.New("http://example.invalid", false), chatclient.User{})
	out, err := app.Dispatch(context.Background(), ".bogus")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, ".help") {
		t.Errorf("unknown-command message should point at .help, got %q", out)
	}
}
