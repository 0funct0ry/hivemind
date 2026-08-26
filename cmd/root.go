package cmd

import (
	"os"

	"github.com/0funct0ry/hivemind/internal/config"
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

func init() {
	configCmd.AddCommand(configShowCmd)
	addConfigFlags(configShowCmd)
}
