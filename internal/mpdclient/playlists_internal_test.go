package mpdclient

import (
	"reflect"
	"testing"
)

func TestFalloutReportCountsAndOrdersTheHoles(t *testing.T) {
	known := map[string]struct{}{
		"a/1.mp3": {}, "a/2.mp3": {}, "a/3.mp3": {},
	}
	got := falloutReport(known, []playlistEntries{
		// Two holes, and a blank line the playlist file happens to
		// carry -- counted towards Total (it is a line) but never
		// reported as a missing track.
		{Name: "road trip", URIs: []string{"a/1.mp3", "gone/x.mp3", "", "gone/y.mp3"}},
		// Intact.
		{Name: "favourites", URIs: []string{"a/1.mp3", "a/2.mp3"}},
		// One hole, and alphabetically after "sunday" so the tie-break
		// is visible.
		{Name: "zzz", URIs: []string{"gone/z.mp3"}},
		{Name: "sunday", URIs: []string{"a/3.mp3", "gone/w.mp3"}},
		// Empty playlist.
		{Name: "new", URIs: nil},
	})

	want := PlaylistFalloutReport{
		Playlists: 5,
		Entries:   9,
		Missing:   4,
		Affected: []PlaylistFallout{
			{Name: "road trip", Total: 4, Missing: []string{"gone/x.mp3", "gone/y.mp3"}},
			{Name: "sunday", Total: 2, Missing: []string{"gone/w.mp3"}},
			{Name: "zzz", Total: 1, Missing: []string{"gone/z.mp3"}},
		},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("falloutReport() =\n%+v\nwant\n%+v", got, want)
	}
}

func TestFalloutReportOnAnIntactCollection(t *testing.T) {
	known := map[string]struct{}{"a/1.mp3": {}}
	got := falloutReport(known, []playlistEntries{{Name: "one", URIs: []string{"a/1.mp3"}}})

	if got.Missing != 0 || got.Affected != nil {
		t.Errorf("falloutReport() = %+v, want no missing entries and no affected playlists", got)
	}
	if got.Entries != 1 || got.Playlists != 1 {
		t.Errorf("falloutReport() = %+v, want 1 entry across 1 playlist", got)
	}
}
