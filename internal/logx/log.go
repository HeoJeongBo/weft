// Package logx configures weft's slog logger. Logs go to stderr; stdout is
// reserved for data output.
package logx

import (
	"io"
	"log/slog"
)

// New returns a logger whose level follows verbosity: 0 -> warn, 1 -> info,
// 2+ -> debug. When jsonLogs is true it emits JSON, otherwise plain text.
func New(w io.Writer, verbosity int, jsonLogs bool) *slog.Logger {
	level := slog.LevelWarn
	switch {
	case verbosity >= 2:
		level = slog.LevelDebug
	case verbosity == 1:
		level = slog.LevelInfo
	}
	opts := &slog.HandlerOptions{Level: level}
	var h slog.Handler
	if jsonLogs {
		h = slog.NewJSONHandler(w, opts)
	} else {
		h = slog.NewTextHandler(w, opts)
	}
	return slog.New(h)
}
