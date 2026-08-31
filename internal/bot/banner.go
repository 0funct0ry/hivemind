package bot

import (
	"fmt"
	"strings"
	"time"
)

// ANSI escape codes for the startup banner. Reused verbatim from internal/chattui/render.go's
// convention of raw escape-code constants rather than a color library — no new dependency.
const (
	ansiReset  = "\x1b[0m"
	ansiBold   = "\x1b[1m"
	ansiDim    = "\x1b[2m"
	ansiCyan   = "\x1b[36m"
	ansiGreen  = "\x1b[32m"
	ansiYellow = "\x1b[33m"
)

// BannerOptions carries every setting `hivemind bot` started with, plus what it discovered, for
// RenderBanner to summarize.
type BannerOptions struct {
	ListenAddr          string
	ScriptsDir          string
	Shell               string
	ScriptTimeout       time.Duration
	DefaultResponseType string
	MaxClockSkew        time.Duration
	LogLevel            string
	LogFormat           string
	Routes              []Route
	Warnings            []string
}

// AssumedWebhookURL guesses the URL a slash command would register to reach trigger directly —
// i.e. what to paste into Settings when the hivemind server was started with
// --allow-insecure-webhooks against this same machine. It is only ever a starting guess: a real
// deployment behind a tunnel replaces the host with the tunnel's public address.
func AssumedWebhookURL(listenAddr, trigger string) string {
	host := listenAddr
	if strings.HasPrefix(host, ":") {
		host = "localhost" + host
	}
	return "http://" + host + "/hooks/" + trigger
}

// RenderBanner formats a human-facing startup summary of every setting hivemind bot is running
// with and every trigger it discovered, including each trigger's assumed webhook URL. color
// controls whether ANSI escape codes are included — callers should pass false when stdout isn't
// a terminal (e.g. piped to a log file).
func RenderBanner(opts BannerOptions, color bool) string {
	c := func(code, s string) string {
		if !color {
			return s
		}
		return code + s + ansiReset
	}

	var b strings.Builder
	fmt.Fprintln(&b)
	fmt.Fprintln(&b, c(ansiBold+ansiCyan, "hivemind bot"))
	fmt.Fprintln(&b, c(ansiDim, strings.Repeat("─", 44)))

	row := func(label string, value string) {
		fmt.Fprintf(&b, "  %-22s %s\n", c(ansiDim, label), value)
	}
	row("listen address", opts.ListenAddr)
	row("scripts directory", opts.ScriptsDir)
	row("shell", opts.Shell)
	row("script timeout", opts.ScriptTimeout.String())
	row("default response type", opts.DefaultResponseType)
	row("max clock skew", opts.MaxClockSkew.String())
	row("log level / format", opts.LogLevel+" / "+logFormatOrAuto(opts.LogFormat))

	fmt.Fprintln(&b)
	if len(opts.Routes) == 0 {
		fmt.Fprintln(&b, c(ansiYellow, "  no triggers registered — every script needs a matching <name>.secret file"))
	} else {
		fmt.Fprintf(&b, "%s\n", c(ansiBold, fmt.Sprintf("Registered triggers (%d)", len(opts.Routes))))
		maxTrigger := 0
		for _, r := range opts.Routes {
			if len(r.Trigger) > maxTrigger {
				maxTrigger = len(r.Trigger)
			}
		}
		for _, r := range opts.Routes {
			trigger := c(ansiGreen, "/"+r.Trigger)
			pad := strings.Repeat(" ", maxTrigger-len(r.Trigger))
			url := AssumedWebhookURL(opts.ListenAddr, r.Trigger)
			fmt.Fprintf(&b, "  %s%s  -> %s  %s\n", trigger, pad, c(ansiDim, url), c(ansiDim, "("+r.ScriptPath+")"))
		}
		fmt.Fprintln(&b)
		fmt.Fprintln(&b, c(ansiDim, "  Register each URL above as a slash command's webhook_url in Settings -> Bots."))
		fmt.Fprintln(&b, c(ansiDim, "  Replace \"localhost\" with your tunnel host unless the server was started with"))
		fmt.Fprintln(&b, c(ansiDim, "  --allow-insecure-webhooks — see scripts/bots/GUIDE.md."))
	}

	if len(opts.Warnings) > 0 {
		fmt.Fprintln(&b)
		fmt.Fprintf(&b, "%s\n", c(ansiYellow, fmt.Sprintf("Skipped (%d)", len(opts.Warnings))))
		for _, w := range opts.Warnings {
			fmt.Fprintf(&b, "  %s %s\n", c(ansiYellow, "!"), w)
		}
	}
	fmt.Fprintln(&b)

	return b.String()
}

func logFormatOrAuto(format string) string {
	if format == "" {
		return "auto"
	}
	return format
}
