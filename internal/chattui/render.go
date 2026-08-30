package chattui

import (
	"bytes"
	"fmt"
	"os"
	"time"
)

const (
	ansiReset     = "\x1b[0m"
	ansiReverse   = "\x1b[7m"
	ansiHome      = "\x1b[H"
	ansiEraseDown = "\x1b[0J" // erase from cursor to end of screen
)

// Line is one rendered line in the message pane: a tag, a timestamp, an author name (with its
// color already resolved), and body text.
type Line struct {
	Tag         string
	TS          int64
	AuthorName  string
	AuthorColor int // ANSI-256 code, or 0 for none
	Body        string
	Reverse     bool // @you highlight, whole line in reverse video
}

// Renderer composes one frame (header + scrollback + input line) using raw ANSI escapes and
// flushes it in a single write — SPEC.md §7.7's "double buffering."
type Renderer struct {
	out *bytes.Buffer
}

// NewRenderer constructs a Renderer.
func NewRenderer() *Renderer { return &Renderer{out: &bytes.Buffer{}} }

// Frame builds one frame's bytes: a header line (omitted entirely when empty — no channel
// joined yet), the visible tail of the scrollback (bounded by maxLines), and a trailing input
// line with a "> " prompt. It homes the cursor and erases everything below in one shot rather
// than clearing line-by-line: a frame that renders fewer lines than the previous one (e.g.
// .clear, or switching to a channel with less history) would otherwise leave stale lines from
// the taller previous frame on screen, since a per-line \x1b[2K only ever clears lines the new
// frame actually redraws.
func (r *Renderer) Frame(header string, lines []Line, input string, maxLines int) []byte {
	r.out.Reset()
	r.out.WriteString(ansiHome)
	r.out.WriteString(ansiEraseDown)

	if header != "" {
		r.out.WriteString(header)
		r.out.WriteString("\r\n")
	}

	start := 0
	if len(lines) > maxLines {
		start = len(lines) - maxLines
	}
	for _, l := range lines[start:] {
		r.writeLine(l)
		r.out.WriteString("\r\n")
	}

	r.out.WriteString("> ")
	r.out.WriteString(input)

	return r.out.Bytes()
}

func (r *Renderer) writeLine(l Line) {
	if l.Reverse {
		r.out.WriteString(ansiReverse)
	}
	ts := time.UnixMilli(l.TS).Format("15:04")
	fmt.Fprintf(r.out, "[%s] %s ", l.Tag, ts)
	if l.AuthorColor > 0 {
		r.out.WriteString(FgSGR(l.AuthorColor))
	}
	r.out.WriteString(l.AuthorName)
	if l.AuthorColor > 0 {
		r.out.WriteString(ansiReset)
		if l.Reverse {
			r.out.WriteString(ansiReverse)
		}
	}
	r.out.WriteString(": ")
	r.out.WriteString(l.Body)
	if l.Reverse {
		r.out.WriteString(ansiReset)
	}
}

// Flush writes a frame to stdout in one call.
func Flush(frame []byte) error {
	_, err := os.Stdout.Write(frame)
	return err
}
