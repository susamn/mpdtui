package ui

import (
	"strings"
	"testing"
	"time"

	"github.com/rivo/tview"

	"mpdtui/internal/mpdclient"
)

func TestBalanceName(t *testing.T) {
	var _ Visualization = newBalanceVisualization(nil)
	if got := newBalanceVisualization(nil).Name(); got != "Balance" {
		t.Errorf("Name() = %q, want %q", got, "Balance")
	}
}

func TestBalanceReturnsExactHeightLines(t *testing.T) {
	viz := newBalanceVisualization(nil)
	st := mpdclient.Status{State: mpdclient.StatePlay, Volume: 80}
	for _, h := range []int{0, 1, 2, 5} {
		if lines := viz.Render(30, h, time.Second, st); len(lines) != h {
			t.Errorf("Render(30, %d) returned %d lines, want %d", h, len(lines), h)
		}
	}
}

func TestBalanceVisibleWidthMatchesPanelWidth(t *testing.T) {
	// The container neither clips nor pads, so a row that doesn't come
	// out exactly `width` columns wide corrupts the panel border.
	viz := newBalanceVisualization(nil)
	st := mpdclient.Status{State: mpdclient.StatePlay, Volume: 90}

	for _, w := range []int{1, 3, 6, 11, 15, 25, 39, 40, 46, 80, 200} {
		for y, line := range viz.Render(w, 2, time.Second, st) {
			if got := tview.TaggedStringWidth(line); got != w {
				t.Errorf("width=%d row=%d: tagged width = %d, want %d (content: %q)", w, y, got, w, line)
			}
		}
	}
}

func TestBalanceZeroWidthIsEmpty(t *testing.T) {
	lines := newBalanceVisualization(nil).Render(0, 2, time.Second, mpdclient.Status{State: mpdclient.StatePlay, Volume: 80})
	if len(lines) != 2 || lines[0] != "" || lines[1] != "" {
		t.Errorf("Render(0, 2) = %q, want two empty lines", lines)
	}
}

func TestBalanceLayoutFitsTheWidth(t *testing.T) {
	for w := 1; w <= 200; w++ {
		bands, barWidth, gap := balanceLayout(w)
		if bands < 1 || barWidth < 1 {
			t.Fatalf("width=%d: layout(%d bands, %d wide, gap %d) has nothing to draw", w, bands, barWidth, gap)
		}
		if used := bands*barWidth + (bands-1)*gap; used > w {
			t.Errorf("width=%d: layout uses %d columns, more than the panel has", w, used)
		}
		if bands > balanceMaxBands {
			t.Errorf("width=%d: %d bands, more than the %d-band maximum", w, bands, balanceMaxBands)
		}
	}
}

func TestBalanceLayoutKeepsBarsWideEnoughToCompare(t *testing.T) {
	// The whole display is a comparison between bars, so at any width
	// with room for it, bar width is preserved at the expense of band
	// count -- not the other way around.
	for _, w := range []int{20, 30, 46, 80} {
		bands, barWidth, _ := balanceLayout(w)
		if barWidth < balanceMinBarWidth {
			t.Errorf("width=%d: bar width %d is below the %d-column minimum (%d bands)", w, barWidth, balanceMinBarWidth, bands)
		}
	}
}

func TestBalanceDeviationsIgnoreOverallLoudness(t *testing.T) {
	// The property the whole visualization exists for: turning the
	// track up moves every band together, and must not move the
	// display at all.
	quiet := []float64{-50, -44, -47, -60, -55}
	loud := make([]float64, len(quiet))
	for i, v := range quiet {
		loud[i] = v + 18 // same balance, 18dB louder
	}

	a := balanceDeviations(quiet)
	b := balanceDeviations(loud)
	for i := range a {
		if a[i] != b[i] {
			t.Errorf("band %d deviation changed with overall level: %g vs %g", i, a[i], b[i])
		}
	}
}

func TestBalanceDeviationsSumToZero(t *testing.T) {
	sum := 0.0
	for _, v := range balanceDeviations([]float64{-30, -55, -42, -61, -38, -50}) {
		sum += v
	}
	if sum > 1e-9 || sum < -1e-9 {
		t.Errorf("deviations sum to %g, want 0 -- they're distances from their own mean", sum)
	}
}

func TestBalanceLevelPutsAverageAtMidHeight(t *testing.T) {
	// A band exactly at the mean has to land on the row boundary at the
	// panel's 2-row height: that's what makes "full bottom row, empty
	// top row" readable as average at a glance.
	const maxLevel = 16
	if got := balanceLevel(0, maxLevel); got != maxLevel/2 {
		t.Errorf("balanceLevel(0, %d) = %d, want %d", maxLevel, got, maxLevel/2)
	}
	if got := balanceLevel(balanceRangeDB, maxLevel); got != maxLevel {
		t.Errorf("balanceLevel(+range) = %d, want a full bar (%d)", got, maxLevel)
	}
	if got := balanceLevel(-balanceRangeDB, maxLevel); got != 0 {
		t.Errorf("balanceLevel(-range) = %d, want an empty bar", got)
	}
	// Beyond the range, bars clamp rather than overflowing the glyphs.
	if got := balanceLevel(4*balanceRangeDB, maxLevel); got != maxLevel {
		t.Errorf("balanceLevel(far above range) = %d, want %d", got, maxLevel)
	}
	if got := balanceLevel(-4*balanceRangeDB, maxLevel); got != 0 {
		t.Errorf("balanceLevel(far below range) = %d, want 0", got)
	}
}

func TestBalanceLevelIsMonotonic(t *testing.T) {
	prev := -1
	for d := -balanceRangeDB; d <= balanceRangeDB; d += 0.5 {
		got := balanceLevel(d, 16)
		if got < prev {
			t.Fatalf("balanceLevel(%g) = %d, lower than the previous deviation's %d", d, got, prev)
		}
		prev = got
	}
}

func TestBalanceColorSignalsDirection(t *testing.T) {
	// Colour carries the above/below reading independently of height,
	// which matters because most bars sit near the middle where heights
	// are similar.
	above := balanceColor(balanceRangeDB * 0.8)
	middle := balanceColor(0)
	below := balanceColor(-balanceRangeDB * 0.8)

	if above == middle || below == middle || above == below {
		t.Errorf("colours don't distinguish direction: above=%s middle=%s below=%s", above, middle, below)
	}
	// Far outside the calibrated range still resolves to a colour
	// rather than falling off the table.
	if balanceColor(-100) == "" || balanceColor(100) == "" {
		t.Error("extreme deviations produced an empty colour")
	}
}

func TestBalanceBlanksWhenNotPlaying(t *testing.T) {
	viz := newBalanceVisualization(nil)
	for _, st := range []mpdclient.Status{
		{State: mpdclient.StatePause, Volume: 80},
		{State: mpdclient.StateStop, Volume: 80},
		{State: mpdclient.StatePlay, Volume: 0},  // muted
		{State: mpdclient.StatePlay, Volume: -1}, // unknown volume
	} {
		for y, line := range viz.Render(40, 2, time.Second, st) {
			if strings.TrimSpace(stripColorTags(line)) != "" {
				t.Errorf("state=%v volume=%d: row %d = %q, want blank", st.State, st.Volume, y, line)
			}
		}
	}
}

func TestBalanceFallsBackToSimulationWithoutAudio(t *testing.T) {
	// No fifo: still shows the same kind of display rather than a blank
	// panel, so the visualization is meaningful without MPD's fifo
	// output configured.
	viz := newBalanceVisualization(nil)
	playing := mpdclient.Status{State: mpdclient.StatePlay, Volume: 80}

	at0 := stripColorTags(viz.Render(46, 2, 0, playing)[1])
	at1s := stripColorTags(viz.Render(46, 2, time.Second, playing)[1])

	if strings.TrimSpace(at0) == "" {
		t.Error("blank output without a spectrum -- expected the simulated fallback")
	}
	if at0 == at1s {
		t.Error("fallback output identical across elapsed times -- expected animation")
	}
}

func TestBalanceDrawsRealSpectrumWhenAudioIsLive(t *testing.T) {
	// A 60Hz tone is far above average in the bass band and far below
	// it everywhere else, so the leftmost bar must be full and the
	// rightmost empty -- the clearest possible case of the display
	// showing balance rather than level.
	viz := newBalanceVisualization(startTestSpectrum(t, 60))

	const width = 46
	lines := viz.Render(width, 2, 0, mpdclient.Status{State: mpdclient.StatePlay, Volume: 100})
	plain := make([]string, len(lines))
	for i, l := range lines {
		plain[i] = stripColorTags(l)
	}
	filled := filledColumns(plain, width)

	bands, barWidth, gap := balanceLayout(width)
	padLeft := (width - (bands*barWidth + (bands-1)*gap)) / 2

	// The top row only holds above-average bands, so the bass band must
	// be present there and the treble band must not.
	topRow := []rune(plain[0])
	bassCol := padLeft
	trebleCol := padLeft + (bands-1)*(barWidth+gap)
	if topRow[bassCol] == ' ' {
		t.Errorf("bass band not above average for a 60Hz tone: %q", plain)
	}
	if topRow[trebleCol] != ' ' {
		t.Errorf("treble band above average for a 60Hz tone: %q", plain)
	}
	if !filled[bassCol] {
		t.Errorf("bass band empty for a 60Hz tone: %q", plain)
	}
}
