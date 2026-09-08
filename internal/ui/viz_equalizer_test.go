package ui

import (
	"encoding/binary"
	"math"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"mpdtui/internal/audio"
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

// startTestSpectrum creates a fifo, keeps a writer on it (the way MPD's
// own fifo plugin does), and streams a full-scale tone at hz into it,
// returning a Spectrum that has live audio by the time it returns.
func startTestSpectrum(t *testing.T, hz float64) *audio.Spectrum {
	t.Helper()

	path := filepath.Join(t.TempDir(), "mpd.fifo")
	if err := syscall.Mkfifo(path, 0o600); err != nil {
		t.Fatalf("mkfifo: %v", err)
	}
	w, err := os.OpenFile(path, os.O_RDWR, 0)
	if err != nil {
		t.Fatalf("open fifo for writing: %v", err)
	}
	t.Cleanup(func() { w.Close() })

	s := audio.NewSpectrum(path)
	s.Start()
	t.Cleanup(s.Close)

	// Keep feeding until the analysis window is full and Bands starts
	// returning data. The sample counter has to run across writes, not
	// restart per chunk: a phase discontinuity at every chunk boundary
	// is a click, and a click is broadband energy that would show up in
	// every band and defeat the point of the test.
	pcm := make([]byte, 4096)
	frames := len(pcm) / 4
	sample := 0

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		for i := 0; i < frames; i++ {
			v := int16(30000 * math.Sin(2*math.Pi*hz*float64(sample)/audio.SampleRate))
			binary.LittleEndian.PutUint16(pcm[i*4:], uint16(v))
			binary.LittleEndian.PutUint16(pcm[i*4+2:], uint16(v))
			sample++
		}
		if _, err := w.Write(pcm); err != nil {
			t.Fatalf("write pcm: %v", err)
		}
		if s.Bands(8) != nil {
			return s
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("test spectrum never went active")
	return nil
}

// filledColumns reports, per column, whether any row of the rendered
// output has a non-space glyph there -- i.e. which bars are up.
func filledColumns(lines []string, width int) []bool {
	filled := make([]bool, width)
	for _, l := range lines {
		for x, r := range []rune(l) {
			if x < width && r != ' ' {
				filled[x] = true
			}
		}
	}
	return filled
}

func TestEqualizerDrawsRealSpectrumWhenAudioIsLive(t *testing.T) {
	// A 60Hz tone is near the bottom of the analyzed range, so a real
	// spectrum must put its energy in the leftmost columns and leave the
	// treble end empty. The simulated fallback spreads a travelling wave
	// across every column, so this distinguishes the two paths.
	eq := newEqualizerVisualization(startTestSpectrum(t, 60))

	const width = 40
	lines := eq.Render(width, 2, 0, mpdclient.Status{State: mpdclient.StatePlay, Volume: 100})
	filled := filledColumns(lines, width)

	if !filled[0] {
		t.Errorf("leftmost column empty for a 60Hz tone: %q", lines)
	}
	for x := width / 2; x < width; x++ {
		if filled[x] {
			t.Errorf("column %d filled for a 60Hz tone -- treble half should be silent: %q", x, lines)
			break
		}
	}
}

func TestEqualizerFallsBackToSimulationWithoutAudio(t *testing.T) {
	// No fifo at all (the nil Spectrum a default-constructed
	// visualization has): the wave animation must still run, so the
	// panel is never blank for a user without the audio_output block.
	eq := newEqualizerVisualization(nil)
	playing := mpdclient.Status{State: mpdclient.StatePlay, Volume: 80}

	at0 := eq.Render(30, 2, 0, playing)
	at1s := eq.Render(30, 2, time.Second, playing)

	blank := true
	for _, l := range at0 {
		if strings.Trim(l, " ") != "" {
			blank = false
		}
	}
	if blank {
		t.Error("no output without a spectrum -- expected the simulated fallback")
	}
	if at0[0] == at1s[0] && at0[1] == at1s[1] {
		t.Error("fallback output identical across elapsed times -- expected animation")
	}
}

func TestEqualizerRealSpectrumStillScalesWithVolume(t *testing.T) {
	// MPD's fifo carries the stream at full scale regardless of the
	// mixer, so the volume scaling has to be applied on top of the real
	// spectrum or turning the volume down would do nothing visible.
	s := startTestSpectrum(t, 60)
	eq := newEqualizerVisualization(s)
	playing := func(v int) mpdclient.Status {
		return mpdclient.Status{State: mpdclient.StatePlay, Volume: v}
	}

	silent := eq.Render(40, 2, 0, playing(0))
	for i, l := range silent {
		if strings.Trim(l, " ") != "" {
			t.Errorf("volume 0: line %d = %q, want all spaces even with live audio", i, l)
		}
	}
}
