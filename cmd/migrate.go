package cmd

import (
	"context"
	"fmt"

	"github.com/0funct0ry/hivemind/internal/config"
	"github.com/0funct0ry/hivemind/internal/logging"
	"github.com/0funct0ry/hivemind/internal/store"
	"github.com/spf13/cobra"
)

// migrateCmd applies pending database migrations.
var migrateCmd = &cobra.Command{
	Use:   "migrate",
	Short: "Apply pending database migrations",
	Long: `Run any pending SQLite schema migrations against the configured data
directory and exit. Safe to run repeatedly; already-applied migrations are
skipped.`,
	Args: cobra.NoArgs,
	RunE: runMigrate,
}

func init() {
	rootCmd.AddCommand(migrateCmd)

	migrateCmd.Flags().String("config", "", "path to config file")
	migrateCmd.Flags().StringP("data-dir", "d", "./data", "data directory")
	migrateCmd.Flags().String("log-level", "info", "log level (debug, info, warn, error)")
	migrateCmd.Flags().String("log-format", "", "log format (text, json); auto-detected when empty")
	migrateCmd.Flags().Int("to", -1, "target migration version; omit to apply all pending migrations")
}

func runMigrate(cmd *cobra.Command, args []string) error {
	loaded, err := config.Load(cmd.Flags())
	if err != nil {
		return err
	}
	logging.New(loaded.Config.LogLevel, loaded.Config.LogFormat)
	ctx := cmd.Context()
	if ctx == nil {
		ctx = context.Background()
	}
	s, err := store.Open(ctx, loaded.Config.DataDir)
	if err != nil {
		return err
	}
	defer func() { _ = s.Close() }()
	target, err := cmd.Flags().GetInt("to")
	if err != nil {
		return fmt.Errorf("read migration target: %w", err)
	}
	if target < -1 {
		return fmt.Errorf("migration target must be omitted or non-negative")
	}
	if target == -1 {
		if err := s.Migrate(ctx); err != nil {
			return err
		}
	} else if err := s.MigrateTo(ctx, target); err != nil {
		return err
	}
	return nil
}
