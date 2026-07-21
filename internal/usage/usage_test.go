package usage

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func writeJSONL(t *testing.T, dir, project, name, content string) string {
	t.Helper()
	p := filepath.Join(dir, "projects", project)
	if err := os.MkdirAll(p, 0o755); err != nil {
		t.Fatal(err)
	}
	f := filepath.Join(p, name)
	if err := os.WriteFile(f, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return f
}

func line(ts, session string, in, out, cacheRead int) string {
	return `{"type":"assistant","sessionId":"` + session + `","timestamp":"` + ts + `","message":{"usage":{"input_tokens":` +
		itoa(int64(in)) + `,"output_tokens":` + itoa(int64(out)) + `,"cache_read_input_tokens":` + itoa(int64(cacheRead)) + `,"cache_creation_input_tokens":5}}}` + "\n"
}

func TestScanAndSummarize(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 7, 21, 15, 0, 0, 0, time.UTC)

	content := line("2026-07-21T10:00:00Z", "s1", 100, 10, 1000) + // today
		line("2026-07-19T10:00:00Z", "s2", 200, 20, 0) + // this week, not today
		line("2026-07-01T10:00:00Z", "s3", 400, 40, 0) + // too old (in-file filter)
		`{"type":"user","timestamp":"2026-07-21T10:01:00Z"}` + "\n" + // not assistant
		`{"type":"assistant","timestamp":"2026-07-21T10:02:00Z","message":{}}` + "\n" + // no usage
		"not json at all\n"
	writeJSONL(t, dir, "-w-a", "a.jsonl", content)

	// A stale file (old mtime) must be skipped without being read.
	old := writeJSONL(t, dir, "-w-b", "b.jsonl", line("2026-07-21T09:00:00Z", "s9", 999, 999, 0))
	stale := now.AddDate(0, 0, -30)
	if err := os.Chtimes(old, stale, stale); err != nil {
		t.Fatal(err)
	}

	s := Summarize(dir, now)
	if s.Today.Msgs != 1 || s.Today.InputTokens != 105 || s.Today.OutputTokens != 10 || s.Today.CacheRead != 1000 || s.Today.Sessions != 1 {
		t.Errorf("today = %+v", s.Today)
	}
	if s.Week.Msgs != 2 || s.Week.InputTokens != 310 || s.Week.OutputTokens != 30 || s.Week.Sessions != 2 {
		t.Errorf("week = %+v", s.Week)
	}
}

func TestScanErrors(t *testing.T) {
	if _, err := Scan("[", time.Time{}, time.Now()); err == nil {
		t.Error("malformed glob pattern: want error")
	}
	rep, err := Scan(t.TempDir(), time.Time{}, time.Now())
	if err != nil || rep.Msgs != 0 {
		t.Errorf("empty dir: %+v err=%v", rep, err)
	}
	// Unreadable file is skipped.
	dir := t.TempDir()
	f := writeJSONL(t, dir, "-p", "x.jsonl", "data")
	if err := os.Chmod(f, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(f, 0o644) })
	if rep, err := Scan(dir, time.Time{}, time.Now()); err != nil || rep.Msgs != 0 {
		t.Errorf("unreadable file: %+v err=%v", rep, err)
	}
}

func TestCompact(t *testing.T) {
	cases := map[int64]string{
		0: "0", 7: "7", 999: "999",
		1000: "1k", 1234: "1.2k", 45000: "45k",
		1_200_000: "1.2M", 3_000_000: "3M",
	}
	for n, want := range cases {
		if got := Compact(n); got != want {
			t.Errorf("Compact(%d) = %q, want %q", n, got, want)
		}
	}
}
