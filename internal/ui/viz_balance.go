package ui

import (
	"fmt"
	"strings"
	"time"

	"mpdtui/internal/audio"
	"mpdtui/internal/mpdclient"
)

// balanceVisualization draws a small number of wide, octave-ish bands
// showing each one's level *relative to the average across all of them*,
// rather than its absolute loudness -- see Visualization in
// visualizer.go for the container contract this implements.
//
// The other two visualizations draw absolute levels, which means the
// largest thing moving on screen is overall loudness: as a track gets
// louder every bar rises together, and that shared movement is bigger
// than the differences between bands. Those differences -- the spectral
// balance, bass pulling ahead of the mids, a cymbal lifting the top end
// -- are what actually distinguishes one moment of a track from another,
// and on an absolute display they're the small signal riding on the big
// one.
//
// So this subtracts the common movement out. Each frame, the mean band
// level is computed and every bar is drawn as its own deviation from
// that mean. Turning the whole track up moves every band equally and so
// changes nothing on screen; only the balance between bands does. The
// geometry falls out nicely at the panel's 2-row height (see
// Visualization's doc comment on dimensions): the mean lands exactly on
// the row boundary, so a full bottom row and empty top row reads as
// "average", anything reaching into the top row is louder than average,
// and a partial bottom row is quieter than average.
//
// Bands are deliberately few and wide. Narrow bands over real music are
// strongly correlated with their neighbours, so a wide display of them
// shows the same signal many times over with per-band noise on top; a
// band spanning roughly an octave averages that noise down and moves
// distinguishably from the band beside it.
type balanceVisualization struct {
	// spectrum may be nil (tests construct it that way), which behaves
	// exactly like a feed that is never active: the simulation is used.
	spectrum *audio.Spectrum
}

func newBalanceVisualization(spectrum *audio.Spectrum) balanceVisualization {
	return balanceVisualization{spectrum: spectrum}
}

func (balanceVisualization) Name() string { return "Balance" }

const (
	// balanceMaxBands is the target band count at a comfortable width.
	// internal/audio analyzes 40Hz-16kHz, a bit under nine octaves, so
	// ten bands is close to one octave each -- the width at which a band
	// stops tracking its neighbour.
	balanceMaxBands = 10
	balanceMinBands = 4

	// balanceMinBarWidth is how wide a bar has to be for the display to
	// stay readable. Bars this wide are what make the between-band
	// comparison easy to see at a glance, so at narrow terminal widths
	// bands are given up before bar width is.
	balanceMinBarWidth = 3

	// balanceRangeDB is how far above or below the mean a band has to
	// sit to reach a full or empty bar. Measured against real playback
	// rather than guessed: band deviations came out with a median of
	// ~6dB and a 95th percentile of ~14dB, so 15dB puts the typical bar
	// at around 70% height -- using most of the range -- while clipping
	// under 4% of bars. Tightening it to 11dB, which looks livelier in
	// isolation, clips a fifth of all bars, and a clipped bar is
	// precisely where the relative movement stops being visible.
	balanceRangeDB = 15.0
)

// balanceColors shade a bar by how far it sits from the mean -- deep
// blue for well below, neutral through the middle, warm for well above,
// so direction reads without having to compare heights.
var balanceColors = []struct {
	minDeviation float64 // as a fraction of balanceRangeDB
	color        string
}{
	{0.55, "#ff6d00"},  // well above average
	{0.2, "#ffd600"},   // above
	{-0.2, "#00e676"},  // near the mean
	{-0.55, "#00b0ff"}, // below
	{-1.01, "#3d5afe"}, // well below average
}

func (b balanceVisualization) Render(width, height int, elapsed time.Duration, st mpdclient.Status) []string {
	lines := make([]string, height)
	if width <= 0 || height <= 0 {
		return lines
	}

	numBands, barWidth, gap := balanceLayout(width)

	// Muted or unknown volume blanks the display. This visualization
	// deliberately doesn't scale with volume the way the others do --
	// scaling would reintroduce exactly the common-mode movement it
	// exists to remove, since spectral balance doesn't change when you
	// turn the volume down. But a muted player showing a dancing
	// display would be plainly wrong, so zero is a special case.
	silent := st.State != mpdclient.StatePlay || st.Volume <= 0

	var deviations []float64
	if !silent {
		db := b.spectrum.BandsDB(numBands)
		if db == nil {
			// No live audio: run the same deviation display over the
			// simulated spectrum, so a setup without MPD's fifo output
			// sees the same kind of picture rather than a blank panel.
			db = balanceSimulatedDB(elapsed.Seconds(), numBands)
		}
		deviations = balanceDeviations(db)
	}

	maxLevel := height * 8
	padLeft := (width - (numBands*barWidth + (numBands-1)*gap)) / 2
	if padLeft < 0 {
		padLeft = 0
	}

	for y := 0; y < height; y++ {
		var row strings.Builder
		row.WriteString(strings.Repeat(" ", padLeft))
		rowBase := (height - 1 - y) * 8

		for i := 0; i < numBands; i++ {
			if i > 0 && gap > 0 {
				row.WriteString(strings.Repeat(" ", gap))
			}

			level := 0
			if deviations != nil {
				level = balanceLevel(deviations[i], maxLevel)
			}

			filled := level - rowBase
			if filled > 8 {
				filled = 8
			}
			if filled <= 0 {
				row.WriteString(strings.Repeat(" ", barWidth))
				continue
			}
			glyph := strings.Repeat(string(equalizerBlocks[filled]), barWidth)
			row.WriteString(fmt.Sprintf("[%s]%s[-]", balanceColor(deviations[i]), glyph))
		}

		// Pad to the full width rather than computing a right pad from
		// padLeft: odd leftovers from the integer division above have to
		// land somewhere, and the container doesn't pad for us.
		written := padLeft + numBands*barWidth + (numBands-1)*gap
		if written < width {
			row.WriteString(strings.Repeat(" ", width-written))
		}
		lines[y] = row.String()
	}
	return lines
}

// balanceLayout picks how many bands to draw at a given width, and how
// wide each bar and the gaps between them are. Bands are given up before
// bar width is: this display is about comparing bars to each other, and
// single-column bars are hard to compare at a glance.
func balanceLayout(width int) (bands, barWidth, gap int) {
	gap = 1
	if width < balanceMinBands*(balanceMinBarWidth+1) {
		gap = 0
	}

	bands = balanceMaxBands
	for bands > balanceMinBands && (width-(bands-1)*gap)/bands < balanceMinBarWidth {
		bands--
	}

	barWidth = (width - (bands-1)*gap) / bands
	if barWidth < 1 {
		// Narrower than even one column per band: drop bands until
		// something fits, and accept single-column bars.
		barWidth = 1
		gap = 0
		bands = width
		if bands < 1 {
			bands = 1
		}
	}
	return bands, barWidth, gap
}

// balanceDeviations converts band decibels into each band's distance
// from the mean of that same frame -- the subtraction that removes
// overall loudness from the display.
func balanceDeviations(db []float64) []float64 {
	if len(db) == 0 {
		return nil
	}
	mean := 0.0
	for _, v := range db {
		mean += v
	}
	mean /= float64(len(db))

	out := make([]float64, len(db))
	for i, v := range db {
		out[i] = v - mean
	}
	return out
}

// balanceLevel maps a deviation in decibels onto a bar height in
// eighth-block units, with a deviation of zero landing at exactly half
// the available height -- the row boundary, at the panel's usual 2 rows.
func balanceLevel(deviationDB float64, maxLevel int) int {
	norm := 0.5 + deviationDB/(2*balanceRangeDB)
	if norm < 0 {
		norm = 0
	}
	if norm > 1 {
		norm = 1
	}
	return int(norm * float64(maxLevel))
}

func balanceColor(deviationDB float64) string {
	frac := deviationDB / balanceRangeDB
	for _, c := range balanceColors {
		if frac >= c.minDeviation {
			return c.color
		}
	}
	return balanceColors[len(balanceColors)-1].color
}

// balanceSimulatedDB is the fallback source: it reuses Cliamp's
// simulated band shape (bass-weighted left, flutter at the top end) and
// puts it on the decibel scale BandsDB would have returned, so the
// deviation display downstream is identical either way.
func balanceSimulatedDB(t float64, bands int) []float64 {
	out := make([]float64, bands)
	for i := range out {
		level := cliampBandLevel(t, i, bands, 1, 1)
		out[i] = audio.DBFromLevel(level)
	}
	return out
}
