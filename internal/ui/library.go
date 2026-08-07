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

const (
	folderClosedIcon = "📁"
	folderOpenIcon   = "📂"
)

type libraryMode int

const (
	libBrowse libraryMode = iota
	libSearch
)

// librarySortMode controls buildNodes' within-type ordering (directories
// are always grouped before files/playlists regardless of mode -- see
// buildNodes). Cycled with 'o' while the Library panel is focused in
// browse mode (see App.handleCycleSort).
type librarySortMode int

const (
	librarySortName   librarySortMode = iota // alphabetical, case-insensitive
	librarySortRecent                        // most recently modified first
)

func (m librarySortMode) label() string {
	if m == librarySortRecent {
		return "recent"
	}
	return "name"
}

func (m librarySortMode) next() librarySortMode {
	return (m + 1) % 2
}

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

	mode     libraryMode
	query    string
	sortMode librarySortMode
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
	for _, n := range buildNodes(entries, p.sortMode) {
		p.root.AddChild(n)
	}
	p.tree.SetTitle(fmt.Sprintf(" Library (%s) ", p.sortMode.label()))
	p.tree.SetCurrentNode(p.root)
}

// cycleSortMode advances to the next sort mode and reloads the root with
// it. Only meaningful in browse mode (see App.handleCycleSort, which
// gates the 'o' key on that); search results have their own fixed
// ordering. Reloading the root is a deliberate simplification -- MPD's
// lazy per-folder loading means an already-expanded subtree fetched under
// the old mode would otherwise keep its old order until collapsed and
// re-expanded, so this resets to a fresh, consistently-sorted top level
// instead of leaving mixed-order state around.
func (p *libraryPanel) cycleSortMode() {
	p.sortMode = p.sortMode.next()
	p.showRoot()
}

// showSearch full-text searches the library by tag (independent of the
// directory tree) and shows the matches as a flat list of file nodes.
// Returns the number of matched tracks.
func (p *libraryPanel) showSearch(query string) int {
	songs, err := p.app.client.Search(query)
	if err != nil {
		p.app.showError(err)
		return 0
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
	return len(songs)
}

// albumGroup marks a TreeNode as an album-search result header rather
// than a real MPD directory: its tracks are already fetched (the same
// search call that found this album also returned them), so expanding it
// needs no further MPD round-trip, unlike toggleDirectory's lazy
// real-directory case.
type albumGroup struct {
	label string
	songs []mpdclient.Song
}

// showAlbumSearch searches the library by Album tag and groups matching
// tracks by (Artist, Album) into expandable headers -- unlike showSearch's
// flat per-track results, "found an album" reads better as "here's the
// album, browse into it" than a flat dump of its tracks. Returns the
// number of matched albums.
func (p *libraryPanel) showAlbumSearch(query string) int {
	songs, err := p.app.client.SearchAlbums(query)
	if err != nil {
		p.app.showError(err)
		return 0
	}

	p.mode = libSearch
	p.query = query
	p.root.ClearChildren()

	groups := groupByAlbum(songs)
	for _, g := range groups {
		g := g
		node := tview.NewTreeNode(fmt.Sprintf("%s (%d)", g.label, len(g.songs))).
			SetReference(g).
			SetSelectedTextStyle(treeSelectedStyle).
			SetExpanded(false)
		for _, s := range g.songs {
			s := s
			entry := mpdclient.DirEntry{Type: mpdclient.EntryFile, Path: s.File, Song: s}
			node.AddChild(tview.NewTreeNode(trackLabel(s)).SetReference(entry).SetSelectedTextStyle(treeSelectedStyle))
		}
		p.root.AddChild(node)
	}

	p.tree.SetTitle(fmt.Sprintf(" Library: album search %q (%d) ", query, len(groups)))
	p.tree.SetCurrentNode(p.root)
	if len(groups) == 0 {
		p.app.showMessage("no albums found for " + query)
	}
	return len(groups)
}

// groupByAlbum groups songs by (Artist, Album), sorted alphabetically.
func groupByAlbum(songs []mpdclient.Song) []*albumGroup {
	index := make(map[string]*albumGroup)
	var order []string
	for _, s := range songs {
		key := s.Artist + "\x00" + s.Album
		g, ok := index[key]
		if !ok {
			label := s.Album
			if s.Artist != "" {
				label = s.Artist + " - " + s.Album
			}
			g = &albumGroup{label: label}
			index[key] = g
			order = append(order, key)
		}
		g.songs = append(g.songs, s)
	}
	sort.Strings(order)
	groups := make([]*albumGroup, len(order))
	for i, key := range order {
		groups[i] = index[key]
	}
	return groups
}

// back handles Backspace: from search results, returns to the directory
// root (same target Esc uses). While browsing, it collapses the current
// node if it's an expanded directory or album-search group, or otherwise
// moves the selection up to (and collapses) its parent -- standard
// file-explorer "go back".
func (p *libraryPanel) back() {
	if p.mode == libSearch {
		p.showRoot()
		return
	}

	current := p.tree.GetCurrentNode()
	if current == nil || current == p.root {
		return
	}
	if current.IsExpanded() && collapseGroup(current) {
		return
	}

	path := p.tree.GetPath(current)
	if len(path) < 2 {
		return
	}
	parent := path[len(path)-2]
	collapseGroup(parent)
	p.tree.SetCurrentNode(parent)
}

// collapseGroup collapses node if it's a directory or an album-search
// group, returning whether it was one -- regardless of prior expanded
// state (SetExpanded(false) on an already-collapsed node is a harmless
// no-op). Directories additionally get their folder icon updated to
// match.
func collapseGroup(node *tview.TreeNode) bool {
	switch ref := node.GetReference().(type) {
	case mpdclient.DirEntry:
		if ref.Type == mpdclient.EntryDirectory {
			setDirExpanded(node, ref.Path, false)
			return true
		}
	case *albumGroup:
		node.SetExpanded(false)
		return true
	}
	return false
}

// onSelect is the TreeView-wide handler for Enter/Space: expand/collapse a
// directory or album-search group, add+play a track, or append a stored
// playlist encountered in the tree. Nodes with no reference (the root, or
// an unloaded/empty placeholder) have nothing to do.
func (p *libraryPanel) onSelect(node *tview.TreeNode) {
	if _, ok := node.GetReference().(*albumGroup); ok {
		node.SetExpanded(!node.IsExpanded())
		return
	}
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
		setDirExpanded(node, entry.Path, false)
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
				for _, n := range buildNodes(fetched, p.sortMode) {
					node.AddChild(n)
				}
			}
		}
	}
	setDirExpanded(node, entry.Path, true)
}

// addSelected implements 'a' (add to queue, no play): the whole subtree
// for a directory (MPD's own "add" command recurses server-side -- no need
// to fetch and iterate children here), every track in an album-search
// group, a single track for a file, or an append for a playlist entry
// (there's no meaningful "add without loading" distinction for a stored
// playlist, so it behaves like Enter).
func (p *libraryPanel) addSelected() {
	node := p.tree.GetCurrentNode()
	if node == nil {
		return
	}
	if g, ok := node.GetReference().(*albumGroup); ok {
		for _, s := range g.songs {
			if err := p.app.client.QueueAdd(s.File); err != nil {
				p.app.showError(err)
				return
			}
		}
		p.app.queue.refresh()
		p.app.showMessage(fmt.Sprintf("added %d track(s) from %s", len(g.songs), g.label))
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

// buildNodes turns entries into tree nodes, directories always grouped
// first (both sort modes keep this -- mixing directories and files by
// recency reads as a jumbled listing, whereas file explorers conventionally
// keep folders first even under "sort by date modified"), then ordered by
// mode within each group: alphabetical (librarySortName, matching common
// file-explorer ordering) or most-recently-modified-first
// (librarySortRecent, falling back to the same alphabetical comparison for
// equal or missing timestamps -- see DirEntry.LastModified's doc comment
// on why "missing" can't just sort as "oldest"... it does here, but
// deterministically, via the name tiebreak, rather than clumping
// unpredictably). Each directory node starts collapsed with a single
// unloaded placeholder child, populated lazily by toggleDirectory.
func buildNodes(entries []mpdclient.DirEntry, mode librarySortMode) []*tview.TreeNode {
	sorted := make([]mpdclient.DirEntry, len(entries))
	copy(sorted, entries)
	sort.SliceStable(sorted, func(i, j int) bool {
		di := sorted[i].Type == mpdclient.EntryDirectory
		dj := sorted[j].Type == mpdclient.EntryDirectory
		if di != dj {
			return di
		}
		if mode == librarySortRecent {
			li, lj := sorted[i].LastModified, sorted[j].LastModified
			if !li.Equal(lj) {
				return li.After(lj)
			}
		}
		// Case-insensitive: this library mixes folder-naming conventions
		// (e.g. both "Alisha Chinoy" and "alisha-chinai" exist as separate
		// top-level directories), and a case-sensitive sort would scatter
		// them apart into a Digits/Uppercase/lowercase clustering instead
		// of the alphabetical order a user actually expects. Also serves
		// as librarySortRecent's tiebreak for equal/missing timestamps.
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
		return folderLabel(e.Path, false) // buildNodes always starts directories collapsed
	case mpdclient.EntryPlaylist:
		return e.Path
	default: // EntryFile
		return trackLabel(e.Song)
	}
}

// folderLabel prefixes a directory's name with a closed or open folder
// icon depending on expanded, so a folder's row visibly reflects its
// current state rather than showing a static icon that looks stale once
// its children are showing underneath it.
func folderLabel(path string, expanded bool) string {
	icon := folderClosedIcon
	if expanded {
		icon = folderOpenIcon
	}
	return icon + " " + baseName(path)
}

// setDirExpanded expands or collapses a directory node, keeping its
// folder icon in sync with the new state.
func setDirExpanded(node *tview.TreeNode, path string, expanded bool) {
	node.SetExpanded(expanded)
	node.SetText(folderLabel(path, expanded))
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
