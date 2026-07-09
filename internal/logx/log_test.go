package logx

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"
)

func TestNew(t *testing.T) {
	tests := []struct {
		name      string
		verbosity int
		jsonLogs  bool
		wantLevel slog.Level
	}{
		{"warn text", 0, false, slog.LevelWarn},
		{"info text", 1, false, slog.LevelInfo},
		{"debug text", 2, false, slog.LevelDebug},
		{"debug text high", 5, false, slog.LevelDebug},
		{"warn json", 0, true, slog.LevelWarn},
		{"info json", 1, true, slog.LevelInfo},
		{"debug json", 2, true, slog.LevelDebug},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			lg := New(&buf, tt.verbosity, tt.jsonLogs)

			// One step below the configured level must be suppressed.
			buf.Reset()
			lg.Log(context.Background(), tt.wantLevel-4, "below")
			if buf.Len() != 0 {
				t.Errorf("expected message below level %v to be suppressed, got %q", tt.wantLevel, buf.String())
			}

			// A record at the configured level must be emitted.
			buf.Reset()
			lg.Log(context.Background(), tt.wantLevel, "atlevel")
			out := buf.String()
			if out == "" {
				t.Fatalf("expected output at level %v, got empty", tt.wantLevel)
			}

			if tt.jsonLogs {
				if !strings.Contains(out, `"level"`) {
					t.Errorf("json output should contain \"level\": %q", out)
				}
				if !strings.HasPrefix(strings.TrimSpace(out), "{") {
					t.Errorf("json output should start with a brace: %q", out)
				}
			} else {
				if strings.Contains(out, "{") {
					t.Errorf("text output should not contain a JSON brace: %q", out)
				}
				if strings.Contains(out, `"level"`) {
					t.Errorf("text output should not contain quoted level key: %q", out)
				}
			}
		})
	}
}

func TestNewDebugEmitsWarnSuppresses(t *testing.T) {
	var dbg bytes.Buffer
	debugLogger := New(&dbg, 2, false)
	debugLogger.Debug("visible")
	if !strings.Contains(dbg.String(), "visible") {
		t.Errorf("debug logger should emit debug records, got %q", dbg.String())
	}

	var warn bytes.Buffer
	warnLogger := New(&warn, 0, false)
	warnLogger.Info("hidden")
	if warn.Len() != 0 {
		t.Errorf("warn logger should suppress info records, got %q", warn.String())
	}
}
