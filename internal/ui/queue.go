package ui

import (
	"fmt"
	"strings"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"mpdtui/internal/mpdclient"
)

// queuePanel shows the current playback queue as a table, with the
// playing track marked, plus a persistent search field pinned below it
// (see search, wired up in app.go's openQueueSearch/closeOverlay).
type queuePanel struct {
	app    *App
	table  *tview.Table
	search *tview.InputField
	songs  []mpdclient.Song
}

func newQueuePanel(app *App) *queuePanel {
	q := &queuePanel{app: app}

	t := tview.NewTable()
	t.SetBorder(true).SetTitle(" Queue ")
	t.SetSelectable(true, false)
	t.SetSelectedStyle(tcell.StyleDefault.Background(colorSelectedBg).Foreground(colorSelectedFg))
	t.SetSelectedFunc(func(row, _ int) {
		if row < 0 || row >= len(q.songs) {
			return
		}
		song := q.songs[row]
		if err := q.app.client.PlayID(song.ID); err != nil {
			q.app.showError(err)
			return
		}
		q.app.refreshNowPlaying()
	})
	q.table = t

	search := tview.NewInputField().SetLabel("Search track: ")
	search.SetBorder(true)
	search.SetDoneFunc(func(key tcell.Key) {
		text := strings.TrimSpace(search.GetText())
		q.app.closeOverlay()
		if key == tcell.KeyEnter && text != "" {
			if !q.jumpToMatch(text) {
				q.app.showMessage("no match for " + text)
			}
		}
	})
	q.search = search

	return q
}

func (q *queuePanel) refresh() {
	songs, err := q.app.client.Queue()
	if err != nil {
		q.app.showError(err)
		return
	}
	q.songs = songs

	curID := -1
	if st, err := q.app.client.Status(); err == nil {
		curID = st.SongID
	}
	q.render(curID)
}

func (q *queuePanel) render(curID int) {
	q.table.Clear()
	for i, s := range q.songs {
		marker := "  "
		if s.ID == curID {
			marker = "▶ "
		}
		q.table.SetCell(i, 0, tview.NewTableCell(marker))
		q.table.SetCell(i, 1, tview.NewTableCell(fmt.Sprintf("%3d", i+1)))
		q.table.SetCell(i, 2, tview.NewTableCell(s.DisplayName()).SetExpansion(1))
		q.table.SetCell(i, 3, formatTagCell(s.File))
		q.table.SetCell(i, 4, tview.NewTableCell(FormatDuration(s.Duration)))
	}
	q.table.SetTitle(fmt.Sprintf(" Queue (%d) ", len(q.songs)))
}

// formatStyle pairs a background/foreground for one track format's badge.
// Chosen so each is legible on its own; not trying to avoid every possible
// clash with the row-selection highlight (blue bg/yellow fg), which
// already makes the whole selected row visually distinct regardless.
type formatStyle struct {
	bg, fg tcell.Color
}

var (
	formatStyles = map[string]formatStyle{
		"FLAC": {tcell.ColorGreen, tcell.ColorBlack},
		"WAV":  {tcell.ColorTeal, tcell.ColorWhite},
		"MP3":  {tcell.ColorSteelBlue, tcell.ColorWhite},
		"M4A":  {tcell.ColorDarkMagenta, tcell.ColorWhite},
		"AAC":  {tcell.ColorDarkMagenta, tcell.ColorWhite},
		"OGG":  {tcell.ColorDarkOrange, tcell.ColorBlack},
		"OPUS": {tcell.ColorDarkOrange, tcell.ColorBlack},
		"WMA":  {tcell.ColorFireBrick, tcell.ColorWhite},
	}
	defaultFormatStyle = formatStyle{tcell.ColorGray, tcell.ColorWhite}
)

// formatTagCell renders file's format (MP3/FLAC/WMA/...) as a small
// colored, button-like badge: tight padding, right-aligned so it sits
// flush against the duration column next to it.
func formatTagCell(file string) *tview.TableCell {
	format := TrackFormat(file)
	if format == "" {
		return tview.NewTableCell("").SetAlign(tview.AlignRight)
	}
	style, ok := formatStyles[format]
	if !ok {
		style = defaultFormatStyle
	}
	return tview.NewTableCell(" " + format + " ").
		SetBackgroundColor(style.bg).
		SetTextColor(style.fg).
		SetAlign(tview.AlignRight)
}

// setCurrent repaints just the playing-track marker column, without
// re-fetching the queue from MPD (cheap enough to call on every tick).
func (q *queuePanel) setCurrent(id int) {
	for i, s := range q.songs {
		cell := q.table.GetCell(i, 0)
		if cell == nil {
			continue
		}
		if s.ID == id {
			cell.SetText("▶ ")
		} else {
			cell.SetText("  ")
		}
	}
}

// jumpToMatch selects (but does not remove or hide) the first queued track
// whose display name contains query, case-insensitive. Returns false if
// nothing matched, leaving the current selection untouched.
func (q *queuePanel) jumpToMatch(query string) bool {
	needle := strings.ToLower(query)
	for i, s := range q.songs {
		if strings.Contains(strings.ToLower(s.DisplayName()), needle) {
			q.table.Select(i, 0)
			return true
		}
	}
	return false
}

func (q *queuePanel) selectedSong() (mpdclient.Song, bool) {
	row, _ := q.table.GetSelection()
	if row < 0 || row >= len(q.songs) {
		return mpdclient.Song{}, false
	}
	return q.songs[row], true
}
