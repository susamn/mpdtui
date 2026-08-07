package ui

import (
	"math"
	"strings"
	"time"

	"mpdtui/internal/mpdclient"
)

// sonarVisualization draws a single circular wave expanding outward from
// the center: it grows linearly from radius 0 to half the available
// width over sonarWaveInterval, then disappears exactly as the next wave
// begins -- one wave in flight at a time, not concentric rings -- see
// Visualization in visualizer.go for the container contract this
// implements.
//
// It's driven by real elapsed time rather than a redraw counter,
// specifically so "a new wave every 2 seconds" holds regardless of how
// often the container actually redraws.
//
// Terminal character cells are roughly twice as tall as they are wide, so
// distances are corrected by sonarAspect when computing how far a cell is
// from the center -- without it, the wave would render as an oval rather
// than a circle. In the current layout (a fixed 2-row-tall visualizer,
// see visualizer.go's doc comment on Visualization) that correction still
// can't fully compensate: the wave's vertical reach tops out at 1 row
// very quickly, so it reads more like an expanding horizontal wave than a
// circle. Growing the Now Playing row's height (app.go's build(), the
// nowPlayingRow height argument) gives this more vertical room to
// actually look circular.
type sonarVisualization struct{}

func (sonarVisualization) Name() string { return "Sonar" }

const (
	// sonarAspect corrects for terminal cells being roughly twice as tall
	// as wide, so radial distance reads as circular rather than oval.
	sonarAspect = 2.0
	// sonarWaveInterval is both how often a new wave starts and how long
	// one takes to travel from the center to the edge -- so the previous
	// wave vanishes at the edge in the same instant the next one begins.
	sonarWaveInterval = 2 * time.Second
	// sonarLineWidth is how close (in radius units) a cell has to be to
	// the wave's current radius to be drawn as part of it. Must be at
	// least ~1.12: with an even width and an even height (the common
	// case -- e.g. the container's actual 2-row height), no cell sits
	// exactly on any given radius -- the nearest one is
	// sqrt(0.5^2 + (sonarAspect*0.5)^2) away. A smaller line width makes
	// the wave skip cells entirely, including at radius 0 right after it
	// spawns, which can render as a fully blank frame.
	sonarLineWidth = 1.2
)

func (sonarVisualization) Render(width, height int, elapsed time.Duration, st mpdclient.Status) []string {
	lines := make([]string, height)
	if width <= 0 || height <= 0 {
		return lines
	}

	// Idle: freeze the wave at the center (radius 0) rather than
	// animating from a stale elapsed time while nothing is playing.
	t := elapsed
	if st.State != mpdclient.StatePlay {
		t = 0
	}

	cx := float64(width) / 2
	cy := float64(height) / 2
	maxRadius := float64(width) / 2
	if maxRadius < 1 {
		maxRadius = 1
	}

	intervalSeconds := sonarWaveInterval.Seconds()
	phase := math.Mod(t.Seconds(), intervalSeconds)
	if phase < 0 {
		phase = 0
	}
	waveRadius := phase / intervalSeconds * maxRadius

	var b strings.Builder
	for y := 0; y < height; y++ {
		b.Reset()
		for x := 0; x < width; x++ {
			dx := float64(x) + 0.5 - cx
			dy := (float64(y) + 0.5 - cy) * sonarAspect
			r := math.Sqrt(dx*dx + dy*dy)

			if math.Abs(r-waveRadius) < sonarLineWidth {
				b.WriteRune('○')
			} else {
				b.WriteRune(' ')
			}
		}
		lines[y] = b.String()
	}
	return lines
}
