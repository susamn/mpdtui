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
