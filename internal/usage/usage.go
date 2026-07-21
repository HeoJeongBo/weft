// Package usage aggregates Claude Code token usage from the conversation
// records under ~/.claude/projects — the shared source of truth for work done
// on the host and inside devcontainers alike.
package usage

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

// Report is the aggregate over one time window.
type Report struct {
	Msgs         int
	InputTokens  int64
	OutputTokens int64
	CacheRead    int64
	Sessions     int
}

// Summary holds the two windows the sidebar shows.
type Summary struct {
	Today Report
	Week  Report
}

// record is the subset of a conversation jsonl line we care about.
type record struct {
	Type      string    `json:"type"`
	SessionID string    `json:"sessionId"`
	Timestamp time.Time `json:"timestamp"`
	Message   struct {
		Usage *struct {
			InputTokens        int64 `json:"input_tokens"`
			OutputTokens       int64 `json:"output_tokens"`
			CacheReadInput     int64 `json:"cache_read_input_tokens"`
			CacheCreationInput int64 `json:"cache_creation_input_tokens"`
		} `json:"usage"`
	} `json:"message"`
}

// Scan aggregates assistant-message usage recorded since the given time.
// Files older than since (by mtime) are skipped entirely; unparsable lines are
// ignored — the jsonl format is claude's, not ours.
func Scan(claudeDir string, since, until time.Time) (Report, error) {
	files, err := filepath.Glob(filepath.Join(claudeDir, "projects", "*", "*.jsonl"))
	if err != nil {
		return Report{}, err
	}
	var rep Report
	sessions := map[string]bool{}
	for _, f := range files {
		info, err := os.Stat(f)
		if err != nil || info.ModTime().Before(since) {
			continue
		}
		scanFile(f, since, until, &rep, sessions)
	}
	rep.Sessions = len(sessions)
	return rep, nil
}

func scanFile(path string, since, until time.Time, rep *Report, sessions map[string]bool) {
	fh, err := os.Open(path)
	if err != nil {
		return
	}
	defer func() { _ = fh.Close() }()
	sc := bufio.NewScanner(fh)
	sc.Buffer(make([]byte, 0, 1024*1024), 16*1024*1024)
	for sc.Scan() {
		var r record
		if err := json.Unmarshal(sc.Bytes(), &r); err != nil {
			continue
		}
		if r.Type != "assistant" || r.Message.Usage == nil {
			continue
		}
		if r.Timestamp.Before(since) || r.Timestamp.After(until) {
			continue
		}
		u := r.Message.Usage
		rep.Msgs++
		rep.InputTokens += u.InputTokens + u.CacheCreationInput
		rep.OutputTokens += u.OutputTokens
		rep.CacheRead += u.CacheReadInput
		if r.SessionID != "" {
			sessions[r.SessionID] = true
		}
	}
}

// Summarize computes the today / last-7-days windows relative to now.
func Summarize(claudeDir string, now time.Time) Summary {
	sod := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	today, _ := Scan(claudeDir, sod, now)
	week, _ := Scan(claudeDir, now.AddDate(0, 0, -7), now)
	return Summary{Today: today, Week: week}
}

// Compact renders n as a short human number (1.2k, 3.4M).
func Compact(n int64) string {
	switch {
	case n >= 1_000_000:
		return trim1(float64(n)/1_000_000) + "M"
	case n >= 1_000:
		return trim1(float64(n)/1_000) + "k"
	default:
		return itoa(n)
	}
}

// trim1 renders f (always >= 1.0 here) with at most one decimal place.
func trim1(f float64) string {
	s := itoa(int64(f*10 + 0.5)) // one decimal, rounded
	whole, frac := s[:len(s)-1], s[len(s)-1:]
	if frac == "0" {
		return whole
	}
	return whole + "." + frac
}

func itoa(n int64) string {
	if n == 0 {
		return "0"
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}
