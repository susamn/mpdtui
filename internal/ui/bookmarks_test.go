package ui

import (
	"strings"
	"testing"

	"github.com/gdamore/tcell/v2"

	"mpdtui/internal/mpdclient"
)

func bmApp(t *testing.T, f *fakeMPD) (*App, mpdclient.Song) {
	t.Helper()
	song := mpdclient.Song{ID: 5, File: "a-r-rahman/guru/05.mp3", Title: "Ay Hairathe"}
	a := newFakeAppWithMetaDB(t, f)
	a.openBookmarkManager(song)
	return a, song
}

// TestBookmarkJumpSeeksTheQueuedTrack covers the point of a bookmark:
// picking one seeks playback to its position. A queued track is sought
// by its own song id, so a jump cannot land on whatever happens to be
// playing if the queue moved underneath it.
func TestBookmarkJumpSeeksTheQueuedTrack(t *testing.T) {
	f := &fakeMPD{}
	a, song := bmApp(t, f)

	if _, err := a.metaDB.CreateBookmark(song.File, 95, "the key change"); err != nil {
		t.Fatalf("CreateBookmark: %v", err)
	}
	a.bookmarkPicker.render(song)

	a.bookmarkPicker.jumpTo(a.bookmarkPicker.bookmarks[0])

	if !f.did("seekid:5:95s") {
		t.Errorf("did %v, want a seek of the queued track to 95s", f.commands())
	}
	if a.mode != modeNormal {
		t.Error("the bookmark overlay stayed open after jumping")
	}
	if got := a.hintBar.GetText(true); !strings.Contains(got, "the key change") {
		t.Errorf("hint bar = %q, want it to name the bookmark jumped to", got)
	}
}

// TestBookmarkJumpForATrackNotInTheQueue falls back to seeking the
// current position, since there is no song id to target.
func TestBookmarkJumpForATrackNotInTheQueue(t *testing.T) {
	f := &fakeMPD{}
	a := newFakeAppWithMetaDB(t, f)
	song := mpdclient.Song{ID: -1, File: "loose/track.mp3"}
	a.openBookmarkManager(song)

	if _, err := a.metaDB.CreateBookmark(song.File, 12, "here"); err != nil {
		t.Fatalf("CreateBookmark: %v", err)
	}
	a.bookmarkPicker.render(song)

	a.bookmarkPicker.jumpTo(a.bookmarkPicker.bookmarks[0])

	if !f.did("seek+12s:rel=false") {
		t.Errorf("did %v, want an absolute seek to 12s", f.commands())
	}
}

func TestBookmarkJumpReportsAFailedSeek(t *testing.T) {
	f := &fakeMPD{err: errTest}
	a, song := bmApp(t, f)

	if _, err := a.metaDB.CreateBookmark(song.File, 30, "x"); err != nil {
		t.Fatalf("CreateBookmark: %v", err)
	}
	a.bookmarkPicker.render(song)

	a.bookmarkPicker.jumpTo(a.bookmarkPicker.bookmarks[0])

	if got := a.hintBar.GetText(true); !strings.Contains(got, errTest.Error()) {
		t.Errorf("hint bar = %q, want the seek failure reported", got)
	}
	if a.mode != modeOverlay {
		t.Error("the overlay closed despite the seek failing")
	}
}

// TestBookmarkEscapeFromTypingReturnsToTheList covers cancelInput: Esc
// while writing a note goes back to the list rather than closing the
// whole manager, so a mistyped note does not lose the overlay.
func TestBookmarkEscapeFromTypingReturnsToTheList(t *testing.T) {
	f := &fakeMPD{}
	a, _ := bmApp(t, f)

	a.bookmarkPicker.startAdd()
	if a.bookmarkPicker.mode != bmModeAdd {
		t.Fatalf("mode = %d after startAdd, want the add sub-view", a.bookmarkPicker.mode)
	}

	esc := tcell.NewEventKey(tcell.KeyEscape, 0, tcell.ModNone)
	if !a.bookmarkPicker.handleKey(esc) {
		t.Error("Escape while typing was not consumed")
	}
	if a.bookmarkPicker.mode != bmModeList {
		t.Errorf("mode = %d after Escape, want back to the list", a.bookmarkPicker.mode)
	}
	if a.mode != modeOverlay {
		t.Error("Escape while typing closed the whole manager")
	}
}

// TestBookmarkAltEnterIsLeftToTheTextBox covers the one key the picker
// deliberately does not claim while typing: Alt+Enter inserts a newline
// in the multi-line note box.
func TestBookmarkAltEnterIsLeftToTheTextBox(t *testing.T) {
	f := &fakeMPD{}
	a, _ := bmApp(t, f)
	a.bookmarkPicker.startAdd()

	altEnter := tcell.NewEventKey(tcell.KeyEnter, 0, tcell.ModAlt)
	if a.bookmarkPicker.handleKey(altEnter) {
		t.Error("Alt+Enter was claimed, want it left for the text box to insert a newline")
	}
	if a.bookmarkPicker.mode != bmModeAdd {
		t.Error("Alt+Enter left the typing sub-view")
	}
}

func TestBookmarkConfirmDeleteKeys(t *testing.T) {
	f := &fakeMPD{}
	a, song := bmApp(t, f)

	if _, err := a.metaDB.CreateBookmark(song.File, 10, "doomed"); err != nil {
		t.Fatalf("CreateBookmark: %v", err)
	}
	a.bookmarkPicker.render(song)
	a.bookmarkPicker.table.Select(0, 0)
	a.bookmarkPicker.startDelete()

	// An unrelated key is swallowed rather than falling through to the
	// hidden list behind the prompt.
	if !a.bookmarkPicker.handleKey(runeKey('x')) {
		t.Error("an unrelated key during a confirm prompt was not swallowed")
	}
	if a.bookmarkPicker.mode != bmModeConfirmDelete {
		t.Error("an unrelated key left the confirm prompt")
	}

	// 'n' backs out, leaving the bookmark alone.
	if !a.bookmarkPicker.handleKey(runeKey('n')) {
		t.Error("'n' was not consumed")
	}
	bms, err := a.metaDB.BookmarksForTrack(song.File)
	if err != nil {
		t.Fatalf("BookmarksForTrack: %v", err)
	}
	if len(bms) != 1 {
		t.Errorf("%d bookmarks after declining, want the one still there", len(bms))
	}

	// 'y' actually deletes it.
	a.bookmarkPicker.table.Select(0, 0)
	a.bookmarkPicker.startDelete()
	if !a.bookmarkPicker.handleKey(runeKey('y')) {
		t.Error("'y' was not consumed")
	}
	bms, err = a.metaDB.BookmarksForTrack(song.File)
	if err != nil {
		t.Fatalf("BookmarksForTrack: %v", err)
	}
	if len(bms) != 0 {
		t.Errorf("%d bookmarks after confirming, want none", len(bms))
	}
}

// TestBookmarkEscapeDuringConfirmCancels covers Esc as an alias for 'n'.
func TestBookmarkEscapeDuringConfirmCancels(t *testing.T) {
	f := &fakeMPD{}
	a, song := bmApp(t, f)
	if _, err := a.metaDB.CreateBookmark(song.File, 10, "keep"); err != nil {
		t.Fatalf("CreateBookmark: %v", err)
	}
	a.bookmarkPicker.render(song)
	a.bookmarkPicker.table.Select(0, 0)
	a.bookmarkPicker.startDelete()

	esc := tcell.NewEventKey(tcell.KeyEscape, 0, tcell.ModNone)
	if !a.bookmarkPicker.handleKey(esc) {
		t.Error("Escape during a confirm prompt was not consumed")
	}
	if a.bookmarkPicker.mode != bmModeList {
		t.Error("Escape did not return to the list")
	}
	bms, _ := a.metaDB.BookmarksForTrack(song.File)
	if len(bms) != 1 {
		t.Error("Escape during the confirm prompt deleted the bookmark")
	}
}

// TestOpenBookmarkManagerNeedsMetadata covers the gate: with the
// track-metadata feature off there is nowhere to store a bookmark, so
// the manager says so rather than opening empty.
func TestOpenBookmarkManagerNeedsMetadata(t *testing.T) {
	a := newFakeApp(t, &fakeMPD{}) // no metaDB

	a.openBookmarkManager(mpdclient.Song{File: "a.mp3"})

	if a.mode == modeOverlay {
		t.Error("the bookmark manager opened without a metadata database")
	}
	if got := a.hintBar.GetText(true); !strings.Contains(got, "track metadata") {
		t.Errorf("hint bar = %q, want it to explain the feature is off", got)
	}
}

func TestFormatBookmarkTime(t *testing.T) {
	cases := []struct {
		seconds float64
		want    string
	}{
		{0, "0:00"},
		{9, "0:09"},
		{95, "1:35"},
		{3599, "59:59"},
		// Minutes keep counting past an hour rather than rolling into
		// an hours field -- the same m:ss shape FormatDuration uses for
		// track times, so a bookmark and the track position it points
		// at read alike. Only noticeable on hour-plus tracks (a live
		// set, a mix).
		{3600, "60:00"},
		{3725, "62:05"},
	}
	for _, tc := range cases {
		if got := formatBookmarkTime(tc.seconds); got != tc.want {
			t.Errorf("formatBookmarkTime(%v) = %q, want %q", tc.seconds, got, tc.want)
		}
	}
}
