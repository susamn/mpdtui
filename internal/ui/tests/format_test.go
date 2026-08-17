package tests

import (
	"testing"
	"time"

	"mpdtui/internal/mpdclient"
	"mpdtui/internal/ui"
)

func TestFormatDuration(t *testing.T) {
	cases := map[time.Duration]string{
		0:                             "0:00",
		-5 * time.Second:              "0:00",
		45 * time.Second:              "0:45",
		90 * time.Second:              "1:30",
		6*time.Minute + 4*time.Second: "6:04",
	}
	for d, want := range cases {
		if got := ui.FormatDuration(d); got != want {
			t.Errorf("FormatDuration(%v) = %q, want %q", d, got, want)
		}
	}
}

func TestProgressBar(t *testing.T) {
	if got := ui.ProgressBar(0, 0, 10); got != "░░░░░░░░░░" {
		t.Errorf("zero duration: got %q", got)
	}
	if got := ui.ProgressBar(5*time.Second, 10*time.Second, 10); got != "█████░░░░░" {
		t.Errorf("half: got %q", got)
	}
	if got := ui.ProgressBar(20*time.Second, 10*time.Second, 10); got != "██████████" {
		t.Errorf("overshoot elapsed should clamp to full: got %q", got)
	}
	if got := ui.ProgressBar(-5*time.Second, 10*time.Second, 10); got != "░░░░░░░░░░" {
		t.Errorf("negative elapsed should clamp to empty: got %q", got)
	}
}

func TestStateGlyph(t *testing.T) {
	cases := map[mpdclient.State]string{
		mpdclient.StatePlay:  "▶",
		mpdclient.StatePause: "⏸",
		mpdclient.StateStop:  "■",
	}
	for state, want := range cases {
		if got := ui.StateGlyph(state); got != want {
			t.Errorf("StateGlyph(%v) = %q, want %q", state, got, want)
		}
	}
}

// TestStateGlyphColor covers the glyph's *color* separately from its
// character (StateGlyph, above) -- bright green for Play, bright yellow
// for Pause, bright red for Stop, explicit direction to color the
// existing glyphs rather than swap them for something else.
func TestStateGlyphColor(t *testing.T) {
	cases := map[mpdclient.State]string{
		mpdclient.StatePlay:  "#00FF00",
		mpdclient.StatePause: "#FFFF00",
		mpdclient.StateStop:  "#FF0000",
	}
	for state, want := range cases {
		if got := ui.StateGlyphColor(state); got != want {
			t.Errorf("StateGlyphColor(%v) = %q, want %q", state, got, want)
		}
	}
}

func TestOnOff(t *testing.T) {
	if ui.OnOff(true) != "on" || ui.OnOff(false) != "off" {
		t.Fatal("OnOff mismatch")
	}
}

func TestVolText(t *testing.T) {
	if ui.VolText(-1) != "?" {
		t.Errorf("expected unknown volume marker, got %q", ui.VolText(-1))
	}
	if ui.VolText(79) != "79" {
		t.Errorf("expected 79, got %q", ui.VolText(79))
	}
}

func TestVolumeColorGradientEndpointsAndMidpoint(t *testing.T) {
	if got := ui.VolumeColor(-1); got != "" {
		t.Errorf("VolumeColor(-1) = %q, want empty (unknown volume)", got)
	}
	if got, want := ui.VolumeColor(0), "#25D366"; got != want {
		t.Errorf("VolumeColor(0) = %q, want %q (WhatsApp green)", got, want)
	}
	if got, want := ui.VolumeColor(50), "#FFD700"; got != want {
		t.Errorf("VolumeColor(50) = %q, want %q (gold)", got, want)
	}
	if got, want := ui.VolumeColor(100), "#DC3545"; got != want {
		t.Errorf("VolumeColor(100) = %q, want %q (red)", got, want)
	}
	// Over 100 clamps rather than extrapolating past red.
	if got := ui.VolumeColor(150); got != ui.VolumeColor(100) {
		t.Errorf("VolumeColor(150) = %q, want it clamped to VolumeColor(100) = %q", got, ui.VolumeColor(100))
	}
}

func TestVolumeTextColorsKnownVolumeOnly(t *testing.T) {
	if got, want := ui.VolumeText(-1), "?%"; got != want {
		t.Errorf("VolumeText(-1) = %q, want %q (no color tag for unknown volume)", got, want)
	}
	got := ui.VolumeText(50)
	want := "[#FFD700]50%[-]"
	if got != want {
		t.Errorf("VolumeText(50) = %q, want %q", got, want)
	}
}

func TestFlagTextColorsAndBoldsTheValue(t *testing.T) {
	if got, want := ui.FlagText("repeat", true), "repeat [#25D366::b]on[-:-:-]"; got != want {
		t.Errorf("FlagText(repeat, true) = %q, want %q", got, want)
	}
	if got, want := ui.FlagText("repeat", false), "repeat [red::b]off[-:-:-]"; got != want {
		t.Errorf("FlagText(repeat, false) = %q, want %q", got, want)
	}
}

func TestTrackFormat(t *testing.T) {
	cases := map[string]string{
		"queen/absolute-greatest/track.mp3": "MP3",
		"track.flac":                        "FLAC",
		"Some Album/Track.M4A":              "M4A",
		"path/to/song.wma":                  "WMA",
		"no-extension":                      "",
		"trailing-dot.":                     "",
		".hidden":                           "",
		"":                                  "",
		"dir.with.dots/track.ogg":           "OGG",
	}
	for file, want := range cases {
		if got := ui.TrackFormat(file); got != want {
			t.Errorf("TrackFormat(%q) = %q, want %q", file, got, want)
		}
	}
}

func TestFormatAudioQuality(t *testing.T) {
	cases := []struct {
		name        string
		bitrate     int
		audioFormat string
		want        string
	}{
		{"full CD quality", 128, "44100:16:2", "128kbps 44.1kHz/16-bit/2ch"},
		{"exact-kHz sample rate", 320, "48000:24:2", "320kbps 48kHz/24-bit/2ch"},
		{"floating point bit depth", 1411, "192000:f:2", "1411kbps 192kHz/float/2ch"},
		{"mono", 96, "22050:8:1", "96kbps 22.05kHz/8-bit/1ch"},
		{"bitrate unknown (still settling)", 0, "44100:16:2", "44.1kHz/16-bit/2ch"},
		{"audio format unknown (not yet reported)", 128, "", "128kbps"},
		{"nothing known", 0, "", ""},
		{"malformed audio format", 128, "not-a-triplet", "128kbps"},
	}
	for _, tc := range cases {
		if got := ui.FormatAudioQuality(tc.bitrate, tc.audioFormat); got != tc.want {
			t.Errorf("%s: FormatAudioQuality(%d, %q) = %q, want %q", tc.name, tc.bitrate, tc.audioFormat, got, tc.want)
		}
	}
}
