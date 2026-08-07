package ui

import (
	"strings"
	"testing"
	"time"

	"github.com/rivo/tview"

	"mpdtui/internal/mpdclient"
)

func TestSortPlaylistsByRecencyMostRecentFirst(t *testing.T) {
	old := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	newer := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	newest := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)

	pls := []mpdclient.Playlist{
		{Name: "Old", LastModified: old},
		{Name: "Newest", LastModified: newest},
		{Name: "Newer", LastModified: newer},
	}
	sortPlaylistsByRecency(pls)

	want := []string{"Newest", "Newer", "Old"}
	for i, w := range want {
		if pls[i].Name != w {
			t.Errorf("pls[%d].Name = %q, want %q (order: %v)", i, pls[i].Name, w, pls)
		}
	}
}

func TestSortPlaylistsByRecencyTiebreaksAlphabetically(t *testing.T) {
	// Same timestamp (including the zero value, for playlists with no
	// reported Last-Modified) must still sort deterministically.
	pls := []mpdclient.Playlist{
		{Name: "Zebra"},
		{Name: "Apple"},
		{Name: "Mango"},
	}
	sortPlaylistsByRecency(pls)

	want := []string{"Apple", "Mango", "Zebra"}
	for i, w := range want {
		if pls[i].Name != w {
			t.Errorf("pls[%d].Name = %q, want %q", i, pls[i].Name, w)
		}
	}
}

func TestRecentPlaylistBadgesTakesFirstN(t *testing.T) {
	pls := []mpdclient.Playlist{{Name: "A"}, {Name: "B"}, {Name: "C"}, {Name: "D"}}
	badged := recentPlaylistBadges(pls, 2)

	if len(badged) != 2 {
		t.Fatalf("got %d badged, want 2", len(badged))
	}
	if !badged["A"] || !badged["B"] {
		t.Errorf("badged = %v, want A and B (the first two)", badged)
	}
	if badged["C"] || badged["D"] {
		t.Errorf("badged = %v, want C and D excluded", badged)
	}
}

func TestRecentPlaylistBadgesNLargerThanSliceBadgesEverything(t *testing.T) {
	pls := []mpdclient.Playlist{{Name: "A"}, {Name: "B"}}
	badged := recentPlaylistBadges(pls, playlistRecentBadgeCount)
	if len(badged) != 2 {
		t.Errorf("got %d badged, want 2 (n larger than the slice)", len(badged))
	}
}

func TestPlaylistsRefreshOrdersByRecencyAndBadgesTopN(t *testing.T) {
	a := newTestApp()
	old := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	newest := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	a.playlists.pls = []mpdclient.Playlist{
		{Name: "Old One", LastModified: old},
		{Name: "New One", LastModified: newest},
	}
	sortPlaylistsByRecency(a.playlists.pls)
	a.playlists.badged = recentPlaylistBadges(a.playlists.pls, 1)
	a.playlists.render()

	name, _ := a.playlists.list.GetItemText(0)
	if !strings.HasPrefix(name, "New One") {
		t.Errorf("first item = %q, want it to start with %q (most recently modified)", name, "New One")
	}
	if got := a.playlists.selectedName(); got != "New One" {
		t.Errorf("selectedName() with the first item selected = %q, want %q (unaffected by any badge decoration)", got, "New One")
	}
}

func TestPlaylistsRealignRightAlignsBadgeIcon(t *testing.T) {
	a := newTestApp()
	a.playlists.pls = []mpdclient.Playlist{{Name: "Rock"}, {Name: "Jazz"}}
	a.playlists.badged = map[string]bool{"Rock": true}
	a.playlists.render()

	a.playlists.list.SetRect(0, 0, 30, 10) // border=true, so inner width = 28
	a.playlists.realign()

	rockText, _ := a.playlists.list.GetItemText(0)
	if !strings.HasSuffix(rockText, playlistRecentIcon) {
		t.Fatalf("badged item text = %q, want it to end with the badge icon %q", rockText, playlistRecentIcon)
	}
	// Use tview's own display-width function, not a rune count -- the
	// icon is a wide (2-column) glyph, same as what realign uses to
	// compute the padding in the first place.
	gotWidth := tview.TaggedStringWidth(rockText)
	_, _, innerWidth, _ := a.playlists.list.GetInnerRect()
	if gotWidth != innerWidth {
		t.Errorf("badged item text display width = %d, want it to exactly fill the inner width %d (right-aligned to the edge)", gotWidth, innerWidth)
	}

	jazzText, _ := a.playlists.list.GetItemText(1)
	if jazzText != "Jazz" {
		t.Errorf("unbadged item text = %q, want plain %q (no padding/icon)", jazzText, "Jazz")
	}
}

func TestPlaylistsRealignSkipsWorkWhenWidthUnchangedAndNotDirty(t *testing.T) {
	a := newTestApp()
	a.playlists.pls = []mpdclient.Playlist{{Name: "Rock"}}
	a.playlists.badged = map[string]bool{"Rock": true}
	a.playlists.render()
	a.playlists.list.SetRect(0, 0, 30, 10)
	a.playlists.realign() // first call: computes and pads

	// Manually revert the item text, then call realign again at the same
	// width -- if it's correctly skipping redundant work (not dirty, same
	// width), the manual revert should stick.
	a.playlists.list.SetItemText(0, "Rock", "")
	a.playlists.realign()

	got, _ := a.playlists.list.GetItemText(0)
	if got != "Rock" {
		t.Errorf("realign recomputed despite unchanged width and no new render(): got %q, want the manually reverted %q", got, "Rock")
	}
}
