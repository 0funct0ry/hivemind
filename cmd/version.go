package cmd

import (
	"encoding/json"
	"fmt"
	"text/tabwriter"

	"github.com/0funct0ry/hivemind/internal/buildinfo"
	"github.com/spf13/cobra"
)

// versionCmd prints build version information.
var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Print version information",
	Long:  `Print the hivemind version, commit, build date, Go version, and OS/arch.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		asJSON, err := cmd.Flags().GetBool("json")
		if err != nil {
			return err
		}

		info := map[string]string{
			"version":    buildinfo.Version,
			"commit":     buildinfo.Commit,
			"date":       buildinfo.Date,
			"go_version": buildinfo.GoVersion(),
			"os":         buildinfo.OS(),
			"arch":       buildinfo.Arch(),
		}

		if asJSON {
			enc := json.NewEncoder(cmd.OutOrStdout())
			enc.SetIndent("", "  ")
			return enc.Encode(info)
		}

		w := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 4, 2, ' ', 0)
		fmt.Fprintf(w, "version:\t%s\n", info["version"])
		fmt.Fprintf(w, "commit:\t%s\n", info["commit"])
		fmt.Fprintf(w, "date:\t%s\n", info["date"])
		fmt.Fprintf(w, "go version:\t%s\n", info["go_version"])
		fmt.Fprintf(w, "os/arch:\t%s/%s\n", info["os"], info["arch"])
		return w.Flush()
	},
}

func init() {
	rootCmd.AddCommand(versionCmd)

	versionCmd.Flags().Bool("json", false, "print version information as JSON")
}
