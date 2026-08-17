package mpdclient

import (
	"testing"

	"github.com/fhs/gompd/v2/mpd"
)

func TestParseSongComposer(t *testing.T) {
	got := parseSong(mpd.Attrs{"file": "track.mp3", "Composer": "Freddie Mercury"}).Composer
	if want := "Freddie Mercury"; got != want {
		t.Errorf("parseSong(...).Composer = %q, want %q", got, want)
	}
}

func TestParseSongComposerMissingField(t *testing.T) {
	got := parseSong(mpd.Attrs{"file": "track.mp3"}).Composer
	if got != "" {
		t.Errorf("parseSong with no Composer key: Composer = %q, want empty", got)
	}
}

func TestParseStatusBitrateAndAudioFormat(t *testing.T) {
	st := parseStatus(mpd.Attrs{"state": "play", "bitrate": "128", "audio": "44100:16:2"})
	if st.Bitrate != 128 {
		t.Errorf("Bitrate = %d, want 128", st.Bitrate)
	}
	if st.AudioFormat != "44100:16:2" {
		t.Errorf("AudioFormat = %q, want %q", st.AudioFormat, "44100:16:2")
	}
}

// TestParseStatusBitrateAndAudioFormatMissing covers the stopped/just-
// started case: MPD omits both fields entirely rather than sending empty
// values, and parseStatus must not error the whole response over it (same
// convention as every other optional field here).
func TestParseStatusBitrateAndAudioFormatMissing(t *testing.T) {
	st := parseStatus(mpd.Attrs{"state": "stop"})
	if st.Bitrate != 0 {
		t.Errorf("Bitrate = %d, want 0", st.Bitrate)
	}
	if st.AudioFormat != "" {
		t.Errorf("AudioFormat = %q, want empty", st.AudioFormat)
	}
}
