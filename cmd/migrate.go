package cmd

import (
	"github.com/spf13/cobra"
)

// migrateCmd applies pending database migrations.
var migrateCmd = &cobra.Command{
	Use:   "migrate",
	Short: "Apply pending database migrations",
	Long: `Run any pending SQLite schema migrations against the configured data
directory and exit. Safe to run repeatedly; already-applied migrations are
skipped.`,
	Run: func(cmd *cobra.Command, args []string) {
		cmd.Println("not implemented yet")
	},
}

func init() {
	rootCmd.AddCommand(migrateCmd)

	migrateCmd.Flags().String("config", "", "path to config file")
	migrateCmd.Flags().String("log-level", "info", "log level (debug, info, warn, error)")
	migrateCmd.Flags().String("log-format", "", "log format (text, json); auto-detected when empty")
}
