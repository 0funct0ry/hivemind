package cmd

import (
	"github.com/spf13/cobra"
)

// shellCmd opens the admin REPL.
var shellCmd = &cobra.Command{
	Use:   "shell",
	Short: "Open the hivemind admin REPL",
	Long: `Open an interactive admin shell against the store, either directly
(server stopped) or against a running server's API with --url.`,
	Run: func(cmd *cobra.Command, args []string) {
		cmd.Println("not implemented yet")
	},
}

func init() {
	rootCmd.AddCommand(shellCmd)

	shellCmd.Flags().String("config", "", "path to config file")
}
