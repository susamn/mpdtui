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

// DisplayName is what every panel, picker and mini-mode line shows for a
// track, so its fallbacks decide what an untagged file looks like
// everywhere at once.
func TestSongDisplayName(t *testing.T) {
	cases := []struct {
		name string
		song Song
		want string
	}{
		{"artist and title", Song{Artist: "Hariharan", Title: "Ay Hairathe"}, "Hariharan - Ay Hairathe"},
		{"title only", Song{Title: "Ay Hairathe"}, "Ay Hairathe"},
		// An artist with no title is not "Artist - ", which would read
		// as a broken row; it falls back to the filename like any other
		// untagged track.
		{"artist only", Song{Artist: "Hariharan", File: "a/b.mp3"}, "a/b.mp3"},
		{"neither", Song{File: "a-r-rahman/guru/05.mp3"}, "a-r-rahman/guru/05.mp3"},
		{"nothing at all", Song{}, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.song.DisplayName(); got != tc.want {
				t.Errorf("DisplayName() = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestNilWatcherYieldsNilChannels covers the nil guards on Watcher.
// App.Run carries on without a watcher when MPD refuses the idle
// connection, and a nil channel in a select blocks forever rather than
// spinning -- which is exactly the wanted behavior, and would be a nil
// dereference without these guards.
func TestNilWatcherYieldsNilChannels(t *testing.T) {
	var w *Watcher

	if ch := w.Events(); ch != nil {
		t.Errorf("Events() on a nil watcher = %v, want nil", ch)
	}
	if ch := w.Errors(); ch != nil {
		t.Errorf("Errors() on a nil watcher = %v, want nil", ch)
	}

	// A nil channel in a select is never ready, which is what lets the
	// event loop fall through to its tickers instead of spinning.
	select {
	case <-w.Events():
		t.Error("a nil watcher's event channel was ready")
	case <-w.Errors():
		t.Error("a nil watcher's error channel was ready")
	default:
	}
}
