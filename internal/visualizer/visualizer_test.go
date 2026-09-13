package visualizer

import (
	"strings"
	"testing"
	"time"

	"github.com/rivo/tview"

	"mpdtui/internal/mpdclient"
)

// fakeViz is a minimal Visualization for exercising Panel's
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
// asserting Panel.Tick actually passes real elapsed time.
type recordingViz struct {
	name string
	got  *time.Duration
}

func (r recordingViz) Name() string { return r.name }

func (r recordingViz) Render(width, height int, elapsed time.Duration, st mpdclient.Status) []string {
	*r.got = elapsed
	return make([]string, height)
}

// newTestPanel builds a Panel over vizs with no audio feed and no
// terminal, the way New would minus the parts that touch the outside
// world (the fifo reader, the real registry).
func newTestPanel(vizs ...Visualization) *Panel {
	v := tview.NewTextView().SetDynamicColors(true)
	v.SetBorder(true).SetTitleAlign(tview.AlignRight)
	p := &Panel{view: v, started: time.Now(), vizs: vizs}
	p.view.SetTitle(" " + p.current().Name() + " ")
	return p
}

func TestPanelNextCyclesAndWraps(t *testing.T) {
	p := newTestPanel(fakeViz{"One"}, fakeViz{"Two"})

	if got := p.CurrentName(); got != "One" {
		t.Fatalf("initial = %q, want %q", got, "One")
	}

	p.Next(mpdclient.Status{})
	if got := p.CurrentName(); got != "Two" {
		t.Errorf("after Next() = %q, want %q", got, "Two")
	}
	if title := p.View().GetTitle(); !strings.Contains(title, "Two") {
		t.Errorf("border title after Next() = %q, want it to contain %q", title, "Two")
	}

	p.Next(mpdclient.Status{})
	if got := p.CurrentName(); got != "One" {
		t.Errorf("after wrapping Next() = %q, want %q", got, "One")
	}
}

func TestPanelNextWithSingleVisualizationIsNoOp(t *testing.T) {
	p := newTestPanel(fakeViz{"Solo"})
	before := p.CurrentName()
	p.Next(mpdclient.Status{})
	if got := p.CurrentName(); got != before {
		t.Errorf("Next() with a single registered visualization changed it: %q -> %q", before, got)
	}
}

func TestPanelTickRendersFromActiveVisualization(t *testing.T) {
	p := newTestPanel(fakeViz{"Probe"})
	p.View().SetRect(0, 0, 20, 3)

	p.Tick(mpdclient.Status{State: mpdclient.StatePlay})

	got := p.View().GetText(true)
	if !strings.Contains(got, "Probe") {
		t.Errorf("view text after Tick() = %q, want it to contain %q", got, "Probe")
	}
}

func TestPanelTickPassesRealElapsedTime(t *testing.T) {
	var got time.Duration
	p := newTestPanel(recordingViz{"Probe", &got})
	p.View().SetRect(0, 0, 20, 3)

	time.Sleep(5 * time.Millisecond)
	p.Tick(mpdclient.Status{})

	if got < 5*time.Millisecond {
		t.Errorf("elapsed passed to Render = %v, want at least 5ms since the panel started", got)
	}
}

// TestPanelTickSkipsRenderWhenCollapsed guards Tick's own size check.
// tview hands a panel a zero-width rect when the layout collapses it
// (a narrow terminal, a hidden flex item), and asking a visualization
// for zero lines is pointless work at best and an out-of-range index
// into its own output at worst -- so Tick must not call Render at all.
// Note this needs an explicit SetRect: a tview.Box that has never been
// laid out reports a nonzero default rect, not 0x0.
func TestPanelTickSkipsRenderWhenCollapsed(t *testing.T) {
	for _, tc := range []struct {
		name          string
		width, height int
	}{
		{"zero width", 0, 10},
		{"zero height", 20, 0},
		{"both zero", 0, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var got time.Duration
			p := newTestPanel(recordingViz{"Probe", &got})
			p.View().SetBorder(false)
			p.View().SetRect(0, 0, tc.width, tc.height)

			p.Tick(mpdclient.Status{})

			if got != 0 {
				t.Errorf("Render was called on a collapsed panel (elapsed=%v), want it skipped", got)
			}
		})
	}
}

// TestNamesReportsRegistryOrder pins Names to the cycle order rather than
// to any particular set of visualizations, so registering a new one
// doesn't fail this test.
func TestNamesReportsRegistryOrder(t *testing.T) {
	p := newTestPanel(fakeViz{"One"}, fakeViz{"Two"}, fakeViz{"Three"})

	want := []string{"One", "Two", "Three"}
	got := p.Names()
	if len(got) != len(want) {
		t.Fatalf("Names() = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("Names()[%d] = %q, want %q", i, got[i], want[i])
		}
	}

	// Names hands out a copy: mutating it must not reorder the registry.
	got[0] = "Mutated"
	if p.Names()[0] != "One" {
		t.Error("mutating the slice returned by Names() changed the registry")
	}
}

// TestNewStartsOnBalance pins the default. Balance is the default on
// purpose -- it is the one showing something about the music rather than
// about its loudness. Named explicitly here (rather than read off the
// registry) precisely because it is a deliberate choice, so reordering
// the registry has to fail this test rather than quietly change what
// users see.
func TestNewStartsOnBalance(t *testing.T) {
	p := New("")
	defer p.Close()

	if got := p.CurrentName(); got != "Balance" {
		t.Errorf("initial visualization = %q, want %q", got, "Balance")
	}
	if title := p.View().GetTitle(); !strings.Contains(title, "Balance") {
		t.Errorf("border title = %q, want it to contain %q", title, "Balance")
	}
}
