package ui

import (
	"strings"
	"testing"

	"github.com/gdamore/tcell/v2"
)

// These cover the App's *wiring* to the visualizer panel -- that build()
// installs a panel showing the intended default, and that 'v' is routed
// to it. The panel's own cycling/rendering logic is tested in
// internal/visualizer, against fake visualizations and no App at all.

func TestAppStartsOnDefaultVisualization(t *testing.T) {
	a := newTestApp()
	defer a.visualizer.Close()

	first := a.visualizer.Names()[0]
	if got := a.visualizer.CurrentName(); got != first {
		t.Errorf("initial visualization = %q, want the first registered %q", got, first)
	}
	if title := a.visualizer.View().GetTitle(); !strings.Contains(title, first) {
		t.Errorf("border title = %q, want it to contain %q", title, first)
	}
}

// TestVKeyCyclesVisualizerPanel walks whatever is registered rather than
// naming each one, so registering a new visualization doesn't fail this
// test. It asserts every registered visualization is reachable by 'v'
// alone and that a full lap wraps back to the start -- the user-visible
// contract of the key.
func TestVKeyCyclesVisualizerPanel(t *testing.T) {
	a := newTestApp()
	defer a.visualizer.Close()

	names := a.visualizer.Names()
	if len(names) < 2 {
		t.Skip("cycling needs at least two registered visualizations")
	}
	if got := a.visualizer.CurrentName(); got != names[0] {
		t.Fatalf("initial visualization = %q, want the first registered %q", got, names[0])
	}

	vKey := tcell.NewEventKey(tcell.KeyRune, 'v', tcell.ModNone)
	seen := map[string]bool{names[0]: true}
	for i := 1; i < len(names); i++ {
		if result := a.globalInputCapture(vKey); result != nil {
			t.Fatalf("'v' should be consumed by the visualizer cycle, got %v", result)
		}
		got := a.visualizer.CurrentName()
		if got != names[i] {
			t.Errorf("after %d presses of 'v' = %q, want the next registered %q", i, got, names[i])
		}
		if seen[got] {
			t.Fatalf("visualization %q repeated at position %d before every one had been shown", got, i)
		}
		seen[got] = true
		if title := a.visualizer.View().GetTitle(); !strings.Contains(title, got) {
			t.Errorf("border title = %q, want it to contain the active %q", title, got)
		}
	}

	if len(seen) != len(names) {
		t.Errorf("cycled through %d distinct visualizations, want %d", len(seen), len(names))
	}

	// One more press completes the lap and wraps back to the start.
	if result := a.globalInputCapture(vKey); result != nil {
		t.Fatalf("'v' should be consumed by the visualizer cycle, got %v", result)
	}
	if got := a.visualizer.CurrentName(); got != names[0] {
		t.Errorf("after a full cycle of 'v' = %q, want it to wrap back to %q", got, names[0])
	}
}
