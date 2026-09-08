package ui

import (
	"testing"

	"github.com/gdamore/tcell/v2"

	"mpdtui/internal/metadata"
	"mpdtui/internal/mpdclient"
)

func seedQueueForGutter(q *queuePanel) {
	q.songs = []mpdclient.Song{
		{ID: 1, Title: "Five", File: "a/5.mp3", Duration: 100},
		{ID: 2, Title: "Four", File: "a/4.mp3", Duration: 100},
		{ID: 3, Title: "Three", File: "a/3.mp3", Duration: 100},
		{ID: 4, Title: "Unrated", File: "a/0.mp3", Duration: 100},
	}
	q.metaCache = map[string]metadata.Track{
		"a/5.mp3": {Rating: 5},
		"a/4.mp3": {Rating: 4},
		"a/3.mp3": {Rating: 3},
	}
	q.table.SetRect(0, 0, 120, 12)
}

func TestGutterCellIsAlwaysTwoColumns(t *testing.T) {
	// The gutter reuses space that already existed between the border
	// and the index number -- it must never widen the column, or every
	// row shifts right.
	for rating := 0; rating <= 5; rating++ {
		for _, playing := range []bool{false, true} {
			cell := queueGutterCell(playing, rating)
			if got := len([]rune(cell.Text)); got != 2 {
				t.Errorf("queueGutterCell(playing=%v, rating=%d) = %q, %d runes, want 2", playing, rating, cell.Text, got)
			}
		}
	}
}

func TestGutterStarOnlyAboveThreeStars(t *testing.T) {
	cases := []struct {
		rating   int
		wantStar bool
	}{
		{0, false}, {1, false}, {2, false}, {3, false}, {4, true}, {5, true},
	}
	for _, tc := range cases {
		text := queueGutterCell(false, tc.rating).Text
		if got := text == " "+ratingStarFilled; got != tc.wantStar {
			t.Errorf("rating %d rendered %q, want star = %v", tc.rating, text, tc.wantStar)
		}
	}
}

func TestGutterStarUsesTheRatingColumnsGlyph(t *testing.T) {
	// The gutter is meant to read as a condensed Rating column, so it
	// has to be the same character that column fills in, not a lookalike.
	if got := []rune(queueGutterCell(false, 5).Text)[1]; string(got) != ratingStarFilled {
		t.Errorf("gutter star = %q, want the rating column's filled star %q", string(got), ratingStarFilled)
	}
	if len([]rune(ratingStars(5))) != 5 {
		t.Errorf("ratingStars(5) = %q, want 5 runes", ratingStars(5))
	}
}

func TestGutterStarColorsByTier(t *testing.T) {
	if queueStarTopColor == queueStarHighColor {
		t.Fatalf("5-star and 4-star colors are both %v -- the tiers would be indistinguishable", queueStarTopColor)
	}
	if got := cellFg(queueGutterCell(false, 5)); got != queueStarTopColor {
		t.Errorf("5-star gutter color = %v, want %v", got, queueStarTopColor)
	}
	if got := cellFg(queueGutterCell(false, 4)); got != queueStarHighColor {
		t.Errorf("4-star gutter color = %v, want %v", got, queueStarHighColor)
	}
}

func TestGutterStarColorsComeFromTheTheme(t *testing.T) {
	// Both tiers have to be real colors, or the star renders invisible
	// the way the Library tree's cursor used to.
	if queueStarTopColor == tcell.ColorDefault || queueStarHighColor == tcell.ColorDefault {
		t.Errorf("star colors not theme-derived: top=%v high=%v", queueStarTopColor, queueStarHighColor)
	}
	// The 4-star tier deliberately matches the Rating column's own gold,
	// with 5 stars lifted above it.
	if queueStarHighColor != queueRatingColor {
		t.Errorf("4-star color %v does not match the Rating column's %v", queueStarHighColor, queueRatingColor)
	}
}

func TestGutterKeepsThePlayingMarker(t *testing.T) {
	if got := queueGutterCell(true, 0).Text; got != queuePlayingMarker+" " {
		t.Errorf("playing, unrated = %q, want the marker plus a pad", got)
	}
	if got := queueGutterCell(true, 5).Text; got != queuePlayingMarker+ratingStarFilled {
		t.Errorf("playing, 5 stars = %q, want the marker and the star together", got)
	}
	if got := queueGutterCell(false, 0).Text; got != "  " {
		t.Errorf("idle, unrated = %q, want two spaces", got)
	}
}

func TestRenderDrawsGutterStars(t *testing.T) {
	a := newTestApp()
	seedQueueForGutter(a.queue)
	a.queue.render(2)

	want := []string{
		" " + ratingStarFilled,
		queuePlayingMarker + ratingStarFilled,
		"  ",
		"  ",
	}
	for i, w := range want {
		cell := a.queue.table.GetCell(i+queueHeaderRows, 0)
		if cell == nil {
			t.Fatalf("row %d has no gutter cell", i)
		}
		if cell.Text != w {
			t.Errorf("row %d gutter = %q, want %q", i, cell.Text, w)
		}
	}
}

// TestSetCurrentKeepsGutterStars is a regression test: setCurrent used
// to rewrite column 0 with a bare marker string, which would wipe the
// star off every row each time the playing track changed.
func TestSetCurrentKeepsGutterStars(t *testing.T) {
	a := newTestApp()
	seedQueueForGutter(a.queue)
	a.queue.render(1)

	a.queue.setCurrent(3) // playback moves to the unstarred third track

	if got := a.queue.table.GetCell(queueHeaderRows, 0).Text; got != " "+ratingStarFilled {
		t.Errorf("5-star row gutter after setCurrent = %q, want its star kept", got)
	}
	if got := a.queue.table.GetCell(queueHeaderRows+1, 0).Text; got != " "+ratingStarFilled {
		t.Errorf("4-star row gutter after setCurrent = %q, want its star kept", got)
	}
	if got := a.queue.table.GetCell(queueHeaderRows+2, 0).Text; got != queuePlayingMarker+" " {
		t.Errorf("newly playing row gutter = %q, want the marker", got)
	}
}

func TestSetCurrentKeepsStarColor(t *testing.T) {
	a := newTestApp()
	seedQueueForGutter(a.queue)
	a.queue.render(1)
	a.queue.setCurrent(3)

	if got := cellFg(a.queue.table.GetCell(queueHeaderRows, 0)); got != queueStarTopColor {
		t.Errorf("5-star row color after setCurrent = %v, want %v", got, queueStarTopColor)
	}
}

// TestApplyTrackMetaPaintsGutterWithoutRatingColumn covers the narrow
// terminal case: the Rating column gets dropped when there isn't room
// for it, but the gutter star costs no width and must still appear.
func TestApplyTrackMetaPaintsGutterWithoutRatingColumn(t *testing.T) {
	a := newTestApp()
	seedQueueForGutter(a.queue)
	a.queue.render(-1)
	a.queue.cols.rating = -1
	a.queue.cols.mark = -1
	a.queue.cols.playcount = -1

	a.queue.applyTrackMeta("a/0.mp3", metadata.Track{Rating: 5})

	cell := a.queue.table.GetCell(queueHeaderRows+3, 0)
	if cell.Text != " "+ratingStarFilled {
		t.Errorf("gutter after a late rating = %q, want a star even with the Rating column hidden", cell.Text)
	}
	if got := cellFg(cell); got != queueStarTopColor {
		t.Errorf("gutter color = %v, want %v", got, queueStarTopColor)
	}
}

func TestApplyTrackMetaKeepsPlayingMarker(t *testing.T) {
	// A rating landing asynchronously must not erase the marker from the
	// row that happens to be playing.
	a := newTestApp()
	seedQueueForGutter(a.queue)
	a.queue.render(4)
	a.queue.setCurrent(4)

	a.queue.applyTrackMeta("a/0.mp3", metadata.Track{Rating: 4})

	if got := a.queue.table.GetCell(queueHeaderRows+3, 0).Text; got != queuePlayingMarker+ratingStarFilled {
		t.Errorf("playing row gutter after a late rating = %q, want marker and star", got)
	}
}

func TestGutterUnratedCellHasNoExplicitColor(t *testing.T) {
	// An unstarred row must look exactly as it did before this existed,
	// rather than being tinted by a leftover star color.
	if got := cellFg(queueGutterCell(true, 2)); got != tcell.ColorDefault {
		t.Errorf("unstarred gutter color = %v, want the default", got)
	}
}
