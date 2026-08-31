package bot

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/0funct0ry/hivemind/internal/chatclient"
)

// PostConfig configures a one-shot proactive post (SPEC.md §4.12(a): a bot posting on its own
// initiative, through the unmodified POST /channels/:id/messages path).
type PostConfig struct {
	BaseURL  string
	Insecure bool
	Token    string
	Channel  string // numeric id or #slug/name
	ThreadID string // optional
	Command  string // inline template, mutually exclusive with Script
	Script   string // path to a template file, mutually exclusive with Command
	Vars     map[string]string
	Shell    string
	Timeout  time.Duration
}

// RunPost renders cfg's command/script through the same template+exec pipeline the slash-command
// listener uses, then posts the trimmed stdout as a message using cfg.Token — reusing
// internal/chatclient exactly as scripts/bots/deploy.py does over raw HTTP, so there is one
// tested HTTP client for every hivemind bot integration in this repo.
func RunPost(ctx context.Context, cfg PostConfig) error {
	if cfg.Token == "" {
		return fmt.Errorf("a bot bearer token is required (--token or HIVEMIND_BOT_TOKEN)")
	}
	if cfg.Channel == "" {
		return fmt.Errorf("--channel is required")
	}
	if (cfg.Command == "") == (cfg.Script == "") {
		return fmt.Errorf("exactly one of --command or --script is required")
	}

	content := cfg.Command
	if cfg.Script != "" {
		raw, err := os.ReadFile(cfg.Script)
		if err != nil {
			return fmt.Errorf("read script: %w", err)
		}
		content = string(raw)
	}

	data := TemplateData{
		ChannelID: cfg.Channel,
		ThreadID:  cfg.ThreadID,
		Vars:      cfg.Vars,
		Time:      time.Now().Format(time.RFC3339),
		Hostname:  hostname(),
	}
	rendered, err := Render(content, data)
	if err != nil {
		return fmt.Errorf("render template: %w", err)
	}

	shell := cfg.Shell
	if shell == "" {
		shell = "/bin/sh"
	}
	stdout, stderr, exitCode, runErr := Run(ctx, shell, rendered, cfg.Timeout)
	if runErr != nil {
		return runErr
	}
	if exitCode != 0 {
		return fmt.Errorf("script exited %d: %s", exitCode, strings.TrimSpace(string(stderr)))
	}

	client := chatclient.New(cfg.BaseURL, cfg.Insecure)
	client.SetToken(cfg.Token)

	channelID, err := resolveChannel(ctx, client, cfg.Channel)
	if err != nil {
		return err
	}

	var threadID *string
	if cfg.ThreadID != "" {
		threadID = &cfg.ThreadID
	}

	body := strings.TrimSpace(string(stdout))
	msg, err := client.PostMessage(ctx, channelID, body, chatclient.NewClientMsgID(), threadID)
	if err != nil {
		return fmt.Errorf("post message: %w", err)
	}
	fmt.Printf("posted message %s to channel %s\n", msg.ID, channelID)
	return nil
}

// resolveChannel accepts either a numeric channel id or a #slug/name and resolves the latter
// via the same ListChannels call the web UI and `hivemind chat` both use — the messages
// endpoint itself only accepts a numeric id.
func resolveChannel(ctx context.Context, client *chatclient.Client, channel string) (string, error) {
	if _, err := strconv.ParseInt(channel, 10, 64); err == nil {
		return channel, nil
	}
	slug := strings.TrimPrefix(channel, "#")
	channels, err := client.ListChannels(ctx)
	if err != nil {
		return "", fmt.Errorf("list channels: %w", err)
	}
	for _, c := range channels {
		if (c.Slug != nil && *c.Slug == slug) || c.Name == channel {
			return c.ID, nil
		}
	}
	return "", fmt.Errorf("no channel found matching %q", channel)
}
