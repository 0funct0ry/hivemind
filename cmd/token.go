package cmd

import (
	"github.com/spf13/cobra"
)

// tokenCmd is the parent for API token management subcommands (create, list,
// revoke), each operating directly on the database.
var tokenCmd = &cobra.Command{
	Use:   "token",
	Short: "Manage API tokens",
	Long: `Manage hivemind API tokens directly against the database. Intended for
use with the server stopped.`,
	Run: func(cmd *cobra.Command, args []string) {
		cmd.Println("not implemented yet")
	},
}

func init() {
	rootCmd.AddCommand(tokenCmd)

	tokenCmd.PersistentFlags().String("config", "", "path to config file")
}
