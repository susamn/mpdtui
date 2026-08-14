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
		// readOnlyOverlayToggles: for these two, the key that opened the
		// overlay also closes it again while it's focused, mirroring Esc
		// -- every other overlay only closes via Esc, but these two are
		// pure read-only displays (nothing to type), so doubling their own
		// key as a close shortcut is unambiguous. 'q' also still quits
		// while either is open, for the same read-only-so-nothing-to-
		// swallow reason (unlike the search/filter/save-playlist inputs,
		// where 'q' has to stay literal text).
		if event.Key() == tcell.KeyRune {
			readOnlyOverlayToggles := []struct {
				key       rune
				primitive tview.Primitive
			}{
				{'i', a.trackInfo},
				{'y', a.lyricsViewer},
			}
			for _, ov := range readOnlyOverlayToggles {
				if a.tv.GetFocus() != ov.primitive {
					continue
				}
				if event.Rune() == ov.key {
					a.closeOverlay()
					return nil
				}
				if event.Rune() == 'q' {
					a.tv.Stop()
					return nil
				}
			}
		}
		return event
	}

	if event.Key() == tcell.KeyRune {
		switch event.Rune() {
		case ' ':
			a.togglePlayPause()
			return nil
		case 's':
			a.stop()
			return nil
		case 'n':
			a.next()
			return nil
		case 'p':
			a.previous()
			return nil
		case ',':
			a.seek(-5 * time.Second)
			return nil
		case '.':
			a.seek(5 * time.Second)
			return nil
		case '-':
			a.changeVolume(-5)
			return nil
		case '=':
			// Same physical key as '+' on a US layout, without needing
			// shift -- matches '-' also needing no modifier.
			a.changeVolume(5)
			return nil
		case 'z':
			a.toggleRandom()
			return nil
		case 'x':
			a.toggleRepeat()
			return nil
		case 'c':
			a.toggleConsume()
			return nil
		case 'Z':
			a.toggleSingle()
			return nil
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
		case '?':
			a.openHelp()
			return nil
		case 'q':
			a.tv.Stop()
			return nil
		case '1':
			a.focusPanel(0)
			return nil
		case '2':
			a.focusPanel(1)
			return nil
		case '3':
			a.focusPanel(2)
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
