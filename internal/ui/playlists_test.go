package ui

import (
	"strings"
	"testing"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"mpdtui/internal/mpdclient"
)

func TestHandleRefreshPlaylistCountsInvalidFromOtherPanel(t *testing.T) {
	a := newTestApp()
	a.tv.SetFocus(a.library.tree) // deliberately not Playlists, and no client -- would panic if this reached refreshTrackCounts

	rKey := tcell.NewEventKey(tcell.KeyRune, 'R', tcell.ModNone)
	if result := a.globalInputCapture(rKey); result != nil {
		t.Errorf("'R' should be consumed, got %v", result)
	}
	if !strings.Contains(a.hintBar.GetText(false), "'R' has no action here") {
		t.Errorf("hint bar after 'R' from a non-Playlists panel = %q, want an invalid-key flash", a.hintBar.GetText(false))
	}
}

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

func TestSortPlaylistsByNameCaseInsensitive(t *testing.T) {
	pls := []mpdclient.Playlist{{Name: "queen"}, {Name: "Alisha Chinoy"}, {Name: "alisha-chinai"}, {Name: "abba"}}
	sortPlaylistsByName(pls)

	want := []string{"abba", "Alisha Chinoy", "alisha-chinai", "queen"}
	for i, w := range want {
		if pls[i].Name != w {
			t.Errorf("pls[%d].Name = %q, want %q (order: %v)", i, pls[i].Name, w, pls)
		}
	}
}

func TestPlaylistsSortModeNextTogglesBetweenRecentAndName(t *testing.T) {
	if got := playlistsSortRecent.next(); got != playlistsSortName {
		t.Errorf("playlistsSortRecent.next() = %v, want playlistsSortName", got)
	}
	if got := playlistsSortName.next(); got != playlistsSortRecent {
		t.Errorf("playlistsSortName.next() = %v, want playlistsSortRecent", got)
	}
}

func TestPlaylistsCycleSortModeReorders(t *testing.T) {
	a := newTestApp()
	old := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	newest := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	a.playlists.pls = []mpdclient.Playlist{
		{Name: "Zebra", LastModified: newest},
		{Name: "Apple", LastModified: old},
	}
	sortPlaylistsByRecency(a.playlists.pls)
	a.playlists.render()

	if title := a.playlists.table.GetTitle(); title != " Playlists (recent) " {
		t.Fatalf("setup: title = %q, want %q", title, " Playlists (recent) ")
	}

	a.playlists.cycleSortMode()

	if a.playlists.sortMode != playlistsSortName {
		t.Errorf("sortMode after cycleSortMode() = %v, want playlistsSortName", a.playlists.sortMode)
	}
	if title := a.playlists.table.GetTitle(); title != " Playlists (name) " {
		t.Errorf("title after cycling = %q, want %q", title, " Playlists (name) ")
	}
	name := a.playlists.table.GetCell(playlistsHeaderRows, 0).Text
	if name != playlistDisplayName("Apple") {
		t.Errorf("first row's Name cell after switching to name sort = %q, want %q", name, playlistDisplayName("Apple"))
	}
}

func TestPlaylistsRefreshOrdersByRecency(t *testing.T) {
	a := newTestApp()
	old := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	newest := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	a.playlists.pls = []mpdclient.Playlist{
		{Name: "Old One", LastModified: old},
		{Name: "New One", LastModified: newest},
	}
	sortPlaylistsByRecency(a.playlists.pls)
	a.playlists.render()

	name := a.playlists.table.GetCell(playlistsHeaderRows, 0).Text
	if name != playlistDisplayName("New One") {
		t.Errorf("first row's Name cell = %q, want %q (most recently modified)", name, playlistDisplayName("New One"))
	}

	// selectedName() reads back whatever's actually selected -- render()
	// alone never selects a row (the table starts on its header row,
	// which is NotSelectable but that only affects arrow-key navigation,
	// not the initial cursor), so explicitly select the first data row
	// first, the same way real navigation would.
	a.playlists.table.Select(playlistsHeaderRows, 0)
	if got := a.playlists.selectedName(); got != "New One" {
		t.Errorf("selectedName() with the first row selected = %q, want %q", got, "New One")
	}
}

func TestPlaylistsHeaderRowLabelsAndAlignment(t *testing.T) {
	a := newTestApp()
	a.playlists.render() // no playlists -- header should still be there

	wantHeaders := []struct {
		col   int
		text  string
		align int
	}{
		{0, "Name", tview.AlignLeft},
		{1, "Count", tview.AlignRight},
	}
	for _, w := range wantHeaders {
		cell := a.playlists.table.GetCell(0, w.col)
		if cell.Text != w.text {
			t.Errorf("header col %d text = %q, want %q", w.col, cell.Text, w.text)
		}
		if cell.Align != w.align {
			t.Errorf("header col %d align = %d, want %d", w.col, cell.Align, w.align)
		}
	}
}

func TestPlaylistsHeaderRowStyledAndNotSelectable(t *testing.T) {
	a := newTestApp()
	a.playlists.render()

	cell := a.playlists.table.GetCell(0, 0)
	if got := cellFg(cell); got != queueHeaderFg {
		t.Errorf("header foreground = %v, want %v (shared with Queue's header)", got, queueHeaderFg)
	}
	if got := cellBg(cell); got != queueHeaderBg {
		t.Errorf("header background = %v, want %v (shared with Queue's header)", got, queueHeaderBg)
	}
	if !cell.NotSelectable {
		t.Error("header cell should not be selectable")
	}
}

func TestPlaylistsHeaderSurvivesRerender(t *testing.T) {
	a := newTestApp()
	setPlaylistsForTest(a.playlists, []string{"Rock"})
	a.playlists.render() // Table.Clear() wipes row 0 too -- header must be rewritten each time

	if got := a.playlists.table.GetCell(0, 0).Text; got != "Name" {
		t.Errorf("header after re-render = %q, want %q", got, "Name")
	}
}

func TestPlaylistsRenderNameColumnExpands(t *testing.T) {
	a := newTestApp()
	setPlaylistsForTest(a.playlists, []string{"Rock"})

	if got := a.playlists.table.GetCell(playlistsHeaderRows, 0).Expansion; got != 1 {
		t.Errorf("Name cell Expansion = %d, want 1 (absorbs leftover width so Count sits flush right)", got)
	}
}

func TestPlaylistsRenderCountBlankUntilFetched(t *testing.T) {
	a := newTestApp()
	setPlaylistsForTest(a.playlists, []string{"Rock"})

	cell := a.playlists.table.GetCell(playlistsHeaderRows, 1)
	if cell.Text != "" {
		t.Errorf("Count cell before any fetch = %q, want empty (not a misleading 0)", cell.Text)
	}
	if cell.Align != tview.AlignRight {
		t.Errorf("Count cell Align = %d, want AlignRight", cell.Align)
	}
}

func TestPlaylistsRenderCountShowsFetchedValue(t *testing.T) {
	a := newTestApp()
	setPlaylistsForTest(a.playlists, []string{"Rock", "Jazz"})
	a.playlists.trackCounts = map[string]int{"Rock": 42}
	a.playlists.render()

	if got := a.playlists.table.GetCell(playlistsHeaderRows, 1).Text; got != "42" {
		t.Errorf("Rock's Count cell = %q, want %q", got, "42")
	}
	if got := a.playlists.table.GetCell(playlistsHeaderRows+1, 1).Text; got != "" {
		t.Errorf("Jazz's Count cell (no known count) = %q, want empty", got)
	}
}
