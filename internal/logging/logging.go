// Package logging builds the process-wide structured logger.
package logging

import (
	"log/slog"
	"os"

	"golang.org/x/term"
)

// New builds an *slog.Logger from level and format and installs it as the
// default logger. format is "text" or "json"; an empty format auto-detects
// text for a TTY and json otherwise. An unrecognized level falls back to info.
func New(level, format string) *slog.Logger {
	opts := &slog.HandlerOptions{Level: parseLevel(level)}

	useText := format == "text"
	if format == "" {
		useText = term.IsTerminal(int(os.Stdout.Fd()))
	}

	var handler slog.Handler
	if useText {
		handler = slog.NewTextHandler(os.Stdout, opts)
	} else {
		handler = slog.NewJSONHandler(os.Stdout, opts)
	}

	logger := slog.New(handler)
	slog.SetDefault(logger)
	return logger
}

func parseLevel(level string) slog.Level {
	switch level {
	case "debug":
		return slog.LevelDebug
	case "warn":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}
