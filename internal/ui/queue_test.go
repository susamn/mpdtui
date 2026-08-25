package ui

import (
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"mpdtui/internal/mpdclient"
)

// cellFg reports the foreground color tview will actually draw for cell,
// mirroring tview's own resolution: SetTextColor writes to the legacy
// Color field if Style was still the zero value at call time, or into
// Style otherwise -- which one depends on whether applyTheme() has
// already mutated the global tview.Styles in this test binary, since
// NewTableCell seeds Style from those globals. Checking only one side
// makes the test's outcome depend on execution order across the whole
// package; this checks whichever side tview itself would use.
func cellFg(cell *tview.TableCell) tcell.Color {
	if cell.Style == tcell.StyleDefault {
		return cell.Color
	}
	fg, _, _ := cell.Style.Decompose()
	return fg
}

// cellBg/cellBold mirror cellFg's same dual-branch resolution, for
// background color and the bold attribute respectively.
func cellBg(cell *tview.TableCell) tcell.Color {
	if cell.Style == tcell.StyleDefault {
		return cell.BackgroundColor
	}
	_, bg, _ := cell.Style.Decompose()
	return bg
}

func cellBold(cell *tview.TableCell) bool {
	if cell.Style == tcell.StyleDefault {
		return cell.Attributes&tcell.AttrBold != 0
	}
	_, _, attrs := cell.Style.Decompose()
	return attrs&tcell.AttrBold != 0
}

func TestFormatTagCellKnownFormatUsesItsColor(t *testing.T) {
	cell := formatTagCell("track.flac")
	if got, want := cell.Text, "FLAC"+formatGap; got != want {
		t.Errorf("text = %q, want %q", got, want)
	}
	if got, want := cellFg(cell), formatColors["FLAC"]; got != want {
		t.Errorf("fg = %v, want %v", got, want)
	}
	if got := cell.Align; got != tview.AlignRight {
		t.Errorf("align = %v, want AlignRight", got)
	}
}

func TestFormatTagCellUnknownFormatUsesDefaultColor(t *testing.T) {
	cell := formatTagCell("track.xyz")
	if got, want := cell.Text, "XYZ"+formatGap; got != want {
		t.Errorf("text = %q, want %q", got, want)
	}
	if got, want := cellFg(cell), defaultFormatColor; got != want {
		t.Errorf("fg = %v, want default %v", got, want)
	}
}

func TestFormatTagCellNoExtensionRendersEmpty(t *testing.T) {
	cell := formatTagCell("no-extension")
	if got := cell.Text; got != "" {
		t.Errorf("text = %q, want empty", got)
	}
}

// TestFormatTagCellTextHasNoBracket guards against a real regression:
// tview.Table has no way to disable dynamic-color tag parsing (unlike
// TextView's SetDynamicColors), so any "[...]" in cell text is always
// parsed as a style/region tag and silently vanishes from the rendered
// output instead of showing as literal brackets.
func TestFormatTagCellTextHasNoBracket(t *testing.T) {
	for _, file := range []string{"track.flac", "track.mp3", "track.unknownformat"} {
		if cell := formatTagCell(file); strings.ContainsAny(cell.Text, "[]") {
			t.Errorf("formatTagCell(%q).Text = %q contains a bracket -- tview will silently swallow it as a tag", file, cell.Text)
		}
	}
}

func TestTruncateWithEllipsisLeavesShortStringsAlone(t *testing.T) {
	if got := truncateWithEllipsis("short", 30); got != "short" {
		t.Errorf("truncateWithEllipsis(%q, 30) = %q, want unchanged", "short", got)
	}
}

func TestTruncateWithEllipsisExactLengthUnchanged(t *testing.T) {
	s := strings.Repeat("x", 30)
	if got := truncateWithEllipsis(s, 30); got != s {
		t.Errorf("truncateWithEllipsis at exactly max length changed: got %q", got)
	}
}

func TestTruncateWithEllipsisTruncatesLongStrings(t *testing.T) {
	s := strings.Repeat("x", 50)
	got := truncateWithEllipsis(s, 30)
	want := strings.Repeat("x", 27) + "..."
	if got != want {
		t.Errorf("truncateWithEllipsis(50 x's, 30) = %q, want %q", got, want)
	}
	if len([]rune(got)) != 30 {
		t.Errorf("truncated length = %d runes, want exactly 30", len([]rune(got)))
	}
}

// TestTruncateWithEllipsisIsRuneSafe guards against splitting a multi-byte
// character mid-codepoint: byte-slicing instead of rune-slicing here would
// corrupt the trailing character(s) of tags containing non-ASCII text,
// which this library's real tags do (e.g. "Bárbara Martínez", "Céline Dion").
func TestTruncateWithEllipsisIsRuneSafe(t *testing.T) {
	s := strings.Repeat("é", 50) // each 'é' is 2 bytes in UTF-8
	got := truncateWithEllipsis(s, 30)
	if !utf8.ValidString(got) {
		t.Fatalf("truncateWithEllipsis produced invalid UTF-8: %q", got)
	}
	want := strings.Repeat("é", 27) + "..."
	if got != want {
		t.Errorf("truncateWithEllipsis(50 é's, 30) = %q, want %q", got, want)
	}
}

// TestYearFromDate covers the helper both the Queue's Year column and the
// track info card use to shrink MPD's "Date" tag down to just a year.
func TestYearFromDate(t *testing.T) {
	cases := map[string]string{
		"1975-10-31": "1975",
		"1992":       "1992",
		"":           "",
		"92":         "92", // shorter than 4: passed through unchanged, not padded
	}
	for date, want := range cases {
		if got := yearFromDate(date); got != want {
			t.Errorf("yearFromDate(%q) = %q, want %q", date, got, want)
		}
	}
}

func TestQueueHeaderRowLabelsAndAlignment(t *testing.T) {
	a := newTestApp()  // musicDir == "" -- no Lyr column
	a.queue.render(-1) // no songs -- header should still be there

	wantHeaders := []struct {
		col   int
		text  string
		align int
	}{
		{0, "", tview.AlignLeft},
		{1, "", tview.AlignLeft},
		{2, "Title" + queueColumnGap, tview.AlignLeft},
		{3, "Album" + queueColumnGap, tview.AlignLeft},
		{4, "Artist" + queueColumnGap, tview.AlignLeft},
		{5, "Year" + queueColumnGap, tview.AlignLeft},
		{6, "Genre" + queueColumnGap, tview.AlignLeft},
		{7, "Composer" + queueColumnGap, tview.AlignLeft},
		{8, "Type" + formatGap, tview.AlignRight}, // formatGap matches formatTagCell's data-cell padding, see setQueueHeader's comment
		{9, "Duration", tview.AlignRight},
	}
	for _, w := range wantHeaders {
		cell := a.queue.table.GetCell(0, w.col)
		if cell.Text != w.text {
			t.Errorf("header col %d text = %q, want %q", w.col, cell.Text, w.text)
		}
		if cell.Align != w.align {
			t.Errorf("header col %d align = %d, want %d", w.col, cell.Align, w.align)
		}
	}
}

// TestQueueHeaderRowIncludesLyrColumnWhenLyricsActive is the counterpart
// to the test above: with a valid musicDir configured, Lyr appears right
// after Title and everything else shifts one column right.
func TestQueueHeaderRowIncludesLyrColumnWhenLyricsActive(t *testing.T) {
	a := newTestAppWithMusicDir(t.TempDir())
	a.queue.render(-1)

	wantHeaders := []struct {
		col  int
		text string
	}{
		{2, "Title" + queueColumnGap},
		{3, "Lyr"},
		{4, "Album" + queueColumnGap},
		{5, "Artist" + queueColumnGap},
		{6, "Year" + queueColumnGap},
		{7, "Genre" + queueColumnGap},
		{8, "Composer" + queueColumnGap},
		{10, "Duration"},
	}
	for _, w := range wantHeaders {
		if got := a.queue.table.GetCell(0, w.col).Text; got != w.text {
			t.Errorf("header col %d text = %q, want %q", w.col, got, w.text)
		}
	}
}

// TestQueueHeaderIncludesMarkAndRatingWhenMetadataActive is the
// metadata counterpart to TestQueueHeaderRowIncludesLyrColumnWhenLyricsActive:
// Plays, Mark and Rating appear right before Type, in that order and
// right-aligned like Type/Duration, only when the track-metadata feature
// is active (App.metaDB != nil).
func TestQueueHeaderIncludesMarkAndRatingWhenMetadataActive(t *testing.T) {
	a := newTestAppWithMetaDB(t)
	a.queue.render(-1)

	wantHeaders := []struct {
		col   int
		text  string
		align int
	}{
		{7, "Composer" + queueColumnGap, tview.AlignLeft},
		{8, "Plays" + queueColumnGap, tview.AlignRight},
		{9, "Mark" + queueColumnGap, tview.AlignRight},
		{10, "Rating" + queueColumnGap, tview.AlignRight},
		{11, "Type" + formatGap, tview.AlignRight},
		{12, "Duration", tview.AlignRight},
	}
	for _, w := range wantHeaders {
		cell := a.queue.table.GetCell(0, w.col)
		if cell.Text != w.text {
			t.Errorf("header col %d text = %q, want %q", w.col, cell.Text, w.text)
		}
		if cell.Align != w.align {
			t.Errorf("header col %d align = %d, want %d", w.col, cell.Align, w.align)
		}
	}
}

// TestQueueRenderShowsDefaultUnratedUnmarkedRow covers the "sensible
// defaults" for a track with no local metadata row yet: "0" plays,
// all-empty gold stars in Rating (same glyphs ratingStars(0) already
// produces), a blank Mark cell -- not an error, not a placeholder icon.
func TestQueueRenderShowsDefaultUnratedUnmarkedRow(t *testing.T) {
	a := newTestAppWithMetaDB(t)
	a.queue.songs = []mpdclient.Song{{ID: 1, File: "artist/track.mp3"}}
	a.queue.render(-1)

	row := queueHeaderRows
	if got, want := a.queue.table.GetCell(row, a.queue.cols.playcount).Text, "0"+queueColumnGap; got != want {
		t.Errorf("default Plays cell = %q, want %q", got, want)
	}
	if got, want := a.queue.table.GetCell(row, a.queue.cols.rating).Text, ratingStars(0)+queueColumnGap; got != want {
		t.Errorf("default Rating cell = %q, want %q", got, want)
	}
	if got, want := a.queue.table.GetCell(row, a.queue.cols.mark).Text, queueColumnGap; got != want {
		t.Errorf("default Mark cell = %q, want %q (blank)", got, want)
	}
}

// TestNewQueueColumnsOmitsLyrWhenInactive is a pure unit test of the
// column-layout logic underlying both TestQueueHeaderRowLabelsAndAlignment
// and TestQueueHeaderRowIncludesLyrColumnWhenLyricsActive above --
// lyr == -1 is the "no such column" sentinel render()/setQueueHeader
// check before ever touching that index.
func TestNewQueueColumnsOmitsLyrWhenInactive(t *testing.T) {
	cols := newQueueColumns(false, false, false)
	if cols.lyr != -1 {
		t.Errorf("lyr = %d, want -1 (no Lyr column when lyrics is inactive)", cols.lyr)
	}
	want := queueColumns{compact: false, lyr: -1, title: 2, album: 3, artist: 4, year: 5, genre: 6, composer: 7, playcount: -1, mark: -1, rating: -1, typ: 8, duration: 9}
	if cols != want {
		t.Errorf("newQueueColumns(false, false, false) = %+v, want %+v", cols, want)
	}
}

func TestNewQueueColumnsIncludesLyrWhenActive(t *testing.T) {
	cols := newQueueColumns(true, false, false)
	want := queueColumns{compact: false, lyr: 3, title: 2, album: 4, artist: 5, year: 6, genre: 7, composer: 8, playcount: -1, mark: -1, rating: -1, typ: 9, duration: 10}
	if cols != want {
		t.Errorf("newQueueColumns(true, false, false) = %+v, want %+v", cols, want)
	}
}

// TestNewQueueColumnsIncludesMarkAndRatingWhenMetadataActive is the
// counterpart covering the new Playcount/Mark/Rating columns: they sit
// right before Type, in that order, and only exist when metadataActive
// (i.e. App.metaDB != nil) -- otherwise the layout is identical to Lyr's
// own "no such column" omission.
func TestNewQueueColumnsIncludesMarkAndRatingWhenMetadataActive(t *testing.T) {
	cols := newQueueColumns(false, true, false)
	want := queueColumns{compact: false, lyr: -1, title: 2, album: 3, artist: 4, year: 5, genre: 6, composer: 7, playcount: 8, mark: 9, rating: 10, typ: 11, duration: 12}
	if cols != want {
		t.Errorf("newQueueColumns(false, true, false) = %+v, want %+v", cols, want)
	}
}

func TestNewQueueColumnsIncludesLyrAndMarkAndRatingTogether(t *testing.T) {
	cols := newQueueColumns(true, true, false)
	want := queueColumns{compact: false, lyr: 3, title: 2, album: 4, artist: 5, year: 6, genre: 7, composer: 8, playcount: 9, mark: 10, rating: 11, typ: 12, duration: 13}
	if cols != want {
		t.Errorf("newQueueColumns(true, true, false) = %+v, want %+v", cols, want)
	}
}

func TestNewQueueColumnsCompactOmitsYearGenreComposer(t *testing.T) {
	cols := newQueueColumns(false, false, true)
	if cols.year != -1 || cols.genre != -1 || cols.composer != -1 {
		t.Errorf("compact columns had year=%d, genre=%d, composer=%d, want all -1", cols.year, cols.genre, cols.composer)
	}
	want := queueColumns{compact: true, lyr: -1, title: 2, album: 3, artist: 4, year: -1, genre: -1, composer: -1, playcount: -1, mark: -1, rating: -1, typ: 5, duration: 6}
	if cols != want {
		t.Errorf("newQueueColumns(false, false, true) = %+v, want %+v", cols, want)
	}
}

func TestNewQueueColumnsCompactWithLyrAndMetadata(t *testing.T) {
	cols := newQueueColumns(true, true, true)
	want := queueColumns{compact: true, lyr: 3, title: 2, album: 4, artist: 5, year: -1, genre: -1, composer: -1, playcount: 6, mark: 7, rating: 8, typ: 9, duration: 10}
	if cols != want {
		t.Errorf("newQueueColumns(true, true, true) = %+v, want %+v", cols, want)
	}
}

func TestQueueHeaderRowCompactOmitsYearGenreComposer(t *testing.T) {
	a := newTestApp()
	a.queue.table.SetRect(0, 0, 80, 40)
	a.queue.render(-1)

	wantHeaders := []struct {
		col   int
		text  string
		align int
	}{
		{0, "", tview.AlignLeft},
		{1, "", tview.AlignLeft},
		{2, "Title" + queueColumnGap, tview.AlignLeft},
		{3, "Album" + queueColumnGap, tview.AlignLeft},
		{4, "Artist" + queueColumnGap, tview.AlignLeft},
		{5, "Type" + formatGap, tview.AlignRight},
		{6, "Duration", tview.AlignRight},
	}
	for _, w := range wantHeaders {
		cell := a.queue.table.GetCell(0, w.col)
		if cell.Text != w.text {
			t.Errorf("header col %d text = %q, want %q", w.col, cell.Text, w.text)
		}
		if cell.Align != w.align {
			t.Errorf("header col %d align = %d, want %d", w.col, cell.Align, w.align)
		}
	}
	if a.queue.table.GetCell(0, 4).Expansion != 1 {
		t.Errorf("Artist header cell in compact mode has expansion %d, want 1", a.queue.table.GetCell(0, 4).Expansion)
	}
}

func TestQueueRenderCompactOmitsYearGenreComposerAndExpandsArtist(t *testing.T) {
	a := newTestApp()
	a.queue.table.SetRect(0, 0, 80, 40)
	a.queue.songs = []mpdclient.Song{{
		ID:       1,
		Title:    "A Very Long Song Title Exceeding Max",
		Album:    "A Very Long Album Name Exceeding Max",
		Artist:   "A Very Long Artist Name Exceeding Max",
		Date:     "1999",
		Genre:    "Rock",
		Composer: "Composer Name",
		File:     "artist/track.mp3",
		Duration: 180 * time.Second,
	}}
	a.queue.render(-1)

	row := queueHeaderRows
	titleLen, albumLen, artistLen := queueColumnTruncation(80, false, false, true)
	wantTitle := truncateWithEllipsis("A Very Long Song Title Exceeding Max", titleLen) + queueColumnGap
	if got := a.queue.table.GetCell(row, 2).Text; got != wantTitle {
		t.Errorf("compact title cell = %q, want %q", got, wantTitle)
	}
	wantAlbum := truncateWithEllipsis("A Very Long Album Name Exceeding Max", albumLen) + queueColumnGap
	if got := a.queue.table.GetCell(row, 3).Text; got != wantAlbum {
		t.Errorf("compact album cell = %q, want %q", got, wantAlbum)
	}
	wantArtist := truncateWithEllipsis("A Very Long Artist Name Exceeding Max", artistLen) + queueColumnGap
	if got := a.queue.table.GetCell(row, 4).Text; got != wantArtist {
		t.Errorf("compact artist cell = %q, want %q", got, wantArtist)
	}
	if a.queue.table.GetCell(row, 4).Expansion != 1 {
		t.Errorf("Artist data cell in compact mode has expansion %d, want 1", a.queue.table.GetCell(row, 4).Expansion)
	}
	if got := a.queue.table.GetCell(row, 5).Text; got != "MP3"+formatGap {
		t.Errorf("compact type cell = %q, want %q", got, "MP3"+formatGap)
	}
	if got := a.queue.table.GetCell(row, 6).Text; got != "3:00" {
		t.Errorf("compact duration cell = %q, want %q", got, "3:00")
	}
}

func TestQueueColumnTruncationAcrossDifferentWidths(t *testing.T) {
	// Full layout on wide screens
	tLen, aLen, arLen := queueColumnTruncation(150, true, true, false)
	if tLen != queueTitleMaxLen || aLen != queueAlbumMaxLen || arLen != queueArtistMaxLen {
		t.Errorf("wide full truncation = (%d, %d, %d), want (%d, %d, %d)", tLen, aLen, arLen, queueTitleMaxLen, queueAlbumMaxLen, queueArtistMaxLen)
	}

	// 1080p @ 1.5x scale (approx 95 width with metadata and lyrics active)
	tLen, aLen, arLen = queueColumnTruncation(95, true, true, true)
	// Fixed width: 2(marker) + 3(pos) + 3(lyr) + 6(plays) + 3(mark) + 7(rating) + 6(type) + 6(duration) + 2(border) = 38
	// Text width: (tLen+2) + (aLen+2) + (arLen+2)
	totalWidth := 38 + (tLen + 2) + (aLen + 2) + (arLen + 2)
	if totalWidth > 95 {
		t.Errorf("95-width queue total column width = %d, exceeds available 95", totalWidth)
	}

	// Very narrow width (e.g. 60 width)
	tLen, aLen, arLen = queueColumnTruncation(60, true, true, true)
	if tLen < 12 || aLen < 8 || arLen < 12 {
		t.Errorf("narrow truncation dropped below floor: (%d, %d, %d)", tLen, aLen, arLen)
	}
}

func TestQueueDynamicCompactResizeOnDraw(t *testing.T) {
	a := newTestApp()
	a.queue.table.SetRect(0, 0, 150, 40)
	a.queue.render(-1)
	if a.queue.cols.compact {
		t.Errorf("initial wide width 150: cols.compact = true, want false")
	}

	// Shrink below threshold and trigger Draw on table (which executes SetDrawFunc)
	screen := tcell.NewSimulationScreen("")
	if err := screen.Init(); err != nil {
		t.Fatalf("screen.Init: %v", err)
	}
	defer screen.Fini()
	screen.SetSize(80, 24)

	a.queue.table.SetRect(0, 0, 80, 24)
	a.queue.table.Draw(screen)

	if !a.queue.cols.compact {
		t.Errorf("after shrinking to width 80: cols.compact = false, want true")
	}
}

func TestQueueHeaderRowStyledAndNotSelectable(t *testing.T) {
	a := newTestApp()
	a.queue.render(-1)

	cell := a.queue.table.GetCell(0, 2) // "Title"
	if got := cellFg(cell); got != queueHeaderFg {
		t.Errorf("header foreground = %v, want %v", got, queueHeaderFg)
	}
	if got := cellBg(cell); got != queueHeaderBg {
		t.Errorf("header background = %v, want %v", got, queueHeaderBg)
	}
	if !cell.NotSelectable {
		t.Error("header cell should not be selectable")
	}
}

// TestQueueHeaderSurvivesRerender guards against render() forgetting to
// rebuild the header after Table.Clear() wipes every cell including row 0.
func TestQueueHeaderSurvivesRerender(t *testing.T) {
	a := newTestApp()
	a.queue.songs = []mpdclient.Song{{ID: 1, Title: "First"}}
	a.queue.render(-1)
	a.queue.songs = []mpdclient.Song{{ID: 1, Title: "First"}, {ID: 2, Title: "Second"}}
	a.queue.render(-1) // a second render, exercising Clear() + rebuild again

	if got, want := a.queue.table.GetCell(0, 2).Text, "Title"+queueColumnGap; got != want {
		t.Errorf("header after a second render = %q, want %q", got, want)
	}
}

func TestQueueRenderTitleCellIsBold(t *testing.T) {
	a := newTestApp()
	a.queue.songs = []mpdclient.Song{{ID: 1, Title: "Bohemian Rhapsody"}}
	a.queue.render(-1)

	titleCell := a.queue.table.GetCell(queueHeaderRows, 2)
	if !cellBold(titleCell) {
		t.Error("track title cell should be bold")
	}
	if got := cellFg(titleCell); got != queueTitleColor {
		t.Errorf("track title cell color = %v, want queueTitleColor %v", got, queueTitleColor)
	}
	albumCell := a.queue.table.GetCell(queueHeaderRows, 3)
	if cellBold(albumCell) {
		t.Error("album cell should not be bold")
	}
}

func TestQueueRenderDurationCellRightAligned(t *testing.T) {
	a := newTestApp()
	a.queue.songs = []mpdclient.Song{{ID: 1, Title: "Track"}}
	a.queue.render(-1)

	if got := a.queue.table.GetCell(queueHeaderRows, 9).Align; got != tview.AlignRight {
		t.Errorf("duration cell align = %d, want AlignRight", got)
	}
}

func TestQueueRenderShowsTitleAlbumArtistInOrder(t *testing.T) {
	a := newTestApp()
	a.queue.songs = []mpdclient.Song{
		{ID: 1, Title: "Bohemian Rhapsody", Album: "A Night at the Opera", Artist: "Queen"},
	}
	a.queue.render(-1)

	if got := a.queue.table.GetCell(queueHeaderRows, 2).Text; got != "Bohemian Rhapsody"+queueColumnGap {
		t.Errorf("title cell = %q, want %q", got, "Bohemian Rhapsody"+queueColumnGap)
	}
	if got := a.queue.table.GetCell(queueHeaderRows, 3).Text; got != "A Night at the Opera"+queueColumnGap {
		t.Errorf("album cell = %q, want %q", got, "A Night at the Opera"+queueColumnGap)
	}
	if got := a.queue.table.GetCell(queueHeaderRows, 4).Text; got != "Queen"+queueColumnGap {
		t.Errorf("artist cell = %q, want %q", got, "Queen"+queueColumnGap)
	}
}

// TestQueueRenderComposerColumnExpands guards against the Type/Duration
// columns floating with dead space after them instead of sitting flush
// against the table's right edge: without any column carrying Expansion,
// tview.Table leaves leftover width undistributed (see Table.Draw's
// "If we have space left, distribute it" step, which only touches columns
// with Expansion > 0). Composer carries it now (not Artist) since it's
// the last flexible text column before Type/Duration.
func TestQueueRenderComposerColumnExpands(t *testing.T) {
	a := newTestApp()
	a.queue.songs = []mpdclient.Song{{ID: 1, Title: "Track", Artist: "Artist", Composer: "Composer"}}
	a.queue.render(-1)

	if got := a.queue.table.GetCell(queueHeaderRows, 7).Expansion; got != 1 {
		t.Errorf("composer cell Expansion = %d, want 1", got)
	}
	if got := a.queue.table.GetCell(queueHeaderRows, 4).Expansion; got != 0 {
		t.Errorf("artist cell Expansion = %d, want 0 (Composer carries it now, not Artist)", got)
	}
}

func TestQueueRenderTitleFallsBackToFilename(t *testing.T) {
	a := newTestApp()
	a.queue.songs = []mpdclient.Song{{ID: 1, File: "music/artist/untagged-track.mp3"}}
	a.queue.render(-1)

	if got := a.queue.table.GetCell(queueHeaderRows, 2).Text; got != "untagged-track.mp3"+queueColumnGap {
		t.Errorf("title cell for an untagged track = %q, want the filename %q", got, "untagged-track.mp3"+queueColumnGap)
	}
}

func TestQueueRenderTruncatesEachColumnToItsOwnMax(t *testing.T) {
	a := newTestApp()
	a.queue.songs = []mpdclient.Song{{
		ID:       1,
		Title:    strings.Repeat("t", 50),
		Album:    strings.Repeat("a", 50),
		Artist:   strings.Repeat("r", 50),
		Genre:    strings.Repeat("g", 50),
		Composer: strings.Repeat("c", 50),
	}}
	a.queue.render(-1)

	wantTitle := strings.Repeat("t", queueTitleMaxLen-3) + "..." + queueColumnGap
	wantAlbum := strings.Repeat("a", queueAlbumMaxLen-3) + "..." + queueColumnGap
	wantArtist := strings.Repeat("r", queueArtistMaxLen-3) + "..." + queueColumnGap
	wantGenre := strings.Repeat("g", queueGenreMaxLen-3) + "..." + queueColumnGap
	wantComposer := strings.Repeat("c", queueComposerMaxLen-3) + "..." + queueColumnGap
	if got := a.queue.table.GetCell(queueHeaderRows, 2).Text; got != wantTitle {
		t.Errorf("title cell = %q, want %q", got, wantTitle)
	}
	if got := a.queue.table.GetCell(queueHeaderRows, 3).Text; got != wantAlbum {
		t.Errorf("album cell = %q, want %q", got, wantAlbum)
	}
	if got := a.queue.table.GetCell(queueHeaderRows, 4).Text; got != wantArtist {
		t.Errorf("artist cell = %q, want %q", got, wantArtist)
	}
	if got := a.queue.table.GetCell(queueHeaderRows, 6).Text; got != wantGenre {
		t.Errorf("genre cell = %q, want %q", got, wantGenre)
	}
	if got := a.queue.table.GetCell(queueHeaderRows, 7).Text; got != wantComposer {
		t.Errorf("composer cell = %q, want %q", got, wantComposer)
	}
}

func TestQueueRenderShowsYearGenreComposer(t *testing.T) {
	a := newTestApp()
	a.queue.songs = []mpdclient.Song{
		{ID: 1, Title: "Bohemian Rhapsody", Date: "1975-10-31", Genre: "Rock", Composer: "F. Mercury"},
	}
	a.queue.render(-1)

	if got := a.queue.table.GetCell(queueHeaderRows, 5).Text; got != "1975"+queueColumnGap {
		t.Errorf("year cell = %q, want %q", got, "1975"+queueColumnGap)
	}
	if got := a.queue.table.GetCell(queueHeaderRows, 6).Text; got != "Rock"+queueColumnGap {
		t.Errorf("genre cell = %q, want %q", got, "Rock"+queueColumnGap)
	}
	if got := a.queue.table.GetCell(queueHeaderRows, 7).Text; got != "F. Mercury"+queueColumnGap {
		t.Errorf("composer cell = %q, want %q", got, "F. Mercury"+queueColumnGap)
	}
}

func TestQueueRenderYearHandlesPlainYearAndEmptyDate(t *testing.T) {
	a := newTestApp()
	a.queue.songs = []mpdclient.Song{
		{ID: 1, Title: "Plain year", Date: "1992"},
		{ID: 2, Title: "No date"},
	}
	a.queue.render(-1)

	if got := a.queue.table.GetCell(queueHeaderRows, 5).Text; got != "1992"+queueColumnGap {
		t.Errorf("year cell for a plain-year Date = %q, want %q", got, "1992"+queueColumnGap)
	}
	if got := a.queue.table.GetCell(queueHeaderRows+1, 5).Text; got != ""+queueColumnGap {
		t.Errorf("year cell for an empty Date = %q, want just the gap %q", got, queueColumnGap)
	}
}

func TestQueueJumpToCurrentSelectsThePlayingRow(t *testing.T) {
	a := newTestApp()
	a.queue.songs = []mpdclient.Song{
		{ID: 1, Title: "First"},
		{ID: 2, Title: "Second"},
		{ID: 3, Title: "Third"},
	}
	a.queue.render(-1)
	a.queue.setCurrent(2)

	if !a.queue.jumpToCurrent() {
		t.Fatal("jumpToCurrent() = false, want true (song id 2 is in the queue)")
	}
	row, _ := a.queue.table.GetSelection()
	if row != 1+queueHeaderRows {
		t.Errorf("selected row = %d, want %d (the row for song id 2)", row, 1+queueHeaderRows)
	}
}

func TestQueueJumpToCurrentFalseWhenNothingPlaying(t *testing.T) {
	a := newTestApp()
	a.queue.songs = []mpdclient.Song{{ID: 1, Title: "First"}}
	a.queue.render(-1)
	// currentID defaults to -1 (see newQueuePanel) -- setCurrent was never called.

	if a.queue.jumpToCurrent() {
		t.Error("jumpToCurrent() = true, want false when nothing is playing")
	}
}

func TestQueueJumpToCurrentFalseWhenCurrentIDNoLongerInQueue(t *testing.T) {
	a := newTestApp()
	a.queue.songs = []mpdclient.Song{{ID: 1, Title: "First"}}
	a.queue.render(-1)
	a.queue.setCurrent(99) // some id not present in songs

	if a.queue.jumpToCurrent() {
		t.Error("jumpToCurrent() = true, want false when the current id isn't in the queue (e.g. it was just removed)")
	}
}

// TestQueueRefreshStatsShowsLibraryTotals is an integration test (needs a
// live MPD server) since LibraryStats itself isn't pure -- it fetches
// from the library, not just formats already-known numbers.
func TestQueueRefreshStatsShowsLibraryTotals(t *testing.T) {
	a := &App{tv: tview.NewApplication(), client: dialOrSkip(t)}
	a.build()

	a.queue.refreshStats()

	text := a.queue.stats.GetText(true)
	if text == "" {
		t.Fatal("stats box text is empty after refreshStats")
	}
	for _, want := range []string{"Tracks:", "Artists:", "Playlists:"} {
		if !strings.Contains(text, want) {
			t.Errorf("stats text = %q, missing %q", text, want)
		}
	}

	// Each value is colored (see refreshStats's own comment on why these
	// specific colors), reusing the same constants Now Playing uses for
	// the same concepts.
	raw := a.queue.stats.GetText(false)
	for _, want := range []string{"[" + nowPlayingTrackColor + "::b]", "[" + nowPlayingArtistColor + "::b]", "[" + nowPlayingBarColor + "::b]"} {
		if !strings.Contains(raw, want) {
			t.Errorf("raw stats text = %q, missing color tag %q", raw, want)
		}
	}
}
