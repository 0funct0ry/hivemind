package chattui

import (
	"os"
	"strings"

	"golang.org/x/term"
)

// terminalWidth returns the current terminal width, defaulting to 80 when it can't be
// determined (e.g. stdout redirected to a file) — a reasonable width for a chat pane.
func terminalWidth() int {
	if w, _, err := term.GetSize(int(os.Stdout.Fd())); err == nil && w > 0 {
		return w
	}
	return 80
}

// FormatColumns lays out items in a column-major grid (down each column, then across — the
// same reading order as `ls -C`), for .channels/.users output. plain holds the same entries as
// items but without ANSI color codes, so column widths are computed from visible width rather
// than escape-sequence length. Capped at 3 columns even on a very wide terminal, since more
// than that reads as a wall of text rather than a scannable list.
func FormatColumns(items, plain []string) string {
	if len(items) == 0 {
		return ""
	}
	maxWidth := 0
	for _, p := range plain {
		if len(p) > maxWidth {
			maxWidth = len(p)
		}
	}
	colWidth := maxWidth + 2
	cols := terminalWidth() / colWidth
	if cols > 3 {
		cols = 3
	}
	if cols < 1 {
		cols = 1
	}
	rows := (len(items) + cols - 1) / cols

	var b strings.Builder
	for r := 0; r < rows; r++ {
		for c := 0; c < cols; c++ {
			i := c*rows + r
			if i >= len(items) {
				continue
			}
			b.WriteString(items[i])
			if c < cols-1 {
				b.WriteString(strings.Repeat(" ", maxWidth-len(plain[i])+2))
			}
		}
		if r < rows-1 {
			b.WriteString("\n")
		}
	}
	return b.String()
}
