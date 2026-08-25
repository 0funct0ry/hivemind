package cmd

import (
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

func TestServeAndBareCommandShareConfigFlags(t *testing.T) {
	shorts := map[string]string{
		"config": "c", "addr": "a", "data-dir": "d", "workspace-name": "w", "base-url": "b",
		"behind-proxy": "B", "signup": "s", "max-upload-size": "m", "session-ttl": "t",
		"log-level": "l", "log-format": "f", "tls-cert": "C", "tls-key": "K", "dev": "D", "dev-proxy": "P",
	}
	for name, shorthand := range shorts {
		if rootCmd.Flags().Lookup(name) == nil {
			t.Errorf("bare command missing --%s", name)
		}
		if serveCmd.Flags().Lookup(name) == nil {
			t.Errorf("serve command missing --%s", name)
		}
		if got := rootCmd.Flags().Lookup(name).Shorthand; got != shorthand {
			t.Errorf("bare command --%s shorthand = %q, want %q", name, got, shorthand)
		}
		if got := serveCmd.Flags().Lookup(name).Shorthand; got != shorthand {
			t.Errorf("serve command --%s shorthand = %q, want %q", name, got, shorthand)
		}
	}
}

func TestServeRejectsInvalidSignup(t *testing.T) {
	cmd := &cobra.Command{}
	addConfigFlags(cmd)
	if err := cmd.Flags().Set("signup", "nonsense"); err != nil {
		t.Fatal(err)
	}
	err := runServe(cmd, nil)
	if err == nil || !strings.Contains(err.Error(), "signup") {
		t.Fatalf("runServe error = %v, want signup validation error", err)
	}
}
