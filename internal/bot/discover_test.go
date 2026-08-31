package bot

import (
	"os"
	"path/filepath"
	"testing"
)

func writeFile(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o755); err != nil {
		t.Fatal(err)
	}
}

func TestDiscover(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "echo", `echo hi`)
	writeFile(t, dir, "echo.secret", "whsec_abc\n")
	writeFile(t, dir, "orphan", `echo nope`) // no matching .secret

	routes, warnings, err := Discover(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(routes) != 1 {
		t.Fatalf("expected 1 route, got %d: %+v", len(routes), routes)
	}
	got := routes[0]
	if got.Trigger != "echo" {
		t.Fatalf("expected trigger %q, got %q", "echo", got.Trigger)
	}
	if got.Secret != "whsec_abc" {
		t.Fatalf("expected trimmed secret %q, got %q", "whsec_abc", got.Secret)
	}
	if got.ScriptPath != filepath.Join(dir, "echo") {
		t.Fatalf("unexpected script path %q", got.ScriptPath)
	}

	if len(warnings) != 1 {
		t.Fatalf("expected 1 warning for the orphaned script, got %d: %v", len(warnings), warnings)
	}
}

func TestDiscoverFlagsMisnamedSecretFileInsteadOfTreatingItAsAScript(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "echo", `echo hi`)
	writeFile(t, dir, "echo.secret", "whsec_abc")
	writeFile(t, dir, "echo.secrets", "whsec_typo") // the common plural-typo case

	routes, warnings, err := Discover(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(routes) != 1 {
		t.Fatalf("expected only the real echo route, got %d: %+v", len(routes), routes)
	}
	if len(warnings) != 1 || warnings[0] == "" {
		t.Fatalf("expected exactly one warning calling out the misnamed secret file, got %v", warnings)
	}
	for _, r := range routes {
		if r.Trigger == "echo.secrets" || r.Trigger != "echo" {
			t.Fatalf("echo.secrets must never become its own route: %+v", routes)
		}
	}
}

func TestDiscoverIgnoresSecretFilesAsRoutes(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "status", `echo status`)
	writeFile(t, dir, "status.secret", "whsec_xyz")

	routes, warnings, err := Discover(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(warnings) != 0 {
		t.Fatalf("expected no warnings, got %v", warnings)
	}
	for _, r := range routes {
		if r.Trigger == "status.secret" {
			t.Fatal("a .secret file must never become its own route")
		}
	}
}
