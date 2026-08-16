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
	q.cols = newQueueColumns(app.musicDir != "", app.metaDB != nil)
	setQueueHeader(t, q.cols)

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
	lyr, title, album, artist, year, genre, composer, playcount, mark, rating, typ, duration int
}

// newQueueColumns computes the column layout for one header/render pass.
// Marker (0) and position (1) are always fixed; Title always follows at
// 2; everything from there on is assigned sequentially, with Lyr
// included only when lyricsActive and Playcount/Mark/Rating included
// only when metadataActive (App.metaDB != nil) -- same "don't reserve
// space for a column that will never show anything" reasoning as Lyr.
// Playcount/Mark/Rating sit right before Type, in that order, per
// explicit request (Playcount ahead of Mark, itself ahead of Rating).
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
	c.year = next
	next++
	c.genre = next
	next++
	c.composer = next
	next++
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
	set(cols.year, "Year"+queueColumnGap, tview.AlignLeft)
	set(cols.genre, "Genre"+queueColumnGap, tview.AlignLeft)
	set(cols.composer, "Composer"+queueColumnGap, tview.AlignLeft)
	t.GetCell(0, cols.composer).SetExpansion(1)
	if cols.playcount >= 0 {
		set(cols.playcount, "Plays"+queueColumnGap, tview.AlignRight)
	}
	if cols.mark >= 0 {
		set(cols.mark, "Mark"+queueColumnGap, tview.AlignRight)
	}
	if cols.rating >= 0 {
		set(cols.rating, "Rating"+queueColumnGap, tview.AlignRight)
	}
	set(cols.typ, "Type"+formatGap, tview.AlignRight)
	set(cols.duration, "Duration", tview.AlignRight)
}

func (q *queuePanel) render(curID int) {
	q.table.Clear()
	lyricsActive := q.app.musicDir != ""
	metadataActive := q.app.metaDB != nil
	cols := newQueueColumns(lyricsActive, metadataActive)
	q.cols = cols
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
			// other column here -- its only content is ever lyricsTick or
			// "", so tview.Table's own auto-sizing-to-content already
			// makes the column exactly as wide as the tick and no wider
			// (the explicit ask: "the column width should only take to
			// contain the icon").
			lyrCell := tview.NewTableCell("")
			if q.hasLyrics(s.File, lyricsDirs) {
				lyrCell = tview.NewTableCell(lyricsTick).SetTextColor(lyricsTickColor)
			}
			q.table.SetCell(row, cols.lyr, lyrCell)
		}
		q.table.SetCell(row, cols.album, tview.NewTableCell(truncateWithEllipsis(s.Album, queueAlbumMaxLen)+queueColumnGap))
		q.table.SetCell(row, cols.artist, tview.NewTableCell(truncateWithEllipsis(s.Artist, queueArtistMaxLen)+queueColumnGap))
		q.table.SetCell(row, cols.year, tview.NewTableCell(yearFromDate(s.Date)+queueColumnGap))
		q.table.SetCell(row, cols.genre, tview.NewTableCell(truncateWithEllipsis(s.Genre, queueGenreMaxLen)+queueColumnGap))
		q.table.SetCell(row, cols.composer, tview.NewTableCell(truncateWithEllipsis(s.Composer, queueComposerMaxLen)+queueColumnGap).
			SetExpansion(1))
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

// queueRatingColor tints the Rating column gold, filled and unfilled
// stars alike (ratingStars renders both in one string) -- a single
// text color per cell is all tview.Table's TableCell supports, unlike a
// TextView's per-rune dynamic-color tags.
var queueRatingColor = tcell.NewRGBColor(0xFF, 0xD7, 0x00)

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
// and across restarts.
var markTickColors = []tcell.Color{
	tcell.ColorRed,
	tcell.NewRGBColor(0xFF, 0xA5, 0x00), // orange
	tcell.ColorGold,
	tcell.ColorFuchsia,
	tcell.ColorDeepSkyBlue,
	tcell.ColorLime,
}

// markCell renders a queue row's Mark column: blank if mark is nil
// (unmarked -- the sensible default for a track with no opinion
// recorded yet, same as Rating's all-empty stars), otherwise
// queueMarkTick colored by the mark reason's id.
func markCell(mark *metadata.MarkReason) *tview.TableCell {
	if mark == nil {
		return tview.NewTableCell(queueColumnGap).SetAlign(tview.AlignRight)
	}
	color := markTickColors[0]
	if mark.ID >= 1 {
		color = markTickColors[(mark.ID-1)%int64(len(markTickColors))]
	}
	return tview.NewTableCell(queueMarkTick + queueColumnGap).
		SetTextColor(color).
		SetAlign(tview.AlignRight)
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
