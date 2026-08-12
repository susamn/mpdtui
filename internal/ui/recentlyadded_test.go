package ui

import (
	"strings"
	"testing"
	"time"

	"github.com/gdamore/tcell/v2"

	"mpdtui/internal/mpdclient"
)

func TestRecentlyAddedURIsOrdersNewestFirst(t *testing.T) {
	old := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	newer := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	newest := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)

	songs := []mpdclient.Song{
		{File: "old.mp3", Added: old},
		{File: "newest.mp3", Added: newest},
		{File: "newer.mp3", Added: newer},
	}
	got := recentlyAddedURIs(songs, 10)
	want := []string{"newest.mp3", "newer.mp3", "old.mp3"}
	for i, w := range want {
		if got[i] != w {
			t.Errorf("recentlyAddedURIs()[%d] = %q, want %q (order: %v)", i, got[i], w, got)
		}
	}
}

func TestRecentlyAddedURIsTruncatesToN(t *testing.T) {
	songs := make([]mpdclient.Song, 100)
	for i := range songs {
		songs[i] = mpdclient.Song{File: string(rune('a' + i%26))}
	}
	if got := len(recentlyAddedURIs(songs, 50)); got != 50 {
		t.Errorf("got %d URIs, want 50 (truncated)", got)
	}
	if got := len(recentlyAddedURIs(songs, 200)); got != 100 {
		t.Errorf("got %d URIs, want 100 (n larger than input is a no-op cap)", got)
	}
}

func TestRecentlyAddedURIsTiebreaksByFileOnEqualAdded(t *testing.T) {
	// Same timestamp (including the zero value, for MPD servers old
	// enough to never report Added at all) must still sort deterministically.
	songs := []mpdclient.Song{
		{File: "zebra.mp3"},
		{File: "apple.mp3"},
		{File: "mango.mp3"},
	}
	got := recentlyAddedURIs(songs, 10)
	want := []string{"apple.mp3", "mango.mp3", "zebra.mp3"}
	for i, w := range want {
		if got[i] != w {
			t.Errorf("recentlyAddedURIs()[%d] = %q, want %q (order: %v)", i, got[i], w, got)
		}
	}
}

func TestRecentlyAddedURIsEmptyInput(t *testing.T) {
	if got := recentlyAddedURIs(nil, 50); len(got) != 0 {
		t.Errorf("recentlyAddedURIs(nil, 50) = %v, want empty", got)
	}
}

func TestPlaylistListTextTintsOnlyRecentlyAdded(t *testing.T) {
	got := playlistListText(recentlyAddedPlaylistName)
	want := "[teal]" + playlistDisplayName(recentlyAddedPlaylistName) + "[-]"
	if got != want {
		t.Errorf("playlistListText(%q) = %q, want %q", recentlyAddedPlaylistName, got, want)
	}

	if got := playlistListText("My Mixtape"); got != playlistDisplayName("My Mixtape") {
		t.Errorf("playlistListText(%q) = %q, want plain %q (no tint for a regular playlist)", "My Mixtape", got, playlistDisplayName("My Mixtape"))
	}
}

func TestHandleRegenerateRecentlyAddedInvalidFromOtherPanel(t *testing.T) {
	a := newTestApp()
	a.tv.SetFocus(a.library.tree) // deliberately not the Playlists panel, and no client -- would panic if this reached regenerateRecentlyAdded

	rKey := tcell.NewEventKey(tcell.KeyRune, 'R', tcell.ModNone)
	if result := a.globalInputCapture(rKey); result != nil {
		t.Errorf("'R' should be consumed, got %v", result)
	}

	if !strings.Contains(a.hintBar.GetText(false), "'R' has no action here") {
		t.Errorf("hint bar after 'R' from a non-Playlists panel = %q, want an invalid-key flash", a.hintBar.GetText(false))
	}
}
