package cmd

import (
	"github.com/spf13/cobra"
)

// userCmd is the parent for user management subcommands (add, list, passwd,
// promote, deactivate), each operating directly on the database.
var userCmd = &cobra.Command{
	Use:   "user",
	Short: "Manage users (add, list, passwd, promote, deactivate)",
	Long: `Manage hivemind user accounts directly against the database. Intended for
use with the server stopped.`,
	Run: func(cmd *cobra.Command, args []string) {
		cmd.Println("not implemented yet")
	},
}

func init() {
	rootCmd.AddCommand(userCmd)

	userCmd.PersistentFlags().String("config", "", "path to config file")
}
