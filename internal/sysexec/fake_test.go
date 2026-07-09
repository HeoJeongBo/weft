package sysexec

import (
	"context"
	"errors"
	"reflect"
	"testing"
)

func TestCallLine(t *testing.T) {
	cases := []struct {
		name string
		call Call
		want string
	}{
		{"no args", Call{Name: "git"}, "git"},
		{"with args", Call{Name: "git", Args: []string{"status", "-s"}}, "git status -s"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.call.Line(); got != tc.want {
				t.Errorf("Line() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestFakeRunnerRecordsAndDelegates(t *testing.T) {
	f := &FakeRunner{
		Handler: func(c Call) (Result, error) {
			return Result{Stdout: c.Kind + ":" + c.Line()}, nil
		},
	}
	ctx := context.Background()

	runRes, err := f.Run(ctx, "git", "status")
	if err != nil || runRes.Stdout != "run:git status" {
		t.Fatalf("Run = %+v, %v", runRes, err)
	}
	mutRes, err := f.Mutate(ctx, "git", "commit")
	if err != nil || mutRes.Stdout != "mutate:git commit" {
		t.Fatalf("Mutate = %+v, %v", mutRes, err)
	}

	want := []Call{
		{Kind: "run", Name: "git", Args: []string{"status"}},
		{Kind: "mutate", Name: "git", Args: []string{"commit"}},
	}
	if !reflect.DeepEqual(f.Calls, want) {
		t.Errorf("Calls = %+v, want %+v", f.Calls, want)
	}
	if last := f.LastCall(); last.Line() != "git commit" {
		t.Errorf("LastCall = %+v", last)
	}
}

func TestFakeRunnerNilHandler(t *testing.T) {
	f := &FakeRunner{}
	res, err := f.Run(context.Background(), "git", "status")
	if err != nil || (res != Result{}) {
		t.Errorf("nil handler Run = %+v, %v; want empty success", res, err)
	}
}

func TestFakeRunnerHandlerError(t *testing.T) {
	sentinel := errors.New("boom")
	f := &FakeRunner{Handler: func(Call) (Result, error) { return Result{}, sentinel }}
	if _, err := f.Mutate(context.Background(), "git"); !errors.Is(err, sentinel) {
		t.Errorf("want sentinel, got %v", err)
	}
}

func TestFakeRunnerStream(t *testing.T) {
	t.Run("splits stdout to sink", func(t *testing.T) {
		f := &FakeRunner{Handler: func(Call) (Result, error) {
			return Result{Stdout: "a\nb\nc\n"}, nil
		}}
		var got []string
		res, err := f.Stream(context.Background(), func(l Line) {
			got = append(got, l.Text)
		}, "git", "log")
		if err != nil {
			t.Fatal(err)
		}
		if want := []string{"a", "b", "c"}; !reflect.DeepEqual(got, want) {
			t.Errorf("sink got %v, want %v", got, want)
		}
		if res.Stdout != "a\nb\nc\n" {
			t.Errorf("res.Stdout = %q", res.Stdout)
		}
		if f.LastCall().Kind != "stream" {
			t.Errorf("LastCall kind = %q, want stream", f.LastCall().Kind)
		}
	})

	t.Run("nil sink", func(t *testing.T) {
		f := &FakeRunner{Handler: func(Call) (Result, error) {
			return Result{Stdout: "x\n"}, nil
		}}
		if _, err := f.Stream(context.Background(), nil, "git"); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("empty stdout does not call sink", func(t *testing.T) {
		f := &FakeRunner{}
		f.Stream(context.Background(), func(l Line) {
			t.Errorf("sink should not be called, got %q", l.Text)
		}, "git")
	})
}

func TestFakeRunnerLook(t *testing.T) {
	def := &FakeRunner{}
	if got, err := def.Look("git"); err != nil || got != "/usr/bin/git" {
		t.Errorf("default Look = %q, %v", got, err)
	}

	sentinel := errors.New("missing")
	custom := &FakeRunner{LookFunc: func(name string) (string, error) {
		if name == "git" {
			return "/opt/git", nil
		}
		return "", sentinel
	}}
	if got, err := custom.Look("git"); err != nil || got != "/opt/git" {
		t.Errorf("custom Look(git) = %q, %v", got, err)
	}
	if _, err := custom.Look("nope"); !errors.Is(err, sentinel) {
		t.Errorf("custom Look(nope) err = %v, want sentinel", err)
	}
}

func TestFakeRunnerDryRunAndLastCall(t *testing.T) {
	if (&FakeRunner{}).DryRun() {
		t.Error("default DryRun should be false")
	}
	if !(&FakeRunner{DryRunMode: true}).DryRun() {
		t.Error("DryRunMode true should report true")
	}
	if last := (&FakeRunner{}).LastCall(); !reflect.DeepEqual(last, Call{}) {
		t.Errorf("LastCall with no calls = %+v, want zero Call", last)
	}
}
