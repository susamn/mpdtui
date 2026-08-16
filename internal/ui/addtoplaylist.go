package ui

import (
	"errors"
	"fmt"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"mpdtui/internal/mpdclient"
)

// maxAddToPlaylistHints caps how many fuzzy playlist matches the popup
// shows at once, mirroring maxGlobalSearchHints -- a glanceable
// shortlist, not a full dump of every stored playlist.
const maxAddToPlaylistHints = 8

// playlistPickerHints is the pure bookkeeping behind the add-to-playlist
// popup's live filter list: given the field's current text and the full
// set of stored playlist names, fuzzy-ranks them and tracks which one is
// highlighted. Mirrors globalSearchHints (globalsearch.go) minus the
// kind-prefix parsing that one needs -- there's only ever one candidate
// list here (stored playlist names), so this is its own small type
// rather than a forced reuse of the heavier one. Split out from the tview
// wiring purely for testability, same reasoning as globalSearchHints.
type playlistPickerHints struct {
	names     []string
	order     []int // indices into names, best-match-first, capped
	total     int   // true match count before the cap
	highlight int   // index into order; -1 if order is empty
}

func (h *playlistPickerHints) rebuild(query string, names []string) {
	h.names = names
	matched := fuzzyFilterSortIndex(query, names)
	h.total = len(matched)
	if h.total > maxAddToPlaylistHints {
		matched = matched[:maxAddToPlaylistHints]
	}
	h.order = matched
	h.highlight = -1
	if len(h.order) > 0 {
		h.highlight = 0
	}
}

func (h *playlistPickerHints) move(delta int) {
	h.highlight = moveHintHighlight(h.highlight, len(h.order), delta)
}

func (h *playlistPickerHints) jumpFirst() {
	if len(h.order) > 0 {
		h.highlight = 0
	}
}

func (h *playlistPickerHints) jumpLast() {
	if len(h.order) > 0 {
		h.highlight = len(h.order) - 1
	}
}

// current returns the currently highlighted playlist name and its index
// into h.names, or ("", -1) if nothing's highlighted (no playlists at
// all, or zero matches for the current query).
func (h *playlistPickerHints) current() (name string, idx int) {
	if h.highlight < 0 || h.highlight >= len(h.order) {
		return "", -1
	}
	idx = h.order[h.highlight]
	return h.names[idx], idx
}

// openAddToPlaylistPicker is 'a' on the Queue panel: fuzzy-search the
// stored playlists and add the currently Queue-selected track to
// whichever one is chosen -- writing straight into that playlist's own
// .m3u file via mpdclient.Client.AddTrackToPlaylist (MPD's "playlistadd"
// command). This is the opposite direction from Library/Playlists' own
// 'a' key: appendPlaylist there loads a stored playlist's tracks INTO the
// queue, never touching any file. A no-op if nothing's selected in the
// Queue (mirrors handleRateSelectedTrack/handleOpenMarkPicker's own
// silent no-op on an empty selection).
//
// j/k/g/G and Up/Down/Ctrl-P/Ctrl-N navigate the hint list, Tab/Backtab
// (and 'f' from the list back to the field, matching openGlobalSearch's
// own muscle memory) toggle focus between typing and navigating, Enter
// adds to the highlighted playlist. Unlike openGlobalSearch's confirm,
// this always closes the popup first and reports the outcome afterward
// via the usual hint-bar flash (success or error, most commonly
// ErrTrackAlreadyInPlaylist) -- mirrors Playlists' own delete
// confirmation (a.confirm closes immediately; the callback's own error
// surfaces after), rather than leaving a stale popup open to retry from.
// No text-entry conflict handling is needed in globalInputCapture (no
// noTextInputOverlays entry, no transport-key passthrough): the field is
// a real text input, exactly like openGlobalSearch's own popup, which
// gets the same (lack of) treatment.
func (a *App) openAddToPlaylistPicker() {
	song, ok := a.queue.selectedSong()
	if !ok {
		return
	}

	names := playlistLabels(a.playlists.pls)

	field := tview.NewInputField().SetLabel("Filter: ").SetFieldWidth(40)
	field.SetBorder(true).SetTitle(" Add \"" + song.DisplayName() + "\" to playlist ")

	list := tview.NewList()
	list.ShowSecondaryText(false)
	list.SetHighlightFullLine(true)
	list.SetSelectedTextColor(colorSelectedFg)
	list.SetSelectedBackgroundColor(colorSelectedBg)
	list.SetBorder(true)

	hints := &playlistPickerHints{}

	syncHighlight := func() {
		if hints.highlight >= 0 {
			list.SetCurrentItem(hints.highlight)
		}
	}

	renderList := func() {
		hints.rebuild(field.GetText(), names)
		list.Clear()
		for _, idx := range hints.order {
			list.AddItem(hints.names[idx], "", 0, nil)
		}
		syncHighlight()
		switch {
		case len(names) == 0:
			list.SetTitle(" no playlists -- save the queue as one first (S) ")
		case hints.total == 0:
			list.SetTitle(" no matching playlist ")
		default:
			list.SetTitle(fmt.Sprintf(" %d playlist(s) ", hints.total))
		}
	}
	field.SetChangedFunc(func(string) { renderList() })

	confirm := func() {
		name, idx := hints.current()
		if idx < 0 {
			return
		}
		a.closeOverlay()
		if err := a.client.AddTrackToPlaylist(name, song.File); err != nil {
			if errors.Is(err, mpdclient.ErrTrackAlreadyInPlaylist) {
				a.flash("[red]already in playlist \"" + name + "\": " + song.DisplayName() + "[-]")
				return
			}
			a.showError(err)
			return
		}
		a.showMessage("added to playlist \"" + name + "\": " + song.DisplayName())
	}

	focusList := func() { a.tv.SetFocus(list) }
	focusField := func() { a.tv.SetFocus(field) }

	field.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		switch event.Key() {
		case tcell.KeyDown, tcell.KeyCtrlN:
			hints.move(1)
			syncHighlight()
			return nil
		case tcell.KeyUp, tcell.KeyCtrlP:
			hints.move(-1)
			syncHighlight()
			return nil
		case tcell.KeyTab, tcell.KeyBacktab:
			focusList()
			return nil
		case tcell.KeyEnter:
			confirm()
			return nil
		}
		return event
	})

	list.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		switch event.Key() {
		case tcell.KeyDown, tcell.KeyCtrlN:
			hints.move(1)
			syncHighlight()
			return nil
		case tcell.KeyUp, tcell.KeyCtrlP:
			hints.move(-1)
			syncHighlight()
			return nil
		case tcell.KeyTab, tcell.KeyBacktab:
			focusField()
			return nil
		case tcell.KeyEnter:
			confirm()
			return nil
		case tcell.KeyRune:
			switch event.Rune() {
			case 'j':
				hints.move(1)
				syncHighlight()
				return nil
			case 'k':
				hints.move(-1)
				syncHighlight()
				return nil
			case 'g':
				hints.jumpFirst()
				syncHighlight()
				return nil
			case 'G':
				hints.jumpLast()
				syncHighlight()
				return nil
			case 'f':
				focusField()
				return nil
			}
		}
		return event
	})

	renderList()
	layout := tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(field, 3, 0, true).
		AddItem(list, 0, 1, false)

	a.showOverlay("add-to-playlist", centered(layout, 60, 3+maxAddToPlaylistHints+2), field)
}
