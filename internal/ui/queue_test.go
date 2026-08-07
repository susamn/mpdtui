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

func TestQueueRenderShowsTitleAlbumArtistInOrder(t *testing.T) {
	a := newTestApp()
	a.queue.songs = []mpdclient.Song{
		{ID: 1, Title: "Bohemian Rhapsody", Album: "A Night at the Opera", Artist: "Queen"},
	}
	a.queue.render(-1)

	if got := a.queue.table.GetCell(0, 2).Text; got != "Bohemian Rhapsody"+queueColumnGap {
		t.Errorf("title cell = %q, want %q", got, "Bohemian Rhapsody"+queueColumnGap)
	}
	if got := a.queue.table.GetCell(0, 3).Text; got != "A Night at the Opera"+queueColumnGap {
		t.Errorf("album cell = %q, want %q", got, "A Night at the Opera"+queueColumnGap)
	}
	if got := a.queue.table.GetCell(0, 4).Text; got != "Queen"+queueColumnGap {
		t.Errorf("artist cell = %q, want %q", got, "Queen"+queueColumnGap)
	}
}

func TestQueueRenderTitleFallsBackToFilename(t *testing.T) {
	a := newTestApp()
	a.queue.songs = []mpdclient.Song{{ID: 1, File: "music/artist/untagged-track.mp3"}}
	a.queue.render(-1)

	if got := a.queue.table.GetCell(0, 2).Text; got != "untagged-track.mp3"+queueColumnGap {
		t.Errorf("title cell for an untagged track = %q, want the filename %q", got, "untagged-track.mp3"+queueColumnGap)
	}
}

func TestQueueRenderTruncatesEachColumnToItsOwnMax(t *testing.T) {
	a := newTestApp()
	a.queue.songs = []mpdclient.Song{{
		ID:     1,
		Title:  strings.Repeat("t", 50),
		Album:  strings.Repeat("a", 50),
		Artist: strings.Repeat("r", 50),
	}}
	a.queue.render(-1)

	wantTitle := strings.Repeat("t", queueTitleMaxLen-3) + "..." + queueColumnGap
	wantAlbum := strings.Repeat("a", queueAlbumMaxLen-3) + "..." + queueColumnGap
	wantArtist := strings.Repeat("r", queueArtistMaxLen-3) + "..." + queueColumnGap
	if got := a.queue.table.GetCell(0, 2).Text; got != wantTitle {
		t.Errorf("title cell = %q, want %q", got, wantTitle)
	}
	if got := a.queue.table.GetCell(0, 3).Text; got != wantAlbum {
		t.Errorf("album cell = %q, want %q", got, wantAlbum)
	}
	if got := a.queue.table.GetCell(0, 4).Text; got != wantArtist {
		t.Errorf("artist cell = %q, want %q", got, wantArtist)
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
	if row != 1 {
		t.Errorf("selected row = %d, want 1 (the row for song id 2)", row)
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
