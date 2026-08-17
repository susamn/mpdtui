package ui

import (
	"fmt"
	"strings"
	"time"

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
// tick, matching the Mark column's own "colored ticks, no icons"
// convention. The column carries no queueColumnGap padding (unlike
// every other Queue column): its content is always the header's own
// width ("Lyr", 3 runes) or narrower -- one tick, two space-separated
// ticks when both formats exist, or "" -- so tview.Table's own
// auto-sizing-to-content already makes the column exactly as wide as it
// needs to be, with no extra code to enforce that.
const lyricsTick = "✓"

// lyricsLRCColor/lyricsTxtColor color a lyrics format's own badge --
// green for LRC (reuses nowPlayingTrackColor's WhatsApp green, the same
// "this is the synced/active one" association the lyrics viewer's own
// highlight already uses) and orange for TXT (reuses markTickColors'
// own orange RGB, #FFA500, rather than inventing a new one) -- explicit
// request ("green for LRC and Orange for TXT"). Used both in the Queue
// table's Lyr column (colored tick(s), via embedded tview color tags
// since a single TableCell can otherwise only carry one SetTextColor for
// its whole text) and the track info card (colored "LRC"/"TXT" text).
const (
	lyricsLRCColor = nowPlayingTrackColor
	lyricsTxtColor = "#FFA500"
)

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
	// syncedLines is the currently loaded track's parsed .lrc content, or
	// nil if it has no synced lyrics loaded (either no .lrc exists for
	// it, or the viewer is showing plain-text lyrics/a placeholder
	// instead). Set once by render (real filesystem I/O -- only on a
	// track change or open, see maybeRefreshLyricsViewer's own doc
	// comment), then read every refresh tick by updateHighlight (pure
	// in-memory recomputation, no I/O) to move the highlighted line
	// without re-reading anything off disk.
	syncedLines []lyrics.LyricLine
	// currentLine is the index into syncedLines currently highlighted, or
	// -1 (nothing highlighted yet -- render's own initial state, before
	// the first updateHighlight call). Tracked so updateHighlight can
	// skip repainting when the line hasn't actually changed since the
	// last tick, which is most ticks: two consecutive lines are rarely
	// less than a second apart, so most 500ms ticks land between two
	// timestamps rather than crossing one.
	currentLine int
	// preferredFormat is the user's sticky lyrics-format choice, cycled
	// with 't' (explicit request: "an option to switch txt, lrc, or in
	// future A2") -- only ever changed by cycleFormat, an explicit user
	// action, never by render's own per-track fallback (see
	// resolveLyricsFormat), so a manual choice persists across track
	// changes instead of needing to be re-toggled every track. Zero value
	// is lyricsFormatLRC, matching this feature's original default
	// (prefer synced lyrics when available).
	preferredFormat lyricsFormat
	// currentFormat is what's actually on screen right now -- set by
	// render on every call (unlike preferredFormat, which render only
	// ever reads), read by updateTitle to paint the right-aligned format
	// badge. Distinct from preferredFormat specifically so a preference
	// that isn't available for the current track (resolveLyricsFormat's
	// own fallback) shows the format that's genuinely displayed, not the
	// one that was merely asked for.
	currentFormat lyricsFormat
}

// lyricsFormat is a lyrics sidecar format the viewer can show, in a
// fixed priority order (lyricsFormatLRC first) used both as render's
// default preference and as the cycle order for 't'.
type lyricsFormat int

const (
	lyricsFormatLRC lyricsFormat = iota
	lyricsFormatTxt
	// A future lyricsFormatA2 (word-level/enhanced LRC) slots in here --
	// lyricsAvailableFormats/resolveLyricsFormat/cycleFormat are all
	// already written generically over a []lyricsFormat, needing no
	// changes beyond adding the new constant and its own Find/Read pair
	// in internal/lyrics once that format is actually supported.
	lyricsFormatNone // sentinel: nothing available for this track at all
)

func (f lyricsFormat) label() string {
	switch f {
	case lyricsFormatLRC:
		return "synced (.lrc)"
	case lyricsFormatTxt:
		return "plain text (.txt)"
	default:
		return "none"
	}
}

// lyricsAvailableFormats reports which formats actually exist for file,
// in lyricsFormat's own priority order -- both render's default fallback
// and cycleFormat's cycle set come from this, so neither one can ever
// offer/select a format that doesn't really have a file backing it.
func lyricsAvailableFormats(musicDir, file string) []lyricsFormat {
	var out []lyricsFormat
	if _, ok := lyrics.FindLRC(musicDir, file); ok {
		out = append(out, lyricsFormatLRC)
	}
	if _, ok := lyrics.Find(musicDir, file); ok {
		out = append(out, lyricsFormatTxt)
	}
	return out
}

// resolveLyricsFormat picks which of available to actually display,
// given the sticky preferred format: preferred itself if it's among
// available, else available's own first (priority-ordered) entry, else
// lyricsFormatNone if nothing's available for this track at all. A pure
// function of its two arguments -- no I/O, no receiver -- so render's
// per-track fallback logic is testable without a real lyricsViewer.
func resolveLyricsFormat(preferred lyricsFormat, available []lyricsFormat) lyricsFormat {
	for _, f := range available {
		if f == preferred {
			return f
		}
	}
	if len(available) > 0 {
		return available[0]
	}
	return lyricsFormatNone
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

// lyricsSyncedHighlightColor is the background used to highlight
// whichever line is currently playing in synced (.lrc) lyrics -- reuses
// nowPlayingTrackColor's own WhatsApp green (nowplaying.go) rather than
// inventing a new color, so "this is the active/now" reads consistently
// with the rest of the app (Now Playing bar's own track-title color, the
// track info card's lyrics-present tick).
const lyricsSyncedHighlightColor = nowPlayingTrackColor

// lyricsSyncedScrollLookback keeps this many already-sung lines visible
// above the highlighted one when auto-scrolling, rather than pinning the
// current line to the very top of the viewport -- reads more like a
// karaoke display (a little "where we just were" context still visible)
// than a jumpy line-at-a-time reveal.
const lyricsSyncedScrollLookback = 3

func newLyricsViewer(app *App) *lyricsViewer {
	v := tview.NewTextView().SetDynamicColors(true)
	v.SetBorder(true)
	// AlignLeft: the title is a single hand-built string with the format
	// badge's own padding baked in (see lyricsViewerTitleText) -- tview's
	// own alignment would shift the whole already-padded string rather
	// than doing anything useful with it.
	v.SetTitleAlign(tview.AlignLeft)
	v.SetBorderColor(lyricsColor).SetTitleColor(lyricsColor)
	v.SetBorderPadding(1, 1, 2, 2)
	v.SetScrollable(true)
	lv := &lyricsViewer{TextView: v, app: app, currentLine: -1, currentFormat: lyricsFormatNone}
	lv.updateTitle()
	return lv
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
// column band (see positionOverQueueColumns) and recomputes the border
// title (see updateTitle) on every frame, then delegates to the embedded
// TextView to actually paint it -- mirrors trackInfoCard.Draw's own
// "reposition from a sibling primitive's current layout, then paint"
// pattern. The title needs the same per-frame treatment as the position:
// its right-aligned format badge has to stay flush against the border as
// the viewer resizes with the terminal.
func (v *lyricsViewer) Draw(screen tcell.Screen) {
	v.positionOverQueueColumns()
	v.updateTitle()
	v.TextView.Draw(screen)
}

// lyricsViewerLeftTitle is the left-anchored, always-shown portion of
// the border title -- scroll and format-switch key hints.
const lyricsViewerLeftTitle = " Lyrics (j/k/g/G to scroll, t to switch format) "

// lyricsViewerTitleText builds the lyrics viewer's border title:
// lyricsViewerLeftTitle on the left, plus, when format is a real loaded
// format (not lyricsFormatNone), a colored "LRC"/"TXT" badge pushed
// flush against the box's own right edge -- explicit request ("put the
// lyrics type top right (colored)"). Built as a single left-aligned
// string with the right segment's own padding baked in, since
// tview.Box only supports one title with one alignment -- there's no
// built-in way to left-align part of a title and right-align another
// part of the same title. width is the box's own current rendered width
// (GetRect()'s w), passed in fresh every frame (see Draw) so the badge
// stays correctly positioned as the viewer resizes with the terminal.
// Split out as a pure function (mirrors lyricsViewerRect/resolveLyricsFormat's
// own testability-first split) so the padding arithmetic is testable
// without a real tview.Box.
func lyricsViewerTitleText(format lyricsFormat, width int) string {
	var badge string
	switch format {
	case lyricsFormatLRC:
		badge = fmt.Sprintf("[%s::b]LRC[-:-:-] ", lyricsLRCColor)
	case lyricsFormatTxt:
		badge = fmt.Sprintf("[%s::b]TXT[-:-:-] ", lyricsTxtColor)
	default:
		return lyricsViewerLeftTitle
	}
	// width-2 mirrors tview's own Box.Draw title budget (it prints the
	// title into b.width-2, leaving the two border corner cells alone).
	pad := width - 2 - tview.TaggedStringWidth(lyricsViewerLeftTitle) - tview.TaggedStringWidth(badge)
	if pad < 1 {
		pad = 1
	}
	return lyricsViewerLeftTitle + strings.Repeat(" ", pad) + badge
}

// updateTitle recomputes and applies the border title from
// v.currentFormat and the viewer's own current width -- called every
// frame (see Draw), not just from render, so the right-aligned badge
// stays flush against the border as the viewer resizes with the
// terminal, not just at the moment a track last changed.
func (v *lyricsViewer) updateTitle() {
	_, _, w, _ := v.GetRect()
	v.SetTitle(lyricsViewerTitleText(v.currentFormat, w))
}

// render loads and shows the lyrics for the currently playing song, fresh
// off disk every time (see internal/lyrics) -- so lyrics added after the
// track was queued still show up without needing a requeue or restart --
// or a placeholder if there's nothing playing, no music directory is
// configured, or there's no matching lyrics file. Which format actually
// gets shown, when more than one exists for a track, is resolved by
// resolveLyricsFormat against v.preferredFormat (defaults to synced,
// explicit request: "auto hint the currently played line"; changeable
// with 't', see cycleFormat) -- render itself never writes to
// preferredFormat, only reads it, so a manual choice survives even a
// track that happens not to have the preferred format available.
// Resets syncedLines/currentLine every call, since this always means a
// new track (or a fresh open) -- the caller (openLyricsViewer/
// maybeRefreshLyricsViewer) is expected to call updateHighlight right
// after, to paint the correct initial highlight for synced content
// rather than leaving it unhighlighted until the next refresh tick.
func (v *lyricsViewer) render(song mpdclient.Song) {
	v.syncedLines = nil
	v.currentLine = -1
	v.ScrollToBeginning()
	// Wrap defaults back on for every render call; renderSyncedLines is
	// the only place that ever turns it off, and only for as long as the
	// "STARTING" banner (which must clip, not wrap) is actually showing --
	// otherwise a track change away from a still-before-first-line LRC
	// track (wrap left off) to a plain .txt one would leave long lines
	// unable to wrap here.
	v.SetWrap(true)
	switch {
	case song.DisplayName() == "":
		v.currentFormat = lyricsFormatNone
		v.SetText("[::d]Nothing playing[-:-:-]")
	case v.app.musicDir == "":
		v.currentFormat = lyricsFormatNone
		// Hardcoded rather than computed via internal/config.ConfigFile:
		// internal/ui doesn't depend on internal/config (see
		// DEPENDENCY.md -- only cmd/mpdtui and mpdclient do), and adding
		// that edge just for this message's path string, which is
		// correct for the vast majority of setups (a custom
		// $XDG_CONFIG_HOME is the rare exception), isn't worth it.
		v.SetText("[::d]No music directory configured -- set music_dir in ~/.config/mpdtui/config[-:-:-]")
	default:
		available := lyricsAvailableFormats(v.app.musicDir, song.File)
		v.currentFormat = resolveLyricsFormat(v.preferredFormat, available)
		switch v.currentFormat {
		case lyricsFormatLRC:
			lines, _ := lyrics.ReadLRC(v.app.musicDir, song.File)
			v.syncedLines = lines
			v.renderSyncedLines()
		case lyricsFormatTxt:
			text, _ := lyrics.Read(v.app.musicDir, song.File)
			// tview.Escape guards against lyrics content that happens to
			// contain "[...]" (e.g. a "[Chorus]"/"[x2]" annotation,
			// common in real lyrics files) -- SetDynamicColors(true)
			// means any such substring would otherwise be misparsed as
			// a style tag and silently vanish instead of rendering as
			// literal text.
			v.SetText(fmt.Sprintf("[%s]%s[-]", lyricsTextColor, tview.Escape(text)))
		default:
			v.SetText(fmt.Sprintf("[::d]No lyrics found for %s[-:-:-]", song.DisplayName()))
		}
	}
}

// cycleFormat is 't', while the lyrics viewer has focus: switches
// preferredFormat to the next format actually available for the
// currently playing track (wrapping around) and re-renders immediately.
// A no-op, flashed rather than silent, if there's nothing to switch
// between -- zero or one format available for this track. Explicit
// request: "an option to switch txt, lrc, or in future A2".
func (v *lyricsViewer) cycleFormat() {
	song := v.app.currentSong
	available := lyricsAvailableFormats(v.app.musicDir, song.File)
	switch len(available) {
	case 0:
		v.app.showMessage("no lyrics available to switch between for this track")
		return
	case 1:
		v.app.showMessage("only one lyrics format (" + available[0].label() + ") available for this track")
		return
	}

	current := resolveLyricsFormat(v.preferredFormat, available)
	idx := 0
	for i, f := range available {
		if f == current {
			idx = i
			break
		}
	}
	v.preferredFormat = available[(idx+1)%len(available)]
	v.render(song)
	v.updateHighlight(v.app.currentStatus.Elapsed)
	v.app.showMessage("lyrics: switched to " + v.preferredFormat.label())
}

// lyricsStartingWord/lyricsStartingDots make up the "Starting....." intro
// shown, as a big block-letter banner, in place of the lyrics list while
// elapsed hasn't reached the first synced line's own timestamp yet (an
// instrumental intro) -- explicit request, LRC-only by construction:
// renderSyncedLines is never called for plain-text (.txt) content, which
// has no timestamps to be "before" in the first place. Followed by a
// blank line before real lyrics content would ever start, per the
// request's own layout.
const (
	lyricsStartingWord = "STARTING"
	lyricsStartingDots = "....."
)

// lyricsStartingGlyphs is a small 5-row block-letter font, just enough
// characters to spell lyricsStartingWord -- explicit "5 times the size"
// request. A terminal character grid has no actual font-size concept
// (tview style tags only ever cover color/bold/italic/blink/etc, never
// size -- an earlier, same-codebase attempt at "bigger text" via
// fullwidth Unicode characters, roughly double-width/same-height, was
// tried and explicitly disliked once seen; see this file's own git
// history / ARCHITECTURE.md's Now Playing section), so genuinely bigger
// means drawing each letter out of multiple rows and columns of block
// characters instead, the same idea a "figlet"/banner-style renderer
// uses -- not a technique unique to this app, just the only one that
// actually changes apparent size rather than just width.
var lyricsStartingGlyphs = map[byte][5]string{
	'S': {"█████", "█    ", "█████", "    █", "█████"},
	'T': {"█████", "  █  ", "  █  ", "  █  ", "  █  "},
	'A': {" ███ ", "█   █", "█████", "█   █", "█   █"},
	'R': {"████ ", "█   █", "████ ", "█ █  ", "█  █ "},
	'I': {"███", " █ ", " █ ", " █ ", "███"},
	'N': {"█   █", "██  █", "█ █ █", "█  ██", "█   █"},
	'G': {" ████", "█    ", "█ ███", "█   █", " ████"},
}

// lyricsStartingBannerLines renders word (assumed all-uppercase and
// entirely covered by lyricsStartingGlyphs -- only ever called with
// lyricsStartingWord) as 5 lines of block characters, one blank column
// of gap between adjacent letters.
func lyricsStartingBannerLines(word string) [5]string {
	var lines [5]string
	for row := range lines {
		var b strings.Builder
		for i := 0; i < len(word); i++ {
			if i > 0 {
				b.WriteByte(' ')
			}
			b.WriteString(lyricsStartingGlyphs[word[i]][row])
		}
		lines[row] = b.String()
	}
	return lines
}

// renderSyncedLines repaints the viewer from v.syncedLines, coloring the
// line at v.currentLine with a solid background band (not just a
// different text color) so the "now singing" line is immediately
// findable by eye, not just subtly different from the rest -- explicit
// "auto hint" request. Blank lines render as a single space rather than
// truly empty, so every entry in syncedLines still produces exactly one
// rendered line -- keeping v.currentLine a valid row index for
// scrollToCurrentLine (ScrollTo operates on rendered rows, not slice
// indices, and the two would drift apart the moment a blank line
// collapsed to zero rendered rows). v.currentLine == -1 (before the
// first timestamp) shows the block-letter "STARTING" banner plus the
// blinking "....." dots instead of the list itself -- 'l' is tview's own
// blink-attribute tag character (lowercase sets the attribute; see
// rivo/tview's strings.go attrs map -- uppercase would clear it instead,
// the same lowercase-sets/uppercase-clears convention already relied on
// for bold ("::b") elsewhere in this file). Word wrap is disabled while
// showing the banner (restored once real lyrics render): a banner row
// that's wider than the viewer's current width must clip cleanly on the
// right rather than wrapping onto the next row, which would slide part
// of one letter row into the next and break every glyph below it.
func (v *lyricsViewer) renderSyncedLines() {
	if v.currentLine < 0 {
		v.SetWrap(false)
		var b strings.Builder
		for _, line := range lyricsStartingBannerLines(lyricsStartingWord) {
			fmt.Fprintf(&b, "[%s::bl]%s[-:-:-]\n", lyricsLRCColor, line)
		}
		fmt.Fprintf(&b, "[%s::bl]%s[-:-:-]\n\n", lyricsLRCColor, lyricsStartingDots)
		v.SetText(b.String())
		return
	}
	v.SetWrap(true)
	var b strings.Builder
	for i, line := range v.syncedLines {
		text := tview.Escape(line.Text) // see render's own doc comment on why this is needed
		if text == "" {
			text = " "
		}
		if i == v.currentLine {
			fmt.Fprintf(&b, "[white:%s:b]%s[-:-:-]\n", lyricsSyncedHighlightColor, text)
		} else {
			fmt.Fprintf(&b, "[%s]%s[-]\n", lyricsTextColor, text)
		}
	}
	v.SetText(b.String())
}

// updateHighlight moves the highlighted line to whichever one is current
// at elapsed (see lyrics.CurrentLineIndex), when the currently loaded
// track has synced lyrics -- a no-op (doesn't touch v.SetText at all) if
// nothing's synced, or if the current line hasn't actually changed since
// the last call, so most refresh ticks (landing between two timestamps)
// do genuinely nothing here.
func (v *lyricsViewer) updateHighlight(elapsed time.Duration) {
	if v.syncedLines == nil {
		return
	}
	idx := lyrics.CurrentLineIndex(v.syncedLines, elapsed)
	if idx == v.currentLine {
		return
	}
	v.currentLine = idx
	v.renderSyncedLines()
	v.scrollToCurrentLine()
}

// scrollToCurrentLine keeps the highlighted line in view, offset by
// lyricsSyncedScrollLookback lines of already-passed context rather than
// pinning it to the very top of the viewport. currentLine == -1 (before
// the first timestamp -- e.g. an instrumental intro) scrolls to the top
// instead, showing the lyrics from the beginning rather than nothing.
func (v *lyricsViewer) scrollToCurrentLine() {
	if v.currentLine < 0 {
		v.ScrollToBeginning()
		return
	}
	top := v.currentLine - lyricsSyncedScrollLookback
	if top < 0 {
		top = 0
	}
	v.ScrollTo(top, 0)
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
	a.lyricsViewer.updateHighlight(a.currentStatus.Elapsed)
	a.showOverlay("lyrics", a.lyricsViewer, a.lyricsViewer)
}
