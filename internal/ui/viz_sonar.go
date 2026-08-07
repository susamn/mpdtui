package ui

import (
	"math"
	"strings"

	"mpdtui/internal/mpdclient"
)

// sonarVisualization draws a center dot with concentric rings expanding
// outward, fading as they travel, like a sonar ping -- see Visualization
// in visualizer.go for the container contract this implements.
//
// It's driven purely by frame (time) and play/pause state, per
// Visualization's contract: there's no real audio data to react to, so
// rings expand at a constant rate rather than pulsing with the music.
//
// Terminal character cells are roughly twice as tall as they are wide, so
// distances are corrected by sonarAspect when computing how far a cell is
// from the center -- without it, "rings" would render as ovals rather
// than circles. In the current layout (a fixed 2-row-tall visualizer, see
// visualizer.go's doc comment on Visualization) that correction still
// can't fully compensate: a ring's vertical reach tops out at 1 row very
// quickly, so it reads more like an expanding horizontal wave than a
// circle. Growing the Now Playing row's height (app.go's build(), the
// nowPlayingRow height argument) gives this more vertical room to
// actually look circular.
type sonarVisualization struct{}

func (sonarVisualization) Name() string { return "Sonar" }

const (
	// sonarAspect corrects for terminal cells being roughly twice as tall
	// as wide, so radial distance reads as circular rather than oval.
	sonarAspect = 2.0
	// sonarRingSpacing is how many frames apart each ring is born after
	// the last -- "waves... one after another" rather than a single
	// expanding ring.
	sonarRingSpacing = 3.0
	// sonarRingCount is how many rings are ever in flight at once.
	sonarRingCount = 3
	// sonarBandWidth is how close (in radius units) a cell has to be to a
	// ring's current radius to be drawn as part of that ring. Must be at
	// least ~1.12: with an even width and an even height (the common
	// case -- e.g. the container's actual 2-row height), no cell sits
	// exactly on the geometric center -- the nearest one is
	// sqrt(0.5^2 + (sonarAspect*0.5)^2) away. A smaller band width makes
	// the center dot (and any ring passing near it) skip cells entirely,
	// which is what produced a fully blank panel while paused.
	sonarBandWidth = 1.2
)

func (sonarVisualization) Render(width, height, frame int, st mpdclient.Status) []string {
	lines := make([]string, height)
	if width <= 0 || height <= 0 {
		return lines
	}

	// Idle: freeze on the center dot rather than animating from a stale
	// frame count while nothing is actually playing.
	t := float64(frame)
	if st.State != mpdclient.StatePlay {
		t = 0
	}

	cx := float64(width) / 2
	cy := float64(height) / 2
	maxRadius := float64(width) / 2
	if maxRadius < 1 {
		maxRadius = 1
	}
	period := maxRadius + sonarRingSpacing

	var b strings.Builder
	for y := 0; y < height; y++ {
		b.Reset()
		for x := 0; x < width; x++ {
			dx := float64(x) + 0.5 - cx
			dy := (float64(y) + 0.5 - cy) * sonarAspect
			r := math.Sqrt(dx*dx + dy*dy)

			if r < sonarBandWidth {
				b.WriteRune('●')
				continue
			}
			b.WriteRune(sonarRingGlyphAt(r, t, period, maxRadius))
		}
		lines[y] = b.String()
	}
	return lines
}

// sonarRingGlyphAt returns the glyph for a cell at radial distance r, or a
// space if no ring currently occupies it. Rings are phase-shifted copies
// of the same expand-then-restart cycle, sonarRingSpacing frames apart;
// a ring only "exists" (is drawn) once its phase has expanded past 0 and
// before it reaches maxRadius, which is what staggers their birth.
func sonarRingGlyphAt(r, t, period, maxRadius float64) rune {
	for k := 0; k < sonarRingCount; k++ {
		ringR := math.Mod(t-float64(k)*sonarRingSpacing, period)
		if ringR < 0 || ringR > maxRadius {
			continue // not born yet this cycle, or already past the edge
		}
		if math.Abs(r-ringR) < sonarBandWidth {
			return sonarRingGlyph(ringR / maxRadius)
		}
	}
	return ' '
}

// sonarRingGlyph picks a glyph by how far through its life a ring is
// (0 = just born at the center, 1 = about to vanish at the edge), so
// younger rings read as bolder/closer and older ones as fainter/farther.
func sonarRingGlyph(fraction float64) rune {
	switch {
	case fraction < 0.33:
		return '◉'
	case fraction < 0.66:
		return '○'
	default:
		return '·'
	}
}
