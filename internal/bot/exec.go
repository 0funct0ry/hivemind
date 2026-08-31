package bot

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"time"
)

// Run executes rendered (already Go-template-expanded script content) as "<shell> -c rendered",
// enforcing timeout on top of ctx. Using "-c" rather than writing a temp file with a shebang
// keeps this generic across interpreters — "python3 -c ..." works exactly the same way as
// "/bin/sh -c ...". exitCode is -1 if the process could not be started at all (err is then
// non-nil); a script that runs and exits non-zero is not itself a Go error — that's the normal,
// expected "the script reported failure" case callers must handle via BuildResponse.
func Run(ctx context.Context, shell, rendered string, timeout time.Duration) (stdout, stderr []byte, exitCode int, err error) {
	runCtx := ctx
	if timeout > 0 {
		var cancel context.CancelFunc
		runCtx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}

	cmd := exec.CommandContext(runCtx, shell, "-c", rendered)
	var outBuf, errBuf bytes.Buffer
	cmd.Stdout = &outBuf
	cmd.Stderr = &errBuf
	// A script like "sleep 10; echo done" forks a child the shell doesn't wait on when killed —
	// without WaitDelay, Wait() would block until that orphaned child's own stdout/stderr pipe
	// closes (i.e. until it finishes on its own), defeating the timeout entirely. WaitDelay
	// forces the pipes closed shortly after cancellation regardless, sized relative to timeout
	// so a short --script-timeout isn't dominated by an oversized fixed grace period.
	if timeout > 0 {
		cmd.WaitDelay = min(timeout/2, 2*time.Second)
	}

	runErr := cmd.Run()
	stdout, stderr = outBuf.Bytes(), errBuf.Bytes()

	timedOut := errors.Is(runCtx.Err(), context.DeadlineExceeded)

	var exitErr *exec.ExitError
	switch {
	case timedOut:
		return stdout, stderr, -1, fmt.Errorf("script exceeded its %s timeout", timeout)
	case runErr == nil:
		return stdout, stderr, 0, nil
	case errors.As(runErr, &exitErr):
		return stdout, stderr, exitErr.ExitCode(), nil
	default:
		return stdout, stderr, -1, fmt.Errorf("run script: %w", runErr)
	}
}
