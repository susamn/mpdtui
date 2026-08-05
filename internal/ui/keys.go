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
			a.handleQueueMove(1)
			return nil
		case 'K':
			a.handleQueueMove(-1)
			return nil
		case 'S':
			a.handleSavePlaylist()
			return nil
		case 'j', 'k', 'g', 'G':
			return translateVimMotion(event, a.tv.GetFocus())
		case 'h', 'l':
			// Table (Queue) handles these natively for horizontal
			// scroll; harmless no-op on List (Library/Playlists).
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
		if a.tv.GetFocus() == a.library.list {
			a.library.back()
			return nil
		}
	case tcell.KeyEscape:
		if a.tv.GetFocus() == a.playlists.list && a.playlists.filter != "" {
			a.playlists.setFilter("")
			a.updateHintBar()
			return nil
		}
		if a.tv.GetFocus() == a.library.list && a.library.level == libSearch {
			a.library.back()
			a.updateHintBar()
			return nil
		}
	}

	return event
}

// translateVimMotion maps j/k/g/G to Down/Up/Home/End for *tview.List,
// which (unlike Table) has no built-in vim motions. Table already
// handles these natively, so other primitives pass the event through
// unchanged.
func translateVimMotion(event *tcell.EventKey, focus tview.Primitive) *tcell.EventKey {
	if _, ok := focus.(*tview.List); !ok {
		return event
	}
	switch event.Rune() {
	case 'j':
		return tcell.NewEventKey(tcell.KeyDown, 0, tcell.ModNone)
	case 'k':
		return tcell.NewEventKey(tcell.KeyUp, 0, tcell.ModNone)
	case 'g':
		return tcell.NewEventKey(tcell.KeyHome, 0, tcell.ModNone)
	case 'G':
		return tcell.NewEventKey(tcell.KeyEnd, 0, tcell.ModNone)
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

func (a *App) handleAdd() {
	switch a.tv.GetFocus() {
	case a.library.list:
		songs, err := a.library.selectedForAdd()
		if err != nil {
			a.showError(err)
			return
		}
		a.queueAddSongs(songs)
	case a.playlists.list:
		name := a.playlists.selectedName()
		if name == "" {
			return
		}
		if err := a.client.PlaylistAppend(name); err != nil {
			a.showError(err)
			return
		}
		a.queue.refresh()
		a.showMessage("appended playlist " + name)
	default:
		a.invalidKey("a")
	}
}

func (a *App) handleDelete() {
	switch a.tv.GetFocus() {
	case a.playlists.list:
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

func (a *App) handleQueueMove(delta int) {
	if a.tv.GetFocus() != a.queue.table {
		if delta > 0 {
			a.invalidKey("J")
		} else {
			a.invalidKey("K")
		}
		return
	}
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
	a.queue.table.Select(newPos, 0)
}
