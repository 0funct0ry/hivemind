package chatclient

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

// withHome points HOME (and USERPROFILE on Windows) at a temp dir so SessionPath resolves
// under the test's sandbox rather than the real user's home directory.
func withHome(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	if runtime.GOOS == "windows" {
		t.Setenv("USERPROFILE", dir)
	}
	return dir
}

func TestLoadSessionMissingFile(t *testing.T) {
	withHome(t)
	s, ok, err := LoadSession("localhost")
	if err != nil || ok || s != nil {
		t.Fatalf("LoadSession on missing file = (%v, %v, %v), want (nil, false, nil)", s, ok, err)
	}
}

func TestSaveAndLoadSessionRoundTrip(t *testing.T) {
	home := withHome(t)
	want := &Session{Host: "localhost", Token: "hm_abc", ExpiresAt: time.Now().Add(time.Hour).UnixMilli()}
	if err := SaveSession(want); err != nil {
		t.Fatal(err)
	}

	path := filepath.Join(home, ".hivemind", "session")
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" && info.Mode().Perm() != 0600 {
		t.Fatalf("session file mode = %v, want 0600", info.Mode().Perm())
	}

	got, ok, err := LoadSession("localhost")
	if err != nil || !ok {
		t.Fatalf("LoadSession = (%v, %v, %v), want a valid session", got, ok, err)
	}
	if got.Token != want.Token {
		t.Fatalf("Token = %q, want %q", got.Token, want.Token)
	}
}

func TestLoadSessionExpired(t *testing.T) {
	withHome(t)
	expired := &Session{Host: "localhost", Token: "hm_abc", ExpiresAt: time.Now().Add(-time.Hour).UnixMilli()}
	if err := SaveSession(expired); err != nil {
		t.Fatal(err)
	}
	s, ok, err := LoadSession("localhost")
	if err != nil || ok || s != nil {
		t.Fatalf("LoadSession on expired session = (%v, %v, %v), want (nil, false, nil)", s, ok, err)
	}
}

func TestLoadSessionDifferentHost(t *testing.T) {
	withHome(t)
	valid := &Session{Host: "example.com", Token: "hm_abc", ExpiresAt: time.Now().Add(time.Hour).UnixMilli()}
	if err := SaveSession(valid); err != nil {
		t.Fatal(err)
	}
	s, ok, err := LoadSession("localhost")
	if err != nil || ok || s != nil {
		t.Fatalf("LoadSession for different host = (%v, %v, %v), want (nil, false, nil)", s, ok, err)
	}
}
