package cmd

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/0funct0ry/hivemind/internal/bot"
	"github.com/0funct0ry/hivemind/internal/logging"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

// botCmd is `hivemind bot`, a long-running reference bot that answers slash-command webhook
// calls by executing a Go-template-rendered shell command per trigger. It exists to exercise
// the Bot SDK & Slash Commands feature (SPEC.md §4.12) end-to-end without a third-party mock
// service — see scripts/bots/GUIDE.md for a full setup walkthrough.
var botCmd = &cobra.Command{
	Use:   "bot",
	Short: "Run a generic scriptable bot that answers hivemind slash commands",
	Long: `Run a long-running bot that answers slash-command webhook calls: for every executable
file found in --scripts-dir with a matching "<name>.secret" file, it registers a
POST /hooks/<name> route, verifies each call's X-Hivemind-Signature against that secret,
Go-template-renders the script with the request's user/channel/args, executes it, and maps the
result to the {response_type, text, attachments} contract slash commands expect.

Use "hivemind bot post" to post a message proactively as a bot, using its own bearer token,
instead of answering a slash command.`,
	RunE: runBotServe,
}

// botPostCmd is `hivemind bot post`.
var botPostCmd = &cobra.Command{
	Use:   "post",
	Short: "Post a one-shot message to a channel as a bot",
	Long: `Render a Go template (inline via --command or from a file via --script), execute it,
and post the trimmed output as a message to --channel using the bot's own bearer token — the
same "bot posts on its own initiative" path scripts/bots/deploy.py demonstrates in Python.`,
	RunE: runBotPost,
}

func init() {
	rootCmd.AddCommand(botCmd)
	botCmd.AddCommand(botPostCmd)
	addBotServeFlags(botCmd)
	addBotPostFlags(botPostCmd)
}

// addBotServeFlags registers hivemind bot's flags. Hand-rolled rather than reusing
// addConfigFlags — this is a client tool pointed at a hivemind server, not the server itself.
func addBotServeFlags(cmd *cobra.Command) {
	cmd.Flags().String("listen-addr", ":8091", "address the slash-command webhook listener binds")
	cmd.Flags().String("scripts-dir", "./scripts/bots/commands", "directory of trigger scripts (one file per trigger, plus a matching <name>.secret)")
	cmd.Flags().String("shell", "/bin/sh", `interpreter scripts run under, invoked as "<shell> -c <rendered script>"`)
	cmd.Flags().Duration("script-timeout", 10*time.Second, "hard kill timeout per script execution")
	cmd.Flags().String("default-response-type", bot.ResponseTypeEphemeral, "response_type used when a script's stdout isn't a recognized JSON response (ephemeral or in_channel)")
	cmd.Flags().Duration("max-clock-skew", 5*time.Minute, "reject calls whose X-Hivemind-Timestamp is older/newer than this; 0 disables the check")
	cmd.Flags().String("log-level", "info", "log level (debug, info, warn, error)")
	cmd.Flags().String("log-format", "", "log format (text, json); auto-detected when empty")
}

func addBotPostFlags(cmd *cobra.Command) {
	cmd.Flags().String("base-url", "http://localhost:8080", "hivemind server base URL")
	cmd.Flags().Bool("insecure", false, "skip TLS certificate verification")
	cmd.Flags().String("token", os.Getenv("HIVEMIND_BOT_TOKEN"), "the bot's bearer token (or set HIVEMIND_BOT_TOKEN)")
	cmd.Flags().String("channel", "", "numeric channel id, or a #slug/name (required)")
	cmd.Flags().String("thread-id", "", "optional numeric id of a root message to reply into")
	cmd.Flags().String("command", "", "inline Go-template command to render and execute (mutually exclusive with --script)")
	cmd.Flags().String("script", "", "path to a Go-template script file to render and execute (mutually exclusive with --command)")
	cmd.Flags().StringArray("var", nil, "key=value pair made available to the template as .Vars.key (repeatable)")
	cmd.Flags().String("shell", "/bin/sh", `interpreter the command/script runs under, invoked as "<shell> -c <rendered>"`)
	cmd.Flags().Duration("timeout", 10*time.Second, "hard kill timeout for the executed command")
}

func runBotServe(cmd *cobra.Command, args []string) error {
	scriptsDir, _ := cmd.Flags().GetString("scripts-dir")
	listenAddr, _ := cmd.Flags().GetString("listen-addr")
	shell, _ := cmd.Flags().GetString("shell")
	scriptTimeout, _ := cmd.Flags().GetDuration("script-timeout")
	defaultResponseType, _ := cmd.Flags().GetString("default-response-type")
	maxClockSkew, _ := cmd.Flags().GetDuration("max-clock-skew")
	logLevel, _ := cmd.Flags().GetString("log-level")
	logFormat, _ := cmd.Flags().GetString("log-format")

	if !bot.ValidResponseType(defaultResponseType) {
		return fmt.Errorf("--default-response-type must be %q or %q", bot.ResponseTypeEphemeral, bot.ResponseTypeInChannel)
	}

	logger := logging.New(logLevel, logFormat)

	routes, warnings, err := bot.Discover(scriptsDir)
	if err != nil {
		return fmt.Errorf("discover scripts: %w", err)
	}
	for _, w := range warnings {
		logger.Warn(w)
	}

	fmt.Fprint(os.Stdout, bot.RenderBanner(bot.BannerOptions{
		ListenAddr:          listenAddr,
		ScriptsDir:          scriptsDir,
		Shell:               shell,
		ScriptTimeout:       scriptTimeout,
		DefaultResponseType: defaultResponseType,
		MaxClockSkew:        maxClockSkew,
		LogLevel:            logLevel,
		LogFormat:           logFormat,
		Routes:              routes,
		Warnings:            warnings,
	}, term.IsTerminal(int(os.Stdout.Fd()))))

	if len(routes) == 0 {
		return fmt.Errorf("no usable triggers found in %q — every script needs a matching <name>.secret file (see scripts/bots/GUIDE.md)", scriptsDir)
	}

	srv := bot.NewServer(routes, bot.Options{
		Shell:               shell,
		ScriptTimeout:       scriptTimeout,
		DefaultResponseType: defaultResponseType,
		MaxClockSkew:        maxClockSkew,
		Logger:              logger,
	})

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	httpServer := &http.Server{Addr: listenAddr, Handler: srv.Handler()}
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = httpServer.Shutdown(shutdownCtx)
	}()

	logger.Info("hivemind bot listening", "addr", listenAddr, "scripts_dir", scriptsDir)
	if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		return fmt.Errorf("serve HTTP: %w", err)
	}
	return nil
}

func runBotPost(cmd *cobra.Command, args []string) error {
	baseURL, _ := cmd.Flags().GetString("base-url")
	insecure, _ := cmd.Flags().GetBool("insecure")
	token, _ := cmd.Flags().GetString("token")
	channel, _ := cmd.Flags().GetString("channel")
	threadID, _ := cmd.Flags().GetString("thread-id")
	command, _ := cmd.Flags().GetString("command")
	script, _ := cmd.Flags().GetString("script")
	vars, _ := cmd.Flags().GetStringArray("var")
	shell, _ := cmd.Flags().GetString("shell")
	timeout, _ := cmd.Flags().GetDuration("timeout")

	varMap, err := parseVars(vars)
	if err != nil {
		return err
	}

	ctx := cmd.Context()
	if ctx == nil {
		ctx = context.Background()
	}

	return bot.RunPost(ctx, bot.PostConfig{
		BaseURL:  baseURL,
		Insecure: insecure,
		Token:    token,
		Channel:  channel,
		ThreadID: threadID,
		Command:  command,
		Script:   script,
		Vars:     varMap,
		Shell:    shell,
		Timeout:  timeout,
	})
}

func parseVars(pairs []string) (map[string]string, error) {
	out := make(map[string]string, len(pairs))
	for _, p := range pairs {
		key, value, ok := strings.Cut(p, "=")
		if !ok {
			return nil, fmt.Errorf("--var must be key=value, got %q", p)
		}
		out[key] = value
	}
	return out, nil
}
