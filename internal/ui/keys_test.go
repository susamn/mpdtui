package ui

import (
	"testing"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"mpdtui/internal/config"
	"mpdtui/internal/mpdclient"
)

// newTestApp builds a fully-wired App (real tview widgets, no running
// screen and no MPD client) so key-handling logic can be driven directly
// through globalInputCapture. Only valid for cases that never reach the
// client (e.g. Playlists filtering, which is pure in-memory state).
func newTestApp() *App {
	a := &App{tv: tview.NewApplication()}
	a.build()
	a.queue.table.SetRect(0, 0, 150, 40)
	return a
}

// setPlaylistsForTest bypasses refresh's MPD call to seed the Playlists
// panel directly, for tests without a live server. Timestamps default to
// the zero value, which is fine for tests exercising filter/selection
// behavior rather than recency sort/badge behavior specifically -- render
// preserves whatever order names are given in here, since only refresh
// (not render) re-sorts by Last-Modified.
func setPlaylistsForTest(p *playlistsPanel, names []string) {
	p.pls = make([]mpdclient.Playlist, len(names))
	for i, n := range names {
		p.pls[i] = mpdclient.Playlist{Name: n}
	}
	p.render()
}

// dialOrSkip mirrors internal/mpdclient/tests' helper of the same name:
// skips the test automatically if no MPD server is reachable, since
// library.back() genuinely calls out to MPD (unlike the Playlists filter).
func dialOrSkip(t *testing.T) *mpdclient.Client {
	t.Helper()
	c, err := mpdclient.Dial(config.Load())
	if err != nil {
		t.Skipf("no MPD server reachable, skipping: %v", err)
	}
	t.Cleanup(func() { c.Close() })
	return c
}

func TestEscapeClearsPlaylistsFilter(t *testing.T) {
	a := newTestApp()
	setPlaylistsForTest(a.playlists, []string{"Rock Anthems", "Jazz Classics", "Rock Ballads"})
	a.playlists.setFilter("rock")
	if got := len(a.playlists.shown); got != 2 {
		t.Fatalf("setup: filtered item count = %d, want 2", got)
	}

	a.tv.SetFocus(a.playlists.table)
	esc := tcell.NewEventKey(tcell.KeyEscape, 0, tcell.ModNone)
	if result := a.globalInputCapture(esc); result != nil {
		t.Errorf("Escape over an active filter should be consumed, got %v", result)
	}

	if a.playlists.filter != "" {
		t.Errorf("filter after Escape = %q, want empty", a.playlists.filter)
	}
	if got := len(a.playlists.shown); got != 3 {
		t.Errorf("item count after Escape = %d, want 3 (unfiltered)", got)
	}
}

func TestEscapeIgnoredWithoutActiveFilter(t *testing.T) {
	a := newTestApp()
	setPlaylistsForTest(a.playlists, []string{"Rock Anthems", "Jazz Classics"})
	a.tv.SetFocus(a.playlists.table)

	esc := tcell.NewEventKey(tcell.KeyEscape, 0, tcell.ModNone)
	if result := a.globalInputCapture(esc); result == nil {
		t.Error("Escape with no active filter should pass through, not be consumed")
	}
}

func TestEscapeDoesNotTouchPlaylistsFilterFromOtherPanel(t *testing.T) {
	a := newTestApp()
	setPlaylistsForTest(a.playlists, []string{"Rock Anthems", "Jazz Classics"})
	a.playlists.setFilter("rock")
	a.tv.SetFocus(a.library.tree)

	esc := tcell.NewEventKey(tcell.KeyEscape, 0, tcell.ModNone)
	if result := a.globalInputCapture(esc); result == nil {
		t.Error("Escape while Library is focused should pass through, not be consumed by the Playlists handler")
	}
	if a.playlists.filter != "rock" {
		t.Errorf("filter changed while Library was focused: got %q, want %q", a.playlists.filter, "rock")
	}
}

func TestEscapeClearsLibrarySearch(t *testing.T) {
	a := &App{tv: tview.NewApplication(), client: dialOrSkip(t)}
	a.build()

	a.library.mode = libSearch
	a.library.query = "test"

	a.tv.SetFocus(a.library.tree)
	esc := tcell.NewEventKey(tcell.KeyEscape, 0, tcell.ModNone)
	if result := a.globalInputCapture(esc); result != nil {
		t.Errorf("Escape over an active library search should be consumed, got %v", result)
	}

	if a.library.mode != libBrowse {
		t.Errorf("mode after Escape = %v, want libBrowse", a.library.mode)
	}
}

func TestEscapeIgnoredWithoutActiveLibrarySearch(t *testing.T) {
	a := newTestApp()
	a.tv.SetFocus(a.library.tree)

	esc := tcell.NewEventKey(tcell.KeyEscape, 0, tcell.ModNone)
	if result := a.globalInputCapture(esc); result == nil {
		t.Error("Escape with no active library search should pass through, not be consumed")
	}
}

func TestOKeyCyclesPlaylistsSortMode(t *testing.T) {
	a := newTestApp()
	setPlaylistsForTest(a.playlists, []string{"Rock", "Jazz"})
	a.tv.SetFocus(a.playlists.table)

	oKey := tcell.NewEventKey(tcell.KeyRune, 'o', tcell.ModNone)
	if result := a.globalInputCapture(oKey); result != nil {
		t.Errorf("'o' should be consumed, got %v", result)
	}
	if a.playlists.sortMode != playlistsSortName {
		t.Errorf("sortMode after 'o' = %v, want playlistsSortName", a.playlists.sortMode)
	}
}

func TestOKeyCyclesLibrarySortModeInBrowseMode(t *testing.T) {
	a := &App{tv: tview.NewApplication(), client: dialOrSkip(t)}
	a.build()
	a.library.showRoot()
	a.tv.SetFocus(a.library.tree)

	oKey := tcell.NewEventKey(tcell.KeyRune, 'o', tcell.ModNone)
	if result := a.globalInputCapture(oKey); result != nil {
		t.Errorf("'o' should be consumed, got %v", result)
	}
	if a.library.sortMode != librarySortRecent {
		t.Errorf("sortMode after 'o' = %v, want librarySortRecent", a.library.sortMode)
	}
}

func TestOKeyInLibrarySearchModeDoesNotChangeSortMode(t *testing.T) {
	a := newTestApp()
	a.library.mode = libSearch
	a.tv.SetFocus(a.library.tree)

	oKey := tcell.NewEventKey(tcell.KeyRune, 'o', tcell.ModNone)
	if result := a.globalInputCapture(oKey); result != nil {
		t.Errorf("'o' should be consumed (flashes invalid) even in search mode, got %v", result)
	}
	if a.library.sortMode != librarySortName {
		t.Errorf("sortMode changed while in search mode: got %v, want unchanged librarySortName", a.library.sortMode)
	}
}

func TestOKeyOnQueuePanelIsInvalid(t *testing.T) {
	a := newTestApp()
	a.tv.SetFocus(a.queue.table)

	oKey := tcell.NewEventKey(tcell.KeyRune, 'o', tcell.ModNone)
	if result := a.globalInputCapture(oKey); result != nil {
		t.Errorf("'o' should be consumed (flashes invalid), got %v", result)
	}
}
