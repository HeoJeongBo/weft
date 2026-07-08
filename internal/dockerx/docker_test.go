package dockerx

import (
	"context"
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
