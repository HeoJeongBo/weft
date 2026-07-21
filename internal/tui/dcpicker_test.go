package tui

import (
	"bytes"
	"context"
	"io"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

func pickerItems() []DcItem {
	return []DcItem{
		{Name: "oasys-ui", Container: "oasys-ui-dev-1", Workspace: "/w/ui", State: "running"},
		{Name: "gantry", Container: "gantry_devcontainer-dev-1-with-a-very-long-name", Workspace: "/w/gantry", State: "exited"},
	}
}

func pickerUpdate(t *testing.T, m dcModel, msg tea.Msg) (dcModel, tea.Cmd) {
	t.Helper()
	next, cmd := m.Update(msg)
	return next.(dcModel), cmd
}

func TestDcPickerNavigation(t *testing.T) {
	m := dcModel{theme: newTheme(), items: pickerItems(), result: DcCancelled}

	m, _ = pickerUpdate(t, m, tea.KeyPressMsg{Code: tea.KeyUp})
	if m.cursor != 0 {
		t.Errorf("up at top: cursor = %d", m.cursor)
	}
	m, _ = pickerUpdate(t, m, tea.KeyPressMsg{Code: tea.KeyDown})
	if m.cursor != 1 {
		t.Errorf("down: cursor = %d", m.cursor)
	}
	m, _ = pickerUpdate(t, m, keyPress("j"))
	if m.cursor != 1 {
		t.Errorf("j at bottom: cursor = %d", m.cursor)
	}
	m, _ = pickerUpdate(t, m, keyPress("k"))
	if m.cursor != 0 {
		t.Errorf("k: cursor = %d", m.cursor)
	}
	m, _ = pickerUpdate(t, m, struct{}{}) // non-key msg is a no-op
	if m.cursor != 0 {
		t.Errorf("non-key msg moved cursor to %d", m.cursor)
	}
}

func TestDcPickerSelectAndQuit(t *testing.T) {
	base := dcModel{theme: newTheme(), items: pickerItems(), result: DcCancelled}

	m, cmd := pickerUpdate(t, base, keyPress("enter"))
	if m.result != 0 || cmd == nil {
		t.Errorf("enter: result = %d, cmd nil = %v", m.result, cmd == nil)
	}

	m, cmd = pickerUpdate(t, base, keyPress("r"))
	if m.result != DcRescan || cmd == nil {
		t.Errorf("r: result = %d", m.result)
	}

	for _, quit := range []tea.KeyPressMsg{keyPress("q"), keyPress("esc"), {Code: 'c', Mod: tea.ModCtrl}} {
		m, cmd = pickerUpdate(t, base, quit)
		if m.result != DcCancelled || cmd == nil {
			t.Errorf("%s: result = %d, cmd nil = %v", quit.String(), m.result, cmd == nil)
		}
	}

	empty := dcModel{theme: newTheme(), result: DcCancelled}
	empty, cmd = pickerUpdate(t, empty, keyPress("enter"))
	if cmd != nil || empty.result != DcCancelled {
		t.Errorf("enter with no items: result = %d, cmd nil = %v", empty.result, cmd == nil)
	}
}

func TestDcPickerView(t *testing.T) {
	m := dcModel{theme: newTheme(), items: pickerItems(), result: DcCancelled}
	v := m.View().Content
	for _, want := range []string{"devcontainers", "●", "○", "❯", "oasys-ui", "/w/gantry", "…", "enter attach"} {
		if !strings.Contains(v, want) {
			t.Errorf("view missing %q:\n%s", want, v)
		}
	}

	empty := dcModel{theme: newTheme(), result: DcCancelled}
	if v := empty.View().Content; !strings.Contains(v, "none found") {
		t.Errorf("empty view missing hint:\n%s", v)
	}
}

func TestPickDc(t *testing.T) {
	drive := func(t *testing.T, input string) (int, error) {
		t.Helper()
		pr, pw := io.Pipe()
		var buf bytes.Buffer
		orig := newProgram
		defer func() { newProgram = orig }()
		newProgram = func(m tea.Model, opts ...tea.ProgramOption) *tea.Program {
			opts = append(opts,
				tea.WithInput(pr),
				tea.WithOutput(&buf),
				tea.WithWindowSize(100, 30),
				tea.WithoutSignalHandler(),
			)
			return orig(m, opts...)
		}
		go func() { _, _ = pw.Write([]byte(input)) }()
		defer pw.Close()
		return PickDc(context.Background(), pickerItems())
	}

	if got, err := drive(t, "q"); err != nil || got != DcCancelled {
		t.Errorf("q: got %d, err %v", got, err)
	}
	if got, err := drive(t, "j\r"); err != nil || got != 1 {
		t.Errorf("j+enter: got %d, err %v", got, err)
	}

	// A cancelled context surfaces the program error.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := PickDc(ctx, pickerItems()); err == nil {
		t.Error("cancelled context: want error")
	}
}
