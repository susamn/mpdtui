package ui

import (
	"strings"
	"testing"
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
		{2, "Title", tview.AlignLeft},
		{3, "Album", tview.AlignLeft},
		{4, "Artist", tview.AlignLeft},
		{5, "Year", tview.AlignLeft},
		{6, "Genre", tview.AlignLeft},
		{7, "Composer", tview.AlignLeft},
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
		{2, "Title"},
		{3, "Lyr"},
		{4, "Album"},
		{5, "Artist"},
		{6, "Year"},
		{7, "Genre"},
		{8, "Composer"},
		{10, "Duration"},
	}
	for _, w := range wantHeaders {
		if got := a.queue.table.GetCell(0, w.col).Text; got != w.text {
			t.Errorf("header col %d text = %q, want %q", w.col, got, w.text)
		}
	}
}

// TestNewQueueColumnsOmitsLyrWhenInactive is a pure unit test of the
// column-layout logic underlying both TestQueueHeaderRowLabelsAndAlignment
// and TestQueueHeaderRowIncludesLyrColumnWhenLyricsActive above --
// lyr == -1 is the "no such column" sentinel render()/setQueueHeader
// check before ever touching that index.
func TestNewQueueColumnsOmitsLyrWhenInactive(t *testing.T) {
	cols := newQueueColumns(false)
	if cols.lyr != -1 {
		t.Errorf("lyr = %d, want -1 (no Lyr column when lyrics is inactive)", cols.lyr)
	}
	want := queueColumns{lyr: -1, title: 2, album: 3, artist: 4, year: 5, genre: 6, composer: 7, typ: 8, duration: 9}
	if cols != want {
		t.Errorf("newQueueColumns(false) = %+v, want %+v", cols, want)
	}
}

func TestNewQueueColumnsIncludesLyrWhenActive(t *testing.T) {
	cols := newQueueColumns(true)
	want := queueColumns{lyr: 3, title: 2, album: 4, artist: 5, year: 6, genre: 7, composer: 8, typ: 9, duration: 10}
	if cols != want {
		t.Errorf("newQueueColumns(true) = %+v, want %+v", cols, want)
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

	if got := a.queue.table.GetCell(0, 2).Text; got != "Title" {
		t.Errorf("header after a second render = %q, want %q", got, "Title")
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
}
