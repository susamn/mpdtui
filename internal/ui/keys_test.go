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
	return a
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
	a.playlists.names = []string{"Rock Anthems", "Jazz Classics", "Rock Ballads"}
	a.playlists.setFilter("rock")
	if got := a.playlists.list.GetItemCount(); got != 2 {
		t.Fatalf("setup: filtered item count = %d, want 2", got)
	}

	a.tv.SetFocus(a.playlists.list)
	esc := tcell.NewEventKey(tcell.KeyEscape, 0, tcell.ModNone)
	if result := a.globalInputCapture(esc); result != nil {
		t.Errorf("Escape over an active filter should be consumed, got %v", result)
	}

	if a.playlists.filter != "" {
		t.Errorf("filter after Escape = %q, want empty", a.playlists.filter)
	}
	if got := a.playlists.list.GetItemCount(); got != 3 {
		t.Errorf("item count after Escape = %d, want 3 (unfiltered)", got)
	}
}

func TestEscapeIgnoredWithoutActiveFilter(t *testing.T) {
	a := newTestApp()
	a.playlists.names = []string{"Rock Anthems", "Jazz Classics"}
	a.tv.SetFocus(a.playlists.list)

	esc := tcell.NewEventKey(tcell.KeyEscape, 0, tcell.ModNone)
	if result := a.globalInputCapture(esc); result == nil {
		t.Error("Escape with no active filter should pass through, not be consumed")
	}
}

func TestEscapeDoesNotTouchPlaylistsFilterFromOtherPanel(t *testing.T) {
	a := newTestApp()
	a.playlists.names = []string{"Rock Anthems", "Jazz Classics"}
	a.playlists.setFilter("rock")
	a.tv.SetFocus(a.library.list)

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

	a.library.level = libSearch
	a.library.query = "test"

	a.tv.SetFocus(a.library.list)
	esc := tcell.NewEventKey(tcell.KeyEscape, 0, tcell.ModNone)
	if result := a.globalInputCapture(esc); result != nil {
		t.Errorf("Escape over an active library search should be consumed, got %v", result)
	}

	if a.library.level != libArtists {
		t.Errorf("level after Escape = %v, want libArtists", a.library.level)
	}
}

func TestEscapeIgnoredWithoutActiveLibrarySearch(t *testing.T) {
	a := newTestApp()
	a.tv.SetFocus(a.library.list)

	esc := tcell.NewEventKey(tcell.KeyEscape, 0, tcell.ModNone)
	if result := a.globalInputCapture(esc); result == nil {
		t.Error("Escape with no active library search should pass through, not be consumed")
	}
}
