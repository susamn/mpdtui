package mpdclient

import (
	"testing"
	"time"

	"github.com/fhs/gompd/v2/mpd"
)

func TestParseDirEntriesClassifiesByType(t *testing.T) {
	attrs := []mpd.Attrs{
		{"directory": "queen"},
		{"playlist": "Favorite Songs"},
		{
			"file":   "queen/absolute-greatest/02-we-are-the-champions.mp3",
			"artist": "Queen", "album": "Absolute Greatest", "title": "We Are The Champions",
			"time": "181", "duration": "181.466",
		},
	}

	entries := parseDirEntries(attrs)
	if len(entries) != 3 {
		t.Fatalf("got %d entries, want 3", len(entries))
	}

	if entries[0].Type != EntryDirectory || entries[0].Path != "queen" {
		t.Errorf("entry 0 = %+v, want directory %q", entries[0], "queen")
	}
	if entries[1].Type != EntryPlaylist || entries[1].Path != "Favorite Songs" {
		t.Errorf("entry 1 = %+v, want playlist %q", entries[1], "Favorite Songs")
	}
	if entries[2].Type != EntryFile {
		t.Fatalf("entry 2 type = %v, want EntryFile", entries[2].Type)
	}
	song := entries[2].Song
	if song.Artist != "Queen" || song.Album != "Absolute Greatest" || song.Title != "We Are The Champions" {
		t.Errorf("entry 2 song = %+v, missing expected tags", song)
	}
	if song.ID != -1 || song.Pos != -1 {
		t.Errorf("entry 2 song ID/Pos = %d/%d, want -1/-1 (not a queue entry)", song.ID, song.Pos)
	}
	if song.Duration.Seconds() < 181.4 || song.Duration.Seconds() > 181.5 {
		t.Errorf("entry 2 duration = %v, want ~181.466s (from the more precise \"duration\" key)", song.Duration)
	}
}

func TestParseDirEntriesDurationFallsBackToTimeKey(t *testing.T) {
	attrs := []mpd.Attrs{
		{"file": "track.mp3", "title": "No duration key", "time": "200"},
	}
	entries := parseDirEntries(attrs)
	if got := entries[0].Song.Duration.Seconds(); got != 200 {
		t.Errorf("duration = %v, want 200s from the \"time\" fallback", entries[0].Song.Duration)
	}
}

func TestParseDirEntriesSkipsUnrecognizedRows(t *testing.T) {
	entries := parseDirEntries([]mpd.Attrs{{}})
	if len(entries) != 0 {
		t.Errorf("got %d entries from a row with no directory/playlist/file key, want 0", len(entries))
	}
}

func TestParseDirEntriesParsesLastModified(t *testing.T) {
	attrs := []mpd.Attrs{
		{"directory": "queen/absolute-greatest", "last-modified": "2026-02-20T04:39:02Z"},
		{"file": "queen/track.mp3", "last-modified": "2022-11-13T21:01:51Z"},
		{"playlist": "Rock", "last-modified": "2026-08-05T03:46:30Z"},
	}
	entries := parseDirEntries(attrs)
	if len(entries) != 3 {
		t.Fatalf("got %d entries, want 3", len(entries))
	}
	want := []string{"2026-02-20T04:39:02Z", "2022-11-13T21:01:51Z", "2026-08-05T03:46:30Z"}
	for i, w := range want {
		if entries[i].LastModified.Format(time.RFC3339) != w {
			t.Errorf("entries[%d].LastModified = %v, want %s", i, entries[i].LastModified, w)
		}
	}
}

func TestParseDirEntriesMissingLastModifiedIsZero(t *testing.T) {
	// Verified against a live server: root-level directories don't
	// always report last-modified even though nested ones do.
	entries := parseDirEntries([]mpd.Attrs{{"directory": "4-non-blondes"}})
	if !entries[0].LastModified.IsZero() {
		t.Errorf("LastModified with no last-modified key = %v, want the zero time", entries[0].LastModified)
	}
}
