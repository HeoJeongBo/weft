package engine

import (
	"context"

	"github.com/HeoJeongBo/weft/internal/domain"
)

// EventKind classifies a progress event emitted during an orchestration.
type EventKind string

const (
	EventStep  EventKind = "step"  // a named step began
	EventLog   EventKind = "log"   // a log line (often streamed from a command)
	EventDone  EventKind = "done"  // the operation finished successfully
	EventError EventKind = "error" // the operation failed
)

// Event is a single progress update. Session is set on EventDone.
type Event struct {
	Kind    EventKind
	Step    string
	Text    string
	Err     error
	Session *domain.Session
}

// Sink receives progress events. A nil Sink is valid (events are dropped).
type Sink func(Event)

func (s Sink) emit(ev Event) {
	if s != nil {
		s(ev)
	}
}

func (s Sink) step(name string) { s.emit(Event{Kind: EventStep, Step: name}) }
func (s Sink) log(text string)  { s.emit(Event{Kind: EventLog, Text: text}) }
func (s Sink) fail(err error)   { s.emit(Event{Kind: EventError, Err: err}) }
func (s Sink) done(x domain.Session) {
	s.emit(Event{Kind: EventDone, Session: &x})
}

// CreateSession runs New in a goroutine and streams events over a channel, which
// is closed when the operation completes. This is the shape the TUI consumes.
func (e *Engine) CreateSession(ctx context.Context, spec NewSpec) <-chan Event {
	ch := make(chan Event, 32)
	go func() {
		defer close(ch)
		sink := func(ev Event) { ch <- ev }
		if _, err := e.New(ctx, spec, sink); err != nil {
			// New already emitted EventError; nothing more to do.
			_ = err
		}
	}()
	return ch
}
