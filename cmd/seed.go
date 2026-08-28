package cmd

import (
	"github.com/0funct0ry/hivemind/internal/store"
	"github.com/spf13/cobra"
)

// seedCmd generates development fixture data. Registered here per SPEC.md
// §2.4 rather than getting its own cmd/ file.
var seedCmd = &cobra.Command{
	Use:   "seed",
	Short: "Generate development fixture data",
	RunE: func(cmd *cobra.Command, args []string) error {
		numUsers, _ := cmd.Flags().GetInt("users")
		numChannels, _ := cmd.Flags().GetInt("channels")
		numMessages, _ := cmd.Flags().GetInt("messages")
		unjoinedChannels, _ := cmd.Flags().GetInt("unjoined-channels")
		dataDir, _ := cmd.Flags().GetString("data-dir")
		password, _ := cmd.Flags().GetString("password")

		ctx := cmd.Context()
		s, err := store.Open(ctx, dataDir)
		if err != nil {
			return err
		}
		defer s.Close()

		if err := s.Migrate(ctx); err != nil {
			return err
		}

		cmd.Printf("Seeding %d users, %d channels (%d unjoined), %d messages in %s...\n", numUsers, numChannels, unjoinedChannels, numMessages, dataDir)
		if err := s.Seed(ctx, numUsers, numChannels, numMessages, unjoinedChannels, password); err != nil {
			return err
		}
		cmd.Println("Seeding completed successfully!")
		cmd.Printf("Log in as any seeded user (e.g. bruce, the admin) with password: %s\n", password)
		return nil
	},
}

func init() {
	seedCmd.Flags().Int("users", 10, "Number of users to seed")
	seedCmd.Flags().Int("channels", 5, "Number of channels to seed")
	seedCmd.Flags().Int("messages", 5000, "Number of messages to seed")
	seedCmd.Flags().Int("unjoined-channels", 0, "Number of seeded public channels to create with no members at all (not even the creator) — exercises Browse Channels / Join instead of every user already belonging to every channel")
	seedCmd.Flags().StringP("data-dir", "d", "./data", "Data directory")
	seedCmd.Flags().String("password", store.DefaultSeedPassword, "Password applied to all seeded users")
}
