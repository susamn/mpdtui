package ui

import (
	"math"
	"time"

	"mpdtui/internal/mpdclient"
)

// equalizerVisualization draws a dense, bar-per-column equalizer that
// undulates over time -- see Visualization in visualizer.go for the
// container contract this implements.
//
// This is decorative, not a real spectrum analyzer: MPD gives clients no
// audio data to actually analyze (see Visualization's doc comment on data
// availability), so each bar's height comes from overlapping sine waves
// phase-shifted per column, not from the music itself. The one genuinely
// real signal driving it is volume (mpdclient.Status.Volume), which
// scales how tall bars can get -- turning the volume down visibly calms
// the whole display, turning it up lets bars reach the top.
//
// The container's actual height is 2 rows (see Visualization's doc
// comment on dimensions), which would normally mean only 3 possible bar
// heights (0/1/2 rows). equalizerBlocks works around that using eighth-
// block Unicode glyphs (▁▂▃▄▅▆▇█) to render a fractional row height
// within a single cell, giving 8 additional levels per row -- 16 usable
// levels total out of just 2 rows, filled from the bottom row up (see
// equalizerColumnGlyphs).
type equalizerVisualization struct{}

func (equalizerVisualization) Name() string { return "Equalizer" }

// equalizerBlocks are eighth-block glyphs, index 0 (empty) through 8
// (a full row).
var equalizerBlocks = []rune{' ', '▁', '▂', '▃', '▄', '▅', '▆', '▇', '█'}

const (
	// equalizerSpeed and equalizerSpread shape the undulation: how fast
	// bars pulse over time, and how much each column's phase is offset
	// from its neighbor's -- together they're what makes it read as
	// motion travelling across the bars rather than every bar pulsing in
	// lockstep.
	equalizerSpeed  = 2.2 // radians/second
	equalizerSpread = 0.5 // radians/column
)

func (equalizerVisualization) Render(width, height int, elapsed time.Duration, st mpdclient.Status) []string {
	lines := make([]string, height)
	if width <= 0 || height <= 0 {
		return lines
	}

	// Idle: freeze mid-wave rather than animating from a stale elapsed
	// time while nothing is playing.
	t := elapsed.Seconds()
	if st.State != mpdclient.StatePlay {
		t = 0
	}

	volumeScale := float64(st.Volume) / 100
	if st.Volume < 0 {
		volumeScale = 0 // unknown volume: don't fabricate a level
	}
	maxLevel := height * 8

	rows := make([][]rune, height)
	for y := range rows {
		rows[y] = make([]rune, width)
	}
	for x := 0; x < width; x++ {
		level := equalizerLevel(t, x, volumeScale, maxLevel)
		glyphs := equalizerColumnGlyphs(level, height)
		for y := 0; y < height; y++ {
			rows[y][x] = glyphs[y]
		}
	}

	for y := 0; y < height; y++ {
		lines[y] = string(rows[y])
	}
	return lines
}

// equalizerLevel returns column x's bar height in eighth-block units
// (0..maxLevel) at time t seconds, scaled by volumeScale (0..1). Two sine
// waves at different speeds/phases per column are summed so neighboring
// columns don't pulse in lockstep, giving the appearance of motion
// travelling across the bars.
func equalizerLevel(t float64, x int, volumeScale float64, maxLevel int) int {
	wave := 0.6*math.Sin(t*equalizerSpeed+float64(x)*equalizerSpread) +
		0.4*math.Sin(t*equalizerSpeed*1.7+float64(x)*equalizerSpread*0.6)
	norm := (wave + 1) / 2 // 0..1
	return int(norm * volumeScale * float64(maxLevel))
}

// equalizerColumnGlyphs splits level (0..height*8) into one glyph per
// row, filled from the bottom row upward -- factored out from Render so
// the row-splitting logic is testable independent of the wave formula
// that produces level.
func equalizerColumnGlyphs(level, height int) []rune {
	glyphs := make([]rune, height)
	for y := height - 1; y >= 0; y-- {
		l := level
		if l > 8 {
			l = 8
		}
		if l < 0 {
			l = 0
		}
		glyphs[y] = equalizerBlocks[l]
		level -= 8
	}
	return glyphs
}
