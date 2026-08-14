package ui

import (
	"fmt"
	"strings"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"mpdtui/internal/lyrics"
	"mpdtui/internal/mpdclient"
)

// queuePanel shows the current playback queue as a table, with the
// playing track marked, plus a persistent search field pinned below it
// (see search, wired up in app.go's openQueueSearch/closeOverlay) and a
// library-stats box sharing that same row (see stats/refreshStats --
// laid out here alongside search since that's where the app.go layout
// puts the spare width, even though the totals it shows are library-wide
// rather than queue-specific).
type queuePanel struct {
	app    *App
	table  *tview.Table
	search *tview.InputField
	stats  *tview.TextView
	songs  []mpdclient.Song

	currentID int // queue id of the playing/selected track, or -1 if none (see setCurrent)
}

// queueHeaderRows is the number of fixed rows (see Table.SetFixed) taken
// up by the column-header row -- every song's table row is offset by this
// much from its index in songs (row = index + queueHeaderRows), since row
// 0 is the header, not the first song.
const queueHeaderRows = 1

func newQueuePanel(app *App) *queuePanel {
	q := &queuePanel{app: app, currentID: -1}

	t := tview.NewTable()
	t.SetBorder(true).SetTitle(" Queue ")
	t.SetSelectable(true, false)
	t.SetFixed(queueHeaderRows, 0)
	t.SetSelectedStyle(tcell.StyleDefault.Background(colorSelectedBg).Foreground(colorSelectedFg))
	t.SetSelectedFunc(func(row, _ int) {
		i := row - queueHeaderRows
		if i < 0 || i >= len(q.songs) {
			return
		}
		song := q.songs[i]
		if err := q.app.client.PlayID(song.ID); err != nil {
			q.app.showError(err)
			return
		}
		q.app.refreshNowPlaying()
	})
	q.table = t
	setQueueHeader(t, newQueueColumns(app.musicDir != ""))

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

	stats := tview.NewTextView().SetDynamicColors(true).SetTextAlign(tview.AlignCenter)
	stats.SetBorder(true).SetTitle(" Stats ")
	q.stats = stats

	return q
}

// refreshStats fetches and displays library-wide totals (tracks, artists,
// stored playlists). Kept to a single line since the box only has one
// row of content to work with (same fixed height as the search field
// beside it).
func (q *queuePanel) refreshStats() {
	stats, err := q.app.client.LibraryStats()
	if err != nil {
		q.app.showError(err)
		return
	}
	q.stats.SetText(fmt.Sprintf(
		"[::b]Tracks:[-] %d  [::b]Artists:[-] %d  [::b]Playlists:[-] %d",
		stats.Tracks, stats.Artists, stats.Playlists,
	))
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

// Queue table column max lengths (runes), title/album/artist/genre/
// composer -- the order they're shown in. Longer values are truncated
// with a trailing "..." (see truncateWithEllipsis); a small trailing gap
// (queueColumnGap) is appended after each so adjacent columns don't run
// together, matching the same manual-padding convention formatGap already
// uses between the format tag and duration columns (tview's Table has no
// automatic column spacing). Year has no max of its own: yearFromDate
// already caps it to at most 4 characters.
const (
	queueTitleMaxLen    = 30
	queueAlbumMaxLen    = 20
	queueArtistMaxLen   = 40
	queueGenreMaxLen    = 9
	queueComposerMaxLen = 14
	queueColumnGap      = "  "
)

// queueTitleColor tints the Title cell WhatsApp's brand green (#25D366),
// on top of its existing bold weight, so the track title reads as the
// row's primary/most prominent field at a glance.
var queueTitleColor = tcell.NewRGBColor(0x25, 0xD3, 0x66)

// queueHeaderBg/Fg give the header row an inverted look (filled
// background, dark text) to set it apart from the data rows below.
// queueHeaderBg is a forced true-RGB white (tcell.NewRGBColor), not the
// basic ANSI tcell.ColorWhite -- that's a legacy 16-color palette slot,
// confirmed via raw ANSI output to render as the SGR 107 "bright white
// background" code, which plenty of terminal color themes customize to
// something duller than actual white. A explicit RGB color is always sent
// as a true 24-bit escape sequence, bypassing any such palette remapping.
var (
	queueHeaderBg = tcell.NewRGBColor(255, 255, 255)
	queueHeaderFg = tcell.ColorBlack
)

// queueColumns holds the Queue table's column indices for one header/
// render pass. Lyr only exists as a column when the lyrics feature is
// actually active -- a music_dir that config.LoadMusicDir has already
// confirmed both exists and is a real directory, not just configured --
// so an install without it (or with a broken/stale setting) looks
// exactly like it did before this feature existed, rather than always
// reserving space for a column that will never show anything. Every
// other column shifts left by one to fill that gap when Lyr is absent
// (lyr == -1).
type queueColumns struct {
	lyr, title, album, artist, year, genre, composer, typ, duration int
}

// newQueueColumns computes the column layout for one header/render pass.
// Marker (0) and position (1) are always fixed; Title always follows at
// 2; everything from there on is assigned sequentially, with Lyr
// included only when lyricsActive.
func newQueueColumns(lyricsActive bool) queueColumns {
	var c queueColumns
	c.title = 2
	next := 3
	if lyricsActive {
		c.lyr = next
		next++
	} else {
		c.lyr = -1
	}
	c.album = next
	next++
	c.artist = next
	next++
	c.year = next
	next++
	c.genre = next
	next++
	c.composer = next
	next++
	c.typ = next
	next++
	c.duration = next
	return c
}

// setQueueHeader (re)writes the fixed header row for the given column
// layout. Table.Clear() wipes every cell including row 0, so render()
// calls this again on every refresh rather than relying on it being set
// once at construction time. Type/Duration are right-aligned to match
// their data columns (see formatTagCell and the Duration cell in
// render()). Type's label carries the same trailing formatGap its data
// cells do (see formatTagCell) -- without it, the right-aligned header
// text would sit flush at the column's edge while the data (padded by
// formatGap to separate it from the Duration column) sits
// formatGap-width to the left of that same edge, visibly misaligning the
// two. Duration needs no such adjustment: neither its header nor its
// data carry any padding.
func setQueueHeader(t *tview.Table, cols queueColumns) {
	set := func(col int, text string, align int) {
		t.SetCell(0, col, tview.NewTableCell(text).
			SetAlign(align).
			SetTextColor(queueHeaderFg).
			SetBackgroundColor(queueHeaderBg).
			SetSelectable(false))
	}
	set(0, "", tview.AlignLeft)
	set(1, "", tview.AlignLeft)
	if cols.lyr >= 0 {
		set(cols.lyr, "Lyr", tview.AlignLeft)
	}
	set(cols.title, "Title", tview.AlignLeft)
	set(cols.album, "Album", tview.AlignLeft)
	set(cols.artist, "Artist", tview.AlignLeft)
	set(cols.year, "Year", tview.AlignLeft)
	set(cols.genre, "Genre", tview.AlignLeft)
	set(cols.composer, "Composer", tview.AlignLeft)
	set(cols.typ, "Type"+formatGap, tview.AlignRight)
	set(cols.duration, "Duration", tview.AlignRight)
}

func (q *queuePanel) render(curID int) {
	q.table.Clear()
	lyricsActive := q.app.musicDir != ""
	cols := newQueueColumns(lyricsActive)
	setQueueHeader(q.table, cols)
	// lyricsDirs caches internal/lyrics.Candidates per directory for the
	// duration of this one render pass only (no caching across renders,
	// see hasLyrics) -- multiple queued tracks from the same album share
	// a directory, so this avoids re-listing it once per track.
	lyricsDirs := map[string]map[string]string{}
	for i, s := range q.songs {
		row := i + queueHeaderRows
		marker := "  "
		if s.ID == curID {
			marker = "▶ "
		}
		title := s.Title
		if title == "" {
			title = baseName(s.File)
		}
		titleText := truncateWithEllipsis(title, queueTitleMaxLen)
		q.table.SetCell(row, 0, tview.NewTableCell(marker))
		q.table.SetCell(row, 1, tview.NewTableCell(fmt.Sprintf("%3d", i+1)))
		q.table.SetCell(row, cols.title, tview.NewTableCell(titleText+queueColumnGap).
			SetAttributes(tcell.AttrBold).
			SetTextColor(queueTitleColor))
		if cols.lyr >= 0 {
			// lyrCell carries no queueColumnGap padding, unlike every
			// other column here -- its only content is ever lyricsIcon or
			// "", so tview.Table's own auto-sizing-to-content already
			// makes the column exactly as wide as the icon and no wider
			// (the explicit ask: "the column width should only take to
			// contain the icon").
			lyrCell := ""
			if q.hasLyrics(s.File, lyricsDirs) {
				lyrCell = lyricsIcon
			}
			q.table.SetCell(row, cols.lyr, tview.NewTableCell(lyrCell))
		}
		q.table.SetCell(row, cols.album, tview.NewTableCell(truncateWithEllipsis(s.Album, queueAlbumMaxLen)+queueColumnGap))
		q.table.SetCell(row, cols.artist, tview.NewTableCell(truncateWithEllipsis(s.Artist, queueArtistMaxLen)+queueColumnGap))
		q.table.SetCell(row, cols.year, tview.NewTableCell(yearFromDate(s.Date)+queueColumnGap))
		q.table.SetCell(row, cols.genre, tview.NewTableCell(truncateWithEllipsis(s.Genre, queueGenreMaxLen)+queueColumnGap))
		q.table.SetCell(row, cols.composer, tview.NewTableCell(truncateWithEllipsis(s.Composer, queueComposerMaxLen)+queueColumnGap).
			SetExpansion(1))
		q.table.SetCell(row, cols.typ, formatTagCell(s.File))
		q.table.SetCell(row, cols.duration, tview.NewTableCell(FormatDuration(s.Duration)).SetAlign(tview.AlignRight))
	}
	q.table.SetTitle(fmt.Sprintf(" Queue (%d) ", len(q.songs)))
}

// hasLyrics reports whether file has a matching lyrics sidecar (see
// internal/lyrics), rechecked live against the real directory contents on
// every render -- there's no caching across render() calls, only within
// this one pass (dirCache), so a lyrics file added after a track was
// already queued shows up as soon as the Queue next repopulates (adding/
// removing/moving a track, or another client's own change via MPD's
// "playlist" idle event), without needing a special "recheck" code path
// of its own. Always false if musicDir is unset (the feature is
// inactive).
func (q *queuePanel) hasLyrics(file string, dirCache map[string]map[string]string) bool {
	if q.app.musicDir == "" {
		return false
	}
	dir := lyrics.Dir(q.app.musicDir, file)
	candidates, cached := dirCache[dir]
	if !cached {
		candidates = lyrics.Candidates(dir)
		dirCache[dir] = candidates
	}
	_, ok := lyrics.Match(file, candidates)
	return ok
}

// truncateWithEllipsis returns s unchanged if it's at most max runes,
// otherwise the first max-3 runes followed by "...". Operates on runes,
// not bytes, so multi-byte characters in track/album/artist tags aren't
// split mid-character.
func truncateWithEllipsis(s string, max int) string {
	runes := []rune(s)
	if len(runes) <= max {
		return s
	}
	return string(runes[:max-3]) + "..."
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
// A first pass here used tcell's extended RGB-based names, then got
// swapped to the basic 16-color ANSI set out of a since-disproven worry
// that RGB colors (tcell.ColorIsRGB, true 24-bit values needing truecolor
// negotiation) weren't rendering -- that finding was actually an artifact
// of sampling the selected row, whose color the table's own selection
// style always overrides regardless of format, not a real rendering
// failure. RGB colors are back, verified rendering correctly on
// non-selected rows.
var formatColors = map[string]tcell.Color{
	"FLAC": tcell.ColorLime,
	"MP3":  tcell.ColorSkyblue,
	"WAV":  tcell.ColorTurquoise,
	"M4A":  tcell.ColorLavender,
	"AAC":  tcell.ColorLavender,
	"OGG":  tcell.ColorAquaMarine,
	"OPUS": tcell.ColorAquaMarine,
	"WMA":  tcell.ColorOrange,
}

const defaultFormatColor = tcell.ColorHotPink

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
// re-fetching the queue from MPD (cheap enough to call on every tick), and
// remembers id for jumpToCurrent.
func (q *queuePanel) setCurrent(id int) {
	q.currentID = id
	for i, s := range q.songs {
		cell := q.table.GetCell(i+queueHeaderRows, 0)
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

// jumpToCurrent selects the currently playing track (see setCurrent),
// without changing focus -- callers that also want focus moved to the
// Queue panel do that separately (see App.jumpToCurrentTrack). Returns
// false if nothing is currently playing/selected, leaving the current
// selection untouched.
func (q *queuePanel) jumpToCurrent() bool {
	if q.currentID < 0 {
		return false
	}
	for i, s := range q.songs {
		if s.ID == q.currentID {
			q.table.Select(i+queueHeaderRows, 0)
			return true
		}
	}
	return false
}

// jumpToMatch selects (but does not remove or hide) the first queued track
// whose display name contains query, case- and diacritic-insensitive (see
// containsFold). Returns false if nothing matched, leaving the current
// selection untouched.
func (q *queuePanel) jumpToMatch(query string) bool {
	for i, s := range q.songs {
		if containsFold(s.DisplayName(), query) {
			q.table.Select(i+queueHeaderRows, 0)
			return true
		}
	}
	return false
}

func (q *queuePanel) selectedSong() (mpdclient.Song, bool) {
	row, _ := q.table.GetSelection()
	i := row - queueHeaderRows
	if i < 0 || i >= len(q.songs) {
		return mpdclient.Song{}, false
	}
	return q.songs[i], true
}
