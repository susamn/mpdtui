package mpdclient

import (
	"testing"
	"time"

	"github.com/fhs/gompd/v2/mpd"
)

func TestParsePlaylistLastModified(t *testing.T) {
	got := parsePlaylistLastModified(mpd.Attrs{"playlist": "Rock", "Last-Modified": "2026-08-05T03:46:30Z"})
	want := time.Date(2026, 8, 5, 3, 46, 30, 0, time.UTC)
	if !got.Equal(want) {
		t.Errorf("parsePlaylistLastModified = %v, want %v", got, want)
	}
}

func TestParsePlaylistLastModifiedMissingField(t *testing.T) {
	got := parsePlaylistLastModified(mpd.Attrs{"playlist": "Rock"})
	if !got.IsZero() {
		t.Errorf("parsePlaylistLastModified with no Last-Modified key = %v, want the zero time", got)
	}
}

func TestParsePlaylistLastModifiedUnparseable(t *testing.T) {
	got := parsePlaylistLastModified(mpd.Attrs{"playlist": "Rock", "Last-Modified": "not-a-timestamp"})
	if !got.IsZero() {
		t.Errorf("parsePlaylistLastModified with a garbled timestamp = %v, want the zero time", got)
	}
}
