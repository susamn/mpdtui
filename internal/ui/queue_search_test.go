package ui

import (
	"testing"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"mpdtui/internal/mpdclient"
)

func testSongs() []mpdclient.Song {
	return []mpdclient.Song{
		{ID: 1, Pos: 0, Artist: "Boards of Canada", Title: "Roygbiv"},
		{ID: 2, Pos: 1, Artist: "Aphex Twin", Title: "Xtal"},
		{ID: 3, Pos: 2, Artist: "Boards of Canada", Title: "Telephasic Workshop"},
	}
}

func TestQueueJumpToMatchSelectsFirstCaseInsensitiveMatch(t *testing.T) {
	a := newTestApp()
	a.queue.songs = testSongs()

	if ok := a.queue.jumpToMatch("boards"); !ok {
		t.Fatal("expected a match for \"boards\"")
	}
	if row, _ := a.queue.table.GetSelection(); row != queueHeaderRows {
		t.Errorf("selected row = %d, want %d (first Boards of Canada track)", row, queueHeaderRows)
	}
}

func TestQueueJumpToMatchNoMatchLeavesSelectionAlone(t *testing.T) {
	a := newTestApp()
	a.queue.songs = testSongs()
	a.queue.table.Select(1+queueHeaderRows, 0)

	if ok := a.queue.jumpToMatch("nonexistent"); ok {
		t.Fatal("expected no match for \"nonexistent\"")
	}
	if row, _ := a.queue.table.GetSelection(); row != 1+queueHeaderRows {
		t.Errorf("selection changed on no-match: row = %d, want %d (untouched)", row, 1+queueHeaderRows)
	}
}

// TestQueueSearchFocusesPersistentField asserts '/' on the Queue panel
// does NOT redirect into a Library search (the bug being fixed) and does
// NOT use a centered popup (unlike Library/Playlists): it focuses the
// search field that's permanently pinned below the Queue table, built
// once in newQueuePanel rather than created per search.
func TestQueueSearchFocusesPersistentField(t *testing.T) {
	a := newTestApp()
	a.queue.songs = testSongs()
	a.tv.SetFocus(a.queue.table)

	a.openSearch()

	if a.mode != modeOverlay {
		t.Fatalf("mode = %d, want modeOverlay", a.mode)
	}
	if a.library.mode == libSearch {
		t.Error("'/' on Queue should not trigger a Library search")
	}
	if a.tv.GetFocus() != a.queue.search {
		t.Errorf("focus after openSearch = %T, want the persistent Queue search field", a.tv.GetFocus())
	}
	if got := a.queue.search.GetLabel(); got != "Search track: " {
		t.Errorf("search field label = %q, want %q", got, "Search track: ")
	}
}

func TestQueueSearchEscapeCancelsAndClearsField(t *testing.T) {
	a := newTestApp()
	a.tv.SetFocus(a.queue.table)
	a.openSearch()
	a.queue.search.SetText("partial query")

	esc := tcell.NewEventKey(tcell.KeyEscape, 0, tcell.ModNone)
	if result := a.globalInputCapture(esc); result != nil {
		t.Errorf("Escape while the queue search field is focused should be consumed, got %v", result)
	}

	if a.mode != modeNormal {
		t.Errorf("mode after Escape = %d, want modeNormal", a.mode)
	}
	if a.tv.GetFocus() != a.queue.table {
		t.Errorf("focus after Escape = %T, want the Queue table", a.tv.GetFocus())
	}
	if got := a.queue.search.GetText(); got != "" {
		t.Errorf("search field text after Escape = %q, want cleared", got)
	}
}

func TestQueueSearchEnterJumpsToMatch(t *testing.T) {
	a := newTestApp()
	a.queue.songs = testSongs()
	a.tv.SetFocus(a.queue.table)
	a.openSearch()
	a.queue.search.SetText("aphex")

	handler := a.queue.search.InputHandler()
	handler(tcell.NewEventKey(tcell.KeyEnter, 0, tcell.ModNone), func(tview.Primitive) {})

	if a.mode != modeNormal {
		t.Fatalf("mode after Enter = %d, want modeNormal", a.mode)
	}
	if row, _ := a.queue.table.GetSelection(); row != 1+queueHeaderRows {
		t.Errorf("selected row after Enter = %d, want %d (Aphex Twin track)", row, 1+queueHeaderRows)
	}
	if a.tv.GetFocus() != a.queue.table {
		t.Errorf("focus after Enter = %T, want the Queue table", a.tv.GetFocus())
	}
	if got := a.queue.search.GetText(); got != "" {
		t.Errorf("search field text after Enter = %q, want cleared", got)
	}
}
