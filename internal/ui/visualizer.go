package ui

import (
	"strings"
	"time"

	"github.com/rivo/tview"

	"mpdtui/internal/mpdclient"
)

// Visualization is a single Now Playing visualization. To add a new one:
//
//  1. Create a new file internal/ui/viz_<name>.go with a type implementing
//     this interface (see viz_sonar.go for a worked example).
//  2. Register an instance of it in newVisualizerPanel's vizs slice,
//     below.
//
// That's the whole contract -- the container (visualizerPanel, in this
// file) handles sizing, the border/title, cycling, and re-rendering on
// every playback tick; a visualization only has to turn (width, height,
// elapsed, status) into `height` lines of text.
//
// Dimensions: the visualizer container occupies the right 50% of the Now
// Playing row (see app.go's build(), where nowPlayingRow splits a.nowPlaying
// and a.visualizer.view 50/50). That row is a fixed 4 rows tall including
// its border, so Render is called with height=2 in the current layout --
// deliberately compact, per an explicit "keep the current height" call
// over growing the row taller. width varies with terminal width (it's
// whatever's left after the 50/50 split minus 2 for the border), so
// Render must degrade gracefully at any width, including very narrow
// terminals.
//
// Data available: MPD does not expose real audio data to clients (no
// spectrum/FFT/waveform feed) -- there is no "microphone into the music"
// here. Every visualization is driven purely by playback state
// (mpdclient.Status: State/Volume/Elapsed/Duration/...) plus real elapsed
// wall-clock time, the same status already polled every ~500ms for the
// Now Playing bar. Don't design a visualization that assumes audio-
// reactive data it can't actually get.
type Visualization interface {
	// Name is shown right-aligned in the visualizer panel's border title
	// while this visualization is the active one.
	Name() string

	// Render returns exactly `height` lines of content, each rendered
	// (dynamic-color tags aside) to at most `width` visible columns --
	// the container does not clip or pad for you. elapsed is real
	// wall-clock time since the container was created (not a tick
	// counter), specifically so time-based effects (e.g. "every 2
	// seconds") stay accurate regardless of how often Render actually
	// gets called -- redraws happen not just on the ~500ms ticker but
	// also on every player/mixer/options event, so call frequency isn't
	// a reliable clock. st is the current playback status.
	Render(width, height int, elapsed time.Duration, st mpdclient.Status) []string
}

// visualizerPanel is the container: it owns the bordered TextView, the
// registry of available visualizations, which one is active, and the
// clock used for elapsed. It doesn't know how to draw any specific
// visualization -- that's entirely delegated to Visualization.Render.
type visualizerPanel struct {
	app     *App
	view    *tview.TextView
	vizs    []Visualization
	idx     int
	started time.Time
}

func newVisualizerPanel(app *App) *visualizerPanel {
	v := tview.NewTextView().SetDynamicColors(true)
	v.SetBorder(true).SetTitleAlign(tview.AlignRight)

	p := &visualizerPanel{
		app:     app,
		view:    v,
		started: time.Now(),
		// New visualizations are registered here, in the order 'v'
		// cycles through them.
		vizs: []Visualization{
			sonarVisualization{},
		},
	}
	p.view.SetTitle(" " + p.current().Name() + " ")
	return p
}

func (p *visualizerPanel) current() Visualization {
	return p.vizs[p.idx]
}

// next cycles to the next registered visualization (wrapping around) and
// updates the border title to match. A single registered visualization
// makes this a harmless no-op rather than a special case to guard against.
func (p *visualizerPanel) next() {
	p.idx = (p.idx + 1) % len(p.vizs)
	p.view.SetTitle(" " + p.current().Name() + " ")
}

// tick redraws the active visualization from the current playback status
// and elapsed wall-clock time. Called from refreshNowPlaying, so it
// shares that same ~500ms/event-driven cadence -- no separate ticker.
func (p *visualizerPanel) tick(st mpdclient.Status) {
	_, _, w, h := p.view.GetInnerRect()
	if w <= 0 || h <= 0 {
		return
	}
	lines := p.current().Render(w, h, time.Since(p.started), st)
	p.view.SetText(strings.Join(lines, "\n"))
}
