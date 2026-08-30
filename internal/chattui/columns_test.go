package chattui

import (
	"strings"
	"testing"
)

func TestFormatColumnsEmpty(t *testing.T) {
	if got := FormatColumns(nil, nil); got != "" {
		t.Fatalf("FormatColumns(nil, nil) = %q, want empty", got)
	}
}

func TestFormatColumnsPreservesAllItems(t *testing.T) {
	plain := []string{"alice", "bob", "carol", "dave", "erin"}
	out := FormatColumns(append([]string(nil), plain...), plain)
	for _, name := range plain {
		if !strings.Contains(out, name) {
			t.Errorf("FormatColumns output missing %q:\n%s", name, out)
		}
	}
}

func TestHashColorIsStableAndInPalette(t *testing.T) {
	c1 := HashColor("channel-42")
	c2 := HashColor("channel-42")
	if c1 != c2 {
		t.Fatalf("HashColor not stable: %d != %d", c1, c2)
	}
	found := false
	for _, p := range palette256 {
		if p == c1 {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("HashColor(%d) not in palette256", c1)
	}
}
