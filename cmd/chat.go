package cmd

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/0funct0ry/hivemind/internal/chatclient"
	"github.com/0funct0ry/hivemind/internal/chattui"
	"github.com/spf13/cobra"
)

// chatCmd opens the end-user terminal chat client — a pure REST+WebSocket client, entirely
// separate from the admin shellCmd (SPEC.md §7.7).
var chatCmd = &cobra.Command{
	Use:   "chat",
	Short: "Open the hivemind terminal chat client",
	Long: `Open an interactive terminal chat client against a hivemind server, over the same
REST + WebSocket surface the web UI uses.`,
	RunE: runChat,
}

func init() {
	rootCmd.AddCommand(chatCmd)
	addChatFlags(chatCmd)
}

// addChatFlags registers hivemind chat's flags. chat is long-running/interactive like shell,
// so it gets --config but not --log-level/--log-format (SPEC.md §2.4's flag-scoping rule).
func addChatFlags(cmd *cobra.Command) {
	cmd.Flags().String("host", "localhost", "server host")
	cmd.Flags().Int("port", 8080, "server port")
	cmd.Flags().Bool("insecure", false, "skip TLS certificate verification")
	cmd.Flags().String("token", "", "bearer token (bypasses the cached session)")
	cmd.Flags().String("config", "", "path to config file")
}

func runChat(cmd *cobra.Command, args []string) error {
	host, err := cmd.Flags().GetString("host")
	if err != nil {
		return err
	}
	port, err := cmd.Flags().GetInt("port")
	if err != nil {
		return err
	}
	insecure, err := cmd.Flags().GetBool("insecure")
	if err != nil {
		return err
	}
	tokenFlag, err := cmd.Flags().GetString("token")
	if err != nil {
		return err
	}

	// hivemind serve defaults to plain HTTP; --insecure only ever governs TLS certificate
	// verification for deployments that terminate TLS themselves, not the scheme chosen
	// here. A future --tls flag could select "https" explicitly if needed.
	baseURL := fmt.Sprintf("http://%s:%s", host, strconv.Itoa(port))

	client := chatclient.New(baseURL, insecure)

	ctx := cmd.Context()
	if ctx == nil {
		ctx = context.Background()
	}

	var bypassCache bool
	token := tokenFlag
	if token != "" {
		bypassCache = true
	} else if sess, ok, err := chatclient.LoadSession(host); err == nil && ok {
		token = sess.Token
	} else if err != nil {
		return err
	}

	if token == "" {
		username, err := promptLine(cmd, "Username: ")
		if err != nil {
			return err
		}
		password, err := readPassword("Password: ")
		if err != nil {
			return err
		}
		cookie, err := client.Login(ctx, username, password)
		if err != nil {
			return fmt.Errorf("login: %w", err)
		}
		token, err = client.IssueToken(ctx, cookie, "chat-cli", 720*time.Hour)
		if err != nil {
			return fmt.Errorf("issue token: %w", err)
		}
		if !bypassCache {
			if err := chatclient.SaveSession(&chatclient.Session{
				Host:      host,
				Token:     token,
				ExpiresAt: time.Now().Add(720 * time.Hour).UnixMilli(),
			}); err != nil {
				return fmt.Errorf("save session: %w", err)
			}
		}
	}
	client.SetToken(token)

	me, err := client.Me(ctx)
	if err != nil {
		return fmt.Errorf("fetch current user: %w", err)
	}

	historyPath, err := chattui.DefaultHistoryPath()
	if err != nil {
		return err
	}

	return chattui.Run(ctx, client, me, host, baseURL, insecure, historyPath)
}

// promptLine reads one line of plain (non-secret) input.
func promptLine(cmd *cobra.Command, prompt string) (string, error) {
	fmt.Fprint(cmd.ErrOrStderr(), prompt)
	var line string
	_, err := fmt.Scanln(&line)
	return line, err
}
