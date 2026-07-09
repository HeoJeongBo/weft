package dockerx

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/HeoJeongBo/weft/internal/sysexec"
)

func TestPsParsesJSONAndLabels(t *testing.T) {
	stdout := strings.Join([]string{
		`{"ID":"abc","Names":"app_devcontainer","Image":"go:1.24","State":"running","Status":"Up 3m","Labels":"weft.project=app,weft.session=app/feat-auth,com.docker.compose.project=x"}`,
		`{"ID":"def","Names":"app_old","Image":"go:1.24","State":"exited","Status":"Exited (0)","Labels":"weft.project=app,weft.session=app/spike"}`,
	}, "\n")

	f := &sysexec.FakeRunner{Handler: func(sysexec.Call) (sysexec.Result, error) {
		return sysexec.Result{Stdout: stdout}, nil
	}}
	d := New(f)

	cs, err := d.Ps(context.Background(), "weft.project=app")
	if err != nil {
		t.Fatal(err)
	}
	if len(cs) != 2 {
		t.Fatalf("want 2 containers, got %d", len(cs))
	}
	if cs[0].ID != "abc" || cs[0].State != "running" {
		t.Errorf("bad container[0]: %+v", cs[0])
	}
	if cs[0].Labels["weft.session"] != "app/feat-auth" {
		t.Errorf("label not parsed: %+v", cs[0].Labels)
	}
	if !strings.Contains(f.LastCall().Line(), "--filter label=weft.project=app") {
		t.Errorf("filter arg missing: %q", f.LastCall().Line())
	}
}

func TestRemoveArgs(t *testing.T) {
	f := &sysexec.FakeRunner{}
	d := New(f)

	// no ids -> no call
	if err := d.Remove(context.Background(), true); err != nil {
		t.Fatal(err)
	}
	if len(f.Calls) != 0 {
		t.Fatalf("expected no docker call for empty ids, got %d", len(f.Calls))
	}

	if err := d.Remove(context.Background(), true, "abc", "def"); err != nil {
		t.Fatal(err)
	}
	line := f.LastCall().Line()
	for _, want := range []string{"rm", "-f", "abc", "def"} {
		if !strings.Contains(line, want) {
			t.Errorf("argv %q missing %q", line, want)
		}
	}
}

func TestRemoveByLabel(t *testing.T) {
	f := &sysexec.FakeRunner{Handler: func(c sysexec.Call) (sysexec.Result, error) {
		if c.Kind == "run" { // the Ps call
			return sysexec.Result{Stdout: `{"ID":"abc","Labels":"weft.session=app/x"}` + "\n" + `{"ID":"def","Labels":"weft.session=app/x"}`}, nil
		}
		return sysexec.Result{}, nil
	}}
	d := New(f)

	n, err := d.RemoveByLabel(context.Background(), "weft.session=app/x", true)
	if err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Errorf("want 2 removed, got %d", n)
	}
	line := f.LastCall().Line()
	if !strings.Contains(line, "abc") || !strings.Contains(line, "def") {
		t.Errorf("rm should target both ids: %q", line)
	}
}

func TestDaemonUp(t *testing.T) {
	f := &sysexec.FakeRunner{Handler: func(sysexec.Call) (sysexec.Result, error) {
		return sysexec.Result{Stdout: "29.4.3"}, nil
	}}
	if !New(f).DaemonUp(context.Background()) {
		t.Error("want daemon up")
	}
}

func TestDaemonDown(t *testing.T) {
	f := &sysexec.FakeRunner{Handler: func(sysexec.Call) (sysexec.Result, error) {
		return sysexec.Result{}, errors.New("cannot connect to the docker daemon")
	}}
	if New(f).DaemonUp(context.Background()) {
		t.Error("want daemon down")
	}
}

func TestPsError(t *testing.T) {
	f := &sysexec.FakeRunner{Handler: func(sysexec.Call) (sysexec.Result, error) {
		return sysexec.Result{}, errors.New("docker down")
	}}
	cs, err := New(f).Ps(context.Background(), "weft.project=app")
	if err == nil {
		t.Fatal("want error")
	}
	if cs != nil {
		t.Errorf("want nil containers, got %+v", cs)
	}
}

// TestStopStart covers Stop and Start: empty ids issue no docker call, and with
// ids they Mutate the "stop"/"start" argv.
func TestStopStart(t *testing.T) {
	tests := []struct {
		name string
		fn   func(*Exec, context.Context, ...string) error
		verb string
	}{
		{"stop", (*Exec).Stop, "stop"},
		{"start", (*Exec).Start, "start"},
	}
	for _, tt := range tests {
		t.Run(tt.name+" empty ids no call", func(t *testing.T) {
			f := &sysexec.FakeRunner{}
			if err := tt.fn(New(f), context.Background()); err != nil {
				t.Fatal(err)
			}
			if len(f.Calls) != 0 {
				t.Fatalf("expected no docker call for empty ids, got %d", len(f.Calls))
			}
		})
		t.Run(tt.name+" with ids", func(t *testing.T) {
			f := &sysexec.FakeRunner{}
			if err := tt.fn(New(f), context.Background(), "abc", "def"); err != nil {
				t.Fatal(err)
			}
			line := f.LastCall().Line()
			for _, want := range []string{tt.verb, "abc", "def"} {
				if !strings.Contains(line, want) {
					t.Errorf("argv %q missing %q", line, want)
				}
			}
			if f.LastCall().Kind != "mutate" {
				t.Errorf("%s should Mutate, got %q", tt.name, f.LastCall().Kind)
			}
		})
	}
}

func TestRemoveByLabelErrors(t *testing.T) {
	t.Run("ps error", func(t *testing.T) {
		f := &sysexec.FakeRunner{Handler: func(sysexec.Call) (sysexec.Result, error) {
			return sysexec.Result{}, errors.New("ps failed")
		}}
		n, err := New(f).RemoveByLabel(context.Background(), "weft.session=app/x", true)
		if err == nil {
			t.Fatal("want error")
		}
		if n != 0 {
			t.Errorf("want 0 removed, got %d", n)
		}
	})
	t.Run("remove error", func(t *testing.T) {
		f := &sysexec.FakeRunner{Handler: func(c sysexec.Call) (sysexec.Result, error) {
			if c.Kind == "run" { // the Ps call
				return sysexec.Result{Stdout: `{"ID":"abc","Labels":"weft.session=app/x"}`}, nil
			}
			return sysexec.Result{}, errors.New("rm failed") // the Remove (Mutate) call
		}}
		n, err := New(f).RemoveByLabel(context.Background(), "weft.session=app/x", true)
		if err == nil {
			t.Fatal("want error")
		}
		if n != 0 {
			t.Errorf("want 0 removed, got %d", n)
		}
	})
}

// TestParsePsSkipsBlankAndMalformed covers the blank-line and malformed-JSON
// continue branches in parsePs, plus a container with empty labels.
func TestParsePsSkipsBlankAndMalformed(t *testing.T) {
	stdout := strings.Join([]string{
		"",                        // blank line -> skipped
		"   ",                     // whitespace-only -> skipped after TrimSpace
		"{not valid json",         // malformed -> skipped
		`{"ID":"ok","Labels":""}`, // valid, empty labels
	}, "\n")
	cs := parsePs(stdout)
	if len(cs) != 1 {
		t.Fatalf("want 1 container, got %d: %+v", len(cs), cs)
	}
	if cs[0].ID != "ok" {
		t.Errorf("ID = %q, want ok", cs[0].ID)
	}
	if len(cs[0].Labels) != 0 {
		t.Errorf("want empty labels map, got %+v", cs[0].Labels)
	}
}

func TestParseLabels(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want map[string]string
	}{
		{"empty", "", map[string]string{}},
		{"single", "k=v", map[string]string{"k": "v"}},
		{"multi", "a=1,b=2", map[string]string{"a": "1", "b": "2"}},
		{"trailing comma and spaces", "a=1, b=2, ", map[string]string{"a": "1", "b": "2"}},
		{"no value", "flag", map[string]string{"flag": ""}},
	}
	for _, tt := range tests {
		got := parseLabels(tt.in)
		if len(got) != len(tt.want) {
			t.Errorf("%s: len = %d, want %d (%+v)", tt.name, len(got), len(tt.want), got)
			continue
		}
		for k, v := range tt.want {
			if got[k] != v {
				t.Errorf("%s: [%q] = %q, want %q", tt.name, k, got[k], v)
			}
		}
	}
}
