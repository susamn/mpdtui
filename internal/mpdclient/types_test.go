package mpdclient

import (
	"testing"
	"time"

	"github.com/fhs/gompd/v2/mpd"
)

func TestParseSongAdded(t *testing.T) {
	got := parseSong(mpd.Attrs{"file": "track.mp3", "Added": "2026-08-03T20:51:45Z"}).Added
	want := time.Date(2026, 8, 3, 20, 51, 45, 0, time.UTC)
	if !got.Equal(want) {
		t.Errorf("parseSong(...).Added = %v, want %v", got, want)
	}
}

func TestParseSongAddedMissingField(t *testing.T) {
	// MPD servers older than 0.24 don't report "Added" at all -- must
	// degrade to the zero time, not error the whole parse.
	got := parseSong(mpd.Attrs{"file": "track.mp3"}).Added
	if !got.IsZero() {
		t.Errorf("parseSong with no Added key: Added = %v, want the zero time", got)
	}
}
