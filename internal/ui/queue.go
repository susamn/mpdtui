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

// formatColors maps a track format to a single foreground color for its
// tag. Deliberately no background fill: a solid-filled cell repeated down
// a column of same-format rows (e.g. a run of consecutive MP3 tracks) has
// no vertical gap between rows in a table, so it reads as one continuous
// colored bar rather than individual tags. Colored text alone avoids
// that, and sidesteps needing a contrasting text color per background
// (which produced unreadable pairings, e.g. white-on-SteelBlue in some
// terminal themes). Note: cell text can't use "[MP3]"-style brackets --
// tview's Table has no way to disable its dynamic-color tag parsing
// (unlike TextView's SetDynamicColors), so "[...]" is always parsed as a
// style/region tag, not literal brackets, and silently disappears.
//
// Colors are deliberately from tcell's basic 16-color ANSI set (Black,
// Maroon, Green, Olive, Navy, Purple, Teal, Silver, Gray, Red, Lime,
// Yellow, Blue, Fuchsia, Aqua, White) rather than tcell's extended
// "DodgerBlue"/"FireBrick"/"MediumOrchid"-style named colors, which are
// true 24-bit RGB values (tcell.ColorIsRGB) requiring the terminal to
// negotiate truecolor support. The basic 16 need no such negotiation and
// are the same family already proven to work elsewhere in this app
// (colorSelectedBg/Fg, colorActiveBorder) -- the safer default absent a
// specific reason to need a wider palette.
var formatColors = map[string]tcell.Color{
	"FLAC": tcell.ColorGreen,
	"WAV":  tcell.ColorTeal,
	"MP3":  tcell.ColorAqua,
	"M4A":  tcell.ColorFuchsia,
	"AAC":  tcell.ColorFuchsia,
	"OGG":  tcell.ColorYellow,
	"OPUS": tcell.ColorYellow,
	"WMA":  tcell.ColorRed,
}

const defaultFormatColor = tcell.ColorGray

// formatGap is trailing space after the tag, separating it from the
// duration column next to it.
const formatGap = "   "

// formatTagCell renders file's format (MP3/FLAC/WMA/...) as small colored
// text, right-aligned so it sits consistently before the duration column,
// with a gap after it rather than touching that column directly.
func formatTagCell(file string) *tview.TableCell {
	format := TrackFormat(file)
	if format == "" {
		return tview.NewTableCell("").SetAlign(tview.AlignRight)
	}
	color, ok := formatColors[format]
	if !ok {
		color = defaultFormatColor
	}
	return tview.NewTableCell(format + formatGap).
		SetTextColor(color).
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
