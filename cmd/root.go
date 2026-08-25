package cmd

import (
	"os"

	"github.com/0funct0ry/hivemind/internal/config"
	"github.com/0funct0ry/hivemind/internal/store"
	"github.com/spf13/cobra"
)

// rootCmd is the base command. Invoked bare, it behaves exactly like `hivemind
// serve` — it shares serveCmd's flags (see addServeFlags in serve.go) and its
// Run is serveCmd's Run.
var rootCmd = &cobra.Command{
	Use:   "hivemind",
	Short: "Self-hosted team chat, one binary and one database file",
	Long: `hivemind is a minimal self-hosted team chat server: a single Go binary with
an embedded web UI and SQLite storage. Run it with no subcommand to start the
server, equivalent to "hivemind serve".`,
	RunE: runServe,
}

func init() {
	addServeFlags(rootCmd)

	rootCmd.AddCommand(configCmd)
	rootCmd.AddCommand(seedCmd)
}

// Execute adds all child commands to the root command and sets flags appropriately.
// This is called by main.main(). It only needs to happen once to the rootCmd.
func Execute() {
	err := rootCmd.Execute()
	if err != nil {
		os.Exit(1)
	}
}

// configCmd contains configuration inspection commands.
var configCmd = &cobra.Command{
	Use:   "config",
	Short: "Inspect hivemind configuration",
}

var configShowCmd = &cobra.Command{
	Use:   "show",
	Short: "Print the effective merged configuration",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		loaded, err := config.Load(cmd.Flags())
		if err != nil {
			return err
		}
		_, err = cmd.OutOrStdout().Write([]byte(loaded.YAML()))
		return err
	},
}

// seedCmd generates development fixture data. Registered here per SPEC.md
// §2.4 rather than getting its own cmd/ file.
var seedCmd = &cobra.Command{
	Use:   "seed",
	Short: "Generate development fixture data",
	RunE: func(cmd *cobra.Command, args []string) error {
		numUsers, _ := cmd.Flags().GetInt("users")
		numChannels, _ := cmd.Flags().GetInt("channels")
		numMessages, _ := cmd.Flags().GetInt("messages")
		dataDir, _ := cmd.Flags().GetString("data-dir")

		ctx := cmd.Context()
		s, err := store.Open(ctx, dataDir)
		if err != nil {
			return err
		}
		defer s.Close()

		if err := s.Migrate(ctx); err != nil {
			return err
		}

		cmd.Printf("Seeding %d users, %d channels, %d messages in %s...\n", numUsers, numChannels, numMessages, dataDir)
		if err := s.Seed(ctx, numUsers, numChannels, numMessages); err != nil {
			return err
		}
		cmd.Println("Seeding completed successfully!")
		return nil
	},
}

func init() {
	configCmd.AddCommand(configShowCmd)
	addConfigFlags(configShowCmd)

	seedCmd.Flags().Int("users", 10, "Number of users to seed")
	seedCmd.Flags().Int("channels", 5, "Number of channels to seed")
	seedCmd.Flags().Int("messages", 5000, "Number of messages to seed")
	seedCmd.Flags().StringP("data-dir", "d", "./data", "Data directory")
}
