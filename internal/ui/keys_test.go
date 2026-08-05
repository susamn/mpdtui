package ui

import (
	"testing"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

// newTestApp builds a fully-wired App (real tview widgets, no running
// screen and no MPD client) so key-handling logic can be driven directly
// through globalInputCapture.
func newTestApp() *App {
	a := &App{tv: tview.NewApplication()}
	a.build()
	return a
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
