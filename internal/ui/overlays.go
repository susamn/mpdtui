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

// openSearch opens the '/' overlay, contextual on the focused panel:
// filters Playlists in place, or full-text searches the Library.
func (a *App) openSearch() {
	if a.tv.GetFocus() == a.playlists.list {
		a.openInput("Filter playlists: ", a.playlists.filter, func(text string) {
			a.playlists.setFilter(strings.TrimSpace(text))
		})
		return
	}
	a.openInput("Search library: ", "", func(text string) {
		text = strings.TrimSpace(text)
		if text == "" {
			return
		}
		a.library.showSearch(text)
		a.focusPanelPrimitive(a.library.list)
	})
}

func (a *App) handleSavePlaylist() {
	if a.tv.GetFocus() != a.playlists.list {
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
  Tab, 1/2/3     cycle / jump focus between panels
  /              search (contextual: Library or Playlists)
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

[::b]Queue panel[-:-:-]
  Enter          play selected track
  d              remove selected track
  J / K          move selected track down / up
  D              clear entire queue (confirm)
`
