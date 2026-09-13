package ui

import (
	"strings"
	"testing"

	"github.com/gdamore/tcell/v2"

	"mpdtui/internal/metadata"
	"mpdtui/internal/mpdclient"
)

// TestLyricsViewerPositionsOverTheQueueColumns covers Draw's
// reposition-from-a-sibling pass: the viewer sits over the Queue's
// Year-through-Type band, recomputed every frame so it follows a
// resize.
func TestLyricsViewerPositionsOverTheQueueColumns(t *testing.T) {
	f := &fakeMPD{}
	a := newFakeAppWithMetaDB(t, f) // metadata on, so the wide layout has a Year column
	a.musicDir = t.TempDir()
	seedQueue(a, f, mpdclient.Song{ID: 1, Pos: 0, Title: "Track", File: "a/b.mp3", Date: "2006"})
	a.queue.table.SetRect(0, 0, 150, 40)
	a.queue.render(-1)

	a.openLyricsViewer()

	screen := tcell.NewSimulationScreen("UTF-8")
	if err := screen.Init(); err != nil {
		t.Fatalf("init simulation screen: %v", err)
	}
	defer screen.Fini()
	screen.SetSize(150, 40)

	a.lyricsViewer.Draw(screen)

	x, _, w, h := a.lyricsViewer.GetRect()
	qx, _, qw, _ := a.queue.table.GetRect()
	if w <= 0 || h <= 0 {
		t.Fatalf("viewer has no size after Draw: %dx%d", w, h)
	}
	if x < qx || x+w > qx+qw {
		t.Errorf("viewer spans %d..%d, want it inside the Queue's %d..%d", x, x+w, qx, qx+qw)
	}
}

// TestLyricsViewerPositionsInCompactLayout covers the other branch: with
// no Year column (a narrow terminal, or metadata off) the viewer takes
// the right half of the Queue instead.
func TestLyricsViewerPositionsInCompactLayout(t *testing.T) {
	f := &fakeMPD{}
	a := newFakeApp(t, f) // no metaDB, so no Year/Plays/Mark/Rating columns
	a.musicDir = t.TempDir()
	seedQueue(a, f, mpdclient.Song{ID: 1, Pos: 0, Title: "Track", File: "a/b.mp3"})
	a.queue.table.SetRect(0, 0, 60, 20)
	a.queue.render(-1)

	a.openLyricsViewer()

	screen := tcell.NewSimulationScreen("UTF-8")
	if err := screen.Init(); err != nil {
		t.Fatalf("init simulation screen: %v", err)
	}
	defer screen.Fini()
	screen.SetSize(60, 20)

	a.lyricsViewer.Draw(screen)

	x, _, w, _ := a.lyricsViewer.GetRect()
	qx, _, qw, _ := a.queue.table.GetRect()
	if w <= 0 {
		t.Fatal("viewer has no width in the compact layout")
	}
	if x < qx+qw/2-2 {
		t.Errorf("viewer starts at %d, want it over the right half of the Queue (from about %d)", x, qx+qw/2)
	}
}

// TestTrackInfoCardDrawRepositions covers the same pattern on the Track
// Info card, which reads the Queue's live rect on every frame too.
func TestTrackInfoCardDrawRepositions(t *testing.T) {
	f := &fakeMPD{}
	a := newFakeApp(t, f)
	seedQueue(a, f, mpdclient.Song{ID: 1, Pos: 0, Title: "Track", File: "a/b.mp3"})
	a.tv.SetFocus(a.queue.table)
	a.queue.table.Select(queueHeaderRows, 0)
	a.openTrackInfo()

	screen := tcell.NewSimulationScreen("UTF-8")
	if err := screen.Init(); err != nil {
		t.Fatalf("init simulation screen: %v", err)
	}
	defer screen.Fini()
	screen.SetSize(150, 40)

	a.trackInfo.Draw(screen)

	_, _, w, h := a.trackInfo.GetRect()
	if w <= 0 || h <= 0 {
		t.Errorf("the Track Info card has no size after Draw: %dx%d", w, h)
	}
}

// TestQueuePanelSearchFieldJumpsToAMatch covers the Queue's own pinned
// search field, which is wired in newQueuePanel rather than reached
// through an overlay.
func TestQueuePanelSearchFieldJumpsToAMatch(t *testing.T) {
	f := &fakeMPD{}
	a := newFakeApp(t, f)
	seedQueue(a, f,
		mpdclient.Song{ID: 1, Pos: 0, Title: "Ay Hairathe", Artist: "Hariharan", File: "a/1.mp3"},
		mpdclient.Song{ID: 2, Pos: 1, Title: "Tere Bina", Artist: "Chinmayi", File: "a/2.mp3"},
	)

	// '/' with the Queue focused focuses its own pinned field and sets
	// up the overlay bookkeeping the Enter handler relies on.
	a.tv.SetFocus(a.queue.table)
	a.openSearch()
	if a.tv.GetFocus() != a.queue.search {
		t.Fatalf("focus is %T, want the Queue's search field", a.tv.GetFocus())
	}

	a.queue.search.SetText("tere")
	sendKey(a.queue.search, tcell.NewEventKey(tcell.KeyEnter, 0, tcell.ModNone), a)

	row, _ := a.queue.table.GetSelection()
	if got := row - queueHeaderRows; got != 1 {
		t.Errorf("selected queue row %d, want the matching track at 1", got)
	}
}

// TestQueuePanelSearchWithNoMatchSaysSo covers the other half: a term
// that matches nothing leaves the selection alone and reports it.
func TestQueuePanelSearchWithNoMatchSaysSo(t *testing.T) {
	f := &fakeMPD{}
	a := newFakeApp(t, f)
	seedQueue(a, f, mpdclient.Song{ID: 1, Pos: 0, Title: "Only", File: "a/1.mp3"})
	a.tv.SetFocus(a.queue.table)
	a.openSearch()

	a.queue.search.SetText("nothing matches this")
	sendKey(a.queue.search, tcell.NewEventKey(tcell.KeyEnter, 0, tcell.ModNone), a)

	if got := a.hintBar.GetText(true); got == "" {
		t.Error("a queue search with no match reported nothing")
	}
}

// TestSameMarks covers the comparison the Now Playing line uses to
// decide whether the mark row actually changed. Both sides come from
// marksForTrack, which orders by catalog id, so order matters.
func TestSameMarks(t *testing.T) {
	a := []metadata.MarkReason{{ID: 1, Reason: "one"}, {ID: 2, Reason: "two"}}

	if !sameMarks(nil, nil) {
		t.Error("two empty mark sets compared unequal")
	}
	if !sameMarks(a, []metadata.MarkReason{{ID: 1, Reason: "one"}, {ID: 2, Reason: "two"}}) {
		t.Error("identical mark sets compared unequal")
	}
	if sameMarks(a, a[:1]) {
		t.Error("different-length mark sets compared equal")
	}
	if sameMarks(a, []metadata.MarkReason{{ID: 2, Reason: "two"}, {ID: 1, Reason: "one"}}) {
		t.Error("reordered marks compared equal -- they arrive ordered by id")
	}
	if sameMarks(a, []metadata.MarkReason{{ID: 1, Reason: "one"}, {ID: 3, Reason: "three"}}) {
		t.Error("different marks compared equal")
	}
}

// TestPlaylistsPanelEnterLoadsThePlaylist covers newPlaylistsPanel's own
// selected-row handler.
func TestPlaylistsPanelEnterLoadsThePlaylist(t *testing.T) {
	f := &fakeMPD{}
	a := newFakeApp(t, f)
	setPlaylistsForTest(a.playlists, []string{"Road Trip", "Chill"})
	a.tv.SetFocus(a.playlists.table)
	a.playlists.table.Select(1, 0)

	sendKey(a.playlists.table, tcell.NewEventKey(tcell.KeyEnter, 0, tcell.ModNone), a)

	if !f.did("plload:Road Trip") {
		t.Errorf("did %v, want the selected playlist loaded", f.commands())
	}
}

// TestBookmarkTableEnterJumps covers newBookmarkPicker's selected-row
// handler, the mouse/Enter route into jumpTo.
func TestBookmarkTableEnterJumps(t *testing.T) {
	f := &fakeMPD{}
	a := newFakeAppWithMetaDB(t, f)
	song := mpdclient.Song{ID: 5, Pos: 0, File: "a/b.mp3", Title: "Track"}
	seedQueue(a, f, song)
	a.openBookmarkManager(song)

	if _, err := a.metaDB.CreateBookmark(song.File, 42, "the drop"); err != nil {
		t.Fatalf("CreateBookmark: %v", err)
	}
	a.bookmarkPicker.render(song)
	a.bookmarkPicker.table.Select(0, 0)

	sendKey(a.bookmarkPicker.table, tcell.NewEventKey(tcell.KeyEnter, 0, tcell.ModNone), a)

	if !f.did("seekid:5:42s") {
		t.Errorf("did %v, want a seek to the bookmark", f.commands())
	}
}

// TestBookmarkNoteBoxKeys covers the multi-line note box's own capture:
// Escape backs out, plain Enter submits, and Alt+Enter is translated to
// a literal newline so a note can span lines.
func TestBookmarkNoteBoxKeys(t *testing.T) {
	f := &fakeMPD{}
	a := newFakeAppWithMetaDB(t, f)
	song := mpdclient.Song{ID: 5, Pos: 0, File: "a/b.mp3", Title: "Track"}
	seedQueue(a, f, song)
	a.openBookmarkManager(song)

	a.bookmarkPicker.startAdd()
	capture := a.bookmarkPicker.input.GetInputCapture()

	alt := capture(tcell.NewEventKey(tcell.KeyEnter, 0, tcell.ModAlt))
	if alt == nil {
		t.Fatal("Alt+Enter was swallowed, want it turned into a newline")
	}
	if alt.Rune() != '\n' {
		t.Errorf("Alt+Enter produced %q, want a newline", alt.Rune())
	}

	if got := capture(tcell.NewEventKey(tcell.KeyEscape, 0, tcell.ModNone)); got != nil {
		t.Error("Escape was not claimed by the note box")
	}
	if a.bookmarkPicker.mode != bmModeList {
		t.Error("Escape did not return to the bookmark list")
	}
}

// TestQueueColumnTruncation covers how the Queue divides the width it
// has between Title, Album and Artist as columns come and go.
func TestQueueColumnTruncation(t *testing.T) {
	// Everything on: the full fixed maxima, regardless of width.
	tl, al, arl := queueColumnTruncation(200, true, true, true, true, true, true)
	if tl != queueTitleMaxLen || al != queueAlbumMaxLen || arl != queueArtistMaxLen {
		t.Errorf("with every column shown = %d/%d/%d, want the fixed maxima %d/%d/%d",
			tl, al, arl, queueTitleMaxLen, queueAlbumMaxLen, queueArtistMaxLen)
	}

	// No width known yet (the panel has not been laid out): the compact
	// defaults, not zeroes, so the first paint is not blank.
	tl, al, arl = queueColumnTruncation(0, true, true, false, false, false, false)
	if tl != queueTitleCompactMaxLen || al != queueAlbumCompactMaxLen || arl != queueArtistCompactMaxLen {
		t.Errorf("with no width = %d/%d/%d, want the compact defaults", tl, al, arl)
	}

	// A terminal too narrow for even the fixed columns still leaves
	// something readable rather than negative widths.
	tl, al, arl = queueColumnTruncation(10, true, true, false, false, false, false)
	if tl < 1 || al < 1 || arl < 1 {
		t.Errorf("a very narrow terminal gave %d/%d/%d, want positive widths", tl, al, arl)
	}

	// Between those, more width never means less room.
	prevT := -1
	for _, width := range []int{60, 80, 100, 140, 200} {
		tl, al, arl = queueColumnTruncation(width, true, true, false, false, false, false)
		if tl < 1 || al < 1 || arl < 1 {
			t.Fatalf("width %d gave %d/%d/%d, want positive widths", width, tl, al, arl)
		}
		if tl < prevT {
			t.Errorf("width %d gave a Title budget of %d, less than the narrower terminal's %d",
				width, tl, prevT)
		}
		prevT = tl
	}
}

// TestLibraryExpandsADirectoryOnDemand covers the lazy load: a
// directory node starts with an "unloaded" placeholder and fetches its
// contents the first time it is opened, once.
func TestLibraryExpandsADirectoryOnDemand(t *testing.T) {
	f := &fakeMPD{dirs: map[string][]mpdclient.DirEntry{
		"": {{Type: mpdclient.EntryDirectory, Path: "artist"}},
		"artist": {
			{Type: mpdclient.EntryFile, Path: "artist/1.mp3", Song: mpdclient.Song{File: "artist/1.mp3", Title: "One"}},
			{Type: mpdclient.EntryFile, Path: "artist/2.mp3", Song: mpdclient.Song{File: "artist/2.mp3", Title: "Two"}},
		},
	}}
	a := newFakeApp(t, f)
	a.library.showRoot()

	children := a.library.tree.GetRoot().GetChildren()
	if len(children) != 1 {
		t.Fatalf("root has %d children, want the one directory", len(children))
	}
	dir := children[0]

	a.library.onSelect(dir) // expands, fetching its contents

	if got := len(dir.GetChildren()); got != 2 {
		t.Errorf("the directory has %d children after expanding, want 2", got)
	}

	// Collapsing and reopening must not refetch.
	a.library.onSelect(dir)
	a.library.onSelect(dir)
	if got := len(dir.GetChildren()); got != 2 {
		t.Errorf("the directory has %d children after reopening, want 2", got)
	}
}

// TestLibraryShowsAnEmptyDirectory covers the placeholder for a
// directory with nothing in it, which would otherwise expand to nothing
// and look broken.
func TestLibraryShowsAnEmptyDirectory(t *testing.T) {
	f := &fakeMPD{dirs: map[string][]mpdclient.DirEntry{
		"":      {{Type: mpdclient.EntryDirectory, Path: "empty"}},
		"empty": {},
	}}
	a := newFakeApp(t, f)
	a.library.showRoot()

	dir := a.library.tree.GetRoot().GetChildren()[0]
	a.library.onSelect(dir)

	kids := dir.GetChildren()
	if len(kids) != 1 {
		t.Fatalf("an empty directory expanded to %d nodes, want one placeholder", len(kids))
	}
	if !strings.Contains(kids[0].GetText(), "empty") {
		t.Errorf("placeholder = %q, want it to say the directory is empty", kids[0].GetText())
	}
}

func TestLibraryReportsAFailedDirectoryFetch(t *testing.T) {
	f := &fakeMPD{dirs: map[string][]mpdclient.DirEntry{
		"": {{Type: mpdclient.EntryDirectory, Path: "artist"}},
	}}
	a := newFakeApp(t, f)
	a.library.showRoot()
	dir := a.library.tree.GetRoot().GetChildren()[0]

	f.err = errTest
	a.library.onSelect(dir)

	if got := a.hintBar.GetText(true); !strings.Contains(got, errTest.Error()) {
		t.Errorf("hint bar = %q, want the fetch failure reported", got)
	}
}

// TestLibraryArtistSearchGroupsByArtist covers 'f”s artist result
// landing in the Library: matching tracks are grouped under their
// artist rather than listed flat, and the count reported is the number
// of artists, not tracks.
func TestLibraryArtistSearchGroupsByArtist(t *testing.T) {
	f := &fakeMPD{songs: []mpdclient.Song{
		{Artist: "A. R. Rahman", Album: "Guru", Title: "One", File: "a/1.mp3"},
		{Artist: "A. R. Rahman", Album: "Roja", Title: "Two", File: "a/2.mp3"},
		{Artist: "Rahman Ali", Album: "Other", Title: "Three", File: "b/1.mp3"},
		{Artist: "Someone Else", Album: "Other", Title: "Four", File: "c/1.mp3"},
	}}
	a := newFakeApp(t, f)

	if got := a.library.showArtistSearch("rahman"); got != 2 {
		t.Errorf("artist search matched %d artists, want 2", got)
	}
	if got := len(a.library.tree.GetRoot().GetChildren()); got != 2 {
		t.Errorf("results grouped into %d nodes, want one per matching artist", got)
	}
	if a.library.mode != libSearch {
		t.Error("the Library did not switch to search mode")
	}

	// A query matching no artist says so rather than showing an empty tree.
	if got := a.library.showArtistSearch("nobody at all"); got != 0 {
		t.Errorf("a non-matching artist search returned %d, want 0", got)
	}
	if got := a.hintBar.GetText(true); !strings.Contains(got, "no artists found") {
		t.Errorf("hint bar = %q, want it to report no artists", got)
	}
}

// TestLocateRevealsATrackInTheTree covers 'L': the Library is put back
// into browse mode and walked down to the playing track.
func TestLocateRevealsATrackInTheTree(t *testing.T) {
	f := &fakeMPD{dirs: map[string][]mpdclient.DirEntry{
		"":       {{Type: mpdclient.EntryDirectory, Path: "artist"}},
		"artist": {{Type: mpdclient.EntryFile, Path: "artist/1.mp3", Song: mpdclient.Song{File: "artist/1.mp3", Title: "One"}}},
	}}
	a := newFakeApp(t, f)
	a.library.showRoot()

	if !a.library.revealInLibrary("artist/1.mp3") {
		t.Error("revealInLibrary did not find a track that is in the tree")
	}
	if a.library.revealInLibrary("nowhere/else.mp3") {
		t.Error("revealInLibrary claimed to find a track that is not in the tree")
	}
}
