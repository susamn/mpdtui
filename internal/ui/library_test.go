package ui

import (
	"testing"

	"github.com/rivo/tview"

	"mpdtui/internal/mpdclient"
)

func testDirEntries() []mpdclient.DirEntry {
	return []mpdclient.DirEntry{
		{Type: mpdclient.EntryFile, Path: "zzz-track.mp3", Song: mpdclient.Song{Title: "Zzz Track"}},
		{Type: mpdclient.EntryDirectory, Path: "queen"},
		{Type: mpdclient.EntryPlaylist, Path: "Favorite Songs"},
		{Type: mpdclient.EntryDirectory, Path: "abba"},
	}
}

func TestBuildNodesSortsDirectoriesFirstThenAlphabetical(t *testing.T) {
	nodes := buildNodes(testDirEntries())
	if len(nodes) != 4 {
		t.Fatalf("got %d nodes, want 4", len(nodes))
	}

	wantOrder := []string{folderClosedIcon + " abba", folderClosedIcon + " queen", "Favorite Songs", "Zzz Track  [0:00]"}
	for i, want := range wantOrder {
		entry := nodes[i].GetReference().(mpdclient.DirEntry)
		if got := entryLabel(entry); got != want {
			t.Errorf("node %d = %q, want %q", i, got, want)
		}
	}
}

// TestBuildNodesSortIsCaseInsensitive covers a real-world case: this
// library has both "Alisha Chinoy" and "alisha-chinai" as separate
// top-level directories. A case-sensitive sort would cluster all
// capitalized names before all lowercase ones (ASCII 'A' < 'a'), instead
// of interleaving them the way a user browsing alphabetically expects.
func TestBuildNodesSortIsCaseInsensitive(t *testing.T) {
	nodes := buildNodes([]mpdclient.DirEntry{
		{Type: mpdclient.EntryDirectory, Path: "queen"},
		{Type: mpdclient.EntryDirectory, Path: "Alisha Chinoy"},
		{Type: mpdclient.EntryDirectory, Path: "alisha-chinai"},
		{Type: mpdclient.EntryDirectory, Path: "abba"},
	})

	got := make([]string, len(nodes))
	for i, n := range nodes {
		got[i] = entryLabel(n.GetReference().(mpdclient.DirEntry))
	}
	want := []string{
		folderClosedIcon + " abba",
		folderClosedIcon + " Alisha Chinoy",
		folderClosedIcon + " alisha-chinai",
		folderClosedIcon + " queen",
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("order = %v, want %v", got, want)
			break
		}
	}
}

func TestBuildNodesDirectoryStartsCollapsedWithUnloadedPlaceholder(t *testing.T) {
	nodes := buildNodes([]mpdclient.DirEntry{{Type: mpdclient.EntryDirectory, Path: "queen"}})
	dir := nodes[0]

	if dir.IsExpanded() {
		t.Error("a fresh directory node should start collapsed")
	}
	children := dir.GetChildren()
	if len(children) != 1 {
		t.Fatalf("got %d children, want 1 (the unloaded placeholder)", len(children))
	}
	kind, ok := children[0].GetReference().(placeholderKind)
	if !ok || kind != placeholderUnloaded {
		t.Errorf("placeholder reference = %v, want placeholderUnloaded", children[0].GetReference())
	}
}

func TestBuildNodesFileHasNoChildren(t *testing.T) {
	nodes := buildNodes([]mpdclient.DirEntry{{Type: mpdclient.EntryFile, Path: "track.mp3", Song: mpdclient.Song{Title: "Track"}}})
	if len(nodes[0].GetChildren()) != 0 {
		t.Error("a file node should have no children")
	}
}

// newBrowsingLibraryPanel builds a libraryPanel wired for pure tree
// navigation (back/toggle over already-built nodes), with no App/client --
// browse-mode navigation never touches either.
func newBrowsingLibraryPanel() *libraryPanel {
	root := tview.NewTreeNode("Library").SetSelectable(false)
	tree := tview.NewTreeView().SetRoot(root).SetTopLevel(1).SetCurrentNode(root)
	return &libraryPanel{tree: tree, root: root, mode: libBrowse}
}

func TestLibraryBackCollapsesExpandedDirectoryFirst(t *testing.T) {
	p := newBrowsingLibraryPanel()
	dirNodes := buildNodes([]mpdclient.DirEntry{{Type: mpdclient.EntryDirectory, Path: "queen"}})
	dir := dirNodes[0]
	setDirExpanded(dir, "queen", true) // simulate an already-expanded, already-loaded directory
	p.root.AddChild(dir)
	p.tree.SetCurrentNode(dir)

	p.back()

	if dir.IsExpanded() {
		t.Error("back() on an expanded directory should collapse it")
	}
	if p.tree.GetCurrentNode() != dir {
		t.Error("back() collapsing a directory should leave it selected, not jump to its parent")
	}
	if got, want := dir.GetText(), folderLabel("queen", false); got != want {
		t.Errorf("folder icon after collapsing = %q, want %q (closed)", got, want)
	}
}

func TestLibraryBackOnFileJumpsToAndCollapsesParent(t *testing.T) {
	p := newBrowsingLibraryPanel()
	dirNodes := buildNodes([]mpdclient.DirEntry{{Type: mpdclient.EntryDirectory, Path: "queen"}})
	dir := dirNodes[0]
	setDirExpanded(dir, "queen", true)
	dir.ClearChildren()
	fileNode := buildNodes([]mpdclient.DirEntry{{Type: mpdclient.EntryFile, Path: "queen/bohemian-rhapsody.mp3", Song: mpdclient.Song{Title: "Bohemian Rhapsody"}}})[0]
	dir.AddChild(fileNode)
	p.root.AddChild(dir)
	p.tree.SetCurrentNode(fileNode)

	p.back()

	if p.tree.GetCurrentNode() != dir {
		t.Errorf("back() on a file should select its parent directory")
	}
	if dir.IsExpanded() {
		t.Error("back() on a file should collapse its parent directory")
	}
	if got, want := dir.GetText(), folderLabel("queen", false); got != want {
		t.Errorf("folder icon after collapsing = %q, want %q (closed)", got, want)
	}
}

func TestLibraryBackAtRootIsNoop(t *testing.T) {
	p := newBrowsingLibraryPanel()
	p.tree.SetCurrentNode(p.root)

	p.back() // must not panic

	if p.tree.GetCurrentNode() != p.root {
		t.Error("back() at the root should leave the current node unchanged")
	}
}

// TestLibraryToggleDirectoryLazyLoads exercises the actual MPD fetch, since
// that's the one part of toggleDirectory that isn't pure. Read-only, so
// safe against a live server.
func TestLibraryToggleDirectoryLazyLoads(t *testing.T) {
	c := dialOrSkip(t)
	a := &App{tv: tview.NewApplication(), client: c}
	a.build()
	a.library.showRoot()

	var dirNode *tview.TreeNode
	var entry mpdclient.DirEntry
	for _, n := range a.library.root.GetChildren() {
		if e, ok := n.GetReference().(mpdclient.DirEntry); ok && e.Type == mpdclient.EntryDirectory {
			dirNode, entry = n, e
			break
		}
	}
	if dirNode == nil {
		t.Skip("library root has no subdirectories to expand")
	}

	a.library.toggleDirectory(dirNode, entry)
	if !dirNode.IsExpanded() {
		t.Fatal("toggleDirectory should expand a collapsed directory")
	}
	if got, want := dirNode.GetText(), folderLabel(entry.Path, true); got != want {
		t.Errorf("folder icon after expanding = %q, want %q (open)", got, want)
	}
	childrenAfterFirstExpand := len(dirNode.GetChildren())
	if childrenAfterFirstExpand == 0 {
		t.Fatal("expanding should have populated at least one child (or the empty-placeholder)")
	}

	a.library.toggleDirectory(dirNode, entry) // collapse
	if dirNode.IsExpanded() {
		t.Fatal("second toggle should collapse the directory")
	}
	if got, want := dirNode.GetText(), folderLabel(entry.Path, false); got != want {
		t.Errorf("folder icon after collapsing = %q, want %q (closed)", got, want)
	}
	if got := len(dirNode.GetChildren()); got != childrenAfterFirstExpand {
		t.Errorf("collapsing changed child count from %d to %d, want unchanged (children are kept, not discarded)", childrenAfterFirstExpand, got)
	}

	a.library.toggleDirectory(dirNode, entry) // re-expand
	if got := len(dirNode.GetChildren()); got != childrenAfterFirstExpand {
		t.Errorf("re-expanding refetched instead of reusing cached children: child count %d, want %d", got, childrenAfterFirstExpand)
	}
}
