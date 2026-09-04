package ui

import (
	"fmt"
	"testing"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"mpdtui/internal/mpdclient"
)

func TestCenterRowOffset(t *testing.T) {
	cases := []struct {
		name   string
		row    int
		height int
		want   int
	}{
		{"row below the first half-screen centers", 61, 38, 42},
		{"row in the first half-screen clamps to the top", 5, 38, 0},
		{"exactly half a screen down is already the top", 19, 38, 0},
		{"a one-line viewport can't center anything", 7, 1, 7},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := centerRowOffset(tc.row, tc.height); got != tc.want {
				t.Errorf("centerRowOffset(%d, %d) = %d, want %d", tc.row, tc.height, got, tc.want)
			}
		})
	}
}

// queueWithSongs fills the Queue with n placeholder tracks and gives the
// table a real rect, so scrolling maths has a viewport to work in.
func queueWithSongs(a *App, n int) {
	songs := make([]mpdclient.Song, n)
	for i := range songs {
		songs[i] = mpdclient.Song{ID: i + 1, Title: fmt.Sprintf("Track %d", i+1), File: fmt.Sprintf("artist/track%d.mp3", i+1)}
	}
	a.queue.songs = songs
	a.queue.table.SetRect(0, 0, 150, 40) // inner height 38 (border)
	a.queue.render(-1)
}

func TestCenterSelectionScrollsSelectedRowToTheMiddle(t *testing.T) {
	a := newTestApp()
	queueWithSongs(a, 200)
	row := 60 + queueHeaderRows
	a.queue.table.Select(row, 0)

	a.queue.centerSelection()

	off, _ := a.queue.table.GetOffset()
	// A row is drawn at line (row - offset) of the table's inner area.
	if line := row - off; line != 19 {
		t.Errorf("selected row drawn at line %d of 38 (offset %d), want the middle line 19", line, off)
	}
}

func TestCenterSelectionLeavesSelectionAlone(t *testing.T) {
	a := newTestApp()
	queueWithSongs(a, 200)
	a.queue.table.Select(60+queueHeaderRows, 0)

	a.queue.centerSelection()

	if row, _ := a.queue.table.GetSelection(); row != 60+queueHeaderRows {
		t.Errorf("selection = row %d, want it untouched at %d", row, 60+queueHeaderRows)
	}
}

// TestCenterSelectionNoopWithoutAViewport: centering runs from a key
// handler, but nothing stops it being reached before the first draw --
// with no height there is no middle, and it must not scroll to a
// nonsense offset.
func TestCenterSelectionNoopWithoutAViewport(t *testing.T) {
	a := newTestApp()
	a.queue.songs = []mpdclient.Song{{ID: 1, File: "a.mp3"}}
	a.queue.render(-1)
	a.queue.table.SetRect(0, 0, 0, 0)
	a.queue.table.Select(queueHeaderRows, 0)

	a.queue.centerSelection()

	if off, _ := a.queue.table.GetOffset(); off != 0 {
		t.Errorf("offset = %d, want 0 (unscrolled)", off)
	}
}

// --- Library tree ---

// libraryTreeWithNodes replaces the Library tree with n flat nodes and a
// real rect, so TreeView.process has a viewport to scroll within.
func libraryTreeWithNodes(a *App, n int) []*tview.TreeNode {
	root := tview.NewTreeNode("root")
	nodes := make([]*tview.TreeNode, n)
	for i := range nodes {
		nodes[i] = tview.NewTreeNode(fmt.Sprintf("node %d", i))
		root.AddChild(nodes[i])
	}
	a.library.root = root
	a.library.tree.SetRoot(root)
	a.library.tree.SetRect(0, 0, 40, 22) // inner height 20 (border)
	return nodes
}

// TestCenterCurrentNodeCentersFromEveryDirection is the whole reason
// centerCurrentNode does its three-Move dance: tview scrolls a node just
// far enough to be visible, so where it lands depends entirely on where
// the view was before. All three starting points must end centered.
func TestCenterCurrentNodeCentersFromEveryDirection(t *testing.T) {
	const target = 60
	cases := []struct {
		name  string
		start int // node scrolled to first, to seed a starting offset
	}{
		{"coming from far above", 0},
		{"coming from far below", 140},
		{"target already on screen", 55},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			a := newTestApp()
			nodes := libraryTreeWithNodes(a, 150)
			a.library.tree.SetCurrentNode(nodes[tc.start])
			a.library.tree.Move(1) // force tview to settle an offset
			a.library.tree.Move(-1)

			a.library.tree.SetCurrentNode(nodes[target])
			a.library.centerCurrentNode()

			// The node list tview walks may or may not include the
			// (hidden, SetTopLevel(1)) root, so allow a row of slack --
			// far tighter than the alternatives this rules out: 60ish
			// (pinned to the top) or 41ish (pinned to the bottom).
			off := a.library.tree.GetScrollOffset()
			if want := target - 10; off < want-1 || off > want+2 {
				t.Errorf("scroll offset = %d, want ~%d (target centered in 20 lines)", off, want)
			}
		})
	}
}

func TestCenterCurrentNodeKeepsTheSelection(t *testing.T) {
	a := newTestApp()
	nodes := libraryTreeWithNodes(a, 150)
	a.library.tree.SetCurrentNode(nodes[60])

	a.library.centerCurrentNode()

	if got := a.library.tree.GetCurrentNode(); got != nodes[60] {
		t.Errorf("current node = %v, want the node centering was asked for", got.GetText())
	}
}

// TestCenterCurrentNodeNearTheEndsPinsInstead: the first and last
// half-screens can't be centered, and must land at the top/bottom rather
// than at a negative or past-the-end offset.
func TestCenterCurrentNodeNearTheEndsPinsInstead(t *testing.T) {
	a := newTestApp()
	nodes := libraryTreeWithNodes(a, 30)

	a.library.tree.SetCurrentNode(nodes[1])
	a.library.centerCurrentNode()
	if off := a.library.tree.GetScrollOffset(); off != 0 {
		t.Errorf("near the top: scroll offset = %d, want 0", off)
	}

	a.library.tree.SetCurrentNode(nodes[29])
	a.library.centerCurrentNode()
	if off := a.library.tree.GetScrollOffset(); off < 9 || off > 11 {
		t.Errorf("near the bottom: scroll offset = %d, want ~10 (last screenful of 30 nodes in 20 lines)", off)
	}
}

func TestCenterCurrentNodeNoopWithoutASelection(t *testing.T) {
	a := newTestApp()
	libraryTreeWithNodes(a, 50)
	a.library.tree.SetCurrentNode(nil)

	a.library.centerCurrentNode() // must not panic or select something

	if got := a.library.tree.GetCurrentNode(); got != nil {
		t.Errorf("current node = %v, want nil (nothing was selected)", got.GetText())
	}
}

// --- The flash ---

// selectedRowBackground draws the Queue table to a simulation screen and
// reports the background color actually painted on the selected row --
// tview has no getter for a table's selected style, so this reads back
// what a user would really see.
func selectedRowBackground(t *testing.T, a *App) tcell.Color {
	t.Helper()
	screen := tcell.NewSimulationScreen("")
	if err := screen.Init(); err != nil {
		t.Fatalf("screen.Init: %v", err)
	}
	defer screen.Fini()
	screen.SetSize(150, 40)
	a.queue.table.Draw(screen)

	row, _ := a.queue.table.GetSelection()
	off, _ := a.queue.table.GetOffset()
	y := 1 + (row - off) // 1 for the table's own top border
	_, _, style, _ := screen.GetContent(2, y)
	_, bg, _ := style.Decompose()
	return bg
}

func TestFlashLocatedRowLightsUpThenRestoresTheSelection(t *testing.T) {
	// Without this the rest of the test would pass vacuously: a palette
	// whose accent equalled its selection color would make the flash
	// invisible while every assertion below still held.
	if locateFlashBg == colorSelectedBg {
		t.Fatalf("flash background %v equals the normal selection background -- the flash would be invisible", locateFlashBg)
	}
	a := newTestApp()
	queueWithSongs(a, 5)
	a.queue.table.Select(2, 0)
	a.tv.SetFocus(a.queue.table)

	a.flashLocatedRow()
	if got := selectedRowBackground(t, a); got != locateFlashBg {
		t.Errorf("selected row background during the flash = %v, want the flash color %v", got, locateFlashBg)
	}

	// Run the sequence out, as the scheduled phases would.
	a.runLocateFlashPhase(a.locateFlashSeq, len(locateFlashPhases))
	if got := selectedRowBackground(t, a); got != colorSelectedBg {
		t.Errorf("selected row background after the flash = %v, want the normal selection %v", got, colorSelectedBg)
	}
}

// TestFlashLocatedRowSupersedesAnEarlierFlash: pressing L again mid-flash
// must not leave the older sequence repainting over the new one.
func TestFlashLocatedRowSupersedesAnEarlierFlash(t *testing.T) {
	a := newTestApp()
	queueWithSongs(a, 5)
	a.queue.table.Select(2, 0)
	a.tv.SetFocus(a.queue.table)

	a.flashLocatedRow()
	stale := a.locateFlashSeq
	a.flashLocatedRow()
	if a.locateFlashSeq == stale {
		t.Fatal("a second flash should start a new sequence")
	}

	// The stale sequence's final phase would restore the normal
	// selection style mid-flash; it must be ignored instead.
	a.runLocateFlashPhase(stale, len(locateFlashPhases))
	if got := selectedRowBackground(t, a); got != locateFlashBg {
		t.Errorf("selected row background = %v, want the newer flash still showing (%v)", got, locateFlashBg)
	}
}

// TestLocateFlashEndsOnTheNormalSelection guards the phase table itself:
// however many blinks it lists, the row must not be left lit.
func TestLocateFlashEndsOnTheNormalSelection(t *testing.T) {
	if n := len(locateFlashPhases); n == 0 || !locateFlashPhases[0].on {
		t.Fatalf("locateFlashPhases = %v, want it to start lit", locateFlashPhases)
	}
	a := newTestApp()
	queueWithSongs(a, 5)
	a.queue.table.Select(2, 0)
	a.tv.SetFocus(a.queue.table)

	a.flashLocatedRow()
	for i := 1; i <= len(locateFlashPhases); i++ {
		a.runLocateFlashPhase(a.locateFlashSeq, i)
	}

	if got := selectedRowBackground(t, a); got != colorSelectedBg {
		t.Errorf("selected row background after every phase = %v, want the normal selection %v", got, colorSelectedBg)
	}
}
