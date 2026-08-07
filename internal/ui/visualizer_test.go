package ui

import (
	"strings"
	"testing"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"mpdtui/internal/mpdclient"
)

// fakeViz is a minimal Visualization for exercising visualizerPanel's
// container logic (cycling, title updates, rendering) independent of any
// specific visualization's own drawing logic.
type fakeViz struct{ name string }

func (f fakeViz) Name() string { return f.name }

func (f fakeViz) Render(width, height, frame int, st mpdclient.Status) []string {
	lines := make([]string, height)
	for i := range lines {
		lines[i] = f.name
	}
	return lines
}

func TestVisualizerPanelStartsOnFirstRegisteredVisualization(t *testing.T) {
	a := newTestApp()
	if got := a.visualizer.current().Name(); got != "Sonar" {
		t.Errorf("initial visualization = %q, want %q", got, "Sonar")
	}
	if title := a.visualizer.view.GetTitle(); !strings.Contains(title, "Sonar") {
		t.Errorf("border title = %q, want it to contain %q", title, "Sonar")
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
	a := newTestApp()
	before := a.visualizer.current().Name()
	a.visualizer.next()
	if got := a.visualizer.current().Name(); got != before {
		t.Errorf("next() with a single registered visualization changed it: %q -> %q", before, got)
	}
}

func TestVisualizerPanelTickRendersFromActiveVisualization(t *testing.T) {
	v := tview.NewTextView().SetDynamicColors(true)
	v.SetRect(0, 0, 20, 3)
	p := &visualizerPanel{view: v, vizs: []Visualization{fakeViz{"Probe"}}}

	p.tick(mpdclient.Status{State: mpdclient.StatePlay})

	got := v.GetText(true)
	if !strings.Contains(got, "Probe") {
		t.Errorf("view text after tick() = %q, want it to contain %q", got, "Probe")
	}
}

func TestVisualizerPanelTickIncrementsFrame(t *testing.T) {
	v := tview.NewTextView().SetDynamicColors(true)
	v.SetRect(0, 0, 20, 3)
	p := &visualizerPanel{view: v, vizs: []Visualization{fakeViz{"Probe"}}}

	if p.frame != 0 {
		t.Fatalf("setup: frame should start at 0, got %d", p.frame)
	}
	p.tick(mpdclient.Status{})
	p.tick(mpdclient.Status{})
	if p.frame != 2 {
		t.Errorf("frame after two ticks = %d, want 2", p.frame)
	}
}

func TestVKeyCyclesVisualizerPanel(t *testing.T) {
	a := newTestApp()
	vKey := tcell.NewEventKey(tcell.KeyRune, 'v', tcell.ModNone)
	if result := a.globalInputCapture(vKey); result != nil {
		t.Errorf("'v' should be consumed by the visualizer cycle, got %v", result)
	}
}
