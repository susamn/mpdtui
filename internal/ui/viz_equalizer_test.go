package ui

import (
	"strings"
	"testing"
	"time"

	"mpdtui/internal/mpdclient"
)

func TestEqualizerName(t *testing.T) {
	var eq equalizerVisualization
	if got := eq.Name(); got != "Equalizer" {
		t.Errorf("Name() = %q, want %q", got, "Equalizer")
	}
}

func TestEqualizerRenderReturnsExactHeightLines(t *testing.T) {
	for _, h := range []int{0, 1, 2, 5} {
		lines := equalizerVisualization{}.Render(20, h, 0, mpdclient.Status{})
		if len(lines) != h {
			t.Errorf("Render(20, %d, ...) returned %d lines, want %d", h, len(lines), h)
		}
	}
}

func TestEqualizerRenderHandlesZeroWidth(t *testing.T) {
	lines := equalizerVisualization{}.Render(0, 3, 0, mpdclient.Status{})
	if len(lines) != 3 {
		t.Fatalf("Render(0, 3, ...) returned %d lines, want 3", len(lines))
	}
	for i, l := range lines {
		if l != "" {
			t.Errorf("Render(0, 3, ...) line %d = %q, want empty", i, l)
		}
	}
}

func TestEqualizerZeroVolumeIsSilent(t *testing.T) {
	playing := mpdclient.Status{State: mpdclient.StatePlay, Volume: 0}
	// Sample a few different elapsed times -- volume 0 should mute the
	// display regardless of where the wave would otherwise be.
	for _, elapsed := range []time.Duration{0, 300 * time.Millisecond, 2 * time.Second} {
		lines := equalizerVisualization{}.Render(40, 2, elapsed, playing)
		for i, l := range lines {
			if strings.Trim(l, " ") != "" {
				t.Errorf("elapsed=%v: line %d = %q, want all spaces at volume 0", elapsed, i, l)
			}
		}
	}
}

func TestEqualizerFreezesWhenNotPlaying(t *testing.T) {
	paused := mpdclient.Status{State: mpdclient.StatePause, Volume: 80}
	at0 := equalizerVisualization{}.Render(30, 2, 0, paused)
	at100s := equalizerVisualization{}.Render(30, 2, 100*time.Second, paused)

	for i := range at0 {
		if at0[i] != at100s[i] {
			t.Errorf("paused: line %d differs across elapsed times: %q vs %q -- should be frozen", i, at0[i], at100s[i])
		}
	}
}

func TestEqualizerAnimatesWhilePlaying(t *testing.T) {
	playing := mpdclient.Status{State: mpdclient.StatePlay, Volume: 80}
	at0 := equalizerVisualization{}.Render(30, 2, 0, playing)
	at1s := equalizerVisualization{}.Render(30, 2, 1*time.Second, playing)

	same := true
	for i := range at0 {
		if at0[i] != at1s[i] {
			same = false
		}
	}
	if same {
		t.Error("playing: rendered output identical at different elapsed times -- expected the bars to animate")
	}
}

func TestEqualizerLevelScalesWithVolume(t *testing.T) {
	// At t=0, x=0 the wave formula evaluates to exactly the midpoint
	// (norm=0.5), giving exact, deterministic expected levels for a few
	// volume scales.
	cases := []struct {
		volumeScale float64
		maxLevel    int
		want        int
	}{
		{0, 16, 0},
		{0.5, 16, 4},
		{1, 16, 8},
	}
	for _, tc := range cases {
		if got := equalizerLevel(0, 0, tc.volumeScale, tc.maxLevel); got != tc.want {
			t.Errorf("equalizerLevel(0, 0, %v, %d) = %d, want %d", tc.volumeScale, tc.maxLevel, got, tc.want)
		}
	}
}

func TestEqualizerColumnGlyphsFillsBottomRowFirst(t *testing.T) {
	// glyphs[0] is the top row, glyphs[1] the bottom row (index order
	// matches Render's row 0..height-1, top to bottom) -- a partial level
	// should show up in the bottom row first.
	cases := []struct {
		level int
		want  []rune
	}{
		{-5, []rune{' ', ' '}},
		{0, []rune{' ', ' '}},
		{4, []rune{' ', '▄'}},
		{8, []rune{' ', '█'}},
		{12, []rune{'▄', '█'}},
		{16, []rune{'█', '█'}},
		{20, []rune{'█', '█'}}, // clamped, doesn't overflow past the top row
	}
	for _, tc := range cases {
		got := equalizerColumnGlyphs(tc.level, 2)
		if string(got) != string(tc.want) {
			t.Errorf("equalizerColumnGlyphs(%d, 2) = %q, want %q", tc.level, string(got), string(tc.want))
		}
	}
}
