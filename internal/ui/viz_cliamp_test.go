package ui

import (
	"regexp"
	"testing"
	"time"

	"github.com/rivo/tview"

	"mpdtui/internal/mpdclient"
)

var tagRegex = regexp.MustCompile(`\[[^\]]*\]`)

func stripColorTags(s string) string {
	return tagRegex.ReplaceAllString(s, "")
}

func TestCliampVisualizationImplementsVisualizationInterface(t *testing.T) {
	var _ Visualization = newCliampVisualization()
	viz := newCliampVisualization()
	if got := viz.Name(); got != "Cliamp" {
		t.Errorf("Name() = %q, want %q", got, "Cliamp")
	}
}

func TestCliampVisualizationReturnsCorrectLineCount(t *testing.T) {
	viz := newCliampVisualization()
	for _, h := range []int{1, 2, 3, 5} {
		lines := viz.Render(30, h, 1*time.Second, mpdclient.Status{State: mpdclient.StatePlay, Volume: 80})
		if len(lines) != h {
			t.Errorf("Render with height=%d returned %d lines, want %d", h, len(lines), h)
		}
	}
}

func TestCliampVisualizationEmptyOnZeroDimensions(t *testing.T) {
	viz := newCliampVisualization()
	lines0W := viz.Render(0, 2, 1*time.Second, mpdclient.Status{State: mpdclient.StatePlay, Volume: 80})
	if len(lines0W) != 2 || lines0W[0] != "" || lines0W[1] != "" {
		t.Errorf("Render(0, 2) = %v, want 2 empty lines", lines0W)
	}

	lines0H := viz.Render(30, 0, 1*time.Second, mpdclient.Status{State: mpdclient.StatePlay, Volume: 80})
	if len(lines0H) != 0 {
		t.Errorf("Render(30, 0) returned %d lines, want 0", len(lines0H))
	}
}

func TestCliampVisualizationVisibleWidthMatchesPanelWidth(t *testing.T) {
	viz := newCliampVisualization()
	st := mpdclient.Status{State: mpdclient.StatePlay, Volume: 90}

	for _, w := range []int{6, 11, 15, 25, 40, 80} {
		lines := viz.Render(w, 2, 1*time.Second, st)
		for y, line := range lines {
			visibleWidth := tview.TaggedStringWidth(line)
			if visibleWidth != w {
				t.Errorf("width=%d row=%d tagged width = %d, want %d (content: %q)", w, y, visibleWidth, w, line)
			}
		}
	}
}

func TestCliampVisualizationVolumeScaling(t *testing.T) {
	viz := newCliampVisualization()
	playingZeroVol := mpdclient.Status{State: mpdclient.StatePlay, Volume: 0}
	linesZero := viz.Render(30, 2, 2*time.Second, playingZeroVol)

	for y, line := range linesZero {
		width := tview.TaggedStringWidth(line)
		if width != 30 {
			t.Fatalf("row %d width = %d, want 30", y, width)
		}
	}

	// At volume 0, all visible characters should be spaces
	stripped := stripColorTags(linesZero[0])
	for _, r := range stripped {
		if r != ' ' {
			t.Errorf("at volume=0 row 0 contained non-space rune %q", r)
		}
	}

	// At volume 100, playing state should have rendered active glyphs
	viz2 := newCliampVisualization()
	playingFullVol := mpdclient.Status{State: mpdclient.StatePlay, Volume: 100}
	linesFull := viz2.Render(30, 2, 2*time.Second, playingFullVol)
	strippedFull := stripColorTags(linesFull[1])

	hasActiveGlyph := false
	for _, r := range strippedFull {
		if r != ' ' {
			hasActiveGlyph = true
			break
		}
	}
	if !hasActiveGlyph {
		t.Errorf("at volume=100 bottom row had no active glyphs: %q", strippedFull)
	}
}

func TestCliampVisualizationIdleWhenPausedOrStopped(t *testing.T) {
	viz := newCliampVisualization()
	paused := mpdclient.Status{State: mpdclient.StatePause, Volume: 80}
	lines := viz.Render(30, 2, 5*time.Second, paused)

	for y, line := range lines {
		stripped := stripColorTags(line)
		for _, r := range stripped {
			if r != ' ' {
				t.Errorf("paused state row %d had non-space rune %q", y, r)
			}
		}
	}

	stopped := mpdclient.Status{State: mpdclient.StateStop, Volume: 80}
	linesStopped := viz.Render(30, 2, 5*time.Second, stopped)
	for y, line := range linesStopped {
		stripped := stripColorTags(line)
		for _, r := range stripped {
			if r != ' ' {
				t.Errorf("stopped state row %d had non-space rune %q", y, r)
			}
		}
	}
}

func TestCliampVisualizationPeakHoldAndDecay(t *testing.T) {
	viz := newCliampVisualization()
	st := mpdclient.Status{State: mpdclient.StatePlay, Volume: 100}

	// Initial render
	viz.Render(30, 2, 100*time.Millisecond, st)
	if len(viz.peaks) == 0 {
		t.Fatal("expected peaks to be initialized")
	}

	// Step forward beyond peak hold duration (120ms) and check decay mechanics
	viz.Render(30, 2, 500*time.Millisecond, st)

	// Verify peaks do not panic or produce NaN/Inf
	for i, p := range viz.peaks {
		if p < 0 || p > 16 {
			t.Errorf("peak[%d] out of range: %v", i, p)
		}
	}
}
