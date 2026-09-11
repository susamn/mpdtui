package ui

import (
	"fmt"
	"strings"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"mpdtui/internal/metadata"
	"mpdtui/internal/mpdclient"
)

// Modes for the bookmark picker sub-views.
const (
	bmModeList = iota
	bmModeAdd
	bmModeEdit
	bmModeConfirmDelete
)

const (
	bookmarkListHints    = " [green::b]Enter[-:-:-] Jump  [green::b]a[-:-:-] Add  [green::b]e[-:-:-] Edit  [green::b]d[-:-:-] Delete  [green::b]Esc/B[-:-:-] Close"
	bookmarkInputHints   = " [green::b]Enter/Ctrl+S[-:-:-] Save  [green::b]Alt+Enter[-:-:-] New line  [green::b]Esc[-:-:-] Cancel"
	bookmarkConfirmHints = " [green::b]y[-:-:-] Yes  [green::b]n/Esc[-:-:-] No"
)

const bookmarkPreRoll = 2 * time.Second

func formatBookmarkTime(sec float64) string {
	if sec < 0 {
		sec = 0
	}
	return FormatDuration(time.Duration(sec * float64(time.Second)))
}

// bookmarkPicker manages bookmarks for a track ('B').
type bookmarkPicker struct {
	*tview.Flex
	app   *App
	pages *tview.Pages

	table       *tview.Table
	input       *tview.TextArea
	confirmView *tview.TextView
	hintBar     *tview.TextView

	mode             int
	song             mpdclient.Song
	bookmarks        []metadata.Bookmark
	selectedBookmark metadata.Bookmark
	addPosition      float64
}

func newBookmarkPicker(app *App) *bookmarkPicker {
	p := &bookmarkPicker{app: app, mode: bmModeList}

	p.table = tview.NewTable()
	p.table.SetBorder(true)
	p.table.SetSelectable(true, false)
	p.table.SetSelectedStyle(tcell.StyleDefault.Background(colorSelectedBg).Foreground(colorSelectedFg))
	p.table.SetSelectedFunc(func(row, _ int) {
		if row >= 0 && row < len(p.bookmarks) {
			p.jumpTo(p.bookmarks[row])
		}
	})

	p.input = tview.NewTextArea()
	p.input.SetBorder(true)
	p.input.SetWordWrap(true)
	p.input.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		if event.Key() == tcell.KeyEscape {
			p.cancelInput()
			return nil
		}
		if event.Key() == tcell.KeyEnter {
			if event.Modifiers()&tcell.ModAlt != 0 {
				return tcell.NewEventKey(tcell.KeyEnter, '\n', tcell.ModNone)
			}
			p.submitInput()
			return nil
		}
		if event.Key() == tcell.KeyCtrlS {
			p.submitInput()
			return nil
		}
		return event
	})

	p.confirmView = tview.NewTextView().SetDynamicColors(true).SetTextAlign(tview.AlignCenter)
	p.confirmView.SetBorder(true).SetTitle(" Confirm delete ")

	p.hintBar = tview.NewTextView().SetDynamicColors(true)

	p.pages = tview.NewPages().
		AddPage("table", p.table, true, true).
		AddPage("input", centered(p.input, 66, 8), true, false).
		AddPage("confirm", centered(p.confirmView, 50, 5), true, false)

	p.Flex = tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(p.pages, 0, 1, true).
		AddItem(p.hintBar, 1, 0, false)

	return p
}

// render prepares the picker for song and populates its bookmarks.
func (p *bookmarkPicker) render(song mpdclient.Song) {
	p.song = song
	p.showTable()
	if p.app.metaDB != nil {
		bms, err := p.app.metaDB.BookmarksForTrack(song.File)
		if err != nil {
			p.app.showError(err)
			return
		}
		p.bookmarks = bms
	} else {
		p.bookmarks = nil
	}
	p.refreshTable()
	if len(p.bookmarks) > 0 {
		p.table.Select(0, 0)
	}
}

func (p *bookmarkPicker) updateTitle() {
	name := p.song.DisplayName()
	if name == "" {
		name = baseName(p.song.File)
	}
	p.table.SetTitle(fmt.Sprintf(" Bookmarks: %q ", name))
}

func (p *bookmarkPicker) showTable() {
	p.mode = bmModeList
	p.pages.SwitchToPage("table")
	p.updateTitle()
	p.hintBar.SetText(bookmarkListHints)
	p.app.tv.SetFocus(p.table)
}

func (p *bookmarkPicker) refreshTable() {
	p.table.Clear()
	if len(p.bookmarks) == 0 {
		p.table.SetCell(0, 0, tview.NewTableCell("(no bookmarks -- press 'a' to add)").
			SetTextColor(tcell.ColorDarkGray).SetSelectable(false))
		return
	}
	for i, bm := range p.bookmarks {
		timeStr := fmt.Sprintf("[%s]", formatBookmarkTime(bm.PositionSeconds))
		p.table.SetCell(i, 0, tview.NewTableCell(timeStr).SetTextColor(tcell.ColorYellow))
		p.table.SetCell(i, 1, tview.NewTableCell(tview.Escape(bm.Text)).SetExpansion(1))
	}
	row, _ := p.table.GetSelection()
	if row >= len(p.bookmarks) {
		p.table.Select(max(0, len(p.bookmarks)-1), 0)
	}
}

func (p *bookmarkPicker) jumpTo(bm metadata.Bookmark) {
	d := time.Duration(bm.PositionSeconds * float64(time.Second))
	if p.song.ID >= 0 && p.app.client != nil {
		if err := p.app.client.SeekSongID(p.song.ID, d); err != nil {
			p.app.showError(err)
			return
		}
	} else if p.app.client != nil {
		if err := p.app.client.SeekCur(d, false); err != nil {
			p.app.showError(err)
			return
		}
	}
	p.app.closeOverlay()
	p.app.showMessage(fmt.Sprintf("jumped to [%s]: %s", formatBookmarkTime(bm.PositionSeconds), bm.Text))
}

func (p *bookmarkPicker) startAdd() {
	p.mode = bmModeAdd
	pos := 0.0
	if p.app.isPlaying() && p.app.currentSong.File == p.song.File {
		d := p.app.currentStatus.Elapsed - bookmarkPreRoll
		if d < 0 {
			d = 0
		}
		pos = d.Seconds()
	}
	p.addPosition = pos
	p.input.SetTitle(fmt.Sprintf(" Add Bookmark at %s (Enter/Ctrl+S to save, Esc to cancel) ", formatBookmarkTime(pos)))
	p.input.SetText("", true)
	p.pages.SwitchToPage("input")
	p.hintBar.SetText(bookmarkInputHints)
	p.app.tv.SetFocus(p.input)
}

func (p *bookmarkPicker) startEdit() {
	row, _ := p.table.GetSelection()
	if row < 0 || row >= len(p.bookmarks) {
		return
	}
	bm := p.bookmarks[row]
	p.selectedBookmark = bm
	p.mode = bmModeEdit
	p.input.SetTitle(fmt.Sprintf(" Edit Bookmark at %s (Enter/Ctrl+S to save, Esc to cancel) ", formatBookmarkTime(bm.PositionSeconds)))
	p.input.SetText(bm.Text, true)
	p.pages.SwitchToPage("input")
	p.hintBar.SetText(bookmarkInputHints)
	p.app.tv.SetFocus(p.input)
}

func (p *bookmarkPicker) startDelete() {
	row, _ := p.table.GetSelection()
	if row < 0 || row >= len(p.bookmarks) {
		return
	}
	bm := p.bookmarks[row]
	p.selectedBookmark = bm
	p.mode = bmModeConfirmDelete
	p.confirmView.SetText(fmt.Sprintf("Delete bookmark [%s] %q?\n\n[green::b]y[-:-:-]es   [red::b]n[-:-:-]o",
		formatBookmarkTime(bm.PositionSeconds), bm.Text))
	p.pages.SwitchToPage("confirm")
	p.hintBar.SetText(bookmarkConfirmHints)
	p.app.tv.SetFocus(p.confirmView)
}

func (p *bookmarkPicker) submitInput() {
	text := strings.TrimSpace(p.input.GetText())
	if text == "" {
		p.cancelInput()
		return
	}
	if p.mode == bmModeAdd {
		pos := p.addPosition
		song := p.song
		p.app.runAsync(func() error {
			_, err := p.app.metaDB.CreateBookmark(song.File, pos, text)
			return err
		}, func() {
			p.reload(song.File)
			p.showTable()
			p.app.showMessage(fmt.Sprintf("added bookmark [%s]: %s", formatBookmarkTime(pos), text))
		})
	} else if p.mode == bmModeEdit {
		bm := p.selectedBookmark
		song := p.song
		p.app.runAsync(func() error {
			return p.app.metaDB.UpdateBookmark(bm.ID, text)
		}, func() {
			p.reload(song.File)
			p.showTable()
			p.app.showMessage(fmt.Sprintf("updated bookmark [%s]: %s", formatBookmarkTime(bm.PositionSeconds), text))
		})
	}
}

func (p *bookmarkPicker) cancelInput() {
	p.showTable()
}

func (p *bookmarkPicker) confirmDeleteNow() {
	bm := p.selectedBookmark
	song := p.song
	p.showTable()
	p.app.runAsync(func() error {
		return p.app.metaDB.DeleteBookmark(bm.ID)
	}, func() {
		p.reload(song.File)
		p.app.showMessage(fmt.Sprintf("deleted bookmark [%s]: %s", formatBookmarkTime(bm.PositionSeconds), bm.Text))
	})
}

func (p *bookmarkPicker) cancelDelete() {
	p.showTable()
}

func (p *bookmarkPicker) reload(file string) {
	p.app.reloadTrackMeta(file)
	if p.app.metaDB != nil {
		bookmarks, err := p.app.metaDB.BookmarksForTrack(file)
		if err != nil {
			p.app.showError(err)
			return
		}
		p.bookmarks = bookmarks
	}
	p.refreshTable()
}

func (p *bookmarkPicker) handleKey(event *tcell.EventKey) bool {
	switch p.mode {
	case bmModeConfirmDelete:
		if event.Key() == tcell.KeyRune {
			switch event.Rune() {
			case 'y', 'Y':
				p.confirmDeleteNow()
				return true
			case 'n', 'N':
				p.cancelDelete()
				return true
			}
		}
		if event.Key() == tcell.KeyEscape {
			p.cancelDelete()
			return true
		}
		return true
	case bmModeAdd, bmModeEdit:
		if event.Key() == tcell.KeyEscape {
			p.cancelInput()
			return true
		}
		if event.Key() == tcell.KeyEnter {
			if event.Modifiers()&tcell.ModAlt != 0 {
				return false
			}
			p.submitInput()
			return true
		}
		if event.Key() == tcell.KeyCtrlS {
			p.submitInput()
			return true
		}
		return false
	default: // bmModeList
		if event.Key() == tcell.KeyRune {
			switch event.Rune() {
			case 'a':
				p.startAdd()
				return true
			case 'e':
				p.startEdit()
				return true
			case 'd':
				p.startDelete()
				return true
			case 'B':
				p.app.closeOverlay()
				return true
			}
		}
		return false
	}
}

func (p *bookmarkPicker) focused() bool {
	focus := p.app.tv.GetFocus()
	return focus == p.table || focus == p.input || focus == p.confirmView || focus == p.pages
}

func (p *bookmarkPicker) allowsGlobalKeys() bool {
	return p.mode == bmModeList
}

// handleBookmarkTrack is 'b': creates a bookmark at the current playback
// position (with a 2-second negative pre-roll offset) while playing.
func (a *App) handleBookmarkTrack() {
	if a.metaDB == nil {
		a.metadataNotEnabled()
		return
	}
	if !a.isPlaying() {
		a.flash("[yellow]nothing playing to bookmark[-]")
		return
	}
	song := a.currentSong
	pos := a.currentStatus.Elapsed - bookmarkPreRoll
	if pos < 0 {
		pos = 0
	}
	title := fmt.Sprintf(" What do you want to remember here? [%s] (Enter to save, Esc to cancel) ", formatBookmarkTime(pos.Seconds()))
	a.openTextBox(title, "", func(text string) {
		a.runAsync(func() error {
			_, err := a.metaDB.CreateBookmark(song.File, pos.Seconds(), text)
			return err
		}, func() {
			a.reloadTrackMeta(song.File)
			a.showMessage(fmt.Sprintf("bookmarked [%s]: %s", formatBookmarkTime(pos.Seconds()), text))
		})
	})
}

// handleOpenBookmarkManager is 'B': opens the bookmark manager overlay for
// the currently playing or selected track.
func (a *App) handleOpenBookmarkManager() {
	if a.metaDB == nil {
		a.metadataNotEnabled()
		return
	}
	song, ok := a.targetSong()
	if !ok {
		a.flash("[yellow]no track selected for bookmarks[-]")
		return
	}
	a.openBookmarkManager(song)
}
