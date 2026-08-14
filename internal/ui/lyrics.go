package ui

import (
	"fmt"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"mpdtui/internal/lyrics"
	"mpdtui/internal/mpdclient"
)

// lyricsIcon marks a Queue row whose track has a matching lyrics file
// (see internal/lyrics), shown in its own narrow "Lyr" column
// (queue.go's render) rather than prefixed to the Title cell -- that was
// the first cut, but reads as cluttering Title rather than as a distinct
// badge. The column carries no queueColumnGap padding (unlike every other
// Queue column): its only content is ever this icon or "", so
// tview.Table's own auto-sizing-to-content already makes it exactly as
// wide as the icon and no wider, with no extra code needed to enforce
// that.
const lyricsIcon = "📝"

// lyricsViewer shows the currently playing track's lyrics. It's
// positioned over the Queue table's own Year-through-Type column band
// (just before Duration), not centered on the full screen -- so it reads
// as replacing that slice of the Queue panel rather than floating
// disconnected from it, while still leaving Title/Album/Artist visible to
// its left. Vertical, per the request that named this ("a vertical
// lyrics viewer"): lyrics can run to many lines and need real scrolling
// room, not just a glanceable summary the way trackInfoCard's small
// floating quadrant card is. Scrolling is entirely tview.TextView's own
// native vim-style key handling (j/k line, g/G top/bottom, h/l
// horizontal, Ctrl-F/Ctrl-B page) once SetScrollable(true) is set -- the
// same free vim bindings Library's TreeView and Queue's Table already
// have built in, so no custom key handling is needed here at all.
type lyricsViewer struct {
	*tview.TextView
	app *App
}

// lyricsColor is teal, distinguishing the viewer's border/title from
// every other panel/overlay's own color (green for the focused-panel
// border, yellow for Now Playing's, default elsewhere) -- a deliberate
// visual cue that this is a different kind of thing (floating over part
// of the Queue panel, not a panel or a centered popup itself).
var lyricsColor = tcell.ColorTeal

func newLyricsViewer(app *App) *lyricsViewer {
	v := tview.NewTextView().SetDynamicColors(true)
	v.SetBorder(true).SetTitle(" Lyrics (j/k/g/G to scroll) ")
	v.SetBorderColor(lyricsColor).SetTitleColor(lyricsColor)
	v.SetBorderPadding(1, 1, 2, 2)
	v.SetScrollable(true)
	return &lyricsViewer{TextView: v, app: app}
}

// queueYearCol/queueDurationCol are the Queue table's column indices for
// Year and Duration (see queue.go's queueHeaderLabels: 0 marker, 1
// position, 2 Title, 3 Lyr, 4 Album, 5 Artist, 6 Year, 7 Genre, 8
// Composer, 9 Type, 10 Duration) -- positionOverQueueColumns reads real
// screen positions bracketing that Year-through-Type band from the
// header row's own already-drawn cells.
const (
	queueYearCol     = 6
	queueDurationCol = 10
)

// lyricsViewerBottomMargin is how many rows short of the Queue table's
// own bottom edge the viewer stops -- so it visibly floats within the
// Queue panel (leaving a bit of the table, and its border, showing below
// the viewer) rather than filling every last row down to the panel's own
// border the way it did before this was asked to be "a bit smaller".
const lyricsViewerBottomMargin = 2

// lyricsViewerRect computes the viewer's rect from the Queue table's own
// vertical extent (queueY, queueHeight) and the actual last-drawn x
// positions of its Year and Duration columns (yearX, durationX).
// Horizontally: from yearX up to (not including) durationX. Vertically:
// starting just below the header row (queueY+queueHeaderRows, so the
// Year/Genre/... column headers stay visible above it, not covered) and
// stopping lyricsViewerBottomMargin rows short of the table's own bottom
// edge (queueY+queueHeight). Split out as a pure function -- no
// tview.Table involved -- so the positioning math is testable without a
// real tcell.Screen, mirroring trackInfoCard's own quadrantRect/cardRect
// split from positionOverQueue/Draw. Clamps height to 0 rather than
// negative for a pathologically short Queue table (smaller than the
// header row plus the margin).
func lyricsViewerRect(queueY, queueHeight, yearX, durationX int) (x, y, width, height int) {
	top := queueY + queueHeaderRows
	height = queueHeight - queueHeaderRows - lyricsViewerBottomMargin
	if height < 0 {
		height = 0
	}
	return yearX, top, durationX - yearX, height
}

// positionOverQueueColumns sets the viewer's rect to span horizontally
// from the Queue table's Year column through the end of Type (i.e. up to
// but not including Duration), and vertically to match the Queue table's
// own rect (see lyricsViewerRect). Reads each boundary column's *actual
// last-drawn* x position (tview.TableCell.GetLastPosition()) rather than
// computing an estimate from the column max-length constants (even where
// those exist, e.g. queueTitleMaxLen): those are truncation caps, not a
// column's real rendered width, which tview.Table sizes to fit whatever's
// actually in the queue and shrinks below the cap for shorter content --
// an estimate would drift out of sync with the real layout.
//
// Relies on the Queue table having already been drawn at least once this
// frame -- true in practice from shortly after startup onward (it's
// always visible, part of the "main" page), and specifically guaranteed
// within a single frame here: tview.Pages.Draw (pages.go) draws its
// pages in the order they were added, "main" (holding the Queue table)
// before the "lyrics" overlay page showOverlay adds on top, so by the
// time this runs the header row's cells reflect this exact frame's
// layout, not a stale one.
func (v *lyricsViewer) positionOverQueueColumns() {
	_, qy, _, qh := v.app.queue.table.GetRect()
	yearX, _, _ := v.app.queue.table.GetCell(0, queueYearCol).GetLastPosition()
	durationX, _, _ := v.app.queue.table.GetCell(0, queueDurationCol).GetLastPosition()
	v.SetRect(lyricsViewerRect(qy, qh, yearX, durationX))
}

// Draw repositions the viewer over the Queue table's Year-through-Type
// column band (see positionOverQueueColumns) on every frame, then
// delegates to the embedded TextView to actually paint it -- mirrors
// trackInfoCard.Draw's own "reposition from a sibling primitive's current
// layout, then paint" pattern.
func (v *lyricsViewer) Draw(screen tcell.Screen) {
	v.positionOverQueueColumns()
	v.TextView.Draw(screen)
}

// render loads and shows the lyrics for the currently playing song, fresh
// off disk every time (see internal/lyrics) -- so lyrics added after the
// track was queued still show up without needing a requeue or restart --
// or a placeholder if there's nothing playing, no music directory is
// configured, or there's no matching lyrics file.
func (v *lyricsViewer) render(song mpdclient.Song) {
	v.ScrollToBeginning()
	switch {
	case song.DisplayName() == "":
		v.SetText("[::d]Nothing playing[-:-:-]")
	case v.app.musicDir == "":
		// Hardcoded rather than computed via internal/config.ConfigFile:
		// internal/ui doesn't depend on internal/config (see
		// DEPENDENCY.md -- only cmd/mpdtui and mpdclient do), and adding
		// that edge just for this message's path string, which is
		// correct for the vast majority of setups (a custom
		// $XDG_CONFIG_HOME is the rare exception), isn't worth it.
		v.SetText("[::d]No music directory configured -- set music_dir in ~/.config/mpdtui/config[-:-:-]")
	default:
		text, ok := lyrics.Read(v.app.musicDir, song.File)
		if !ok {
			v.SetText(fmt.Sprintf("[::d]No lyrics found for %s[-:-:-]", song.DisplayName()))
			return
		}
		v.SetText(text)
	}
}

// openLyricsViewer is 'y': opens the lyrics viewer for whichever track is
// currently playing (not whatever's selected in Queue -- same convention
// trackInfoCard already uses), reachable from any panel. 'y' again or Esc
// (both handled globally in overlay mode, see globalInputCapture) close
// it and restore whichever panel was focused before 'y' was pressed, same
// as every other overlay.
//
// Uses a.currentSong (refreshNowPlaying's own last-fetched CurrentSong)
// rather than fetching it again here -- no extra MPD round-trip on open,
// and mirrors trackInfoCard, which relies on the exact same continuously-
// updated state instead of fetching on demand.
func (a *App) openLyricsViewer() {
	a.lyricsViewer.render(a.currentSong)
	a.showOverlay("lyrics", a.lyricsViewer, a.lyricsViewer)
}
