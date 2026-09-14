package picker

import (
	"sync"
	"testing"
	"time"

	"github.com/gdamore/tcell/v2"
)

// These cover run -- the tview event loop the finder's state machine is
// wrapped in, and the only part of this package that owns a terminal.
//
// Driven on a tcell simulation screen rather than a real one: tview
// opens /dev/tty directly, which a test process has no controlling
// terminal for, and a simulation screen exercises the same event loop
// without needing one.

// runOnSimScreen starts f.run() against a simulation screen and returns
// a channel carrying its result plus a function that waits for the
// first paint, which is when input handling goes live.
func runOnSimScreen(t *testing.T, f *finder) (<-chan int, func()) {
	t.Helper()

	screen := tcell.NewSimulationScreen("UTF-8")
	if err := screen.Init(); err != nil {
		t.Fatalf("init simulation screen: %v", err)
	}
	screen.SetSize(120, 40)
	f.app.SetScreen(screen)

	drawn := make(chan struct{})
	var once sync.Once
	f.app.SetAfterDrawFunc(func(tcell.Screen) { once.Do(func() { close(drawn) }) })

	out := make(chan int, 1)
	go func() {
		idx, err := f.run()
		if err != nil {
			t.Errorf("run returned %v, want nil", err)
		}
		out <- idx
	}()

	return out, func() {
		t.Helper()
		select {
		case <-drawn:
		case <-time.After(5 * time.Second):
			f.app.Stop()
			t.Fatal("the picker never drew")
		}
	}
}

func awaitPick(t *testing.T, out <-chan int, f *finder) int {
	t.Helper()
	select {
	case idx := <-out:
		return idx
	case <-time.After(5 * time.Second):
		f.app.Stop()
		t.Fatal("run did not return")
		return -1
	}
}

func TestRunConfirmsThroughTheEventLoop(t *testing.T) {
	f := newFinder("Tracks", []string{"Alpha", "Beta", "Gamma"})
	out, waitDrawn := runOnSimScreen(t, f)
	waitDrawn()

	f.app.QueueEvent(tcell.NewEventKey(tcell.KeyDown, 0, tcell.ModNone))
	f.app.QueueEvent(tcell.NewEventKey(tcell.KeyEnter, 0, tcell.ModNone))

	if idx := awaitPick(t, out, f); idx != 1 {
		t.Errorf("run selected %d, want 1 (the second row)", idx)
	}
}

func TestRunCancelsThroughTheEventLoop(t *testing.T) {
	f := newFinder("Tracks", []string{"Alpha", "Beta"})
	out, waitDrawn := runOnSimScreen(t, f)
	waitDrawn()

	f.app.QueueEvent(tcell.NewEventKey(tcell.KeyEscape, 0, tcell.ModNone))

	if idx := awaitPick(t, out, f); idx != -1 {
		t.Errorf("run returned %d after Escape, want -1", idx)
	}
}

// TestRunFiltersThenConfirms covers typing reaching the input field
// through the real event loop, which is what the fall-through in
// handleKey exists for.
func TestRunFiltersThenConfirms(t *testing.T) {
	f := newFinder("Tracks", []string{"Alpha", "Beta", "Gamma"})
	out, waitDrawn := runOnSimScreen(t, f)
	waitDrawn()

	for _, r := range "gam" {
		f.app.QueueEvent(tcell.NewEventKey(tcell.KeyRune, r, tcell.ModNone))
	}
	f.app.QueueEvent(tcell.NewEventKey(tcell.KeyEnter, 0, tcell.ModNone))

	if idx := awaitPick(t, out, f); idx != 2 {
		t.Errorf("run selected %d, want 2 (Gamma, after typing a filter)", idx)
	}
}
