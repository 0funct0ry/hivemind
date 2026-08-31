package bot

import (
	"strings"
	"testing"
	"time"
)

func TestAssumedWebhookURL(t *testing.T) {
	if got := AssumedWebhookURL(":8091", "echo"); got != "http://localhost:8091/hooks/echo" {
		t.Fatalf("got %q", got)
	}
	if got := AssumedWebhookURL("0.0.0.0:9000", "status"); got != "http://0.0.0.0:9000/hooks/status" {
		t.Fatalf("got %q", got)
	}
}

func TestRenderBannerPlainContainsEveryConfiguredValue(t *testing.T) {
	opts := BannerOptions{
		ListenAddr:          ":8091",
		ScriptsDir:          "./scripts/bots/commands",
		Shell:               "/bin/sh",
		ScriptTimeout:       10 * time.Second,
		DefaultResponseType: "ephemeral",
		MaxClockSkew:        5 * time.Minute,
		LogLevel:            "info",
		LogFormat:           "",
		Routes: []Route{
			{Trigger: "echo", ScriptPath: "scripts/bots/commands/echo"},
			{Trigger: "announce", ScriptPath: "scripts/bots/commands/announce"},
		},
		Warnings: []string{`skipping "slow": no slow.secret file found`},
	}

	out := RenderBanner(opts, false)

	for _, want := range []string{
		":8091", "./scripts/bots/commands", "/bin/sh", "10s", "ephemeral", "5m0s", "info / auto",
		"/echo", "http://localhost:8091/hooks/echo", "scripts/bots/commands/echo",
		"/announce", "http://localhost:8091/hooks/announce",
		"Registered triggers (2)",
		`skipping "slow": no slow.secret file found`,
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("banner missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "\x1b[") {
		t.Fatalf("expected no ANSI escapes when color=false:\n%s", out)
	}
}

func TestRenderBannerColorAddsEscapeCodes(t *testing.T) {
	out := RenderBanner(BannerOptions{ListenAddr: ":8091", Routes: []Route{{Trigger: "echo"}}}, true)
	if !strings.Contains(out, "\x1b[") {
		t.Fatalf("expected ANSI escapes when color=true:\n%s", out)
	}
}

func TestRenderBannerNoRoutesWarnsInsteadOfListing(t *testing.T) {
	out := RenderBanner(BannerOptions{ListenAddr: ":8091"}, false)
	if !strings.Contains(out, "no triggers registered") {
		t.Fatalf("expected a no-triggers warning, got:\n%s", out)
	}
}
