package ui

import (
	"strings"
	"testing"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"mpdtui/internal/mpdclient"
)

// seedSearchResults puts the Library panel into search mode with the
// given groups, mirroring what showAlbumSearch/showArtistSearch do after
// their MPD call -- so selection and add-all behaviour can be tested
// without a live server.
func seedSearchResults(p *libraryPanel, query string, groups []*albumGroup) {
	p.mode = libSearch
	p.query = query
	p.root.ClearChildren()
	p.addGroupNodes(groups)
	p.selectFirstNode()
}

// seedFlatResults does the same for showSearch's flat per-track results.
func seedFlatResults(p *libraryPanel, query string, songs []mpdclient.Song) {
	p.mode = libSearch
	p.query = query
	p.root.ClearChildren()
	for _, s := range songs {
		entry := mpdclient.DirEntry{Type: mpdclient.EntryFile, Path: s.File, Song: s}
		p.root.AddChild(tview.NewTreeNode(trackLabel(s)).SetReference(entry).SetSelectedTextStyle(treeSelectedStyle))
	}
	p.selectFirstNode()
}

func testGroups() []*albumGroup {
	return []*albumGroup{
		{label: "Queen - A Night at the Opera", songs: []mpdclient.Song{
			{Title: "Death on Two Legs", File: "queen/opera/01.mp3"},
			{Title: "Bohemian Rhapsody", File: "queen/opera/11.mp3"},
		}},
		{label: "Queen - News of the World", songs: []mpdclient.Song{
			{Title: "We Will Rock You", File: "queen/news/01.mp3"},
		}},
	}
}

// TestTreeSelectedStyleIsVisible is a regression test for a cursor that
// was invisible in the Library panel. treeSelectedStyle used to be built
// in its own package-level declaration, from colorSelectedFg/
// colorSelectedBg -- but variable initializers all run before any init
// function, so it captured those colors while they were still the zero
// Color and every node ended up styled default-on-default.
func TestTreeSelectedStyleIsVisible(t *testing.T) {
	fg, bg, _ := treeSelectedStyle.Decompose()
	if fg == tcell.ColorDefault && bg == tcell.ColorDefault {
		t.Fatal("treeSelectedStyle is default-on-default: the Library cursor would be invisible")
	}
	if fg == bg {
		t.Errorf("treeSelectedStyle foreground and background are both %v: the selected row would be unreadable", fg)
	}
	if got := colorSelectedBg; bg != got {
		t.Errorf("treeSelectedStyle background = %v, want the theme's selection color %v", bg, got)
	}
}

func TestLibraryRestyleNodesPicksUpThemeChanges(t *testing.T) {
	// TreeNode bakes the style in at construction, so a theme reload has
	// to walk the tree. Without this, nodes built before the change keep
	// the old theme's selection color for the rest of the session.
	a := newTestApp()
	seedSearchResults(a.library, "queen", testGroups())

	original := treeSelectedStyle
	t.Cleanup(func() { treeSelectedStyle = original })

	treeSelectedStyle = tcell.StyleDefault.Foreground(tcell.ColorRed).Background(tcell.ColorBlue)
	a.library.restyleNodes()

	a.library.root.Walk(func(node, parent *tview.TreeNode) bool {
		fg, bg, _ := node.GetSelectedTextStyle().Decompose()
		if fg != tcell.ColorRed || bg != tcell.ColorBlue {
			t.Errorf("node %q kept style fg=%v bg=%v after a theme change", node.GetText(), fg, bg)
		}
		return true
	})
}

// TestSearchResultsSelectFirstRow covers the reported bug directly: with
// the cursor left on the hidden root, tview relocates it to the first
// selectable node while drawing, so the panel shows no cursor and 'a'
// acts on a row the user never chose.
func TestSearchResultsSelectFirstRow(t *testing.T) {
	a := newTestApp()
	groups := testGroups()
	seedSearchResults(a.library, "queen", groups)

	current := a.library.tree.GetCurrentNode()
	if current == nil {
		t.Fatal("no current node after a search")
	}
	if current == a.library.root {
		t.Fatal("cursor left on the hidden root after a search -- the panel would show no selection")
	}
	got, ok := current.GetReference().(*albumGroup)
	if !ok {
		t.Fatalf("current node reference = %T, want the first result group", current.GetReference())
	}
	if got != groups[0] {
		t.Errorf("current node = %q, want the first result %q", got.label, groups[0].label)
	}
}

func TestFlatSearchResultsSelectFirstRow(t *testing.T) {
	a := newTestApp()
	songs := []mpdclient.Song{
		{Title: "One", File: "a/1.mp3"},
		{Title: "Two", File: "a/2.mp3"},
	}
	seedFlatResults(a.library, "one", songs)

	current := a.library.tree.GetCurrentNode()
	if current == a.library.root || current == nil {
		t.Fatal("cursor not moved to the first result after a flat search")
	}
	entry, ok := current.GetReference().(mpdclient.DirEntry)
	if !ok {
		t.Fatalf("current node reference = %T, want a DirEntry", current.GetReference())
	}
	if entry.Path != songs[0].File {
		t.Errorf("current node = %q, want the first result %q", entry.Path, songs[0].File)
	}
}

func TestSelectFirstNodeWithNoResults(t *testing.T) {
	// An empty result set has nothing to select; falling back to the
	// root must not panic or leave a stale cursor pointing into the
	// previous search's nodes.
	a := newTestApp()
	seedSearchResults(a.library, "queen", testGroups())
	seedSearchResults(a.library, "nothingmatchesthis", nil)

	if got := a.library.tree.GetCurrentNode(); got != a.library.root {
		t.Errorf("current node with no results = %v, want the root", got)
	}
}

// TestSelectionSurvivesADraw guards the actual mechanism behind the bug:
// tview silently relocates an unselectable current node during Draw. The
// cursor set here must be the one still selected afterwards.
func TestSelectionSurvivesADraw(t *testing.T) {
	a := newTestApp()
	groups := testGroups()
	seedSearchResults(a.library, "queen", groups)
	a.library.tree.SetRect(0, 0, 60, 20)

	// Move to the second result, the way a user pressing 'j' would.
	second := a.library.root.GetChildren()[1]
	a.library.tree.SetCurrentNode(second)

	screen := tcell.NewSimulationScreen("UTF-8")
	if err := screen.Init(); err != nil {
		t.Fatalf("init simulation screen: %v", err)
	}
	defer screen.Fini()
	a.library.tree.Draw(screen)

	if got := a.library.tree.GetCurrentNode(); got != second {
		t.Errorf("cursor moved during Draw: %q, want %q", got.GetText(), second.GetText())
	}
}

func TestResultTracksCollectsEveryTrackInOrder(t *testing.T) {
	a := newTestApp()
	seedSearchResults(a.library, "queen", testGroups())

	want := []string{
		"queen/opera/01.mp3",
		"queen/opera/11.mp3",
		"queen/news/01.mp3",
	}
	got := a.library.resultTracks()
	if len(got) != len(want) {
		t.Fatalf("resultTracks() returned %d tracks (%v), want %d", len(got), got, len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("track %d = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestResultTracksCollectsFlatResults(t *testing.T) {
	a := newTestApp()
	seedFlatResults(a.library, "one", []mpdclient.Song{
		{Title: "One", File: "a/1.mp3"},
		{Title: "Two", File: "a/2.mp3"},
	})

	got := a.library.resultTracks()
	if len(got) != 2 || got[0] != "a/1.mp3" || got[1] != "a/2.mp3" {
		t.Errorf("resultTracks() = %v, want both flat results in order", got)
	}
}

func TestResultTracksIgnoresUnexpandedGroupChildren(t *testing.T) {
	// A group's tracks come from its albumGroup reference, not from
	// walking its child nodes -- so collapsed and expanded groups must
	// contribute exactly the same tracks.
	a := newTestApp()
	seedSearchResults(a.library, "queen", testGroups())
	collapsed := a.library.resultTracks()

	for _, node := range a.library.root.GetChildren() {
		node.SetExpanded(true)
	}
	expanded := a.library.resultTracks()

	if strings.Join(collapsed, ",") != strings.Join(expanded, ",") {
		t.Errorf("expanding groups changed the tracks: %v vs %v", collapsed, expanded)
	}
}

func TestAddAllRefusesOutsideSearchMode(t *testing.T) {
	// 'A' in browse mode would mean "queue the entire library", which is
	// not something to do on a keypress next to the one that adds a
	// single row. It has to say so rather than silently doing nothing --
	// and must not reach the (nil) MPD client.
	a := newTestApp()
	a.library.mode = libBrowse
	a.tv.SetFocus(a.library.tree)

	a.handleAddAll()

	if got := a.hintBar.GetText(true); !strings.Contains(got, "search first") {
		t.Errorf("hint bar after 'A' in browse mode = %q, want it to explain that a search is needed", got)
	}
}

func TestAddAllOnAnotherPanelReportsInvalidKey(t *testing.T) {
	a := newTestApp()
	a.tv.SetFocus(a.queue.table)

	a.handleAddAll()

	if got := a.hintBar.GetText(true); !strings.Contains(got, "'A' has no action here") {
		t.Errorf("hint bar after 'A' on the Queue = %q, want the invalid-key feedback", got)
	}
}

func TestAddAllHintShownOnlyForSearchResults(t *testing.T) {
	a := newTestApp()
	a.tv.SetFocus(a.library.tree)

	a.library.mode = libBrowse
	a.updateHintBar()
	if got := a.hintBar.GetText(true); strings.Contains(got, "add all") {
		t.Errorf("browse-mode hints = %q, want no 'add all' hint", got)
	}

	a.library.mode = libSearch
	a.updateHintBar()
	if got := a.hintBar.GetText(true); !strings.Contains(got, "add all") {
		t.Errorf("search-mode hints = %q, want an 'add all' hint", got)
	}
}

func TestAddAllKeyIsRouted(t *testing.T) {
	// 'A' used to fall through to the unbound-key default; this checks
	// it now reaches handleAddAll rather than reporting itself invalid.
	a := newTestApp()
	a.tv.SetFocus(a.library.tree)
	a.library.mode = libBrowse

	if result := a.globalInputCapture(tcell.NewEventKey(tcell.KeyRune, 'A', tcell.ModNone)); result != nil {
		t.Errorf("'A' should be consumed by the add-all handler, got %v", result)
	}
	if got := a.hintBar.GetText(true); !strings.Contains(got, "search first") {
		t.Errorf("hint bar after 'A' = %q, want the add-all handler's message", got)
	}
}
