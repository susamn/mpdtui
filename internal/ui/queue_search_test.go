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
	if row, _ := a.queue.table.GetSelection(); row != 0 {
		t.Errorf("selected row = %d, want 0 (first Boards of Canada track)", row)
	}
}

func TestQueueJumpToMatchNoMatchLeavesSelectionAlone(t *testing.T) {
	a := newTestApp()
	a.queue.songs = testSongs()
	a.queue.table.Select(1, 0)

	if ok := a.queue.jumpToMatch("nonexistent"); ok {
		t.Fatal("expected no match for \"nonexistent\"")
	}
	if row, _ := a.queue.table.GetSelection(); row != 1 {
		t.Errorf("selection changed on no-match: row = %d, want 1 (untouched)", row)
	}
}

// TestQueueSearchOpensBottomBarNotPopup asserts '/' on the Queue panel does
// NOT redirect into a Library search (the bug being fixed) and instead
// swaps the hint bar for an inline input, leaving the Queue table itself
// still part of the layout (nothing filtered out from under it).
func TestQueueSearchOpensBottomBarNotPopup(t *testing.T) {
	a := newTestApp()
	a.queue.songs = testSongs()
	a.tv.SetFocus(a.queue.table)

	a.openSearch()

	if a.mode != modeOverlay {
		t.Fatalf("mode = %d, want modeOverlay", a.mode)
	}
	if a.library.level == libSearch {
		t.Error("'/' on Queue should not trigger a Library search")
	}
	input, ok := a.tv.GetFocus().(*tview.InputField)
	if !ok {
		t.Fatalf("focus after openSearch = %T, want *tview.InputField", a.tv.GetFocus())
	}
	if input.GetLabel() != "/" {
		t.Errorf("input label = %q, want \"/\"", input.GetLabel())
	}
}

func TestQueueSearchEscapeCancelsAndRestoresHintBar(t *testing.T) {
	a := newTestApp()
	a.tv.SetFocus(a.queue.table)
	a.openSearch()

	esc := tcell.NewEventKey(tcell.KeyEscape, 0, tcell.ModNone)
	if result := a.globalInputCapture(esc); result != nil {
		t.Errorf("Escape while the queue search bar is open should be consumed, got %v", result)
	}

	if a.mode != modeNormal {
		t.Errorf("mode after Escape = %d, want modeNormal", a.mode)
	}
	if a.tv.GetFocus() != a.queue.table {
		t.Errorf("focus after Escape = %T, want the Queue table", a.tv.GetFocus())
	}
}

func TestQueueSearchEnterJumpsToMatch(t *testing.T) {
	a := newTestApp()
	a.queue.songs = testSongs()
	a.tv.SetFocus(a.queue.table)
	a.openSearch()

	input, ok := a.tv.GetFocus().(*tview.InputField)
	if !ok {
		t.Fatalf("focus after openSearch = %T, want *tview.InputField", a.tv.GetFocus())
	}
	input.SetText("aphex")

	handler := input.InputHandler()
	handler(tcell.NewEventKey(tcell.KeyEnter, 0, tcell.ModNone), func(tview.Primitive) {})

	if a.mode != modeNormal {
		t.Fatalf("mode after Enter = %d, want modeNormal", a.mode)
	}
	if row, _ := a.queue.table.GetSelection(); row != 1 {
		t.Errorf("selected row after Enter = %d, want 1 (Aphex Twin track)", row)
	}
	if a.tv.GetFocus() != a.queue.table {
		t.Errorf("focus after Enter = %T, want the Queue table", a.tv.GetFocus())
	}
}
