package cmd

import (
	"os"

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
	Run: runServe,
}

func init() {
	cobra.OnInitialize(initConfig)
	addServeFlags(rootCmd)

	rootCmd.AddCommand(configShowCmd)
	rootCmd.AddCommand(seedCmd)
}

// initConfig loads configuration and installs the logger for the invoked
// subcommand. It is a no-op for subcommands that never registered --config,
// --log-level, or --log-format.
func initConfig() {
	// Real viper wiring lands in M2; nothing to do yet.
}

// Execute adds all child commands to the root command and sets flags appropriately.
// This is called by main.main(). It only needs to happen once to the rootCmd.
func Execute() {
	err := rootCmd.Execute()
	if err != nil {
		os.Exit(1)
	}
}

// configShowCmd prints the effective merged configuration. Registered here per
// SPEC.md §2.4 rather than getting its own cmd/ file.
var configShowCmd = &cobra.Command{
	Use:   "config show",
	Short: "Print the effective merged configuration",
	Run: func(cmd *cobra.Command, args []string) {
		cmd.Println("not implemented yet")
	},
}

// seedCmd generates development fixture data. Registered here per SPEC.md
// §2.4 rather than getting its own cmd/ file.
var seedCmd = &cobra.Command{
	Use:   "seed",
	Short: "Generate development fixture data",
	Run: func(cmd *cobra.Command, args []string) {
		cmd.Println("not implemented yet")
	},
}
