package ui

import (
	"testing"

	"github.com/gdamore/tcell/v2"

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
