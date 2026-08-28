package ui

import (
	"strings"
	"testing"

	"github.com/rivo/tview"
)

func TestSearchResultColumnsPerKind(t *testing.T) {
	cases := []struct {
		kind globalSearchKind
		want int
	}{
		{globalSearchTrack, 2},
		{globalSearchLyrics, 3},
		{globalSearchAlbum, 2},
		{globalSearchArtist, 1},
		{globalSearchPlaylist, 1},
	}
	for _, tc := range cases {
		if got := len(searchResultColumns(tc.kind)); got != tc.want {
			t.Errorf("searchResultColumns(%v) has %d columns, want %d", tc.kind, got, tc.want)
		}
	}
}

func TestSearchCellText(t *testing.T) {
	plain := searchColumn{icon: "🎵", maxWidth: 10}

	if got := searchCellText(plain, "Short"); got != "🎵 Short" {
		t.Errorf("got %q, want icon-prefixed value", got)
	}
	// Value past maxWidth is truncated with an ellipsis.
	if got := searchCellText(plain, "A very long track title indeed"); !strings.HasSuffix(got, "...") || len([]rune(got)) > 10+3 {
		t.Errorf("got %q, want truncated to the column max with an ellipsis", got)
	}
	// Empty value -> just the icon, no trailing space.
	if got := searchCellText(plain, ""); got != "🎵" {
		t.Errorf("empty value got %q, want just the icon", got)
	}
	// Bracket in a non-markup value is escaped.
	wide := searchColumn{icon: "💿", maxWidth: 40}
	if got := searchCellText(wide, "Live [2024]"); !strings.Contains(got, "[2024[]") {
		t.Errorf("got %q, want the bracket escaped", got)
	}
	// A markup column is passed through verbatim (tags kept, not escaped).
	markup := searchColumn{maxWidth: 10, markup: true}
	if got := searchCellText(markup, "[yellow]hi[-] there and then some more"); got != "[yellow]hi[-] there and then some more" {
		t.Errorf("markup column got %q, want verbatim passthrough", got)
	}
}

func TestRenderSearchResultsFillsTable(t *testing.T) {
	table := tview.NewTable()
	rows := [][]string{
		{"Bohemian Rhapsody", "Queen"},
		{"Under Pressure", "Queen & David Bowie"},
	}
	renderSearchResults(table, globalSearchTrack, rows, 1)

	if table.GetRowCount() != 2 {
		t.Fatalf("row count = %d, want 2", table.GetRowCount())
	}
	if table.GetColumnCount() != 2 {
		t.Fatalf("column count = %d, want 2", table.GetColumnCount())
	}
	if got := table.GetCell(0, 0).Text; !strings.Contains(got, "Bohemian Rhapsody") || !strings.HasPrefix(got, iconTrackCell) {
		t.Errorf("cell(0,0) = %q, want the track title with its icon", got)
	}
	if got := table.GetCell(1, 1).Text; !strings.Contains(got, "Queen & David Bowie") {
		t.Errorf("cell(1,1) = %q, want the artist", got)
	}
	if r, _ := table.GetSelection(); r != 1 {
		t.Errorf("selection row = %d, want 1 (the highlight)", r)
	}
}

func TestRenderSearchResultsShortRowLeavesTrailingColumnsBlank(t *testing.T) {
	table := tview.NewTable()
	// Lyrics layout is 3 columns; give a row with only 2 cells.
	renderSearchResults(table, globalSearchLyrics, [][]string{{"Title", "Artist"}}, 0)

	if table.GetColumnCount() != 3 {
		t.Fatalf("column count = %d, want 3", table.GetColumnCount())
	}
	if got := table.GetCell(0, 2).Text; got != iconLyricsCell {
		t.Errorf("missing 3rd cell = %q, want just the lyrics icon", got)
	}
}
