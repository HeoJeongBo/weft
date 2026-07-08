package tui

import "charm.land/bubbles/v2/key"

type keyMap struct {
	Up      key.Binding
	Down    key.Binding
	Attach  key.Binding
	New     key.Binding
	Stop    key.Binding
	Start   key.Binding
	Delete  key.Binding
	Refresh key.Binding
	Help    key.Binding
	Quit    key.Binding
	Cancel  key.Binding
}

func newKeyMap() keyMap {
	return keyMap{
		Up:      key.NewBinding(key.WithKeys("k", "up"), key.WithHelp("↑/k", "up")),
		Down:    key.NewBinding(key.WithKeys("j", "down"), key.WithHelp("↓/j", "down")),
		Attach:  key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "attach")),
		New:     key.NewBinding(key.WithKeys("n"), key.WithHelp("n", "new")),
		Stop:    key.NewBinding(key.WithKeys("s"), key.WithHelp("s", "stop")),
		Start:   key.NewBinding(key.WithKeys("S"), key.WithHelp("S", "start")),
		Delete:  key.NewBinding(key.WithKeys("d"), key.WithHelp("d", "delete")),
		Refresh: key.NewBinding(key.WithKeys("r"), key.WithHelp("r", "refresh")),
		Help:    key.NewBinding(key.WithKeys("?"), key.WithHelp("?", "help")),
		Quit:    key.NewBinding(key.WithKeys("q", "ctrl+c"), key.WithHelp("q", "quit")),
		Cancel:  key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "cancel")),
	}
}

// ShortHelp and FullHelp satisfy help.KeyMap.
func (k keyMap) ShortHelp() []key.Binding {
	return []key.Binding{k.Attach, k.New, k.Stop, k.Delete, k.Refresh, k.Help, k.Quit}
}

func (k keyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{
		{k.Up, k.Down, k.Attach},
		{k.New, k.Stop, k.Start, k.Delete},
		{k.Refresh, k.Help, k.Quit},
	}
}
