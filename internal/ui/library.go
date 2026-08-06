package ui

import (
	"fmt"
	"sort"
	"strings"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"mpdtui/internal/mpdclient"
)

// treeSelectedStyle matches the blue-bg/yellow-fg selection convention
// List/Table use elsewhere in this app. TreeNode's zero-value default
// style resolves to invisible (default-on-default) once applyTheme
// flattens PrimitiveBackgroundColor/PrimaryTextColor to the terminal's own
// colors, so every selectable node needs this set explicitly.
var treeSelectedStyle = tcell.StyleDefault.Foreground(colorSelectedFg).Background(colorSelectedBg)

type libraryMode int

const (
	libBrowse libraryMode = iota
	libSearch
)

// placeholderKind marks a TreeNode as synthetic bookkeeping rather than a
// real DirEntry: either "this directory's children haven't been fetched
// yet" (shown as an expand target) or "fetched and confirmed empty" (so
// re-expanding doesn't refetch). Distinguishing the two by reference type
// means toggleDirectory doesn't need separate state on libraryPanel itself.
type placeholderKind int

const (
	placeholderUnloaded placeholderKind = iota
	placeholderEmpty
)

// libraryPanel browses MPD's actual directory structure (lsinfo) as an
// expandable, lazily-loaded tree, or shows free-text search results as a
// flat list of leaf nodes in place of it.
type libraryPanel struct {
	app  *App
	tree *tview.TreeView
	root *tview.TreeNode

	mode  libraryMode
	query string
}

func newLibraryPanel(app *App) *libraryPanel {
	root := tview.NewTreeNode("Library").SetSelectable(false).SetSelectedTextStyle(treeSelectedStyle)
	tree := tview.NewTreeView().SetRoot(root).SetTopLevel(1).SetCurrentNode(root)
	tree.SetBorder(true).SetTitle(" Library ")

	p := &libraryPanel{app: app, tree: tree, root: root}
	tree.SetSelectedFunc(p.onSelect)
	return p
}

// showRoot (re)loads the library root, replacing whatever's currently
// shown -- browsing at any depth, or search results.
func (p *libraryPanel) showRoot() {
	entries, err := p.app.client.ListDirectory("")
	if err != nil {
		p.app.showError(err)
		return
	}

	p.mode = libBrowse
	p.root.ClearChildren()
	for _, n := range buildNodes(entries) {
		p.root.AddChild(n)
	}
	p.tree.SetTitle(" Library ")
	p.tree.SetCurrentNode(p.root)
}

// showSearch full-text searches the library by tag (independent of the
// directory tree) and shows the matches as a flat list of file nodes.
func (p *libraryPanel) showSearch(query string) {
	songs, err := p.app.client.Search(query)
	if err != nil {
		p.app.showError(err)
		return
	}

	p.mode = libSearch
	p.query = query
	p.root.ClearChildren()
	for _, s := range songs {
		s := s
		entry := mpdclient.DirEntry{Type: mpdclient.EntryFile, Path: s.File, Song: s}
		p.root.AddChild(tview.NewTreeNode(trackLabel(s)).SetReference(entry).SetSelectedTextStyle(treeSelectedStyle))
	}
	p.tree.SetTitle(fmt.Sprintf(" Library: search %q (%d) ", query, len(songs)))
	p.tree.SetCurrentNode(p.root)
	if len(songs) == 0 {
		p.app.showMessage("no results for " + query)
	}
}

// back handles Backspace: from search results, returns to the directory
// root (same target Esc uses). While browsing, it collapses the current
// node if it's an expanded directory, or otherwise moves the selection up
// to (and collapses) its parent -- standard file-explorer "go back".
func (p *libraryPanel) back() {
	if p.mode == libSearch {
		p.showRoot()
		return
	}

	current := p.tree.GetCurrentNode()
	if current == nil || current == p.root {
		return
	}
	if entry, ok := current.GetReference().(mpdclient.DirEntry); ok && entry.Type == mpdclient.EntryDirectory && current.IsExpanded() {
		current.SetExpanded(false)
		return
	}

	path := p.tree.GetPath(current)
	if len(path) < 2 {
		return
	}
	parent := path[len(path)-2]
	parent.SetExpanded(false)
	p.tree.SetCurrentNode(parent)
}

// onSelect is the TreeView-wide handler for Enter/Space: expand/collapse a
// directory, add+play a track, or append a stored playlist encountered in
// the tree. Nodes with no DirEntry reference (the root, or an unloaded/
// empty placeholder) have nothing to do.
func (p *libraryPanel) onSelect(node *tview.TreeNode) {
	entry, ok := node.GetReference().(mpdclient.DirEntry)
	if !ok {
		return
	}
	switch entry.Type {
	case mpdclient.EntryDirectory:
		p.toggleDirectory(node, entry)
	case mpdclient.EntryFile:
		p.app.addAndPlay(entry.Song)
	case mpdclient.EntryPlaylist:
		p.app.appendPlaylist(entry.Path)
	}
}

// toggleDirectory expands or collapses node, fetching its children from
// MPD the first time it's expanded (detected via the unloaded placeholder
// child every directory node starts with -- see buildNodes).
func (p *libraryPanel) toggleDirectory(node *tview.TreeNode, entry mpdclient.DirEntry) {
	if node.IsExpanded() {
		node.SetExpanded(false)
		return
	}

	children := node.GetChildren()
	if len(children) == 1 {
		if kind, ok := children[0].GetReference().(placeholderKind); ok && kind == placeholderUnloaded {
			fetched, err := p.app.client.ListDirectory(entry.Path)
			if err != nil {
				p.app.showError(err)
				return
			}
			node.ClearChildren()
			if len(fetched) == 0 {
				node.AddChild(tview.NewTreeNode("[::d](empty)[-:-:-]").SetSelectable(false).SetReference(placeholderEmpty))
			} else {
				for _, n := range buildNodes(fetched) {
					node.AddChild(n)
				}
			}
		}
	}
	node.SetExpanded(true)
}

// addSelected implements 'a' (add to queue, no play): the whole subtree
// for a directory (MPD's own "add" command recurses server-side -- no need
// to fetch and iterate children here), a single track for a file, or an
// append for a playlist entry (there's no meaningful "add without loading"
// distinction for a stored playlist, so it behaves like Enter).
func (p *libraryPanel) addSelected() {
	node := p.tree.GetCurrentNode()
	if node == nil {
		return
	}
	entry, ok := node.GetReference().(mpdclient.DirEntry)
	if !ok {
		return
	}
	switch entry.Type {
	case mpdclient.EntryDirectory, mpdclient.EntryFile:
		p.app.queueAddPath(entry.Path)
	case mpdclient.EntryPlaylist:
		p.app.appendPlaylist(entry.Path)
	}
}

// buildNodes turns entries into tree nodes, directories first then
// everything else alphabetically, matching common file-explorer ordering.
// Each directory node starts collapsed with a single unloaded placeholder
// child, populated lazily by toggleDirectory.
func buildNodes(entries []mpdclient.DirEntry) []*tview.TreeNode {
	sorted := make([]mpdclient.DirEntry, len(entries))
	copy(sorted, entries)
	sort.SliceStable(sorted, func(i, j int) bool {
		di := sorted[i].Type == mpdclient.EntryDirectory
		dj := sorted[j].Type == mpdclient.EntryDirectory
		if di != dj {
			return di
		}
		// Case-insensitive: this library mixes folder-naming conventions
		// (e.g. both "Alisha Chinoy" and "alisha-chinai" exist as separate
		// top-level directories), and a case-sensitive sort would scatter
		// them apart into a Digits/Uppercase/lowercase clustering instead
		// of the alphabetical order a user actually expects.
		return strings.ToLower(entryLabel(sorted[i])) < strings.ToLower(entryLabel(sorted[j]))
	})

	nodes := make([]*tview.TreeNode, len(sorted))
	for i, e := range sorted {
		node := tview.NewTreeNode(entryLabel(e)).SetReference(e).SetSelectedTextStyle(treeSelectedStyle)
		if e.Type == mpdclient.EntryDirectory {
			node.SetExpanded(false)
			node.AddChild(tview.NewTreeNode("").SetSelectable(false).SetReference(placeholderUnloaded))
		}
		nodes[i] = node
	}
	return nodes
}

func entryLabel(e mpdclient.DirEntry) string {
	switch e.Type {
	case mpdclient.EntryDirectory:
		return baseName(e.Path)
	case mpdclient.EntryPlaylist:
		return e.Path
	default: // EntryFile
		return trackLabel(e.Song)
	}
}

func baseName(path string) string {
	if i := strings.LastIndexByte(path, '/'); i >= 0 {
		return path[i+1:]
	}
	return path
}

func trackLabel(s mpdclient.Song) string {
	return fmt.Sprintf("%s  [%s]", s.DisplayName(), FormatDuration(s.Duration))
}
