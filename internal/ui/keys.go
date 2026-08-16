package ui

import (
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

// globalInputCapture is the single place all keybindings are decided.
// It runs before any focused primitive sees the event (tview semantics),
// so panel-local keys (a, d, D, J, K, S) are dispatched here too, based
// on whichever panel currently has focus, rather than being scattered
// across per-panel SetInputCapture handlers.
func (a *App) globalInputCapture(event *tcell.EventKey) *tcell.EventKey {
	if a.mode == modeOverlay {
		if event.Key() == tcell.KeyEscape {
			a.closeOverlay()
			return nil
		}
		// The Settings overlay ('e') spans two tabs with several of its
		// own focusable widgets (two read-only tables, an input field, a
		// confirm prompt), unlike every other overlay here which assumes
		// one fixed primitive -- routed separately rather than folded
		// into noTextInputOverlays below. handleKey only claims
		// Tab/Backtab/Left/Right/'a'/'d'/'y'/'n'; anything else (typing,
		// Enter, Backspace) falls through to whatever's actually focused.
		if a.settings.focused() {
			if a.settings.handleKey(event) {
				return nil
			}
			// 'q' and the transport cluster stay live while Settings is
			// open, same as trackInfoCard/lyricsViewer/markPicker below --
			// except while addInput is actually accepting typed text
			// (allowsGlobalKeys is false there), since a mark reason or
			// tag like "single" needs 'q'/'s'/etc. to stay literal.
			if event.Key() == tcell.KeyRune && a.settings.allowsGlobalKeys() {
				if event.Rune() == 'q' {
					a.tv.Stop()
					return nil
				}
				if a.handleTransportKey(event.Rune()) {
					return nil
				}
			}
			return event
		}
		// noTextInputOverlays: none of these three have anything to type
		// (a track-info/lyrics display, or a plain selection list), so
		// 'q' can safely still quit while any of them is open -- unlike
		// the search/filter/save-playlist inputs, where 'q' has to stay
		// literal text. The first two also double their own opening key
		// as a close shortcut, mirroring Esc (closeKey == 0 for
		// markPicker: there's no natural "same key" for a selection list,
		// only Esc/Enter make sense there).
		if event.Key() == tcell.KeyRune {
			noTextInputOverlays := []struct {
				closeKey  rune
				primitive tview.Primitive
			}{
				{'i', a.trackInfo},
				{'y', a.lyricsViewer},
				{0, a.markPicker},
			}
			for _, ov := range noTextInputOverlays {
				if a.tv.GetFocus() != ov.primitive {
					continue
				}
				if ov.closeKey != 0 && event.Rune() == ov.closeKey {
					a.closeOverlay()
					return nil
				}
				if event.Rune() == 'q' {
					a.tv.Stop()
					return nil
				}
			}
			// Transport controls (play/pause, stop, skip, seek, volume,
			// shuffle, repeat, consume, single) stay live while the lyrics
			// viewer or the mark picker specifically is open -- unlike
			// every other overlay (including trackInfoCard, which doesn't
			// get this), both are meant to be usable *while music plays*,
			// so pausing or skipping shouldn't require closing them first.
			// None of these touch focus or open another overlay (unlike
			// e.g. '?'/help or 'i'/track info, deliberately left out
			// here), which is what would actually conflict with an
			// overlay already being open -- see maybeJumpToCurrentTrack's
			// own doc comment on why stealing focus out from under an open
			// overlay is the specific thing to avoid. A
			// search/filter/save-playlist input still needs 's'/Space to
			// stay literal typed text, so this stays scoped to just these
			// two rather than a blanket rule.
			focus := a.tv.GetFocus()
			if (focus == a.lyricsViewer || focus == a.markPicker) && a.handleTransportKey(event.Rune()) {
				return nil
			}
		}
		return event
	}

	if event.Key() == tcell.KeyRune {
		if a.handleTransportKey(event.Rune()) {
			return nil
		}
		switch event.Rune() {
		case '/':
			a.openSearch()
			return nil
		case 'f':
			a.openGlobalSearch()
			return nil
		case 'F':
			a.clearAllSearches()
			return nil
		case 'i':
			a.openTrackInfo()
			return nil
		case 'y':
			a.openLyricsViewer()
			return nil
		case 'v':
			a.visualizer.next()
			return nil
		case 'o':
			a.handleCycleSort()
			return nil
		case 'L':
			a.jumpToCurrentTrack()
			return nil
		case 'm':
			a.handleOpenMarkPicker()
			return nil
		case 'e':
			a.openSettings()
			return nil
		case '?':
			a.openHelp()
			return nil
		case 'q':
			a.tv.Stop()
			return nil
		case '1', '2', '3', '4', '5':
			// While the Queue panel is focused, 1-5 rate the selected
			// track instead of jumping panel focus -- an explicit tradeoff
			// (Tab/Backtab still cycle panels regardless of focus, so
			// nothing is unreachable, just not on this specific shortcut
			// from inside Queue).
			if a.tv.GetFocus() == a.queue.table {
				a.handleRateSelectedTrack(int(event.Rune() - '0'))
				return nil
			}
			switch event.Rune() {
			case '1':
				a.focusPanel(0)
			case '2':
				a.focusPanel(1)
			case '3':
				a.focusPanel(2)
			default:
				a.invalidKey(string(event.Rune()))
			}
			return nil
		case 'a':
			a.handleAdd()
			return nil
		case 'd':
			a.handleDelete()
			return nil
		case 'D':
			a.handleClearQueue()
			return nil
		case 'J':
			if a.tv.GetFocus() == a.queue.table {
				a.handleQueueMove(1)
				return nil
			}
			// Not on Queue: let the focused primitive handle it natively
			// (e.g. the Library tree's own "jump to child" motion).
			return event
		case 'K':
			if a.tv.GetFocus() == a.queue.table {
				a.handleQueueMove(-1)
				return nil
			}
			return event
		case 'S':
			a.handleSavePlaylist()
			return nil
		case 'R':
			a.handleRefreshPlaylistCounts()
			return nil
		case 'j', 'k', 'g', 'G', 'h', 'l':
			// Table (Queue, Playlists) and TreeView (Library) all handle
			// these natively -- nothing in this app needs vim-motion
			// translation, so these just pass through unchanged.
			return event
		default:
			a.invalidKey(string(event.Rune()))
			return nil
		}
	}

	switch event.Key() {
	case tcell.KeyTab:
		a.cycleFocus(1)
		return nil
	case tcell.KeyBacktab:
		a.cycleFocus(-1)
		return nil
	case tcell.KeyBackspace, tcell.KeyBackspace2:
		if a.tv.GetFocus() == a.library.tree {
			a.library.back()
			return nil
		}
	case tcell.KeyEscape:
		if a.tv.GetFocus() == a.library.tree && a.library.mode == libSearch {
			a.library.back()
			a.updateHintBar()
			return nil
		}
		if a.tv.GetFocus() == a.playlists.table && a.playlists.filter != "" {
			a.playlists.setFilter("")
			a.updateHintBar()
			return nil
		}
	}

	return event
}

// invalidKey flashes feedback for a keypress that has no meaning in the
// current context (an unbound key, or a panel-local key pressed while
// the wrong panel is focused), so it's visible rather than a silent
// no-op.
func (a *App) invalidKey(key string) {
	a.flash("[red]'" + key + "' has no action here[-]")
}

// handleTransportKey handles the playback-transport cluster of global
// keys -- play/pause, stop, skip, seek, volume, shuffle, repeat, consume,
// single -- and reports whether r was one of them. Split out from
// globalInputCapture's normal-mode switch so the exact same handling can
// also run while the lyrics viewer overlay is open (see
// globalInputCapture's modeOverlay branch): unlike every other global key
// (panel focus, search, other overlays), none of these touch focus or
// open another overlay, which is what actually makes them safe to keep
// live under an already-open overlay.
func (a *App) handleTransportKey(r rune) bool {
	switch r {
	case ' ':
		a.togglePlayPause()
	case 's':
		a.stop()
	case 'n':
		a.next()
	case 'p':
		a.previous()
	case ',':
		a.seek(-5 * time.Second)
	case '.':
		a.seek(5 * time.Second)
	case '-':
		a.changeVolume(-5)
	case '=':
		// Same physical key as '+' on a US layout, without needing shift --
		// matches '-' also needing no modifier.
		a.changeVolume(5)
	case 'z':
		a.toggleRandom()
	case 'x':
		a.toggleRepeat()
	case 'c':
		a.toggleConsume()
	case 'Z':
		a.toggleSingle()
	default:
		return false
	}
	return true
}

func (a *App) togglePlayPause() {
	if err := a.client.TogglePlayPause(); err != nil {
		a.showError(err)
	}
}

func (a *App) stop() {
	if err := a.client.Stop(); err != nil {
		a.showError(err)
	}
}

func (a *App) next() {
	if err := a.client.Next(); err != nil {
		a.showError(err)
	}
}

func (a *App) previous() {
	if err := a.client.Previous(); err != nil {
		a.showError(err)
	}
}

func (a *App) seek(d time.Duration) {
	if err := a.client.SeekCur(d, true); err != nil {
		a.showError(err)
	}
}

func (a *App) changeVolume(delta int) {
	if err := a.client.ChangeVolume(delta); err != nil {
		a.showError(err)
	}
}

func (a *App) toggleRandom() {
	st, err := a.client.Status()
	if err != nil {
		a.showError(err)
		return
	}
	if err := a.client.SetRandom(!st.Random); err != nil {
		a.showError(err)
	}
}

func (a *App) toggleRepeat() {
	st, err := a.client.Status()
	if err != nil {
		a.showError(err)
		return
	}
	if err := a.client.SetRepeat(!st.Repeat); err != nil {
		a.showError(err)
	}
}

func (a *App) toggleSingle() {
	st, err := a.client.Status()
	if err != nil {
		a.showError(err)
		return
	}
	if err := a.client.SetSingle(!st.Single); err != nil {
		a.showError(err)
	}
}

func (a *App) toggleConsume() {
	st, err := a.client.Status()
	if err != nil {
		a.showError(err)
		return
	}
	if err := a.client.SetConsume(!st.Consume); err != nil {
		a.showError(err)
	}
}

// handleCycleSort is 'o': cycles the focused panel's sort mode (name vs.
// recency). Library only honors this in browse mode -- search results
// have their own fixed ordering, so cycling sort there would silently do
// nothing useful, hence the explicit invalidKey instead of a no-op.
func (a *App) handleCycleSort() {
	switch a.tv.GetFocus() {
	case a.library.tree:
		if a.library.mode != libBrowse {
			a.invalidKey("o")
			return
		}
		a.library.cycleSortMode()
	case a.playlists.table:
		a.playlists.cycleSortMode()
	default:
		a.invalidKey("o")
	}
}

// jumpToCurrentTrack is 'L': selects the currently playing track in the
// Queue panel and moves focus there, from any panel -- and also reveals
// that track's location in the Library tree (expanding every directory
// along its path and selecting it there), without moving focus away from
// Queue. Flashes a message instead of silently doing nothing when there's
// no current track (queue empty, or nothing playing/selected).
func (a *App) jumpToCurrentTrack() {
	if !a.queue.jumpToCurrent() {
		a.showMessage("nothing playing")
		return
	}
	if song, ok := a.queue.selectedSong(); ok {
		a.library.revealInLibrary(song.File)
	}
	a.focusPanelPrimitive(a.queue.table)
}

func (a *App) handleAdd() {
	switch a.tv.GetFocus() {
	case a.library.tree:
		a.library.addSelected()
	case a.playlists.table:
		name := a.playlists.selectedName()
		if name == "" {
			return
		}
		a.appendPlaylist(name)
	case a.queue.table:
		a.openAddToPlaylistPicker()
	default:
		a.invalidKey("a")
	}
}

func (a *App) handleDelete() {
	switch a.tv.GetFocus() {
	case a.playlists.table:
		name := a.playlists.selectedName()
		if name == "" {
			return
		}
		a.confirm("Delete playlist \""+name+"\"?", func() {
			if err := a.client.PlaylistDelete(name); err != nil {
				a.showError(err)
				return
			}
			a.playlists.refresh()
			a.showMessage("deleted playlist " + name)
		})
	case a.queue.table:
		song, ok := a.queue.selectedSong()
		if !ok {
			return
		}
		if err := a.client.QueueRemoveID(song.ID); err != nil {
			a.showError(err)
			return
		}
		a.queue.refresh()
	default:
		a.invalidKey("d")
	}
}

// handleClearQueue is global (like the other transport controls) rather
// than gated to the Queue panel: it doesn't depend on a selection, and
// gating it made the binding look like it didn't exist when pressed
// from any other panel.
func (a *App) handleClearQueue() {
	a.confirm("Clear the entire queue?", func() {
		if err := a.client.QueueClear(); err != nil {
			a.showError(err)
			return
		}
		a.queue.refresh()
	})
}

// handleQueueMove is only ever called with the Queue table focused --
// globalInputCapture gates J/K on that before dispatching here.
func (a *App) handleQueueMove(delta int) {
	song, ok := a.queue.selectedSong()
	if !ok {
		return
	}
	newPos := song.Pos + delta
	if newPos < 0 || newPos >= len(a.queue.songs) {
		return
	}
	if err := a.client.QueueMoveID(song.ID, newPos); err != nil {
		a.showError(err)
		return
	}
	a.queue.refresh()
	a.queue.table.Select(newPos+queueHeaderRows, 0)
}
