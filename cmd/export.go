package cmd

import (
	"github.com/spf13/cobra"
)

// exportCmd exports workspace data.
var exportCmd = &cobra.Command{
	Use:   "export",
	Short: "Export workspace data",
	Long:  `Export hivemind workspace data (channels, messages, users) from the database.`,
	Run: func(cmd *cobra.Command, args []string) {
		cmd.Println("not implemented yet")
	},
}

func init() {
	rootCmd.AddCommand(exportCmd)

	exportCmd.Flags().String("config", "", "path to config file")
}
