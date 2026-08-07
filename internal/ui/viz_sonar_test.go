package ui

import (
	"strings"
	"testing"

	"mpdtui/internal/mpdclient"
)

func TestSonarName(t *testing.T) {
	var sonar sonarVisualization
	if got := sonar.Name(); got != "Sonar" {
		t.Errorf("Name() = %q, want %q", got, "Sonar")
	}
}

func TestSonarRenderReturnsExactHeightLines(t *testing.T) {
	for _, h := range []int{0, 1, 2, 5} {
		lines := sonarVisualization{}.Render(20, h, 0, mpdclient.Status{})
		if len(lines) != h {
			t.Errorf("Render(20, %d, ...) returned %d lines, want %d", h, len(lines), h)
		}
	}
}

func TestSonarRenderHandlesZeroWidth(t *testing.T) {
	lines := sonarVisualization{}.Render(0, 3, 0, mpdclient.Status{})
	if len(lines) != 3 {
		t.Fatalf("Render(0, 3, ...) returned %d lines, want 3", len(lines))
	}
	for i, l := range lines {
		if l != "" {
			t.Errorf("Render(0, 3, ...) line %d = %q, want empty", i, l)
		}
	}
}

func TestSonarRenderDrawsCenterDot(t *testing.T) {
	// Odd width/height=1 puts an exact-center cell at x=width/2, y=0
	// (see the +0.5 centering in Render/sonarRingGlyphAt) -- frame 0
	// always places the sonar's origin there regardless of play state.
	lines := sonarVisualization{}.Render(5, 1, 0, mpdclient.Status{State: mpdclient.StatePlay})
	line := []rune(lines[0])
	if line[2] != '●' {
		t.Errorf("center cell = %q, want %q", string(line[2]), "●")
	}
}

// TestSonarRenderReachableAtEvenDimensions is a regression test: with an
// even width and an even height (e.g. the container's real 2-row height),
// no cell sits exactly on the geometric center, so a too-narrow band width
// made the center dot -- and thus the whole frozen/paused frame -- render
// as entirely blank. See sonarBandWidth's doc comment.
func TestSonarRenderReachableAtEvenDimensions(t *testing.T) {
	lines := sonarVisualization{}.Render(40, 2, 0, mpdclient.Status{State: mpdclient.StatePause})
	for _, l := range lines {
		if strings.ContainsRune(l, '●') {
			return
		}
	}
	t.Errorf("Render(40, 2, 0, paused) has no center dot in any line: %q", lines)
}

func TestSonarFreezesWhenNotPlaying(t *testing.T) {
	paused := mpdclient.Status{State: mpdclient.StatePause}
	frame0 := sonarVisualization{}.Render(20, 2, 0, paused)
	frame100 := sonarVisualization{}.Render(20, 2, 100, paused)

	for i := range frame0 {
		if frame0[i] != frame100[i] {
			t.Errorf("paused: line %d differs across frames: %q vs %q -- should be frozen", i, frame0[i], frame100[i])
		}
	}
}

func TestSonarAnimatesWhilePlaying(t *testing.T) {
	playing := mpdclient.Status{State: mpdclient.StatePlay}
	frame0 := sonarVisualization{}.Render(20, 2, 0, playing)
	frame5 := sonarVisualization{}.Render(20, 2, 5, playing)

	same := true
	for i := range frame0 {
		if frame0[i] != frame5[i] {
			same = false
		}
	}
	if same {
		t.Error("playing: rendered output identical across different frames -- expected the rings to animate")
	}
}
