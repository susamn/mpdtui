package ui

import (
	"fmt"
	"strings"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"mpdtui/internal/lyrics"
	"mpdtui/internal/metadata"
	"mpdtui/internal/mpdclient"
	"mpdtui/internal/version"
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

	// cols is the column layout render() most recently drew, kept around
	// so applyTrackMeta (an async DB-fetch result landing later, or an
	// immediate rate/mark write) can address the right cells without
	// recomputing it -- must only be read/written on the main (tview)
	// goroutine, same as every other field here.
	cols queueColumns

	// lastRenderedWidth is the table width (runes) at the last render pass,
	// tracked so SetDrawFunc can trigger a re-render when terminal resize
	// alters the available column space on smaller screens.
	lastRenderedWidth int

	// metaCache holds the last known local metadata (rating/mark) per
	// song file, populated asynchronously (see refreshTrackMeta) so
	// render() never blocks the UI goroutine on a database read. Absent
	// entries render as the zero-value Track (unrated, unmarked) until
	// the background fetch fills them in. Main-goroutine-only.
	metaCache map[string]metadata.Track

	// metaSeq is bumped every time render() kicks off a fresh background
	// metadata fetch; a fetch's result is only applied if metaSeq still
	// matches the value captured when it started, so a queue change that
	// happens while an old fetch is still in flight can't clobber newer
	// data with stale results. Main-goroutine-only (only ever touched
	// from render() and from inside QueueUpdateDraw callbacks).
	metaSeq int
}

// queueHeaderRows is the number of fixed rows (see Table.SetFixed) taken
// up by the column-header row -- every song's table row is offset by this
// much from its index in songs (row = index + queueHeaderRows), since row
// 0 is the header, not the first song.
const queueHeaderRows = 1

func newQueuePanel(app *App) *queuePanel {
	q := &queuePanel{app: app, currentID: -1, metaCache: map[string]metadata.Track{}}

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
	_, _, w, _ := t.GetRect()
	showYear, showGenre, showComposer, showType := queueOptionalColumns(w, app.musicDir != "", app.metaDB != nil)
	q.cols = newQueueColumns(app.musicDir != "", app.metaDB != nil, showYear, showGenre, showComposer, showType)
	setQueueHeader(t, q.cols)

	t.SetDrawFunc(func(screen tcell.Screen, x, y, width, height int) (int, int, int, int) {
		if width > 0 && width != q.lastRenderedWidth {
			q.lastRenderedWidth = width
			q.render(q.currentID)
		}
		if width <= 2 || height <= 2 {
			return x, y, 0, 0
		}
		return x + 1, y + 1, width - 2, height - 2
	})

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
	// tview.Box only supports one title string (one alignment for the
	// whole thing), so the version can't just be a second SetTitle --
	// SetDrawFunc runs after Box.Draw has already painted the border and
	// the "Stats" title, letting this stamp version.String directly onto
	// the same top border line, right-aligned, without disturbing
	// "Stats". Must still return the correct inner content rect itself
	// (replacing Box.Draw's own default calculation): border but no
	// custom padding, so it's just (x+1, y+1, width-2, height-2).
	stats.SetDrawFunc(func(screen tcell.Screen, x, y, width, height int) (int, int, int, int) {
		labelX := x + width - 2 - len(version.String)
		if labelX > x {
			tview.Print(screen, version.String, labelX, y, len(version.String), tview.AlignLeft, tview.Styles.BorderColor)
		}
		return x + 1, y + 1, width - 2, height - 2
	})
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
	// Values reuse the same colors those concepts already have elsewhere
	// in the app rather than inventing new ones: Tracks in
	// nowPlayingTrackColor (WhatsApp green, matching every other "track"
	// -- Queue's Title cell, Now Playing's title), Artists in
	// nowPlayingArtistColor (sky blue, matching Now Playing's artist),
	// Playlists in nowPlayingBarColor (cyan) -- no established "playlist"
	// color existed yet, so this reuses the one remaining accent color
	// already in use elsewhere (the progress bar) rather than adding a
	// fourth. Labels stay bold and uncolored, matching FlagText's own
	// "color the value, not the label" convention.
	q.stats.SetText(fmt.Sprintf(
		"[::b]Tracks:[-] [%s::b]%d[-:-:-]  [::b]Artists:[-] [%s::b]%d[-:-:-]  [::b]Playlists:[-] [%s::b]%d[-:-:-]",
		nowPlayingTrackColor, stats.Tracks,
		nowPlayingArtistColor, stats.Artists,
		nowPlayingBarColor, stats.Playlists,
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
	queueTitleMaxLen         = 30
	queueAlbumMaxLen         = 20
	queueArtistMaxLen        = 40
	queueGenreMaxLen         = 9
	queueComposerMaxLen      = 14
	queueTitleCompactMaxLen  = 22
	queueAlbumCompactMaxLen  = 16
	queueArtistCompactMaxLen = 20
	queueColumnGap           = "  "

	// queueCompactWidthThreshold is the Queue table width (runes) below
	// which the table drops Year, Genre, and Composer columns to preserve
	// space for Title, Album, Artist, Play count, Mark, Rating, Type, and
	// Duration on smaller or scaled screens (e.g. 1080p @ 1.5x scaling).
	queueCompactWidthThreshold = 130
)

// queueTitleColor tints the Title cell with the active theme's Green,
// on top of its existing bold weight, so the track title reads as the
// row's primary/most prominent field at a glance. Set by theme.go's
// deriveColors (from palette), not a literal here -- see that file for
// the actual mapping from palette to every color in this block.
var queueTitleColor tcell.Color

// queueHeaderBg/Fg give the header row an inverted look (filled
// background, contrasting text) to set it apart from the data rows
// below. Both theme-derived (deriveColors) rather than a fixed white-
// on-black -- see theme.go.
var (
	queueHeaderBg tcell.Color
	queueHeaderFg tcell.Color
)

// queueColumns holds the Queue table's column indices for one header/
// render pass. Lyr only exists as a column when the lyrics feature is
// actually active. Playcount/Mark/Rating exist when metadata is active.
// Year, Genre, Composer, and Type are optional columns included
// progressively based on available screen width in order of priority:
// Year first, then Genre, Composer, and finally Type on wide screens.
type queueColumns struct {
	lyr, title, album, artist, year, genre, composer, playcount, mark, rating, typ, duration int
}

// newQueueColumns computes the column layout for one header/render pass.
// Marker (0) and position (1) are always fixed; Title always follows at
// 2; everything from there on is assigned sequentially, with Lyr
// included only when lyricsActive and Playcount/Mark/Rating included
// only when metadataActive (App.metaDB != nil). Year, Genre, Composer,
// and Type are included conditionally according to available space and priority.
func newQueueColumns(lyricsActive, metadataActive, showYear, showGenre, showComposer, showType bool) queueColumns {
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
	if showYear {
		c.year = next
		next++
	} else {
		c.year = -1
	}
	if showGenre {
		c.genre = next
		next++
	} else {
		c.genre = -1
	}
	if showComposer {
		c.composer = next
		next++
	} else {
		c.composer = -1
	}
	if metadataActive {
		c.playcount = next
		next++
		c.mark = next
		next++
		c.rating = next
		next++
	} else {
		c.playcount = -1
		c.mark = -1
		c.rating = -1
	}
	if showType {
		c.typ = next
		next++
	} else {
		c.typ = -1
	}
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
//
// Title/Album/Artist/Year/Genre/Composer carry the same trailing
// queueColumnGap their data cells do, for the same reason as Type: with
// an empty queue there are no data rows at all, so tview.Table sizes
// each column purely from this header row -- without the gap here too,
// every one of those columns would visibly shrink to just its label's
// width the moment the queue empties out, then jump back wider again as
// soon as a track (with its own gap-padded cell) was added, a jarring
// layout shift for something that should look the same regardless of
// queue length.
//
// The Composer header cell also gets its own SetExpansion(1), matching
// its data cells (render()) -- tview.Table only evaluates *visible* rows
// per column (Table.Draw, evaluateAllRows is never set here), so with an
// empty queue the header is the only row it looks at; without expansion
// on the header cell too, an empty queue has no cell anywhere reporting
// Expansion > 0 for that column, so the leftover terminal width past
// Duration goes completely undistributed instead of widening Composer,
// and every column from Composer rightward (Plays/Mark/Rating/Type/
// Duration) collapses back to its bare minimum width and bunches up on
// the left the moment the queue empties -- the actual dominant cause of
// "the header shrinks", more than the missing per-label gap above.
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
	set(cols.title, "Title"+queueColumnGap, tview.AlignLeft)
	set(cols.album, "Album"+queueColumnGap, tview.AlignLeft)
	set(cols.artist, "Artist"+queueColumnGap, tview.AlignLeft)
	if cols.year >= 0 {
		set(cols.year, "Year"+queueColumnGap, tview.AlignLeft)
	}
	if cols.genre >= 0 {
		set(cols.genre, "Genre"+queueColumnGap, tview.AlignLeft)
	}
	if cols.composer >= 0 {
		set(cols.composer, "Composer"+queueColumnGap, tview.AlignLeft)
		t.GetCell(0, cols.composer).SetExpansion(1)
	} else {
		t.GetCell(0, cols.artist).SetExpansion(1)
	}
	if cols.playcount >= 0 {
		set(cols.playcount, "Plays"+queueColumnGap, tview.AlignRight)
	}
	if cols.mark >= 0 {
		set(cols.mark, "Mark"+queueColumnGap, tview.AlignRight)
	}
	if cols.rating >= 0 {
		set(cols.rating, "Rating"+queueColumnGap, tview.AlignRight)
	}
	if cols.typ >= 0 {
		set(cols.typ, "Type"+formatGap, tview.AlignRight)
	}
	set(cols.duration, "Duration", tview.AlignRight)
}

// queueOptionalColumns determines which optional columns (Year, Genre,
// Composer, Type) should be shown given the available table width and active
// features. The core columns (Title, Lyr, Album, Artist, Plays, Mark,
// Rating, Duration) are always preserved on all displays. Extra
// space is allocated progressively by priority: Year first, then Genre,
// Composer, and finally Type on wide screens.
func queueOptionalColumns(width int, lyricsActive, metadataActive bool) (showYear, showGenre, showComposer, showType bool) {
	if width <= 0 {
		return false, false, false, false
	}
	fixed := 2 + 3 + 8 + 2 // marker(2) + pos(3) + duration(8) + border(2)
	if lyricsActive {
		fixed += 3
	}
	if metadataActive {
		fixed += 7 + 4 + 8 // plays(7) + mark(4) + rating(8)
	}
	avail := width - fixed
	// Base comfortable text space for Title (24), Album (16), Artist (22) + gaps (6) = 68
	const baseTextSpace = 68
	if avail >= baseTextSpace+6 { // Priority 1: Year (4 + 2 gap = 6)
		showYear = true
	}
	if avail >= baseTextSpace+6+11 { // Priority 2: Genre (9 + 2 gap = 11)
		showGenre = true
	}
	if avail >= baseTextSpace+6+11+16 { // Priority 3: Composer (14 + 2 gap = 16)
		showComposer = true
	}
	if avail >= baseTextSpace+6+11+16+6 { // Priority 4 (Last): Type (4 + 2 gap = 6)
		showType = true
	}
	return showYear, showGenre, showComposer, showType
}

// queueColumnTruncation calculates the maximum text lengths (runes) for Title,
// Album, and Artist based on available table width and optional columns.
// Fixed standard caps (30/20/40) are used when all columns fit comfortably.
// On narrower screens, text column caps scale proportionally to the available width
// after reserving fixed space for marker, pos, lyrics, play count, mark, rating,
// type, and duration columns, ensuring the trailing metadata and duration columns
// are never pushed off screen.
func queueColumnTruncation(width int, lyricsActive, metadataActive, showYear, showGenre, showComposer, showType bool) (titleLen, albumLen, artistLen int) {
	if showYear && showGenre && showComposer && showType {
		return queueTitleMaxLen, queueAlbumMaxLen, queueArtistMaxLen
	}
	if width <= 0 {
		return queueTitleCompactMaxLen, queueAlbumCompactMaxLen, queueArtistCompactMaxLen
	}
	fixed := 2 + 3 + 8 + 2
	if lyricsActive {
		fixed += 3
	}
	if metadataActive {
		fixed += 7 + 4 + 8
	}
	if showYear {
		fixed += 6
	}
	if showGenre {
		fixed += 11
	}
	if showComposer {
		fixed += 16
	}
	if showType {
		fixed += 6
	}
	avail := width - fixed
	if avail <= 0 {
		return 12, 8, 12
	}
	// Distribute available width proportionally: Title ~38%, Album ~26%, Artist ~36%
	// Subtract 2 per column for queueColumnGap
	tLen := (avail*38)/100 - 2
	aLen := (avail*26)/100 - 2
	arLen := (avail*36)/100 - 2

	if tLen < 12 {
		tLen = 12
	}
	if aLen < 8 {
		aLen = 8
	}
	if arLen < 12 {
		arLen = 12
	}

	if tLen > queueTitleMaxLen {
		tLen = queueTitleMaxLen
	}
	if aLen > queueAlbumMaxLen {
		aLen = queueAlbumMaxLen
	}
	if arLen > queueArtistMaxLen {
		arLen = queueArtistMaxLen
	}

	return tLen, aLen, arLen
}

func (q *queuePanel) render(curID int) {
	q.table.Clear()
	lyricsActive := q.app.musicDir != ""
	metadataActive := q.app.metaDB != nil
	_, _, w, _ := q.table.GetRect()
	q.lastRenderedWidth = w

	showYear, showGenre, showComposer, showType := queueOptionalColumns(w, lyricsActive, metadataActive)
	cols := newQueueColumns(lyricsActive, metadataActive, showYear, showGenre, showComposer, showType)
	q.cols = cols
	setQueueHeader(q.table, cols)
	// lrcDirs/txtDirs cache internal/lyrics.LRCCandidates/Candidates per
	// directory for the duration of this one render pass only (no
	// caching across renders, see lyricsPresence) -- multiple queued
	// tracks from the same album share a directory, so this avoids
	// re-listing it (once per format) more than once per directory.
	lrcDirs := map[string]map[string]string{}
	txtDirs := map[string]map[string]string{}

	titleMaxLen, albumMaxLen, artistMaxLen := queueColumnTruncation(w, lyricsActive, metadataActive, showYear, showGenre, showComposer, showType)

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
		titleText := truncateWithEllipsis(title, titleMaxLen)
		q.table.SetCell(row, 0, tview.NewTableCell(marker))
		q.table.SetCell(row, 1, tview.NewTableCell(fmt.Sprintf("%3d", i+1)))
		q.table.SetCell(row, cols.title, tview.NewTableCell(titleText+queueColumnGap).
			SetAttributes(tcell.AttrBold).
			SetTextColor(queueTitleColor))
		if cols.lyr >= 0 {
			// lyrCell carries no queueColumnGap padding, unlike every
			// other column here -- its content never exceeds the header's
			// own width ("Lyr", 3 runes), so tview.Table's own
			// auto-sizing-to-content already makes the column exactly as
			// wide as it needs to be (the explicit ask: "the column width
			// should only take to contain the icon").
			q.table.SetCell(row, cols.lyr, tview.NewTableCell(lyricsCellText(q.lyricsPresence(s.File, lrcDirs, txtDirs))))
		}
		q.table.SetCell(row, cols.album, tview.NewTableCell(truncateWithEllipsis(s.Album, albumMaxLen)+queueColumnGap))

		artistCell := tview.NewTableCell(truncateWithEllipsis(s.Artist, artistMaxLen) + queueColumnGap)
		if cols.composer < 0 {
			artistCell.SetExpansion(1)
		}
		q.table.SetCell(row, cols.artist, artistCell)

		if cols.year >= 0 {
			q.table.SetCell(row, cols.year, tview.NewTableCell(yearFromDate(s.Date)+queueColumnGap))
		}
		if cols.genre >= 0 {
			q.table.SetCell(row, cols.genre, tview.NewTableCell(truncateWithEllipsis(s.Genre, queueGenreMaxLen)+queueColumnGap))
		}
		if cols.composer >= 0 {
			q.table.SetCell(row, cols.composer, tview.NewTableCell(truncateWithEllipsis(s.Composer, queueComposerMaxLen)+queueColumnGap).
				SetExpansion(1))
		}
		if cols.playcount >= 0 || cols.mark >= 0 || cols.rating >= 0 {
			// Whatever's cached so far (possibly the zero-value Track, if
			// the background fetch below hasn't landed yet) -- never a
			// direct DB read here, so render() itself never blocks on I/O.
			meta := q.metaCache[s.File]
			if cols.playcount >= 0 {
				q.table.SetCell(row, cols.playcount, playCountCell(meta.PlayCount))
			}
			if cols.mark >= 0 {
				q.table.SetCell(row, cols.mark, markCell(meta.Mark))
			}
			if cols.rating >= 0 {
				q.table.SetCell(row, cols.rating, ratingCell(meta.Rating))
			}
		}
		if cols.typ >= 0 {
			q.table.SetCell(row, cols.typ, formatTagCell(s.File))
		}
		q.table.SetCell(row, cols.duration, tview.NewTableCell(FormatDuration(s.Duration)).SetAlign(tview.AlignRight))
	}
	q.table.SetTitle(fmt.Sprintf(" Queue (%d) ", len(q.songs)))

	if metadataActive {
		q.metaSeq++
		q.refreshTrackMeta(q.metaSeq)
	}
}

// refreshTrackMeta fetches local metadata (rating/mark) for the current
// q.songs from the database in the background (see App.runAsync), then
// applies the result to the table -- keeps render() itself free of any
// DB I/O so opening/refreshing the Queue panel never blocks the UI
// goroutine on disk reads. seq guards against a stale fetch (started
// before a since-superseded queue change) overwriting newer data:
// application is skipped if q.metaSeq has moved on by the time it lands.
func (q *queuePanel) refreshTrackMeta(seq int) {
	db := q.app.metaDB
	files := make([]string, 0, len(q.songs))
	seen := map[string]bool{}
	for _, s := range q.songs {
		if seen[s.File] {
			continue
		}
		seen[s.File] = true
		files = append(files, s.File)
	}

	result := make(map[string]metadata.Track, len(files))
	q.app.runAsync(func() error {
		for _, f := range files {
			t, err := db.Get(f)
			if err != nil {
				return err
			}
			result[f] = t
		}
		return nil
	}, func() {
		if seq != q.metaSeq {
			return // queue changed again since this fetch started; discard
		}
		for file, t := range result {
			q.applyTrackMeta(file, t)
		}
	})
}

// applyTrackMeta records t as file's current metadata and, if it's
// showing anywhere in the currently rendered queue, repaints just its
// Playcount/Mark/Rating cells in place -- used both by refreshTrackMeta's
// background fetch and by a rating/mark/play-count write completing (see
// trackmetadata.go) to reflect a change without a full re-render or a
// synchronous DB round-trip on the UI goroutine. A file queued more than
// once (same track added twice) updates every matching row.
func (q *queuePanel) applyTrackMeta(file string, t metadata.Track) {
	q.metaCache[file] = t
	if q.cols.playcount < 0 && q.cols.mark < 0 && q.cols.rating < 0 {
		return
	}
	for i, s := range q.songs {
		if s.File != file {
			continue
		}
		row := i + queueHeaderRows
		if q.cols.playcount >= 0 {
			q.table.SetCell(row, q.cols.playcount, playCountCell(t.PlayCount))
		}
		if q.cols.mark >= 0 {
			q.table.SetCell(row, q.cols.mark, markCell(t.Mark))
		}
		if q.cols.rating >= 0 {
			q.table.SetCell(row, q.cols.rating, ratingCell(t.Rating))
		}
	}
}

// playCountCell renders a queue row's Plays column: the local play
// count as plain right-aligned text -- "0" (not blank) is already the
// correct, honest default for a track with no recorded plays yet, same
// as Rating's all-empty stars.
func playCountCell(count int) *tview.TableCell {
	return tview.NewTableCell(fmt.Sprintf("%d", count) + queueColumnGap).
		SetAlign(tview.AlignRight)
}

// queueRatingColor tints the Rating column with the active theme's
// Yellow, filled and unfilled stars alike (ratingStars renders both in
// one string) -- a single text color per cell is all tview.Table's
// TableCell supports, unlike a TextView's per-rune dynamic-color tags.
// Theme-derived (deriveColors), see theme.go.
var queueRatingColor tcell.Color

// ratingCell renders a queue row's Rating column from its local rating
// (0-5): ratingStars' filled+empty star glyphs, in gold.
func ratingCell(rating int) *tview.TableCell {
	return tview.NewTableCell(ratingStars(rating) + queueColumnGap).
		SetTextColor(queueRatingColor).
		SetAlign(tview.AlignRight)
}

// queueMarkTick is the glyph shown in the Mark column for a marked
// track -- a plain colored tick, not an icon/emoji, per explicit
// direction (colored ticks for Mark, distinct from the Lyr column's
// icon and from Rating's stars).
const queueMarkTick = "✓"

// markTickColors gives each mark_reason a distinct tick color, cycling
// through this palette by id -- the catalog is user-editable (see
// internal/metadata's seedMarkReasons doc comment) and open-ended, so
// there's no fixed reason-to-color mapping to hardcode; a deterministic
// cycle at least keeps the same reason the same color across a session
// and across restarts. Theme-derived (deriveColors, from the active
// theme's Red/Orange/Yellow/Magenta/Blue/Green), see theme.go.
var markTickColors []tcell.Color

// markColor picks mark's deterministic color from markTickColors by id --
// split out from markCell so trackInfoCard's metadata table (which shows
// a mark reason's full text, not just a tick) can color it the same way,
// and a given reason reads as the same color everywhere it appears.
// Callers are expected to have already checked mark != nil; a nil mark
// here just falls back to markTickColors[0] rather than panicking.
func markColor(mark *metadata.MarkReason) tcell.Color {
	if mark == nil || mark.ID < 1 {
		return markTickColors[0]
	}
	return markTickColors[(mark.ID-1)%int64(len(markTickColors))]
}

// markCell renders a queue row's Mark column: blank if mark is nil
// (unmarked -- the sensible default for a track with no opinion
// recorded yet, same as Rating's all-empty stars), otherwise
// queueMarkTick colored by the mark reason's id.
func markCell(mark *metadata.MarkReason) *tview.TableCell {
	if mark == nil {
		return tview.NewTableCell(queueColumnGap).SetAlign(tview.AlignRight)
	}
	return tview.NewTableCell(queueMarkTick + queueColumnGap).
		SetTextColor(markColor(mark)).
		SetAlign(tview.AlignRight)
}

// lyricsPresence reports which lyrics format(s) file has a matching
// sidecar for (see internal/lyrics), rechecked live against the real
// directory contents on every render -- there's no caching across
// render() calls, only within this one pass (lrcDirs/txtDirs), so a
// lyrics file added after a track was already queued shows up as soon as
// the Queue next repopulates (adding/removing/moving a track, or another
// client's own change via MPD's "playlist" idle event), without needing
// a special "recheck" code path of its own. Both false if musicDir is
// unset (the feature is inactive). Checks both formats independently
// (not lyricsAvailableFormats, lyrics.go -- that one has no caching of
// its own, fine for a single lookup but would re-list every directory
// twice per queued track here).
func (q *queuePanel) lyricsPresence(file string, lrcDirs, txtDirs map[string]map[string]string) (hasLRC, hasTxt bool) {
	if q.app.musicDir == "" {
		return false, false
	}
	dir := lyrics.Dir(q.app.musicDir, file)

	lrcCandidates, cached := lrcDirs[dir]
	if !cached {
		lrcCandidates = lyrics.LRCCandidates(dir)
		lrcDirs[dir] = lrcCandidates
	}
	_, hasLRC = lyrics.Match(file, lrcCandidates)

	txtCandidates, cached := txtDirs[dir]
	if !cached {
		txtCandidates = lyrics.Candidates(dir)
		txtDirs[dir] = txtCandidates
	}
	_, hasTxt = lyrics.Match(file, txtCandidates)
	return hasLRC, hasTxt
}

// lyricsCellText builds the Queue Lyr column's cell content from which
// formats are present: a single colored tick for just one format, two
// adjacent colored ticks -- no gap between them, explicit "overlap"
// request, the closest a character-grid terminal can get to that (tcell
// renders one rune per cell, each with its own single color -- there's
// no way to actually blend two colors within one cell the way a
// graphical UI could) -- (green LRC, orange TXT, in that order) when
// both exist, or "" for neither. Uses embedded tview color tags rather
// than TableCell.SetTextColor, since a cell can carry only one
// SetTextColor for its whole text but needs two different colors here.
func lyricsCellText(hasLRC, hasTxt bool) string {
	var ticks []string
	if hasLRC {
		ticks = append(ticks, fmt.Sprintf("[%s]%s[-]", lyricsLRCColor, lyricsTick))
	}
	if hasTxt {
		ticks = append(ticks, fmt.Sprintf("[%s]%s[-]", lyricsTxtColor, lyricsTick))
	}
	return strings.Join(ticks, "")
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
// Theme-derived (deriveColors), see theme.go for the actual
// format-to-palette-field mapping.
var formatColors map[string]tcell.Color

var defaultFormatColor tcell.Color

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
