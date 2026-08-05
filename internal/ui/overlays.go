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
// centered popup), or jumps to a match in the Queue (via an input attached
// to the Queue panel itself -- see openQueueSearch).
func (a *App) openSearch() {
	switch a.tv.GetFocus() {
	case a.playlists.list:
		a.openInput("Filter playlists: ", a.playlists.filter, func(text string) {
			a.playlists.setFilter(strings.TrimSpace(text))
		})
	case a.queue.table:
		a.openQueueSearch()
	case a.library.list:
		a.openInput("Search library: ", "", func(text string) {
			text = strings.TrimSpace(text)
			if text == "" {
				return
			}
			a.library.showSearch(text)
			a.focusPanelPrimitive(a.library.list)
		})
	}
}

// openQueueSearch adds a search input directly above the Queue table,
// inside the Queue column: it takes over the Queue's slot in the main
// layout with a small vertical stack (input + the same table below it),
// so the Queue stays fully visible and unfiltered while typing rather
// than being replaced by a centered popup. Enter jumps the selection to
// the first track whose name matches (no match: a flash message,
// selection unchanged); Esc (handled globally in overlay mode) cancels.
// Either way the Queue table returns to its normal slot and focus.
func (a *App) openQueueSearch() {
	input := tview.NewInputField().SetLabel("Search track: ")
	input.SetBorder(true)

	wrap := tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(input, 3, 0, true).
		AddItem(a.queue.table, 0, 1, false)

	a.beforeOverlayFocus = a.tv.GetFocus()
	a.mode = modeOverlay
	a.closeOverlay = func() {
		a.main.RemoveItem(wrap)
		a.main.AddItem(a.queue.table, 0, 2, false)
		a.mode = modeNormal
		if a.beforeOverlayFocus != nil {
			a.tv.SetFocus(a.beforeOverlayFocus)
		}
	}

	input.SetDoneFunc(func(key tcell.Key) {
		text := strings.TrimSpace(input.GetText())
		a.closeOverlay()
		if key == tcell.KeyEnter && text != "" {
			if !a.queue.jumpToMatch(text) {
				a.showMessage("no match for " + text)
			}
		}
	})

	a.main.RemoveItem(a.queue.table)
	a.main.AddItem(wrap, 0, 2, true)
	a.tv.SetFocus(input)
}

func (a *App) handleSavePlaylist() {
	if a.tv.GetFocus() != a.playlists.list {
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
  ?              this help
  q              quit

[::b]Library panel[-:-:-]
  Enter          drill into artist/album, or add+play a track
  a              add selected artist/album/track to queue (no play)
  Backspace      go back up a level

[::b]Playlists panel[-:-:-]
  Enter          load playlist into queue and play
  a              append playlist to queue
  d              delete playlist (confirm)
  S              save current queue as a new playlist
  /              filter playlists by name
  Esc            clear active filter

[::b]Queue panel[-:-:-]
  Enter          play selected track
  d              remove selected track
  J / K          move selected track down / up
  /              search: jump to first match (Esc cancels)
`
