package bot

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestRunReturnsExitCodeAndOutput(t *testing.T) {
	stdout, stderr, exitCode, err := Run(context.Background(), "/bin/sh", `echo out; echo err >&2`, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if exitCode != 0 {
		t.Fatalf("expected exit 0, got %d", exitCode)
	}
	if strings.TrimSpace(string(stdout)) != "out" {
		t.Fatalf("unexpected stdout %q", stdout)
	}
	if strings.TrimSpace(string(stderr)) != "err" {
		t.Fatalf("unexpected stderr %q", stderr)
	}
}

func TestRunReportsNonZeroExit(t *testing.T) {
	_, _, exitCode, err := Run(context.Background(), "/bin/sh", `exit 7`, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if exitCode != 7 {
		t.Fatalf("expected exit 7, got %d", exitCode)
	}
}

// A script that forks a long-running child (here, "sleep 5" as a background job so the shell
// itself exits immediately) must not let Run block past its timeout waiting on that orphaned
// child's stdout pipe to close.
func TestRunEnforcesTimeoutDespiteOrphanedChild(t *testing.T) {
	start := time.Now()
	_, _, exitCode, err := Run(context.Background(), "/bin/sh", `sleep 5 & echo started; wait $!`, 300*time.Millisecond)
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("expected a timeout error")
	}
	if exitCode != -1 {
		t.Fatalf("expected exit code -1 on timeout, got %d", exitCode)
	}
	if elapsed > 2*time.Second {
		t.Fatalf("Run took %s, expected it to return shortly after the 300ms timeout", elapsed)
	}
}
