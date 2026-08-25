package cmd

import (
	"fmt"

	"github.com/0funct0ry/hivemind/internal/config"
	"github.com/0funct0ry/hivemind/internal/logging"
	"github.com/spf13/cobra"
)

// serveCmd starts the hivemind server.
var serveCmd = &cobra.Command{
	Use:   "serve",
	Short: "Start the hivemind server",
	Long: `Start the hivemind HTTP/WebSocket server: opens the SQLite database, runs
pending migrations, and serves the API and embedded web UI.`,
	RunE: runServe,
}

func init() {
	rootCmd.AddCommand(serveCmd)
	addServeFlags(serveCmd)
}

// addServeFlags registers the flags shared by "hivemind serve" and bare
// "hivemind" (see rootCmd in root.go). Kept in one place so the two commands
// can never drift apart.
func addServeFlags(cmd *cobra.Command) {
	addConfigFlags(cmd)
}

func addConfigFlags(cmd *cobra.Command) {
	cmd.Flags().StringP("config", "c", "", "path to config file")
	cmd.Flags().StringP("addr", "a", ":8080", "HTTP listen address")
	cmd.Flags().StringP("data-dir", "d", "./data", "data directory")
	cmd.Flags().StringP("workspace-name", "w", "Hivemind", "workspace display name")
	cmd.Flags().StringP("base-url", "b", "", "public base URL")
	cmd.Flags().BoolP("behind-proxy", "B", false, "trust one reverse-proxy hop")
	cmd.Flags().StringP("signup", "s", "invite", "signup mode (invite, closed, open)")
	cmd.Flags().StringP("max-upload-size", "m", "25MB", "maximum upload size")
	cmd.Flags().StringP("session-ttl", "t", "720h", "session lifetime")
	cmd.Flags().StringP("log-level", "l", "info", "log level (debug, info, warn, error)")
	cmd.Flags().StringP("log-format", "f", "", "log format (text, json); auto-detected when empty")
	cmd.Flags().StringP("tls-cert", "C", "", "TLS certificate path")
	cmd.Flags().StringP("tls-key", "K", "", "TLS private key path")
	cmd.Flags().BoolP("dev", "D", false, "enable development UI proxy")
	cmd.Flags().StringP("dev-proxy", "P", "http://localhost:5173", "development UI proxy URL")
}

// runServe is shared by rootCmd and serveCmd so bare "hivemind" behaves
// exactly like "hivemind serve".
func runServe(cmd *cobra.Command, args []string) error {
	loaded, err := config.Load(cmd.Flags())
	if err != nil {
		return err
	}
	if err := loaded.Config.Validate(); err != nil {
		return fmt.Errorf("invalid configuration: %w", err)
	}
	logging.New(loaded.Config.LogLevel, loaded.Config.LogFormat)
	cmd.Println("not implemented yet")
	return nil
}
