package cmd

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/0funct0ry/hivemind/internal/api"
	"github.com/0funct0ry/hivemind/internal/auth"
	"github.com/0funct0ry/hivemind/internal/config"
	"github.com/0funct0ry/hivemind/internal/logging"
	"github.com/0funct0ry/hivemind/internal/store"
	"github.com/spf13/cobra"
)

// serveCmd starts the hivemind server.
var serveCmd = &cobra.Command{
	Use:   "serve",
	Short: "Start the hivemind server",
	Long: `Start the hivemind HTTP/WebSocket server: opens the SQLite database, runs
pending migrations, and serves the API and embedded web UI.`,
	RunE: runServe,
}

func init() {
	rootCmd.AddCommand(serveCmd)
	addServeFlags(serveCmd)
}

// addServeFlags registers the flags shared by "hivemind serve" and bare
// "hivemind" (see rootCmd in root.go). Kept in one place so the two commands
// can never drift apart.
func addServeFlags(cmd *cobra.Command) {
	addConfigFlags(cmd)
}

func addConfigFlags(cmd *cobra.Command) {
	cmd.Flags().StringP("config", "c", "", "path to config file")
	cmd.Flags().StringP("addr", "a", ":8080", "HTTP listen address")
	cmd.Flags().StringP("data-dir", "d", "./data", "data directory")
	cmd.Flags().StringP("workspace-name", "w", "Hivemind", "workspace display name")
	cmd.Flags().StringP("base-url", "b", "", "public base URL")
	cmd.Flags().BoolP("behind-proxy", "B", false, "trust one reverse-proxy hop")
	cmd.Flags().StringP("signup", "s", "invite", "signup mode (invite, closed, open)")
	cmd.Flags().StringP("max-upload-size", "m", "25MB", "maximum upload size")
	cmd.Flags().StringP("session-ttl", "t", "720h", "session lifetime")
	cmd.Flags().StringP("log-level", "l", "info", "log level (debug, info, warn, error)")
	cmd.Flags().StringP("log-format", "f", "", "log format (text, json); auto-detected when empty")
	cmd.Flags().StringP("tls-cert", "C", "", "TLS certificate path")
	cmd.Flags().StringP("tls-key", "K", "", "TLS private key path")
	cmd.Flags().BoolP("dev", "D", false, "enable development UI proxy")
	cmd.Flags().StringP("dev-proxy", "P", "http://localhost:5173", "development UI proxy URL")
}

// runServe is shared by rootCmd and serveCmd so bare "hivemind" behaves
// exactly like "hivemind serve".
func runServe(cmd *cobra.Command, args []string) error {
	loaded, err := config.Load(cmd.Flags())
	if err != nil {
		return err
	}
	if err := loaded.Config.Validate(); err != nil {
		return fmt.Errorf("invalid configuration: %w", err)
	}
	logging.New(loaded.Config.LogLevel, loaded.Config.LogFormat)
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	s, err := store.Open(ctx, loaded.Config.DataDir)
	if err != nil {
		return fmt.Errorf("open store: %w", err)
	}
	defer s.Close()
	if err := s.Migrate(ctx); err != nil {
		return fmt.Errorf("migrate store: %w", err)
	}
	a := auth.New(s, loaded.Config.SessionTTL)
	go func() {
		t := time.NewTicker(15 * time.Minute)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case now := <-t.C:
				if err := s.SweepSessions(ctx, now.UnixMilli()); err != nil {
					slog.Default().Error("session sweep failed", "error", err)
				}
			}
		}
	}()
	h := api.NewRouter(s, a, loaded.Config)
	server := &http.Server{Addr: loaded.Config.Addr, Handler: h}
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdownCtx)
	}()
	var serveErr error
	if loaded.Config.TLS.Cert != "" {
		serveErr = server.ListenAndServeTLS(loaded.Config.TLS.Cert, loaded.Config.TLS.Key)
	} else {
		serveErr = server.ListenAndServe()
	}
	if serveErr != nil && serveErr != http.ErrServerClosed {
		return fmt.Errorf("serve HTTP: %w", serveErr)
	}
	return nil
}
