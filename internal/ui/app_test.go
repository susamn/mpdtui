package ui

import (
	"strings"
	"testing"

	"github.com/gdamore/tcell/v2"

	"mpdtui/internal/mpdclient"
)

func TestFormatHintsBoldsOnlyTheKey(t *testing.T) {
	got := formatHints([]hint{{"Enter", "play"}})
	want := "[::b]Enter[-:-:-]:play"
	if got != want {
		t.Errorf("formatHints = %q, want %q", got, want)
	}
}

func TestFormatHintsJoinsMultipleWithTwoSpaces(t *testing.T) {
	got := formatHints([]hint{{"a", "one"}, {"b", "two"}})
	want := "[::b]a[-:-:-]:one  [::b]b[-:-:-]:two"
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
	if !strings.Contains(text, "[::b]Space[-:-:-]:play/pause") {
		t.Errorf("hint bar text = %q, want the Space key bolded", text)
	}
	if !strings.Contains(text, "[::b]Enter[-:-:-]:play") {
		t.Errorf("hint bar text = %q, want the Queue panel's Enter hint bolded", text)
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
	if !strings.Contains(libraryText, "[::b]o[-:-:-]:sort") {
		t.Errorf("Library hint bar text = %q, want an %q hint", libraryText, "o:sort")
	}

	a.tv.SetFocus(a.queue.table)
	a.updateHintBar()
	queueText := a.hintBar.GetText(false)
	if strings.Contains(queueText, ":sort") {
		t.Errorf("Queue hint bar text = %q, want no sort hint (o is invalid on Queue)", queueText)
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
	if row != 1 {
		t.Errorf("selected row after 'L' = %d, want 1 (song id 2)", row)
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
