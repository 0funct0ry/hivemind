package cmd

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/0funct0ry/hivemind/internal/auth"
	"github.com/0funct0ry/hivemind/internal/config"
	"github.com/0funct0ry/hivemind/internal/store"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

var userCmd = &cobra.Command{Use: "user", Short: "Manage users", RunE: func(cmd *cobra.Command, args []string) error { return cmd.Help() }}

func init() {
	rootCmd.AddCommand(userCmd)
	userCmd.PersistentFlags().String("config", "", "path to config file")
	userCmd.PersistentFlags().StringP("data-dir", "d", "./data", "data directory")
	userCmd.AddCommand(&cobra.Command{Use: "add <username> <email>", Args: cobra.ExactArgs(2), RunE: runUserAdd})
	userCmd.AddCommand(&cobra.Command{Use: "list", Args: cobra.NoArgs, RunE: runUserList})
	userCmd.AddCommand(&cobra.Command{Use: "passwd <username>", Args: cobra.ExactArgs(1), RunE: runUserPasswd})
	userCmd.AddCommand(&cobra.Command{Use: "promote <username>", Args: cobra.ExactArgs(1), RunE: runUserPromote})
	userCmd.AddCommand(&cobra.Command{Use: "deactivate <username>", Args: cobra.ExactArgs(1), RunE: runUserDeactivate})
}

func openUserStore(cmd *cobra.Command) (context.Context, *store.Store, error) {
	loaded, err := config.Load(cmd.Flags())
	if err != nil {
		return nil, nil, err
	}
	ctx := cmd.Context()
	if ctx == nil {
		ctx = context.Background()
	}
	s, err := store.Open(ctx, loaded.Config.DataDir)
	if err != nil {
		return nil, nil, err
	}
	if err := s.Migrate(ctx); err != nil {
		_ = s.Close()
		return nil, nil, err
	}
	return ctx, s, nil
}
func readPassword(prompt string) (string, error) {
	fmt.Fprint(os.Stderr, prompt)
	b, err := term.ReadPassword(int(os.Stdin.Fd()))
	fmt.Fprintln(os.Stderr)
	return string(b), err
}
func runUserAdd(cmd *cobra.Command, args []string) error {
	ctx, s, err := openUserStore(cmd)
	if err != nil {
		return err
	}
	defer s.Close()
	p, err := readPassword("Password: ")
	if err != nil {
		return err
	}
	q, err := readPassword("Confirm password: ")
	if err != nil {
		return err
	}
	if p != q {
		return fmt.Errorf("passwords do not match")
	}
	if err := auth.ValidatePassword(p); err != nil {
		return err
	}
	h, err := auth.HashPassword(p)
	if err != nil {
		return err
	}
	u, err := s.CreateUser(ctx, store.UserInput{Username: args[0], Email: args[1], PasswordHash: h})
	if err != nil {
		return err
	}
	fmt.Fprintf(cmd.OutOrStdout(), "created %s (%s)\n", u.Username, u.Email)
	return nil
}
func runUserList(cmd *cobra.Command, args []string) error {
	ctx, s, err := openUserStore(cmd)
	if err != nil {
		return err
	}
	defer s.Close()
	users, err := s.ListUsers(ctx, "", 50, true)
	if err != nil {
		return err
	}
	for _, u := range users {
		fmt.Fprintf(cmd.OutOrStdout(), "%s\t%s\t%s\t%s\n", u.Username, u.Email, u.Role, u.Status)
	}
	return nil
}
func findUser(ctx context.Context, s *store.Store, login string) (store.User, error) {
	return s.GetUserByLogin(ctx, strings.ToLower(login))
}
func runUserPasswd(cmd *cobra.Command, args []string) error {
	ctx, s, err := openUserStore(cmd)
	if err != nil {
		return err
	}
	defer s.Close()
	u, err := findUser(ctx, s, args[0])
	if err != nil {
		return err
	}
	p, err := readPassword("Password: ")
	if err != nil {
		return err
	}
	q, err := readPassword("Confirm password: ")
	if err != nil {
		return err
	}
	if p != q {
		return fmt.Errorf("passwords do not match")
	}
	if err := auth.ValidatePassword(p); err != nil {
		return err
	}
	h, err := auth.HashPassword(p)
	if err != nil {
		return err
	}
	return s.SetPassword(ctx, u.ID, h)
}
func runUserPromote(cmd *cobra.Command, args []string) error {
	ctx, s, err := openUserStore(cmd)
	if err != nil {
		return err
	}
	defer s.Close()
	u, err := findUser(ctx, s, args[0])
	if err != nil {
		return err
	}
	return s.Promote(ctx, u.ID)
}
func runUserDeactivate(cmd *cobra.Command, args []string) error {
	ctx, s, err := openUserStore(cmd)
	if err != nil {
		return err
	}
	defer s.Close()
	u, err := findUser(ctx, s, args[0])
	if err != nil {
		return err
	}
	if u.Role == "admin" {
		n, err := s.CountAdmins(ctx)
		if err != nil {
			return err
		}
		if n <= 1 {
			return fmt.Errorf("the last administrator cannot be deactivated")
		}
	}
	return s.Deactivate(ctx, u.ID)
}
