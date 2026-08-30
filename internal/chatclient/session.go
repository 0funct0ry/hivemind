package chatclient

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// Session is the cached bearer-token session persisted at ~/.hivemind/session (SPEC.md §7.7).
type Session struct {
	Host      string `json:"host"`
	Token     string `json:"token"`
	ExpiresAt int64  `json:"expires_at"`
}

// SessionPath returns the path to the cached session file.
func SessionPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home directory: %w", err)
	}
	return filepath.Join(home, ".hivemind", "session"), nil
}

// LoadSession reads the cached session for host. It returns (nil, false, nil) — not an error
// — when the file is absent, expired, or cached for a different host, since all three simply
// mean "prompt for login," not a failure.
func LoadSession(host string) (*Session, bool, error) {
	path, err := SessionPath()
	if err != nil {
		return nil, false, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, false, nil
		}
		return nil, false, fmt.Errorf("read session: %w", err)
	}
	var s Session
	if err := json.Unmarshal(data, &s); err != nil {
		return nil, false, fmt.Errorf("parse session: %w", err)
	}
	if s.Host != host || s.ExpiresAt <= time.Now().UnixMilli() {
		return nil, false, nil
	}
	return &s, true, nil
}

// SaveSession writes the session cache, mode 0600, creating ~/.hivemind (0700) if needed.
func SaveSession(s *Session) error {
	path, err := SessionPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return fmt.Errorf("create session directory: %w", err)
	}
	data, err := json.Marshal(s)
	if err != nil {
		return fmt.Errorf("encode session: %w", err)
	}
	if err := os.WriteFile(path, data, 0600); err != nil {
		return fmt.Errorf("write session: %w", err)
	}
	return nil
}
