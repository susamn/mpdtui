package ui

import (
	"strings"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

// centered wraps p in a fixed-size box centered on the full screen, the
// usual tview pattern for modal-style overlays on top of a Pages root.
func centered(p tview.Primitive, width, height int) tview.Primitive {
	return tview.NewGrid().
		SetColumns(0, width, 0).
		SetRows(0, height, 0).
		AddItem(p, 1, 1, 1, 1, 0, 0, true)
}

// showOverlay adds root as a page on top of "main", remembers the focus
// to restore, and switches into overlay mode (see globalInputCapture).
func (a *App) showOverlay(name string, root, focus tview.Primitive) {
	a.beforeOverlayFocus = a.tv.GetFocus()
	a.mode = modeOverlay
	a.closeOverlay = func() {
		a.pages.RemovePage(name)
		a.pages.SwitchToPage("main")
		a.mode = modeNormal
		if a.beforeOverlayFocus != nil {
			a.tv.SetFocus(a.beforeOverlayFocus)
		}
	}
	a.pages.AddPage(name, root, true, true)
	a.tv.SetFocus(focus)
}

// openInput shows a single-line text input overlay. onSubmit is called
// with the entered text on Enter; Esc cancels without calling it.
func (a *App) openInput(label, initial string, onSubmit func(string)) {
	field := tview.NewInputField().SetLabel(label).SetText(initial).SetFieldWidth(40)
	field.SetBorder(true)
	field.SetDoneFunc(func(key tcell.Key) {
		text := field.GetText()
		a.closeOverlay()
		if key == tcell.KeyEnter {
			onSubmit(text)
		}
	})
	a.showOverlay("input", centered(field, 60, 3), field)
}

// openSearch opens the '/' search, contextual on the focused panel:
// filters Playlists in place, full-text searches the Library (both via a
// centered popup), or focuses the Queue's own persistent search field
// (see openQueueSearch and queuePanel.search).
func (a *App) openSearch() {
	switch a.tv.GetFocus() {
	case a.playlists.table:
		a.openInput("Filter playlists: ", a.playlists.filter, func(text string) {
			a.playlists.setFilter(strings.TrimSpace(text))
		})
	case a.queue.table:
		a.openQueueSearch()
	case a.library.tree:
		a.openInput("Search library: ", "", func(text string) {
			text = strings.TrimSpace(text)
			if text == "" {
				return
			}
			a.library.showSearch(text)
			a.focusPanelPrimitive(a.library.tree)
		})
	}
}

// openQueueSearch focuses the "Search track: " field permanently pinned
// below the Queue table (built once in newQueuePanel, not created/torn
// down per search) -- unlike Library/Playlists, nothing is added to or
// removed from the layout here. queuePanel's own SetDoneFunc handles
// Enter (jump to the first match, or a flash message if none); Esc is
// handled globally in overlay mode and calls closeOverlay below either
// way, clearing the field and returning focus to the Queue table.
func (a *App) openQueueSearch() {
	a.beforeOverlayFocus = a.tv.GetFocus()
	a.mode = modeOverlay
	a.closeOverlay = func() {
		a.queue.search.SetText("")
		a.mode = modeNormal
		if a.beforeOverlayFocus != nil {
			a.tv.SetFocus(a.beforeOverlayFocus)
		}
	}
	a.tv.SetFocus(a.queue.search)
}

func (a *App) handleSavePlaylist() {
	if a.tv.GetFocus() != a.playlists.table {
		a.invalidKey("S")
		return
	}
	a.openInput("Save queue as playlist: ", "", func(name string) {
		name = strings.TrimSpace(name)
		if name == "" {
			return
		}
		if err := a.client.SaveQueueAsPlaylist(name); err != nil {
			a.showError(err)
			return
		}
		a.playlists.refresh()
		a.showMessage("saved playlist " + name)
	})
}

// confirm shows a Yes/No modal; onYes runs only if the user picks Yes.
// Esc (handled globally in overlay mode) cancels without calling it.
func (a *App) confirm(text string, onYes func()) {
	modal := tview.NewModal().SetText(text).AddButtons([]string{"Yes", "No"})
	modal.SetDoneFunc(func(_ int, label string) {
		a.closeOverlay()
		if label == "Yes" {
			onYes()
		}
	})
	a.showOverlay("confirm", modal, modal)
}

func (a *App) openHelp() {
	view := tview.NewTextView().SetDynamicColors(true).SetText(helpText)
	view.SetBorder(true).SetTitle(" Help (Esc to close) ")
	a.showOverlay("help", centered(view, 76, 22), view)
}

const helpText = `[::b]Global[-:-:-]
  Space          play / pause
  s              stop
  n / p          next / previous track
  , / .          seek -5s / +5s
  - / =          volume down / up
  z              toggle shuffle (random)
  x              toggle repeat
  c              toggle consume
  Z              toggle single
  D              clear entire queue (confirm)
  Tab, 1/2/3     cycle / jump focus between panels
  /              search (contextual: Library/Playlists filter, Queue jump)
  f              global search from any panel: "a/al/p/t <term>" for
                 artist/album/playlist/track (e.g. "a queen", "al hello"),
                 with live fzf-style hints as you type -- Up/Down (or
                 Ctrl-P/Ctrl-N) move the highlight, Enter acts on it
                 directly: track adds+plays, playlist loads+plays, artist/
                 album jump into that group in the Library -- accent-
                 insensitive, so "buble" matches "Bublé"
  F              clear any active search/filter, in every panel at once
  i              track info card for the currently playing track
  v              cycle Now Playing visualizations
  L              locate the currently playing track in the Queue
  ?              this help
  q              quit

[::b]Library panel[-:-:-]
  Enter          expand/collapse a folder, or add+play a track
  a              add selected folder/track to queue (no play)
  Backspace      collapse folder, or go up to its parent
  o              cycle sort: name / most recently modified
  Esc            clear active search

[::b]Playlists panel[-:-:-]
  Enter          load playlist into queue and play
  a              append playlist to queue
  d              delete playlist (confirm)
  S              save current queue as a new playlist
  R              refresh track counts now (also happens automatically
                 every 10 minutes in the background)
  /              filter playlists by name
  o              cycle sort: most recently updated / name
  Esc            clear active filter
  🆕 badge shows the 5 most recently updated playlists,
  regardless of sort mode

[::b]Queue panel[-:-:-]
  Enter          play selected track
  d              remove selected track
  J / K          move selected track down / up
  /              search: jump to first match (Esc cancels)
`
