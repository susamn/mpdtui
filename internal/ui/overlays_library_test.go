package ui

import (
	"strings"
	"testing"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"mpdtui/internal/mpdclient"
)

// openInputField returns the input field of whatever text-entry overlay
// is currently open, and submits text through it the way Enter does.
func submitInput(t *testing.T, a *App, text string) {
	t.Helper()
	field, ok := a.tv.GetFocus().(*tview.InputField)
	if !ok {
		t.Fatalf("focus is %T, want a text input overlay", a.tv.GetFocus())
	}
	field.SetText(text)
	field.InputHandler()(tcell.NewEventKey(tcell.KeyEnter, 0, tcell.ModNone), func(tview.Primitive) {})
}

// cancelInput dismisses an open text-entry overlay with Escape.
func cancelInput(t *testing.T, a *App) {
	t.Helper()
	field, ok := a.tv.GetFocus().(*tview.InputField)
	if !ok {
		t.Fatalf("focus is %T, want a text input overlay", a.tv.GetFocus())
	}
	field.InputHandler()(tcell.NewEventKey(tcell.KeyEscape, 0, tcell.ModNone), func(tview.Primitive) {})
}

// --- openInput ----------------------------------------------------------

func TestOpenInputSubmitsOnEnter(t *testing.T) {
	a := newFakeApp(t, &fakeMPD{})

	var got string
	var called bool
	a.openInput("Name: ", "initial", func(text string) { got, called = text, true })

	if a.mode != modeOverlay {
		t.Fatal("openInput did not enter overlay mode")
	}
	field, ok := a.tv.GetFocus().(*tview.InputField)
	if !ok {
		t.Fatalf("focus is %T, want the input field", a.tv.GetFocus())
	}
	if field.GetText() != "initial" {
		t.Errorf("field starts with %q, want the initial value", field.GetText())
	}

	submitInput(t, a, "typed name")

	if !called {
		t.Fatal("onSubmit was not called on Enter")
	}
	if got != "typed name" {
		t.Errorf("onSubmit got %q, want %q", got, "typed name")
	}
	if a.mode != modeNormal {
		t.Error("the overlay stayed open after submitting")
	}
}

// TestOpenInputEscapeDoesNotSubmit is the cancel contract: Esc must
// close the overlay without running the action.
func TestOpenInputEscapeDoesNotSubmit(t *testing.T) {
	a := newFakeApp(t, &fakeMPD{})

	called := false
	a.openInput("Name: ", "", func(string) { called = true })
	cancelInput(t, a)

	if called {
		t.Error("onSubmit ran on Escape")
	}
	if a.mode != modeNormal {
		t.Error("the overlay stayed open after Escape")
	}
}

func TestOpenInputWithTitle(t *testing.T) {
	a := newFakeApp(t, &fakeMPD{})

	a.openInputWithTitle(" Rename ", "New name: ", "", func(string) {})

	field, ok := a.tv.GetFocus().(*tview.InputField)
	if !ok {
		t.Fatalf("focus is %T, want the input field", a.tv.GetFocus())
	}
	if got := field.GetTitle(); !strings.Contains(got, "Rename") {
		t.Errorf("border title = %q, want it to carry the given title", got)
	}
}

// --- '/' search routing -------------------------------------------------

func TestSearchKeyRoutesByFocusedPanel(t *testing.T) {
	t.Run("playlists filters in place", func(t *testing.T) {
		a := newFakeApp(t, &fakeMPD{})
		setPlaylistsForTest(a.playlists, []string{"Road Trip", "Chill"})
		a.tv.SetFocus(a.playlists.table)

		a.openSearch()
		submitInput(t, a, "  chill  ")

		if a.playlists.filter != "chill" {
			t.Errorf("playlists filter = %q, want the trimmed query", a.playlists.filter)
		}
	})

	t.Run("queue focuses its own pinned field", func(t *testing.T) {
		a := newFakeApp(t, &fakeMPD{})
		a.tv.SetFocus(a.queue.table)

		a.openSearch()

		if a.tv.GetFocus() != a.queue.search {
			t.Errorf("focus = %T, want the Queue's own search field", a.tv.GetFocus())
		}
		if a.mode != modeOverlay {
			t.Error("the Queue search did not enter overlay mode")
		}

		// Closing clears the field and restores focus.
		a.closeOverlay()
		if got := a.queue.search.GetText(); got != "" {
			t.Errorf("search field = %q after closing, want it cleared", got)
		}
		if a.tv.GetFocus() != a.queue.table {
			t.Errorf("focus after closing = %T, want the Queue table", a.tv.GetFocus())
		}
	})

	t.Run("library searches the whole library", func(t *testing.T) {
		f := &fakeMPD{songs: []mpdclient.Song{
			{Title: "Ay Hairathe", Artist: "Hariharan", File: "a/1.mp3"},
			{Title: "Barso Re", Artist: "Shreya", File: "a/2.mp3"},
		}}
		a := newFakeApp(t, f)
		a.tv.SetFocus(a.library.tree)

		a.openSearch()
		submitInput(t, a, "hairathe")

		if a.library.mode != libSearch {
			t.Error("the Library did not switch to search mode")
		}
		if got := a.library.tree.GetRoot().GetChildren(); len(got) != 1 {
			t.Errorf("search matched %d tracks, want 1", len(got))
		}
	})

	t.Run("an empty library query is ignored", func(t *testing.T) {
		a := newFakeApp(t, &fakeMPD{})
		a.tv.SetFocus(a.library.tree)

		a.openSearch()
		submitInput(t, a, "   ")

		if a.library.mode == libSearch {
			t.Error("a blank query switched the Library into search mode")
		}
	})
}

// TestLibrarySearchIsAccentInsensitive is the README's own promise, and
// the reason the search is done client-side rather than through MPD's
// own substring matching.
func TestLibrarySearchIsAccentInsensitive(t *testing.T) {
	f := &fakeMPD{songs: []mpdclient.Song{
		{Title: "Sway", Artist: "Michael Bublé", File: "b/1.mp3"},
		{Title: "Other", Artist: "Someone", File: "b/2.mp3"},
	}}
	a := newFakeApp(t, f)

	if got := a.library.showSearch("buble"); got != 1 {
		t.Errorf("searching %q matched %d tracks, want 1 (accent-folded)", "buble", got)
	}
}

func TestLibrarySearchWithNoResultsSaysSo(t *testing.T) {
	f := &fakeMPD{songs: []mpdclient.Song{{Title: "Only", File: "a.mp3"}}}
	a := newFakeApp(t, f)

	if got := a.library.showSearch("nothing matches this"); got != 0 {
		t.Errorf("matched %d tracks, want 0", got)
	}
	if got := a.hintBar.GetText(true); !strings.Contains(got, "no results") {
		t.Errorf("hint bar = %q, want it to report no results", got)
	}
}

func TestLibrarySearchReportsAFailedFetch(t *testing.T) {
	a := newFakeApp(t, &fakeMPD{err: errTest})

	if got := a.library.showSearch("anything"); got != 0 {
		t.Errorf("matched %d tracks on a failed fetch, want 0", got)
	}
	if got := a.hintBar.GetText(true); !strings.Contains(got, errTest.Error()) {
		t.Errorf("hint bar = %q, want the failure reported", got)
	}
}

// --- Library Enter/add --------------------------------------------------

func TestLibraryEnterOnATrackAddsAndPlays(t *testing.T) {
	f := &fakeMPD{}
	a := newFakeApp(t, f)

	node := tview.NewTreeNode("track").SetReference(mpdclient.DirEntry{
		Type: mpdclient.EntryFile,
		Song: mpdclient.Song{File: "a/1.mp3"},
	})
	a.library.onSelect(node)

	if !f.did("queueaddid:a/1.mp3") || !f.did("play:1") {
		t.Errorf("did %v, want the track queued and played", f.commands())
	}
}

func TestLibraryEnterOnAPlaylistAppendsIt(t *testing.T) {
	f := &fakeMPD{}
	a := newFakeApp(t, f)

	node := tview.NewTreeNode("pl").SetReference(mpdclient.DirEntry{
		Type: mpdclient.EntryPlaylist,
		Path: "Road Trip",
	})
	a.library.onSelect(node)

	if !f.did("plappend:Road Trip") {
		t.Errorf("did %v, want the playlist appended", f.commands())
	}
}

// TestLibraryEnterOnAnAlbumGroupJustExpands covers the grouping node
// from album search: it is a container, so Enter toggles it rather than
// queueing anything.
func TestLibraryEnterOnAnAlbumGroupJustExpands(t *testing.T) {
	f := &fakeMPD{}
	a := newFakeApp(t, f)

	node := tview.NewTreeNode("Guru").SetReference(&albumGroup{
		label: "Guru",
		songs: []mpdclient.Song{{File: "a/1.mp3"}},
	})
	node.SetExpanded(false)

	a.library.onSelect(node)

	if !node.IsExpanded() {
		t.Error("Enter on an album group did not expand it")
	}
	if got := f.commands(); len(got) != 0 {
		t.Errorf("Enter on an album group issued %v, want nothing", got)
	}
}

func TestLibraryEnterOnANodeWithNothingToDo(t *testing.T) {
	f := &fakeMPD{}
	a := newFakeApp(t, f)

	a.library.onSelect(tview.NewTreeNode("placeholder")) // no reference

	if got := f.commands(); len(got) != 0 {
		t.Errorf("Enter on a reference-less node issued %v, want nothing", got)
	}
}

func TestLibraryAddSelectedAddsAnAlbumGroupWholesale(t *testing.T) {
	f := &fakeMPD{}
	a := newFakeApp(t, f)

	node := tview.NewTreeNode("Guru").SetReference(&albumGroup{
		label: "Guru",
		songs: []mpdclient.Song{{File: "a/1.mp3"}, {File: "a/2.mp3"}},
	})
	a.library.tree.GetRoot().AddChild(node)
	a.library.tree.SetCurrentNode(node)

	if !a.library.addSelected() {
		t.Fatal("addSelected reported nothing added for an album group")
	}
	if !f.did("queueadd:a/1.mp3") || !f.did("queueadd:a/2.mp3") {
		t.Errorf("did %v, want every track in the group added", f.commands())
	}
	if got := a.hintBar.GetText(true); !strings.Contains(got, "2 track(s)") {
		t.Errorf("hint bar = %q, want it to report how many were added", got)
	}
}

func TestLibraryAddSelectedReportsNothingToAdd(t *testing.T) {
	f := &fakeMPD{}
	a := newFakeApp(t, f)

	// No current node at all.
	a.library.tree.SetCurrentNode(nil)
	if a.library.addSelected() {
		t.Error("addSelected reported success with no node selected")
	}

	// A node with no reference (an unloaded-directory placeholder).
	node := tview.NewTreeNode("placeholder")
	a.library.tree.GetRoot().AddChild(node)
	a.library.tree.SetCurrentNode(node)
	if a.library.addSelected() {
		t.Error("addSelected reported success for a node with nothing to add")
	}
}

// --- 'a' routing --------------------------------------------------------

// TestAddKeyRoutesByFocusedPanel covers handleAdd's fan-out, including
// that focus only moves to the Queue on a real add.
func TestAddKeyRoutesByFocusedPanel(t *testing.T) {
	t.Run("library add moves focus to the queue", func(t *testing.T) {
		f := &fakeMPD{}
		a := newFakeApp(t, f)
		node := tview.NewTreeNode("track").SetReference(mpdclient.DirEntry{
			Type: mpdclient.EntryFile,
			Path: "a/1.mp3",
			Song: mpdclient.Song{File: "a/1.mp3"},
		})
		a.library.tree.GetRoot().AddChild(node)
		a.library.tree.SetCurrentNode(node)
		a.tv.SetFocus(a.library.tree)

		a.handleAdd()

		if !f.did("queueadd:a/1.mp3") {
			t.Errorf("did %v, want the track added", f.commands())
		}
		if a.tv.GetFocus() != a.queue.table {
			t.Errorf("focus = %T, want it moved to the Queue after a real add", a.tv.GetFocus())
		}
	})

	t.Run("library add of nothing leaves focus alone", func(t *testing.T) {
		a := newFakeApp(t, &fakeMPD{})
		node := tview.NewTreeNode("placeholder")
		a.library.tree.GetRoot().AddChild(node)
		a.library.tree.SetCurrentNode(node)
		a.tv.SetFocus(a.library.tree)

		a.handleAdd()

		if a.tv.GetFocus() != a.library.tree {
			t.Errorf("focus = %T, want it left on the Library when nothing was added", a.tv.GetFocus())
		}
	})

	t.Run("playlists appends the selected playlist", func(t *testing.T) {
		f := &fakeMPD{}
		a := newFakeApp(t, f)
		setPlaylistsForTest(a.playlists, []string{"Road Trip"})
		a.tv.SetFocus(a.playlists.table)
		a.playlists.table.Select(1, 0)

		a.handleAdd()

		if !f.did("plappend:Road Trip") {
			t.Errorf("did %v, want the playlist appended", f.commands())
		}
	})

	t.Run("anywhere else is an invalid key", func(t *testing.T) {
		a := newFakeApp(t, &fakeMPD{})
		a.tv.SetFocus(a.nowPlaying)

		a.handleAdd()

		if got := a.hintBar.GetText(true); !strings.Contains(got, "a") {
			t.Errorf("hint bar = %q, want an invalid-key flash", got)
		}
	})
}

// --- 'S' save queue as playlist ----------------------------------------

func TestSaveQueueAsPlaylist(t *testing.T) {
	f := &fakeMPD{}
	a := newFakeApp(t, f)
	a.tv.SetFocus(a.playlists.table)

	a.handleSavePlaylist()
	submitInput(t, a, "  My Mix  ")

	if !f.did("plsave:My Mix") {
		t.Errorf("did %v, want the queue saved under the trimmed name", f.commands())
	}
	if got := a.hintBar.GetText(true); !strings.Contains(got, "My Mix") {
		t.Errorf("hint bar = %q, want it to confirm the save", got)
	}
}

func TestSaveQueueAsPlaylistIgnoresABlankName(t *testing.T) {
	f := &fakeMPD{}
	a := newFakeApp(t, f)
	a.tv.SetFocus(a.playlists.table)

	a.handleSavePlaylist()
	submitInput(t, a, "   ")

	if got := f.commands(); len(got) != 0 {
		t.Errorf("did %v for a blank name, want nothing", got)
	}
}

func TestSaveQueueAsPlaylistOnlyFromThePlaylistsPanel(t *testing.T) {
	f := &fakeMPD{}
	a := newFakeApp(t, f)
	a.tv.SetFocus(a.queue.table)

	a.handleSavePlaylist()

	if a.mode == modeOverlay {
		t.Error("opened the save prompt from the wrong panel")
	}
	if got := a.hintBar.GetText(true); !strings.Contains(got, "S") {
		t.Errorf("hint bar = %q, want an invalid-key flash", got)
	}
}

func TestSaveQueueAsPlaylistReportsFailure(t *testing.T) {
	f := &fakeMPD{err: errTest}
	a := newFakeApp(t, f)
	a.tv.SetFocus(a.playlists.table)

	a.handleSavePlaylist()
	submitInput(t, a, "My Mix")

	if got := a.hintBar.GetText(true); !strings.Contains(got, errTest.Error()) {
		t.Errorf("hint bar = %q, want the failure reported", got)
	}
}
