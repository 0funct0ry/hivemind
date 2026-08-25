package cmd

import (
	"github.com/spf13/cobra"
)

// serveCmd starts the hivemind server.
var serveCmd = &cobra.Command{
	Use:   "serve",
	Short: "Start the hivemind server",
	Long: `Start the hivemind HTTP/WebSocket server: opens the SQLite database, runs
pending migrations, and serves the API and embedded web UI.`,
	Run: runServe,
}

func init() {
	rootCmd.AddCommand(serveCmd)
	addServeFlags(serveCmd)
}

// addServeFlags registers the flags shared by "hivemind serve" and bare
// "hivemind" (see rootCmd in root.go). Kept in one place so the two commands
// can never drift apart.
func addServeFlags(cmd *cobra.Command) {
	cmd.Flags().String("config", "", "path to config file")
	cmd.Flags().String("log-level", "info", "log level (debug, info, warn, error)")
	cmd.Flags().String("log-format", "", "log format (text, json); auto-detected when empty")
}

// runServe is shared by rootCmd and serveCmd so bare "hivemind" behaves
// exactly like "hivemind serve".
func runServe(cmd *cobra.Command, args []string) {
	cmd.Println("not implemented yet")
}
