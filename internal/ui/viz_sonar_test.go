package ui

import (
	"strings"
	"testing"
	"time"

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

func TestSonarRenderDrawsWaveAtCenterWhenJustSpawned(t *testing.T) {
	// Odd width/height=1 puts an exact-center cell at x=width/2, y=0
	// (see the +0.5 centering in Render) -- elapsed=0 always places the
	// wave's origin there regardless of play state.
	lines := sonarVisualization{}.Render(5, 1, 0, mpdclient.Status{State: mpdclient.StatePlay})
	line := []rune(lines[0])
	if line[2] != '○' {
		t.Errorf("center cell = %q, want %q", string(line[2]), "○")
	}
}

// TestSonarRenderReachableAtEvenDimensions is a regression test: with an
// even width and an even height (e.g. the container's real 2-row height),
// no cell sits exactly on the geometric center, so a too-narrow line
// width made a just-spawned wave -- and thus the whole frozen/paused
// frame -- render as entirely blank. See sonarLineWidth's doc comment.
func TestSonarRenderReachableAtEvenDimensions(t *testing.T) {
	lines := sonarVisualization{}.Render(40, 2, 0, mpdclient.Status{State: mpdclient.StatePause})
	for _, l := range lines {
		if strings.ContainsRune(l, '○') {
			return
		}
	}
	t.Errorf("Render(40, 2, 0, paused) has no wave in any line: %q", lines)
}

func TestSonarFreezesWhenNotPlaying(t *testing.T) {
	paused := mpdclient.Status{State: mpdclient.StatePause}
	at0 := sonarVisualization{}.Render(20, 2, 0, paused)
	at100s := sonarVisualization{}.Render(20, 2, 100*time.Second, paused)

	for i := range at0 {
		if at0[i] != at100s[i] {
			t.Errorf("paused: line %d differs across elapsed times: %q vs %q -- should be frozen", i, at0[i], at100s[i])
		}
	}
}

func TestSonarAnimatesWhilePlaying(t *testing.T) {
	playing := mpdclient.Status{State: mpdclient.StatePlay}
	at0 := sonarVisualization{}.Render(20, 2, 0, playing)
	at1s := sonarVisualization{}.Render(20, 2, 1*time.Second, playing)

	same := true
	for i := range at0 {
		if at0[i] != at1s[i] {
			same = false
		}
	}
	if same {
		t.Error("playing: rendered output identical at different elapsed times -- expected the wave to animate")
	}
}

func TestSonarWaveReachesTheEdgeJustBeforeTheInterval(t *testing.T) {
	// height=1 keeps the vertical aspect correction out of the way (row 0
	// is exactly on the center line), isolating the horizontal reach.
	playing := mpdclient.Status{State: mpdclient.StatePlay}
	width := 20

	justBeforeEdge := sonarVisualization{}.Render(width, 1, sonarWaveInterval-50*time.Millisecond, playing)
	line := []rune(justBeforeEdge[0])
	if line[0] != '○' && line[width-1] != '○' {
		t.Errorf("just before the interval elapses, want the wave near the outer columns, got %q", justBeforeEdge[0])
	}

	justSpawned := sonarVisualization{}.Render(width, 1, 0, playing)
	line = []rune(justSpawned[0])
	if line[0] == '○' || line[width-1] == '○' {
		t.Errorf("a just-spawned wave (elapsed=0) should not already be at the outer columns, got %q", justSpawned[0])
	}
}

func TestSonarWaveRestartsExactlyAtTheInterval(t *testing.T) {
	// The previous wave should vanish at the edge in the same instant the
	// next one begins -- i.e. the frame at exactly one interval in should
	// be identical to the frame at elapsed=0.
	playing := mpdclient.Status{State: mpdclient.StatePlay}
	at0 := sonarVisualization{}.Render(20, 2, 0, playing)
	atOneInterval := sonarVisualization{}.Render(20, 2, sonarWaveInterval, playing)

	for i := range at0 {
		if at0[i] != atOneInterval[i] {
			t.Errorf("line %d at elapsed=0 = %q, at elapsed=one interval = %q, want them equal (wave restarted)", i, at0[i], atOneInterval[i])
		}
	}
}
