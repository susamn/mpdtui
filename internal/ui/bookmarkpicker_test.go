package ui

import (
	"strings"
	"testing"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"mpdtui/internal/mpdclient"
)

func TestBookmarkKeyWhenNotEnabled(t *testing.T) {
	a := newTestApp()
	bKey := tcell.NewEventKey(tcell.KeyRune, 'b', tcell.ModNone)
	if res := a.globalInputCapture(bKey); res != nil {
		t.Errorf("'b' should be consumed, got %v", res)
	}
	if !strings.Contains(a.hintBar.GetText(false), "track metadata not enabled") {
		t.Errorf("hint bar = %q, want metadata not enabled flash", a.hintBar.GetText(false))
	}

	capBKey := tcell.NewEventKey(tcell.KeyRune, 'B', tcell.ModNone)
	if res := a.globalInputCapture(capBKey); res != nil {
		t.Errorf("'B' should be consumed, got %v", res)
	}
	if !strings.Contains(a.hintBar.GetText(false), "track metadata not enabled") {
		t.Errorf("hint bar = %q, want metadata not enabled flash", a.hintBar.GetText(false))
	}
}

func TestBookmarkKeyWhenNothingPlaying(t *testing.T) {
	a := newTestAppWithMetaDB(t)
	a.currentStatus = mpdclient.Status{State: mpdclient.StateStop}

	bKey := tcell.NewEventKey(tcell.KeyRune, 'b', tcell.ModNone)
	if res := a.globalInputCapture(bKey); res != nil {
		t.Errorf("'b' should be consumed, got %v", res)
	}
	if !strings.Contains(a.hintBar.GetText(false), "nothing playing to bookmark") {
		t.Errorf("hint bar = %q, want nothing playing flash", a.hintBar.GetText(false))
	}
}

func TestBookmarkKeyOffsetAndCreation(t *testing.T) {
	a := newTestAppWithMetaDB(t)
	song := mpdclient.Song{File: "artist/album/track.mp3", Title: "Test Track"}
	a.currentSong = song
	a.currentStatus = mpdclient.Status{State: mpdclient.StatePlay, Elapsed: 10 * time.Second}

	bKey := tcell.NewEventKey(tcell.KeyRune, 'b', tcell.ModNone)
	if res := a.globalInputCapture(bKey); res != nil {
		t.Errorf("'b' should be consumed, got %v", res)
	}

	if a.mode != modeOverlay {
		t.Fatalf("a.mode = %d, want modeOverlay", a.mode)
	}

	// Focus is on text box
	field, ok := a.tv.GetFocus().(*tview.TextArea)
	if !ok {
		t.Fatalf("focus = %T, want *tview.TextArea", a.tv.GetFocus())
	}
	if !strings.Contains(field.GetTitle(), "What do you want to remember here") {
		t.Errorf("title = %q, want 'What do you want to remember here'", field.GetTitle())
	}

	// Check that pressing Esc cancels without saving
	escKey := tcell.NewEventKey(tcell.KeyEscape, 0, tcell.ModNone)
	a.globalInputCapture(escKey)
	if a.mode != modeNormal {
		t.Fatalf("a.mode after Esc = %d, want modeNormal", a.mode)
	}
	bms, err := a.metaDB.BookmarksForTrack(song.File)
	if err != nil {
		t.Fatalf("BookmarksForTrack: %v", err)
	}
	if len(bms) != 0 {
		t.Fatalf("expected 0 bookmarks after cancel, got %d", len(bms))
	}

	// Press 'b' again and submit text
	a.globalInputCapture(bKey)
	field, _ = a.tv.GetFocus().(*tview.TextArea)
	field.SetText("awesome intro", true)
	enterKey := tcell.NewEventKey(tcell.KeyEnter, 0, tcell.ModNone)
	field.InputHandler()(enterKey, nil)

	if a.mode != modeNormal {
		t.Fatalf("a.mode after Enter = %d, want modeNormal", a.mode)
	}

	bms, err = a.metaDB.BookmarksForTrack(song.File)
	if err != nil {
		t.Fatalf("BookmarksForTrack: %v", err)
	}
	if len(bms) != 1 {
		t.Fatalf("expected 1 bookmark, got %d", len(bms))
	}
	if bms[0].PositionSeconds != 8.0 {
		t.Errorf("PositionSeconds = %v, want 8.0", bms[0].PositionSeconds)
	}
	if bms[0].Text != "awesome intro" {
		t.Errorf("Text = %q, want 'awesome intro'", bms[0].Text)
	}
}

func TestBookmarkKeyClampingAtZero(t *testing.T) {
	a := newTestAppWithMetaDB(t)
	song := mpdclient.Song{File: "artist/album/intro.mp3", Title: "Intro"}
	a.currentSong = song
	a.currentStatus = mpdclient.Status{State: mpdclient.StatePlay, Elapsed: 1 * time.Second}

	bKey := tcell.NewEventKey(tcell.KeyRune, 'b', tcell.ModNone)
	a.globalInputCapture(bKey)

	field, ok := a.tv.GetFocus().(*tview.TextArea)
	if !ok {
		t.Fatalf("focus = %T, want *tview.TextArea", a.tv.GetFocus())
	}
	field.SetText("very start", true)
	enterKey := tcell.NewEventKey(tcell.KeyEnter, 0, tcell.ModNone)
	field.InputHandler()(enterKey, nil)

	bms, err := a.metaDB.BookmarksForTrack(song.File)
	if err != nil {
		t.Fatalf("BookmarksForTrack: %v", err)
	}
	if len(bms) != 1 {
		t.Fatalf("expected 1 bookmark, got %d", len(bms))
	}
	if bms[0].PositionSeconds != 0.0 {
		t.Errorf("PositionSeconds = %v, want 0.0 (clamped)", bms[0].PositionSeconds)
	}
}

func TestBookmarkManagerListingAndCRUD(t *testing.T) {
	a := newTestAppWithMetaDB(t)
	song := mpdclient.Song{File: "rock/anthem.mp3", Title: "Anthem", ID: 10}
	a.currentSong = song
	a.currentStatus = mpdclient.Status{State: mpdclient.StatePlay, Elapsed: 30 * time.Second}

	// Press 'B' to open bookmark manager
	capBKey := tcell.NewEventKey(tcell.KeyRune, 'B', tcell.ModNone)
	a.globalInputCapture(capBKey)

	if a.mode != modeOverlay {
		t.Fatalf("a.mode = %d, want modeOverlay", a.mode)
	}
	if !a.bookmarkPicker.focused() {
		t.Fatal("bookmarkPicker should be focused")
	}
	if len(a.bookmarkPicker.bookmarks) != 0 {
		t.Fatalf("expected 0 bookmarks initially, got %d", len(a.bookmarkPicker.bookmarks))
	}
	if !a.bookmarkPicker.allowsGlobalKeys() {
		t.Error("table mode should allow global keys")
	}

	// Test 'a' to add a bookmark
	aKey := tcell.NewEventKey(tcell.KeyRune, 'a', tcell.ModNone)
	a.globalInputCapture(aKey)
	if a.bookmarkPicker.mode != bmModeAdd {
		t.Fatalf("bookmarkPicker.mode = %d, want bmModeAdd", a.bookmarkPicker.mode)
	}
	if a.bookmarkPicker.allowsGlobalKeys() {
		t.Error("add mode should NOT allow global keys (typing text)")
	}
	// Expected addPosition is 30s - 2s = 28s
	if a.bookmarkPicker.addPosition != 28.0 {
		t.Errorf("addPosition = %v, want 28.0", a.bookmarkPicker.addPosition)
	}

	// Submit input
	a.bookmarkPicker.input.SetText("guitar riff", true)
	enterKey := tcell.NewEventKey(tcell.KeyEnter, 0, tcell.ModNone)
	a.bookmarkPicker.input.InputHandler()(enterKey, nil)

	// Mode should return to list
	if a.bookmarkPicker.mode != bmModeList {
		t.Fatalf("mode after submit = %d, want bmModeList", a.bookmarkPicker.mode)
	}
	if len(a.bookmarkPicker.bookmarks) != 1 {
		t.Fatalf("expected 1 bookmark, got %d", len(a.bookmarkPicker.bookmarks))
	}
	if a.bookmarkPicker.bookmarks[0].Text != "guitar riff" {
		t.Errorf("bookmark text = %q, want 'guitar riff'", a.bookmarkPicker.bookmarks[0].Text)
	}
	if a.bookmarkPicker.bookmarks[0].PositionSeconds != 28.0 {
		t.Errorf("bookmark position = %v, want 28.0", a.bookmarkPicker.bookmarks[0].PositionSeconds)
	}

	// Test 'e' to edit bookmark
	eKey := tcell.NewEventKey(tcell.KeyRune, 'e', tcell.ModNone)
	a.globalInputCapture(eKey)
	if a.bookmarkPicker.mode != bmModeEdit {
		t.Fatalf("bookmarkPicker.mode = %d, want bmModeEdit", a.bookmarkPicker.mode)
	}
	if a.bookmarkPicker.input.GetText() != "guitar riff" {
		t.Errorf("input text = %q, want 'guitar riff'", a.bookmarkPicker.input.GetText())
	}
	// Edit text and submit
	a.bookmarkPicker.input.SetText("solo starts here", true)
	a.bookmarkPicker.input.InputHandler()(enterKey, nil)

	if a.bookmarkPicker.mode != bmModeList {
		t.Fatalf("mode after edit submit = %d, want bmModeList", a.bookmarkPicker.mode)
	}
	if a.bookmarkPicker.bookmarks[0].Text != "solo starts here" {
		t.Errorf("bookmark text after edit = %q, want 'solo starts here'", a.bookmarkPicker.bookmarks[0].Text)
	}

	// Test 'd' to delete bookmark
	dKey := tcell.NewEventKey(tcell.KeyRune, 'd', tcell.ModNone)
	a.globalInputCapture(dKey)
	if a.bookmarkPicker.mode != bmModeConfirmDelete {
		t.Fatalf("bookmarkPicker.mode = %d, want bmModeConfirmDelete", a.bookmarkPicker.mode)
	}

	// Cancel delete first with 'n'
	nKey := tcell.NewEventKey(tcell.KeyRune, 'n', tcell.ModNone)
	a.globalInputCapture(nKey)
	if a.bookmarkPicker.mode != bmModeList {
		t.Fatalf("mode after cancel delete = %d, want bmModeList", a.bookmarkPicker.mode)
	}
	if len(a.bookmarkPicker.bookmarks) != 1 {
		t.Fatalf("expected bookmark to remain after cancel, got %d", len(a.bookmarkPicker.bookmarks))
	}

	// Start delete again and confirm with 'y'
	a.globalInputCapture(dKey)
	yKey := tcell.NewEventKey(tcell.KeyRune, 'y', tcell.ModNone)
	a.globalInputCapture(yKey)
	if a.bookmarkPicker.mode != bmModeList {
		t.Fatalf("mode after confirm delete = %d, want bmModeList", a.bookmarkPicker.mode)
	}
	if len(a.bookmarkPicker.bookmarks) != 0 {
		t.Fatalf("expected 0 bookmarks after delete, got %d", len(a.bookmarkPicker.bookmarks))
	}

	// Close overlay with 'B'
	a.globalInputCapture(capBKey)
	if a.mode != modeNormal {
		t.Fatalf("a.mode after 'B' = %d, want modeNormal", a.mode)
	}
}

func TestBookmarkManagerEscapeCloses(t *testing.T) {
	a := newTestAppWithMetaDB(t)
	song := mpdclient.Song{File: "track.mp3", Title: "Track"}
	a.currentSong = song
	a.currentStatus = mpdclient.Status{State: mpdclient.StatePlay}

	a.openBookmarkManager(song)
	if a.mode != modeOverlay {
		t.Fatalf("a.mode = %d, want modeOverlay", a.mode)
	}

	escKey := tcell.NewEventKey(tcell.KeyEscape, 0, tcell.ModNone)
	a.globalInputCapture(escKey)
	if a.mode != modeNormal {
		t.Fatalf("a.mode after Esc = %d, want modeNormal", a.mode)
	}
}

func TestOpenTextBox(t *testing.T) {
	a := newTestApp()

	var submitted string
	a.openTextBox("Test Box", "initial text", func(text string) {
		submitted = text
	})

	if a.mode != modeOverlay {
		t.Fatalf("a.mode = %d, want modeOverlay", a.mode)
	}

	ta, ok := a.tv.GetFocus().(*tview.TextArea)
	if !ok {
		t.Fatalf("focus = %T, want *tview.TextArea", a.tv.GetFocus())
	}
	if ta.GetTitle() != "Test Box" {
		t.Errorf("title = %q, want 'Test Box'", ta.GetTitle())
	}
	if ta.GetText() != "initial text" {
		t.Errorf("text = %q, want 'initial text'", ta.GetText())
	}

	// Test Esc cancels
	escKey := tcell.NewEventKey(tcell.KeyEscape, 0, tcell.ModNone)
	ta.InputHandler()(escKey, nil)
	if a.mode != modeNormal {
		t.Fatalf("a.mode after Esc = %d, want modeNormal", a.mode)
	}
	if submitted != "" {
		t.Fatalf("submitted = %q after Esc, want empty", submitted)
	}

	// Test Enter submits
	a.openTextBox("Enter Test", "", func(text string) {
		submitted = text
	})
	ta, _ = a.tv.GetFocus().(*tview.TextArea)
	ta.SetText("note with enter", true)
	enterKey := tcell.NewEventKey(tcell.KeyEnter, 0, tcell.ModNone)
	ta.InputHandler()(enterKey, nil)
	if a.mode != modeNormal {
		t.Fatalf("a.mode after Enter = %d, want modeNormal", a.mode)
	}
	if submitted != "note with enter" {
		t.Errorf("submitted = %q, want 'note with enter'", submitted)
	}

	// Test Ctrl+S submits
	submitted = ""
	a.openTextBox("Ctrl+S Test", "", func(text string) {
		submitted = text
	})
	ta, _ = a.tv.GetFocus().(*tview.TextArea)
	ta.SetText("note with ctrl+s", true)
	ctrlSKey := tcell.NewEventKey(tcell.KeyCtrlS, 0, tcell.ModNone)
	ta.InputHandler()(ctrlSKey, nil)
	if a.mode != modeNormal {
		t.Fatalf("a.mode after Ctrl+S = %d, want modeNormal", a.mode)
	}
	if submitted != "note with ctrl+s" {
		t.Errorf("submitted = %q, want 'note with ctrl+s'", submitted)
	}
}
