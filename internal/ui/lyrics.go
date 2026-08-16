package ui

import (
	"fmt"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"mpdtui/internal/lyrics"
	"mpdtui/internal/mpdclient"
)

// lyricsTick marks a Queue row whose track has a matching lyrics file
// (see internal/lyrics), shown in its own narrow "Lyr" column
// (queue.go's render) rather than prefixed to the Title cell -- that was
// the first cut, but reads as cluttering Title rather than as a distinct
// badge. Originally an emoji icon (📝); replaced with a plain colored
// tick (lyricsTickColor), matching the Mark column's own "colored ticks,
// no icons" convention. The column carries no queueColumnGap padding
// (unlike every other Queue column): its only content is ever this tick
// or "", so tview.Table's own auto-sizing-to-content already makes it
// exactly as wide as the tick and no wider, with no extra code needed to
// enforce that.
const lyricsTick = "✓"

// lyricsTickColor tints lyricsTick sky blue -- distinct from the Mark
// column's own tick colors (markTickColors, cycled by mark reason), so
// the two badge columns don't read as the same signal at a glance.
var lyricsTickColor = tcell.ColorSkyblue

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

// lyricsColor matches colorActiveBorder, the same green a focused
// panel's (Library/Playlists/Queue) border and title use -- explicit
// request ("make the lyrics viewer border just like the border of the
// player panels... when they are selected"), replacing an earlier
// distinct teal that set this viewer visually apart from every panel.
var lyricsColor = colorActiveBorder

// lyricsTextColor tints the actual lyrics text a muted (not bright)
// gold/yellow -- explicit request. A tview color tag string, not a
// tcell.Color: applied via SetText's own markup (see render), not
// SetTextColor, since only part of the TextView's content (the real
// lyrics, not the "Nothing playing"/"No lyrics found" placeholders) is
// meant to be this color.
const lyricsTextColor = "#DAA520"

func newLyricsViewer(app *App) *lyricsViewer {
	v := tview.NewTextView().SetDynamicColors(true)
	v.SetBorder(true).SetTitle(" Lyrics (j/k/g/G to scroll) ")
	v.SetBorderColor(lyricsColor).SetTitleColor(lyricsColor)
	v.SetBorderPadding(1, 1, 2, 2)
	v.SetScrollable(true)
	return &lyricsViewer{TextView: v, app: app}
}

// lyricsViewerBottomMargin is how many of the Queue table's own data rows
// stay visible below the viewer (in addition to the table's own bottom
// border row, which is never covered either way) -- so it visibly floats
// within the Queue panel rather than filling every last row down to the
// panel's own border.
const lyricsViewerBottomMargin = 2

// lyricsViewerRect computes the viewer's rect from the Queue table's own
// rect (queueY, queueHeight -- GetRect(), which includes the table's own
// top and bottom border rows) and the actual last-drawn x positions of
// its Year and Duration columns (yearX, durationX).
//
// Horizontally: from yearX up to (not including) durationX.
//
// Vertically: queueY itself is the table's own top border row, queueY+1
// is the header row, so the first actual data row -- where the viewer
// starts, leaving the header row visible above it -- is
// queueY+1+queueHeaderRows. It stops lyricsViewerBottomMargin data rows
// before the table's own bottom border row (at queueY+queueHeight-1),
// which itself is also never covered.
//
// Split out as a pure function -- no tview.Table involved -- so the
// positioning math is testable without a real tcell.Screen, mirroring
// trackInfoCard's own quadrantRect/cardRect split from
// positionOverQueue/Draw. Clamps height to 0 rather than negative for a
// pathologically short Queue table (smaller than its own two border rows
// plus the header row plus the margin).
func lyricsViewerRect(queueY, queueHeight, yearX, durationX int) (x, y, width, height int) {
	top := queueY + 1 + queueHeaderRows
	height = queueHeight - queueHeaderRows - lyricsViewerBottomMargin - 2
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
// The column indices themselves come from queue.go's newQueueColumns,
// the same function render() uses to lay out the table in the first
// place -- Year/Duration's positions shift by one when the Lyr column
// isn't there (musicDir unset or invalid), and reading from a single
// shared source of truth is what keeps this from silently drifting out
// of sync with whatever render() actually drew.
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
	cols := newQueueColumns(v.app.musicDir != "", v.app.metaDB != nil)
	yearX, _, _ := v.app.queue.table.GetCell(0, cols.year).GetLastPosition()
	durationX, _, _ := v.app.queue.table.GetCell(0, cols.duration).GetLastPosition()
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
		// tview.Escape guards against lyrics content that happens to
		// contain "[...]" (e.g. a "[Chorus]"/"[x2]" annotation, common
		// in real lyrics files) -- SetDynamicColors(true) means any such
		// substring would otherwise be misparsed as a style tag and
		// silently vanish instead of rendering as literal text.
		v.SetText(fmt.Sprintf("[%s]%s[-]", lyricsTextColor, tview.Escape(text)))
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
