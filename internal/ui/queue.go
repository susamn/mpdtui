package ui

import (
	"fmt"
	"strings"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"mpdtui/internal/lyrics"
	"mpdtui/internal/metadata"
	"mpdtui/internal/mpdclient"
	"mpdtui/internal/uitheme"
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
	t.SetSelectedStyle(uitheme.SelectedStyle())
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
	q.cols = newQueueColumns(app.musicDir != "", app.metaDB != nil)
	// Pin the identifying half of the layout; Table's own input handler
	// pans the rest with h/l, since columns aren't selectable here
	// (SetSelectable's second argument is false above).
	t.SetFixed(queueHeaderRows, q.cols.frozen)
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

// columnOnScreen reports whether a Queue column is currently drawn.
// Three things can hide one: the feature it belongs to being off (a -1
// index), horizontal scrolling having panned past it, or it falling off
// the right edge of the window. The first two are answered from the
// layout and Table's own column offset; the last from whether the
// column's header cell got a width the last time it was drawn, since
// tview only assigns one to cells it actually put on screen.
//
// Anything relying on a column's rendered position has to ask this
// first: an off-screen cell keeps whatever position it last had, so its
// coordinates look perfectly plausible while being stale by an
// arbitrary number of scroll steps.
func (q *queuePanel) columnOnScreen(col int) bool {
	if col < 0 {
		return false
	}
	if col >= q.cols.frozen {
		if _, offset := q.table.GetOffset(); col < q.cols.frozen+offset {
			return false
		}
	}
	_, _, width := q.table.GetCell(0, col).GetLastPosition()
	return width > 0
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

	// queueScrollWindowMax is the widest strip of the terminal the
	// pinned columns will give up so that the scrolling ones have
	// somewhere to appear. Freezing columns is only half a feature:
	// Table draws the frozen block first and pans the rest into
	// whatever is left over, so if Title/Album/Artist expand to fill
	// every last cell, h/l move the offset and nothing changes on
	// screen. 20 fits the widest scrolling column (Duration, 8) with
	// room for a second beside it.
	queueScrollWindowMax = 20

	// queueTitleFloor, queueAlbumFloor and queueArtistFloor are how far
	// the pinned text columns may be squeezed to buy that window. They
	// only bite on a genuinely cramped panel; anywhere with room to
	// spare, the proportional split lands well above them.
	queueTitleFloor  = 8
	queueAlbumFloor  = 6
	queueArtistFloor = 8
)

// queueTitleColor tints the Title cell with the active theme's Green,
// on top of its existing bold weight, so the track title reads as the
// row's primary/most prominent field at a glance. Set by theme.go's
// deriveColors (from palette), not a literal here -- see that file for
// the actual mapping from palette to every color in this block.
var queueTitleColor tcell.Color

// queueColumns holds the Queue table's column indices for one header/
// render pass, plus frozen: how many leading columns stay pinned on
// screen while the rest scroll past them (see Table.SetFixed).
//
// The layout is in two halves. Everything up to and including Rating --
// marker, position, Title, Lyr, Album, Artist, Rating -- identifies the
// track, and is on screen no matter how narrow the terminal gets.
// Everything after it (Plays, Mark, Year, Genre, Composer, Type,
// Duration) is a window the user pans with h/l, so a column that
// doesn't fit is one keypress away rather than gone.
//
// Lyr exists only when the lyrics feature is active, and Playcount/
// Mark/Rating only when metadata is (App.metaDB != nil); an absent
// column is -1. Nothing else is ever dropped for want of width -- that
// is what the scrolling half is for.
type queueColumns struct {
	lyr, title, album, artist, rating, playcount, mark, year, genre, composer, typ, duration int

	// frozen is what Table.SetFixed gets as its column count: the index
	// one past Rating (one past Artist without metadata), which is also
	// the first column that scrolls.
	frozen int
}

// newQueueColumns computes the column layout for one header/render
// pass. Marker (0) and position (1) are always fixed; Title always
// follows at 2; everything from there on is assigned sequentially.
// The result deliberately does not depend on the terminal width: every
// column always exists, and width decides only how much of the
// scrolling half is on screen at once.
func newQueueColumns(lyricsActive, metadataActive bool) queueColumns {
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
	if metadataActive {
		c.rating = next
		next++
	} else {
		c.rating = -1
	}

	// Everything assigned from here on scrolls.
	c.frozen = next

	if metadataActive {
		c.playcount = next
		next++
		c.mark = next
		next++
	} else {
		c.playcount = -1
		c.mark = -1
	}
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
// and every column from Composer rightward (Type/Duration) collapses
// back to its bare minimum width and bunches up on the left the moment
// the queue empties -- the actual dominant cause of
// "the header shrinks", more than the missing per-label gap above.
func setQueueHeader(t *tview.Table, cols queueColumns) {
	set := func(col int, text string, align int) {
		t.SetCell(0, col, tview.NewTableCell(text).
			SetAlign(align).
			SetTextColor(uitheme.TableHeaderFg()).
			SetBackgroundColor(uitheme.TableHeaderBg()).
			SetSelectable(false))
	}
	set(0, "", tview.AlignLeft)
	set(1, "", tview.AlignLeft)
	set(cols.title, "Title"+queueColumnGap, tview.AlignLeft)
	if cols.lyr >= 0 {
		set(cols.lyr, "Lyr", tview.AlignLeft)
	}
	set(cols.album, "Album"+queueColumnGap, tview.AlignLeft)
	set(cols.artist, "Artist"+queueColumnGap, tview.AlignLeft)
	if cols.rating >= 0 {
		set(cols.rating, "Rating"+queueColumnGap, tview.AlignRight)
	}
	if cols.playcount >= 0 {
		set(cols.playcount, "Plays"+queueColumnGap, tview.AlignRight)
	}
	if cols.mark >= 0 {
		set(cols.mark, "Mark"+queueColumnGap, tview.AlignRight)
	}
	set(cols.year, "Year"+queueColumnGap, tview.AlignLeft)
	set(cols.genre, "Genre"+queueColumnGap, tview.AlignLeft)
	set(cols.composer, "Composer"+queueColumnGap, tview.AlignLeft)
	t.GetCell(0, cols.composer).SetExpansion(1)
	set(cols.typ, "Type"+formatGap, tview.AlignRight)
	set(cols.duration, "Duration", tview.AlignRight)
}

// queueFrozenWidth returns the width (runes) consumed by the pinned
// half of the Queue table other than Title/Album/Artist themselves --
// marker, position, the table's own border, plus Lyr when lyricsActive
// and Rating when metadataActive. It is the budget queueColumnTruncation
// sizes the three text columns against, and it deliberately counts only
// frozen columns: everything past Rating scrolls, so it must not compete
// for the width the pinned half needs to stay readable.
func queueFrozenWidth(lyricsActive, metadataActive bool) int {
	fixed := 2 + 3 + 2 // marker(2) + pos(3) + border(2)
	if lyricsActive {
		fixed += 3
	}
	if metadataActive {
		fixed += 8 // rating
	}
	return fixed
}

// queueColumnTruncation calculates the maximum text lengths (runes) for
// Title, Album, and Artist -- the only pinned columns whose width is
// negotiable. They share whatever the terminal has left after
// queueFrozenWidth, split by a fixed ratio and clamped between a floor
// (so a very narrow terminal still shows something of each) and the
// standard caps (so a very wide one doesn't stretch them absurdly).
//
// The scrolling columns are not considered here at all: they no longer
// have to fit, so letting them shrink Title/Album/Artist would be
// paying a price for nothing.
func queueColumnTruncation(width int, lyricsActive, metadataActive bool) (titleLen, albumLen, artistLen int) {
	if width <= 0 {
		return queueTitleCompactMaxLen, queueAlbumCompactMaxLen, queueArtistCompactMaxLen
	}
	avail := width - queueFrozenWidth(lyricsActive, metadataActive)
	if avail <= 0 {
		return queueTitleFloor, queueAlbumFloor, queueArtistFloor
	}

	// Hold back a strip for the scrolling half before splitting the
	// rest, but never more than what is there above the floors: on a
	// panel too cramped to afford a window at all, the pinned columns
	// keep everything and the layout degrades to not scrolling rather
	// than to being unreadable.
	floors := queueTitleFloor + queueAlbumFloor + queueArtistFloor + 3*len(queueColumnGap)
	reserve := avail - floors
	if reserve < 0 {
		reserve = 0
	}
	if reserve > queueScrollWindowMax {
		reserve = queueScrollWindowMax
	}
	budget := avail - reserve

	// Distribute the budget proportionally: Title ~38%, Album ~26%, Artist ~36%
	// Subtract 2 per column for queueColumnGap
	tLen := (budget*38)/100 - 2
	aLen := (budget*26)/100 - 2
	arLen := (budget*36)/100 - 2

	if tLen < queueTitleFloor {
		tLen = queueTitleFloor
	}
	if aLen < queueAlbumFloor {
		aLen = queueAlbumFloor
	}
	if arLen < queueArtistFloor {
		arLen = queueArtistFloor
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

	cols := newQueueColumns(lyricsActive, metadataActive)
	q.cols = cols
	q.table.SetFixed(queueHeaderRows, cols.frozen)
	setQueueHeader(q.table, cols)
	// lrcDirs/txtDirs cache internal/lyrics.LRCCandidates/Candidates per
	// directory for the duration of this one render pass only (no
	// caching across renders, see lyricsPresence) -- multiple queued
	// tracks from the same album share a directory, so this avoids
	// re-listing it (once per format) more than once per directory.
	lrcDirs := map[string]map[string]string{}
	txtDirs := map[string]map[string]string{}

	titleMaxLen, albumMaxLen, artistMaxLen := queueColumnTruncation(w, lyricsActive, metadataActive)

	for i, s := range q.songs {
		row := i + queueHeaderRows
		title := s.Title
		if title == "" {
			title = baseName(s.File)
		}
		titleText := cellText(title, titleMaxLen)
		q.table.SetCell(row, 0, queueGutterCell(s.ID == curID, q.metaCache[s.File].Rating))
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
		q.table.SetCell(row, cols.album, tview.NewTableCell(cellText(s.Album, albumMaxLen)+queueColumnGap))

		q.table.SetCell(row, cols.artist, tview.NewTableCell(cellText(s.Artist, artistMaxLen)+queueColumnGap))

		if cols.playcount >= 0 || cols.mark >= 0 || cols.rating >= 0 {
			// Whatever's cached so far (possibly the zero-value Track, if
			// the background fetch below hasn't landed yet) -- never a
			// direct DB read here, so render() itself never blocks on I/O.
			meta := q.metaCache[s.File]
			if cols.playcount >= 0 {
				q.table.SetCell(row, cols.playcount, playCountCell(meta.PlayCount))
			}
			if cols.mark >= 0 {
				q.table.SetCell(row, cols.mark, markCell(meta.Marks))
			}
			if cols.rating >= 0 {
				q.table.SetCell(row, cols.rating, ratingCell(meta.Rating))
			}
		}
		q.table.SetCell(row, cols.year, tview.NewTableCell(yearFromDate(s.Date)+queueColumnGap))
		q.table.SetCell(row, cols.genre, tview.NewTableCell(cellText(s.Genre, queueGenreMaxLen)+queueColumnGap))
		q.table.SetCell(row, cols.composer, tview.NewTableCell(cellText(s.Composer, queueComposerMaxLen)+queueColumnGap).
			SetExpansion(1))
		q.table.SetCell(row, cols.typ, formatTagCell(s.File))
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
	for i, s := range q.songs {
		if s.File != file {
			continue
		}
		row := i + queueHeaderRows
		// The gutter star is repainted unconditionally: unlike the
		// metadata columns it survives every layout, so a narrow
		// terminal that has dropped the Rating column still shows it.
		q.table.SetCell(row, 0, queueGutterCell(s.ID == q.currentID, t.Rating))
		if q.cols.playcount >= 0 {
			q.table.SetCell(row, q.cols.playcount, playCountCell(t.PlayCount))
		}
		if q.cols.mark >= 0 {
			q.table.SetCell(row, q.cols.mark, markCell(t.Marks))
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

// queueGutterStarMin is the lowest rating that earns a gutter star.
// "More than 3 stars" -- so 4 and 5, the two tiers worth spotting while
// scrolling; 3 is a middling rating and starring it would leave most of
// a rated library flagged, which flags nothing.
const queueGutterStarMin = 4

// queueStarTopColor and queueStarHighColor tint the gutter star by
// rating: 5 stars get the Rating column's own color at full strength, 4
// stars a weakened version of that same color, so the tiers are told
// apart by shade rather than by a second glyph and the gutter reads as a
// condensed version of that column.
//
// Both are derived from one palette field (see uitheme.RecedeFrom),
// deliberately: the first version of this used the theme's "yellow" and
// "bright_yellow" for the two tiers, and on most real themes those are
// the same color or near enough that the tiers were indistinguishable.
// queueStarMinRatio is the luminance separation the weaker tier is
// guaranteed to reach -- well past the ~1.2x where two shades stop being
// tellable apart at the size of a single glyph.
// Theme-derived (deriveColors), see theme.go.
var (
	queueStarTopColor  tcell.Color
	queueStarHighColor tcell.Color
)

// queueStarMinRatio trades off against the 4-star glyph's own visibility:
// the weaker tier is dimmed by mixing it toward the background, so
// separating the tiers further necessarily leaves less contrast between
// the 4-star star and the panel behind it. Measured across the 15
// Omarchy themes on hand, this value separates the tiers by at least
// 2.23x while still leaving the dimmest 4-star 2.01x above its own
// background. Raising it to 3.0 would buy 3.01x separation at the cost
// of dropping that to 1.52x, which is too faint to pick out reliably.
const queueStarMinRatio = 2.2

func queueStarColor(rating int) tcell.Color {
	if rating >= 5 {
		return queueStarTopColor
	}
	return queueStarHighColor
}

// queueGutterCell builds a queue row's column 0: the two columns between
// the panel border and the index number.
//
// That space already existed -- it held the "▶ " playing marker and its
// trailing pad -- so the star goes in the pad rather than widening
// anything. Position is fixed (marker first, star second) so stars line
// up vertically down the panel whether or not a row is the playing one.
//
// The marker and the star necessarily share a color, since a
// tview.TableCell carries one text color for the whole cell and
// splitting this into two columns would cost a separator column of real
// width. So a starred row's ▶ takes the star's tint; an unstarred row is
// left at the default color exactly as before.
func queueGutterCell(playing bool, rating int) *tview.TableCell {
	marker := " "
	if playing {
		marker = queuePlayingMarker
	}
	if rating < queueGutterStarMin {
		return tview.NewTableCell(marker + " ")
	}
	return tview.NewTableCell(marker + ratingStarFilled).SetTextColor(queueStarColor(rating))
}

// queuePlayingMarker flags the currently playing row in the gutter.
const queuePlayingMarker = "▶"

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
// An out-of-range id falls back to markTickColors[0] rather than
// panicking.
func markColor(mark metadata.MarkReason) tcell.Color {
	if mark.ID < 1 {
		return markTickColors[0]
	}
	return markTickColors[(mark.ID-1)%int64(len(markTickColors))]
}

// queueMarkTicksMax caps how many ticks a row shows. A track can carry
// every mark in the catalog, and the column is sized by its widest cell,
// so without a cap one heavily-marked track would widen the column for
// the whole queue. Past the cap the count is shown instead, which is
// both narrower and more informative than a row of identical ticks.
const queueMarkTicksMax = 3

// markCell renders a queue row's Mark column: one tick per mark, each in
// that mark's own color, or blank when unmarked (the sensible default
// for a track with no opinion recorded yet, same as Rating's all-empty
// stars).
//
// Per-tick color needs dynamic color tags rather than the cell's own
// SetTextColor, which is a single color for the whole cell. tview.Table
// always parses tags in cell text (see formatColors' note on why "[MP3]"
// cannot be written literally), so this works -- and is in fact the only
// way to get more than one color into a cell.
func markCell(marks []metadata.MarkReason) *tview.TableCell {
	if len(marks) == 0 {
		return tview.NewTableCell(queueColumnGap).SetAlign(tview.AlignRight)
	}
	if len(marks) > queueMarkTicksMax {
		// Colored by the first mark, since a count cannot be striped.
		return tview.NewTableCell(fmt.Sprintf("[%s]%s%d[-]%s",
			markColor(marks[0]).String(), queueMarkTick, len(marks), queueColumnGap)).
			SetAlign(tview.AlignRight)
	}
	var b strings.Builder
	for _, m := range marks {
		fmt.Fprintf(&b, "[%s]%s[-]", markColor(m).String(), queueMarkTick)
	}
	b.WriteString(queueColumnGap)
	return tview.NewTableCell(b.String()).SetAlign(tview.AlignRight)
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
// cellText prepares MPD-provided text for a table cell: truncated to the
// column's budget, then made safe to lay out (see splitConjuncts). Every
// queue column carrying tag text goes through this, so no one column can
// be the one that forgets and knocks the row's alignment out.
func cellText(s string, max int) string {
	return splitConjuncts(truncateWithEllipsis(s, max))
}

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
		if q.table.GetCell(i+queueHeaderRows, 0) == nil {
			continue
		}
		// Rebuilt rather than SetText'd: the gutter also carries the
		// rating star, so editing just the text here would drop it off
		// every row each time the playing track changes.
		q.table.SetCell(i+queueHeaderRows, 0, queueGutterCell(s.ID == id, q.metaCache[s.File].Rating))
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

// centerRowOffset is the Table row offset (see Table.SetOffset) that puts
// row at the vertical middle of a viewport height lines tall. tview's own
// scrolling only ever moves a row just far enough to *be* visible, so
// jumping to a track otherwise parks it on the very first or very last
// line, with no surrounding queue visible to place it in.
//
// A negative offset clamps to 0: a row in the first half-screen simply
// can't be centered, and there's no scrolling further up than the top.
// The bottom end needs no clamp of its own -- tview's Draw pins the
// offset to the last screenful once it runs past the end (its trackEnd
// handling), which is the same "as close to centered as the list allows"
// answer.
func centerRowOffset(row, height int) int {
	off := row - height/2
	if off < 0 {
		return 0
	}
	return off
}

// centerSelection scrolls the Queue so the currently selected row sits
// vertically centered. Call it after Select (jumpToCurrent and friends),
// never instead of it: this only moves the viewport, not the selection.
//
// The offset survives the next draw because a centered row is, by
// definition, still visible -- Table.Draw's clamp-to-selection only
// overrides the offset when the selection would otherwise fall outside
// the viewport. A no-op before the first draw, when the table has no
// height to center within yet.
func (q *queuePanel) centerSelection() {
	_, _, _, h := q.table.GetInnerRect()
	if h <= 0 {
		return
	}
	row, _ := q.table.GetSelection()
	q.table.SetOffset(centerRowOffset(row, h), 0)
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

// indexOf locates song in the queue, returning -1 if it is not there.
//
// Matches on MPD's song id first, which is unique per queue entry, and
// only falls back to the file path for a song that carries no id (one
// resolved from the Library rather than the queue). The fallback cannot
// be the primary: the same file may legitimately sit at several
// positions in a queue, and the first of them is not necessarily the one
// being asked about.
func (q *queuePanel) indexOf(song mpdclient.Song) int {
	if song.ID > 0 {
		for i, s := range q.songs {
			if s.ID == song.ID {
				return i
			}
		}
	}
	if song.File == "" {
		return -1
	}
	for i, s := range q.songs {
		if s.File == song.File {
			return i
		}
	}
	return -1
}

func (q *queuePanel) selectedSong() (mpdclient.Song, bool) {
	row, _ := q.table.GetSelection()
	i := row - queueHeaderRows
	if i < 0 || i >= len(q.songs) {
		return mpdclient.Song{}, false
	}
	return q.songs[i], true
}
