package ui

import (
	"strings"
	"testing"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"mpdtui/internal/mpdclient"
)

// fakeViz is a minimal Visualization for exercising visualizerPanel's
// container logic (cycling, title updates, rendering) independent of any
// specific visualization's own drawing logic.
type fakeViz struct{ name string }

func (f fakeViz) Name() string { return f.name }

func (f fakeViz) Render(width, height int, elapsed time.Duration, st mpdclient.Status) []string {
	lines := make([]string, height)
	for i := range lines {
		lines[i] = f.name
	}
	return lines
}

// recordingViz captures the elapsed duration it was last called with, for
// asserting visualizerPanel.tick actually passes real elapsed time.
type recordingViz struct {
	name string
	got  *time.Duration
}

func (r recordingViz) Name() string { return r.name }

func (r recordingViz) Render(width, height int, elapsed time.Duration, st mpdclient.Status) []string {
	*r.got = elapsed
	return make([]string, height)
}

func TestVisualizerPanelStartsOnFirstRegisteredVisualization(t *testing.T) {
	a := newTestApp()
	// Balance is the default on purpose -- it is the one showing
	// something about the music rather than about its loudness. Named
	// explicitly here (rather than read off the registry) precisely
	// because it is a deliberate choice, so reordering the registry has
	// to fail this test rather than quietly change what users see.
	if got := a.visualizer.current().Name(); got != "Balance" {
		t.Errorf("initial visualization = %q, want %q", got, "Balance")
	}
	if title := a.visualizer.view.GetTitle(); !strings.Contains(title, "Balance") {
		t.Errorf("border title = %q, want it to contain %q", title, "Balance")
	}
}

func TestVisualizerPanelNextCyclesAndWraps(t *testing.T) {
	v := tview.NewTextView().SetDynamicColors(true)
	v.SetBorder(true).SetTitleAlign(tview.AlignRight)
	p := &visualizerPanel{view: v, vizs: []Visualization{fakeViz{"One"}, fakeViz{"Two"}}}
	p.view.SetTitle(" " + p.current().Name() + " ")

	if got := p.current().Name(); got != "One" {
		t.Fatalf("initial = %q, want %q", got, "One")
	}

	p.next()
	if got := p.current().Name(); got != "Two" {
		t.Errorf("after next() = %q, want %q", got, "Two")
	}
	if title := p.view.GetTitle(); !strings.Contains(title, "Two") {
		t.Errorf("border title after next() = %q, want it to contain %q", title, "Two")
	}

	p.next()
	if got := p.current().Name(); got != "One" {
		t.Errorf("after wrapping next() = %q, want %q", got, "One")
	}
}

func TestVisualizerPanelNextWithSingleVisualizationIsNoOp(t *testing.T) {
	v := tview.NewTextView().SetDynamicColors(true)
	p := &visualizerPanel{view: v, vizs: []Visualization{fakeViz{"Solo"}}}
	before := p.current().Name()
	p.next()
	if got := p.current().Name(); got != before {
		t.Errorf("next() with a single registered visualization changed it: %q -> %q", before, got)
	}
}

func TestAppVisualizerPanelCyclesRegisteredVisualizations(t *testing.T) {
	// Walks whatever is registered rather than naming each one, so
	// registering a new visualization doesn't fail this test.
	a := newTestApp()
	first := a.visualizer.vizs[0].Name()
	if got := a.visualizer.current().Name(); got != first {
		t.Fatalf("initial visualization = %q, want the first registered %q", got, first)
	}

	seen := make(map[string]bool)
	for i := range a.visualizer.vizs {
		name := a.visualizer.current().Name()
		if seen[name] {
			t.Fatalf("visualization %q repeated at position %d before every one had been shown", name, i)
		}
		seen[name] = true
		a.visualizer.next()
	}
	if len(seen) != len(a.visualizer.vizs) {
		t.Errorf("cycled through %d distinct visualizations, want %d", len(seen), len(a.visualizer.vizs))
	}
	if got := a.visualizer.current().Name(); got != first {
		t.Errorf("after a full cycle = %q, want it to wrap back to %q", got, first)
	}
}

func TestVisualizerPanelTickRendersFromActiveVisualization(t *testing.T) {
	v := tview.NewTextView().SetDynamicColors(true)
	v.SetRect(0, 0, 20, 3)
	p := &visualizerPanel{view: v, started: time.Now(), vizs: []Visualization{fakeViz{"Probe"}}}

	p.tick(mpdclient.Status{State: mpdclient.StatePlay})

	got := v.GetText(true)
	if !strings.Contains(got, "Probe") {
		t.Errorf("view text after tick() = %q, want it to contain %q", got, "Probe")
	}
}

func TestVisualizerPanelTickPassesRealElapsedTime(t *testing.T) {
	v := tview.NewTextView().SetDynamicColors(true)
	v.SetRect(0, 0, 20, 3)
	var got time.Duration
	p := &visualizerPanel{view: v, started: time.Now(), vizs: []Visualization{recordingViz{"Probe", &got}}}

	time.Sleep(5 * time.Millisecond)
	p.tick(mpdclient.Status{})

	if got < 5*time.Millisecond {
		t.Errorf("elapsed passed to Render = %v, want at least 5ms since the panel started", got)
	}
}

func TestVKeyCyclesVisualizerPanel(t *testing.T) {
	a := newTestApp()
	first, second := a.visualizer.vizs[0].Name(), a.visualizer.vizs[1].Name()
	if got := a.visualizer.current().Name(); got != first {
		t.Fatalf("initial visualization = %q, want the first registered %q", got, first)
	}
	vKey := tcell.NewEventKey(tcell.KeyRune, 'v', tcell.ModNone)
	if result := a.globalInputCapture(vKey); result != nil {
		t.Errorf("'v' should be consumed by the visualizer cycle, got %v", result)
	}
	if got := a.visualizer.current().Name(); got != second {
		t.Errorf("after 'v' visualization = %q, want the next registered %q", got, second)
	}

	// One 'v' per remaining visualization brings it back around.
	for i := 1; i < len(a.visualizer.vizs); i++ {
		if result := a.globalInputCapture(vKey); result != nil {
			t.Errorf("'v' should be consumed by the visualizer cycle, got %v", result)
		}
	}
	if got := a.visualizer.current().Name(); got != first {
		t.Errorf("after a full cycle of 'v' = %q, want %q", got, first)
	}
}
