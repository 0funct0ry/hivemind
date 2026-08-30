// Package chattui renders the hivemind chat CLI's terminal UI on top of internal/chatclient —
// ANSI frame rendering, dot-command dispatch, and readline-based input. Kept separate from
// chatclient so the network layer stays testable without a terminal.
package chattui

import (
	"fmt"
	"strconv"
	"strings"
)

// ansi256Cube are the 6 intensity levels the 216-color 6x6x6 cube uses per channel.
var ansi256Cube = [6]int{0, 95, 135, 175, 215, 255}

// NearestANSI256 downsamples a "#RRGGBB" hex color to the nearest ANSI-256 color code, for
// rendering a user's stored avatar_color as a stable terminal foreground color (SPEC.md §7.7).
func NearestANSI256(hex string) int {
	r, g, b, ok := parseHex(hex)
	if !ok {
		return 7 // default: white/light gray
	}
	return 16 + 36*nearestCubeIndex(r) + 6*nearestCubeIndex(g) + nearestCubeIndex(b)
}

func parseHex(hex string) (r, g, b int, ok bool) {
	hex = strings.TrimPrefix(hex, "#")
	if len(hex) != 6 {
		return 0, 0, 0, false
	}
	rv, err1 := strconv.ParseInt(hex[0:2], 16, 0)
	gv, err2 := strconv.ParseInt(hex[2:4], 16, 0)
	bv, err3 := strconv.ParseInt(hex[4:6], 16, 0)
	if err1 != nil || err2 != nil || err3 != nil {
		return 0, 0, 0, false
	}
	return int(rv), int(gv), int(bv), true
}

func nearestCubeIndex(v int) int {
	best, bestDist := 0, 1<<30
	for i, level := range ansi256Cube {
		d := level - v
		if d < 0 {
			d = -d
		}
		if d < bestDist {
			best, bestDist = i, d
		}
	}
	return best
}

// ColorCache maps a user id to its downsampled ANSI-256 color, populated lazily so each user's
// hex is decoded only once per session.
type ColorCache struct {
	byUserID map[string]int
}

// NewColorCache constructs an empty ColorCache.
func NewColorCache() *ColorCache { return &ColorCache{byUserID: map[string]int{}} }

// Get returns the cached ANSI-256 code for a user, computing and caching it from avatarColor
// on first use.
func (c *ColorCache) Get(userID, avatarColor string) int {
	if code, ok := c.byUserID[userID]; ok {
		return code
	}
	code := NearestANSI256(avatarColor)
	c.byUserID[userID] = code
	return code
}

// palette256 is a curated set of readable, evenly distinct ANSI-256 foreground colors — avoids
// near-black/near-white codes that vanish against a terminal's own background. Used to give
// items with no natural color of their own (channel names) a stable, distinct color per name.
var palette256 = []int{31, 32, 33, 34, 35, 36, 37, 100, 101, 103, 108, 109, 110, 116, 130, 136, 142, 148, 168, 172, 178}

// HashColor deterministically maps a string (e.g. a channel id) to one of palette256 — the
// same input always yields the same color within a session, without needing a stored hex like
// a user's avatar_color.
func HashColor(s string) int {
	var h uint32 = 2166136261
	for i := 0; i < len(s); i++ {
		h ^= uint32(s[i])
		h *= 16777619
	}
	return palette256[h%uint32(len(palette256))]
}

// FgSGR returns the ANSI SGR escape sequence to set the 256-color foreground.
func FgSGR(code int) string { return fmt.Sprintf("\x1b[38;5;%dm", code) }

// HighlightsYou reports whether body contains the literal "@you" self-mention token that the
// CLI renders in reverse video — SPEC.md §7.7 scans body text client-side for this rather than
// relying on the server's mentions array, which publicMessage always emits empty today.
func HighlightsYou(body string) bool {
	return strings.Contains(body, "@you")
}
