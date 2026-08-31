package bot

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const secretExt = ".secret"

// Route is one discovered trigger: the script that answers it and the signing secret that
// verifies calls to it (copied from the slash command's shown-once secret at registration).
// SecretPath is re-read fresh on every request (see server.go), not just at startup — so
// pasting a regenerated secret into the file takes effect immediately, exactly like editing the
// script itself already does, without needing to restart the listener.
type Route struct {
	Trigger    string
	ScriptPath string
	SecretPath string
	Secret     string // the secret's content at discovery time; server.go re-reads SecretPath per request
}

// Discover scans scriptsDir for one route per regular file that isn't itself a .secret file and
// has a sibling "<name>.secret" file. A script missing its secret is skipped (reported as a
// warning, not a fatal error) so an in-progress, partially-configured scripts directory doesn't
// prevent the listener from serving the triggers that are ready.
func Discover(scriptsDir string) ([]Route, []string, error) {
	entries, err := os.ReadDir(scriptsDir)
	if err != nil {
		return nil, nil, fmt.Errorf("read scripts dir %q: %w", scriptsDir, err)
	}

	var routes []Route
	var warnings []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		ext := filepath.Ext(e.Name())
		if ext == secretExt {
			continue // paired with its script below via secretPath
		}
		// A close miss on the secret extension (e.g. "echo.secrets", a common typo) would
		// otherwise be treated as its own script and produce a confusing duplicate warning —
		// call it out specifically instead.
		if strings.HasPrefix(strings.ToLower(ext), secretExt) {
			warnings = append(warnings, fmt.Sprintf("ignoring %q: its extension %q looks like a misnamed secret file — secret files must be named exactly \"<trigger>%s\" (singular)", e.Name(), ext, secretExt))
			continue
		}
		trigger := strings.TrimSuffix(e.Name(), ext)
		scriptPath := filepath.Join(scriptsDir, e.Name())
		secretPath := filepath.Join(scriptsDir, trigger+secretExt)

		secret, err := os.ReadFile(secretPath)
		if err != nil {
			if os.IsNotExist(err) {
				warnings = append(warnings, fmt.Sprintf("skipping %q: no %s file found (register the slash command and paste its secret there)", e.Name(), trigger+secretExt))
				continue
			}
			return nil, warnings, fmt.Errorf("read secret for %q: %w", e.Name(), err)
		}

		routes = append(routes, Route{
			Trigger:    trigger,
			ScriptPath: scriptPath,
			SecretPath: secretPath,
			Secret:     strings.TrimSpace(string(secret)),
		})
	}
	return routes, warnings, nil
}
