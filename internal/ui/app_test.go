package ui

import (
	"strings"
	"testing"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"mpdtui/internal/mpdclient"
)

func TestFormatHintsBoldsOnlyTheKey(t *testing.T) {
	got := formatHints([]hint{{"Enter", "play"}})
	want := "[skyblue::b]Enter[-:-:-]:play"
	if got != want {
		t.Errorf("formatHints = %q, want %q", got, want)
	}
}

func TestFormatHintsJoinsMultipleWithTwoSpaces(t *testing.T) {
	got := formatHints([]hint{{"a", "one"}, {"b", "two"}})
	want := "[skyblue::b]a[-:-:-]:one  [skyblue::b]b[-:-:-]:two"
	if got != want {
		t.Errorf("formatHints = %q, want %q", got, want)
	}
}

func TestFormatHintsEmptyIsEmptyString(t *testing.T) {
	if got := formatHints(nil); got != "" {
		t.Errorf("formatHints(nil) = %q, want empty", got)
	}
}

func TestUpdateHintBarShowsGlobalLabelAndBoldedKeys(t *testing.T) {
	a := newTestApp()
	a.tv.SetFocus(a.queue.table)
	a.updateHintBar()

	text := a.hintBar.GetText(false)
	if !strings.Contains(text, "Global:") {
		t.Errorf("hint bar text = %q, want it to contain a %q label", text, "Global:")
	}
	if !strings.Contains(text, "[skyblue::b]Space[-:-:-]:toggle") {
		t.Errorf("hint bar text = %q, want the Space key bolded/colored", text)
	}
	if !strings.Contains(text, "[skyblue::b]Enter[-:-:-]:play") {
		t.Errorf("hint bar text = %q, want the Queue panel's Enter hint bolded/colored", text)
	}
}

func TestUpdateHintBarPutsContextualSortInPanelSectionNotGlobal(t *testing.T) {
	// 'o' behaves differently per panel (and is invalid on Queue), so it
	// belongs in the per-panel hints, not the always-the-same-everywhere
	// Global section.
	a := newTestApp()
	a.tv.SetFocus(a.library.tree)
	a.updateHintBar()
	libraryText := a.hintBar.GetText(false)
	if !strings.Contains(libraryText, "[skyblue::b]o[-:-:-]:sort") {
		t.Errorf("Library hint bar text = %q, want an %q hint", libraryText, "o:sort")
	}

	a.tv.SetFocus(a.queue.table)
	a.updateHintBar()
	queueText := a.hintBar.GetText(false)
	if strings.Contains(queueText, ":sort") {
		t.Errorf("Queue hint bar text = %q, want no sort hint (o is invalid on Queue)", queueText)
	}
}

func TestClearAllSearchesResetsPlaylistsFilterRegardlessOfFocus(t *testing.T) {
	a := newTestApp()
	setPlaylistsForTest(a.playlists, []string{"Rock Anthems", "Jazz Classics", "Rock Ballads"})
	a.playlists.setFilter("rock")
	a.tv.SetFocus(a.queue.table) // deliberately not the Playlists panel

	a.clearAllSearches()

	if a.playlists.filter != "" {
		t.Errorf("playlists.filter after clearAllSearches = %q, want empty", a.playlists.filter)
	}
	if got := a.playlists.list.GetItemCount(); got != 3 {
		t.Errorf("playlists item count after clearAllSearches = %d, want 3 (filter lifted)", got)
	}
}

func TestClearAllSearchesFlashesNothingToClearWhenAlreadyClear(t *testing.T) {
	a := newTestApp()

	a.clearAllSearches()

	if !strings.Contains(a.hintBar.GetText(false), "nothing to clear") {
		t.Errorf("hint bar after clearAllSearches with nothing active = %q, want it to mention nothing to clear", a.hintBar.GetText(false))
	}
}

// TestClearAllSearchesResetsLibrarySearchMode exercises the actual MPD
// fetch clearAllSearches triggers via library.showRoot(), unlike the
// Playlists-only cases above which are pure in-memory state. Read-only.
func TestClearAllSearchesResetsLibrarySearchMode(t *testing.T) {
	c := dialOrSkip(t)
	a := &App{tv: tview.NewApplication(), client: c}
	a.build()
	a.library.mode = libSearch // simulate a completed search without a real query round-trip

	a.clearAllSearches()

	if a.library.mode != libBrowse {
		t.Errorf("library.mode after clearAllSearches = %v, want libBrowse", a.library.mode)
	}
}

func TestLKeyJumpsToCurrentTrackAndFocusesQueue(t *testing.T) {
	a := newTestApp()
	a.queue.songs = []mpdclient.Song{{ID: 1, Title: "First"}, {ID: 2, Title: "Second"}}
	a.queue.render(-1)
	a.queue.setCurrent(2)
	a.tv.SetFocus(a.library.tree)

	lKey := tcell.NewEventKey(tcell.KeyRune, 'L', tcell.ModNone)
	if result := a.globalInputCapture(lKey); result != nil {
		t.Errorf("'L' should be consumed, got %v", result)
	}

	if a.tv.GetFocus() != a.queue.table {
		t.Errorf("focus after 'L' = %T, want the Queue table", a.tv.GetFocus())
	}
	row, _ := a.queue.table.GetSelection()
	if row != 1+queueHeaderRows {
		t.Errorf("selected row after 'L' = %d, want %d (song id 2)", row, 1+queueHeaderRows)
	}
}

func TestTrackChangedForJump(t *testing.T) {
	cases := []struct {
		name              string
		startedUp         bool
		previousID, newID int
		want              bool
	}{
		{"genuine change after startup", true, 5, 7, true},
		{"suppressed before startup completes", false, 5, 7, false},
		{"same track re-confirmed on a tick", true, 5, 5, false},
		{"playback stopped (newID -1)", true, 5, -1, false},
		{"first-ever real track after nothing playing", true, -1, 3, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := trackChangedForJump(tc.startedUp, tc.previousID, tc.newID); got != tc.want {
				t.Errorf("trackChangedForJump(%v, %d, %d) = %v, want %v", tc.startedUp, tc.previousID, tc.newID, got, tc.want)
			}
		})
	}
}

// TestMaybeJumpToCurrentTrack* exercise maybeJumpToCurrentTrack directly
// (via newTestApp, no live client) rather than refreshNowPlaying/
// refreshAll end-to-end: those need a real MPD client for Status/
// CurrentSong, and -- more importantly -- refreshNowPlaying's
// albumArt.onTrackChanged spawns a background fetch goroutine that
// outlives the test if it hasn't returned by the time dialOrSkip's
// t.Cleanup closes the connection; mpdclient.Client.Close doesn't nil out
// its underlying conn, and gompd panics (rather than erroring) when a
// command runs on a closed one. maybeJumpToCurrentTrack contains all the
// actual decision/focus logic worth testing here, with none of that risk.

func TestMaybeJumpToCurrentTrackFocusesQueueWhenChanged(t *testing.T) {
	a := newTestApp()
	a.queue.songs = []mpdclient.Song{{ID: 1, Title: "First"}, {ID: 2, Title: "Second"}}
	a.queue.render(-1)
	a.queue.setCurrent(2)
	a.tv.SetFocus(a.library.tree)

	a.maybeJumpToCurrentTrack(true)

	if a.tv.GetFocus() != a.queue.table {
		t.Errorf("focus after maybeJumpToCurrentTrack(true) = %T, want the Queue table", a.tv.GetFocus())
	}
	row, _ := a.queue.table.GetSelection()
	if row != 1+queueHeaderRows {
		t.Errorf("selected row = %d, want %d (song id 2)", row, 1+queueHeaderRows)
	}
}

func TestMaybeJumpToCurrentTrackNoOpWhenNotChanged(t *testing.T) {
	a := newTestApp()
	a.queue.songs = []mpdclient.Song{{ID: 1, Title: "First"}}
	a.queue.render(-1)
	a.queue.setCurrent(1)
	a.tv.SetFocus(a.library.tree)

	a.maybeJumpToCurrentTrack(false)

	if a.tv.GetFocus() != a.library.tree {
		t.Errorf("focus after maybeJumpToCurrentTrack(false) = %T, want it left alone (still Library)", a.tv.GetFocus())
	}
}

func TestMaybeJumpToCurrentTrackSkipsWhenOverlayOpen(t *testing.T) {
	a := newTestApp()
	a.queue.songs = []mpdclient.Song{{ID: 1, Title: "First"}}
	a.queue.render(-1)
	a.queue.setCurrent(1)
	a.tv.SetFocus(a.library.tree)
	a.mode = modeOverlay

	a.maybeJumpToCurrentTrack(true)

	if a.tv.GetFocus() != a.library.tree {
		t.Errorf("focus while an overlay is open = %T, want it left alone (still Library)", a.tv.GetFocus())
	}
}

func TestMaybeJumpToCurrentTrackNoOpWhenSongNotInQueue(t *testing.T) {
	a := newTestApp()
	a.queue.songs = []mpdclient.Song{{ID: 1, Title: "First"}}
	a.queue.render(-1)
	a.queue.setCurrent(99) // not present in songs
	a.tv.SetFocus(a.library.tree)

	a.maybeJumpToCurrentTrack(true)

	if a.tv.GetFocus() != a.library.tree {
		t.Errorf("focus when the current id isn't in the queue = %T, want it left alone (still Library)", a.tv.GetFocus())
	}
}

func TestLKeyWithNothingPlayingFlashesMessageWithoutChangingFocus(t *testing.T) {
	a := newTestApp()
	a.tv.SetFocus(a.library.tree)

	lKey := tcell.NewEventKey(tcell.KeyRune, 'L', tcell.ModNone)
	if result := a.globalInputCapture(lKey); result != nil {
		t.Errorf("'L' should be consumed, got %v", result)
	}
	if a.tv.GetFocus() != a.library.tree {
		t.Errorf("focus after 'L' with nothing playing = %T, want it to stay on Library", a.tv.GetFocus())
	}
}
