package ui

import (
	"fmt"
	"strings"
	"testing"

	"mpdtui/internal/mpdclient"
)

func TestRenderNowPlayingOmitsRatingWithoutMetaDB(t *testing.T) {
	a := newTestApp() // nil metaDB -- would panic if renderNowPlaying touched it
	a.renderNowPlaying(mpdclient.Status{}, mpdclient.Song{File: "artist/track.mp3", Title: "Track"})
	if got := a.nowPlaying.GetText(true); strings.Contains(got, "rating") {
		t.Errorf("Now Playing text = %q, want no rating shown without metaDB", got)
	}
}

func TestRenderNowPlayingShowsRatingWhenMetaDBActive(t *testing.T) {
	a := newTestAppWithMetaDB(t)
	if err := a.metaDB.Rate("artist/track.mp3", 4); err != nil {
		t.Fatalf("Rate: %v", err)
	}
	if err := a.metaDB.IncrementPlayCount("artist/track.mp3"); err != nil {
		t.Fatalf("IncrementPlayCount: %v", err)
	}
	a.renderNowPlaying(mpdclient.Status{}, mpdclient.Song{File: "artist/track.mp3", Title: "Track"})
	got := a.nowPlaying.GetText(true)
	if !strings.Contains(got, "rating") {
		t.Errorf("Now Playing text = %q, want it to mention rating", got)
	}
	if !strings.Contains(got, "★★★★☆") {
		t.Errorf("Now Playing text = %q, want the 4-star rating shown", got)
	}
	if !strings.Contains(got, "played 1x") {
		t.Errorf("Now Playing text = %q, want the play count shown", got)
	}
}

// TestNowPlayingTrackTextTitleThenArtist covers the explicit ordering
// correction: "Title - Artist", not DisplayName's own "Artist - Title".
func TestNowPlayingTrackTextTitleThenArtist(t *testing.T) {
	got := nowPlayingTrackText(mpdclient.Song{Artist: "Ajay-Atul", Title: "Vaat Disu De"})
	want := fmt.Sprintf("[%s::b]%s[-:-:-] - [%s::b]%s[-:-:-]",
		nowPlayingTrackColor, "Vaat Disu De", nowPlayingArtistColor, "Ajay-Atul")
	if got != want {
		t.Errorf("nowPlayingTrackText(artist+title) = %q, want %q", got, want)
	}
}

func TestNowPlayingTrackTextTitleOnly(t *testing.T) {
	got := nowPlayingTrackText(mpdclient.Song{Title: "Vaat Disu De"})
	want := fmt.Sprintf("[%s::b]%s[-:-:-]", nowPlayingTrackColor, "Vaat Disu De")
	if got != want {
		t.Errorf("nowPlayingTrackText(title only) = %q, want %q", got, want)
	}
}

// TestNowPlayingTrackTextFallsBackToFileThenNothingPlaying covers the
// two fallbacks that stay uncolored: a bare filename (no Artist/Title
// tags at all) and nothing playing.
func TestNowPlayingTrackTextFallsBackToFileThenNothingPlaying(t *testing.T) {
	if got, want := nowPlayingTrackText(mpdclient.Song{File: "artist/track.mp3"}), "artist/track.mp3"; got != want {
		t.Errorf("nowPlayingTrackText(file only) = %q, want %q (uncolored)", got, want)
	}
	if got, want := nowPlayingTrackText(mpdclient.Song{}), "(nothing playing)"; got != want {
		t.Errorf("nowPlayingTrackText(empty) = %q, want %q", got, want)
	}
}

// TestRenderNowPlayingOmitsRatingWhenNothingPlaying covers the "nothing
// queued/playing yet" state (empty Song.File): showing a zero-opinion
// rating for a track that isn't even known would be misleading, so the
// whole rating segment is skipped rather than showing "☆☆☆☆☆".
func TestRenderNowPlayingOmitsRatingWhenNothingPlaying(t *testing.T) {
	a := newTestAppWithMetaDB(t)
	a.renderNowPlaying(mpdclient.Status{}, mpdclient.Song{})
	if got := a.nowPlaying.GetText(true); strings.Contains(got, "rating") {
		t.Errorf("Now Playing text = %q, want no rating row when nothing's playing", got)
	}
}

func TestRenderNowPlayingSyncsExternalRatingChangeToQueue(t *testing.T) {
	a := newTestAppWithMetaDB(t)
	song := mpdclient.Song{File: "artist/track.mp3", Title: "Track"}
	a.queue.songs = []mpdclient.Song{song}
	a.queue.render(-1)

	// Initially 0 stars in queue table
	cols := a.queue.cols
	if got := a.queue.table.GetCell(queueHeaderRows, cols.rating).Text; !strings.Contains(got, ratingStars(0)) {
		t.Fatalf("initial rating cell = %q, want %q", got, ratingStars(0))
	}

	// External write updates rating in metaDB
	if err := a.metaDB.Rate(song.File, 5); err != nil {
		t.Fatalf("Rate: %v", err)
	}

	// Now playing render runs on the next tick
	a.renderNowPlaying(mpdclient.Status{}, song)

	// Queue table rating cell should be updated
	if got := a.queue.table.GetCell(queueHeaderRows, cols.rating).Text; !strings.Contains(got, ratingStars(5)) {
		t.Errorf("queue rating cell after external rate = %q, want %q", got, ratingStars(5))
	}
}

func TestRenderNowPlayingSyncsExternalMarkChangeToQueue(t *testing.T) {
	a := newTestAppWithMetaDB(t)
	song := mpdclient.Song{File: "artist/track.mp3", Title: "Track"}
	a.queue.songs = []mpdclient.Song{song}
	a.queue.render(-1)

	cols := a.queue.cols
	// Initially blank/empty mark cell
	if got := a.queue.table.GetCell(queueHeaderRows, cols.mark).Text; strings.TrimSpace(got) != "" {
		t.Fatalf("initial mark cell = %q, want blank", got)
	}

	// External write sets mark in metaDB
	markID := int64(1) // seeded "mark for deletion"
	if err := a.metaDB.SetMarks(song.File, []int64{markID}); err != nil {
		t.Fatalf("SetMark: %v", err)
	}

	// Now playing render runs on the next tick
	a.renderNowPlaying(mpdclient.Status{}, song)

	// Queue table mark cell should now have mark tick
	if got := a.queue.table.GetCell(queueHeaderRows, cols.mark).Text; strings.TrimSpace(got) == "" {
		t.Errorf("queue mark cell after external mark set = %q, want non-empty mark tick", got)
	}
}

func TestRenderNowPlayingSyncsExternalPlayCountChangeToQueue(t *testing.T) {
	a := newTestAppWithMetaDB(t)
	song := mpdclient.Song{File: "artist/track.mp3", Title: "Track"}
	a.queue.songs = []mpdclient.Song{song}
	a.queue.render(-1)

	cols := a.queue.cols
	// Initially 0 plays
	if got := a.queue.table.GetCell(queueHeaderRows, cols.playcount).Text; !strings.Contains(got, "0") {
		t.Fatalf("initial playcount cell = %q, want %q", got, "0")
	}

	// External write increments play count in metaDB
	if err := a.metaDB.IncrementPlayCount(song.File); err != nil {
		t.Fatalf("IncrementPlayCount: %v", err)
	}

	// Now playing render runs on the next tick
	a.renderNowPlaying(mpdclient.Status{}, song)

	// Queue table playcount cell should now show 1
	if got := a.queue.table.GetCell(queueHeaderRows, cols.playcount).Text; !strings.Contains(got, "1") {
		t.Errorf("queue playcount cell after external increment = %q, want %q", got, "1")
	}
}

// TestNowPlayingLine1LayoutUnknownWidth covers the not-yet-drawn case
// (GetInnerRect returns 0): full bar, no truncation budget.
func TestNowPlayingLine1LayoutUnknownWidth(t *testing.T) {
	bar, track := nowPlayingLine1Layout(0, 9)
	if bar != nowPlayingBarWidth || track != 0 {
		t.Errorf("layout(0, 9) = (%d, %d), want (%d, 0)", bar, track, nowPlayingBarWidth)
	}
}

func TestNowPlayingLine1LayoutFitsItsWidth(t *testing.T) {
	const times = 9 // "1:23/4:56"
	for _, width := range []int{20, 30, 45, 60, 80, 120} {
		bar, track := nowPlayingLine1Layout(width, times)
		if bar < nowPlayingBarMinWidth || bar > nowPlayingBarWidth {
			t.Errorf("width %d: bar = %d, want within [%d, %d]", width, bar, nowPlayingBarMinWidth, nowPlayingBarWidth)
		}
		if track < 1 {
			t.Errorf("width %d: track budget = %d, want at least 1", width, track)
		}
		// The line must never be wider than the panel unless even the
		// two minimums cannot fit (the clipped, last-resort case).
		total := nowPlayingLine1Fixed + times + bar + track
		if total > width && bar != nowPlayingBarMinWidth {
			t.Errorf("width %d: line needs %d cells, want it to fit", width, total)
		}
	}
}

// TestNowPlayingLine1LayoutShrinksBarBeforeTrack: the bar is what gives
// first, and only down to its minimum.
func TestNowPlayingLine1LayoutShrinksBarBeforeTrack(t *testing.T) {
	wide, _ := nowPlayingLine1Layout(120, 9)
	if wide != nowPlayingBarWidth {
		t.Errorf("wide terminal: bar = %d, want the full %d", wide, nowPlayingBarWidth)
	}
	narrow, track := nowPlayingLine1Layout(40, 9)
	if narrow >= nowPlayingBarWidth {
		t.Errorf("narrow terminal: bar = %d, want it shrunk below %d", narrow, nowPlayingBarWidth)
	}
	if narrow < nowPlayingBarMinWidth {
		t.Errorf("narrow terminal: bar = %d, want at least %d", narrow, nowPlayingBarMinWidth)
	}
	if track < nowPlayingTrackMinWidth {
		t.Errorf("narrow terminal: track budget = %d, want the bar to pay down to %d first", track, nowPlayingTrackMinWidth)
	}
}

// TestNowPlayingTrackTextWidthTruncatesWithinBudget checks the visible
// text (tags stripped, as tview renders it) stays within the budget.
func TestNowPlayingTrackTextWidthTruncatesWithinBudget(t *testing.T) {
	song := mpdclient.Song{
		Title:  "A Very Long Track Title That Will Not Fit Anywhere",
		Artist: "An Equally Long Artist Name Indeed",
	}
	for _, max := range []int{8, 12, 20, 30, 40} {
		got := nowPlayingTrackTextWidth(song, max)
		plain := stripStyleTags(got)
		if n := len([]rune(plain)); n > max {
			t.Errorf("max %d: visible text %q is %d cells, want at most %d", max, plain, n, max)
		}
	}
}

// TestNowPlayingTrackTextWidthKeepsTagsValid: truncation happens on the
// plain text, so no style tag is ever cut in half.
func TestNowPlayingTrackTextWidthKeepsTagsValid(t *testing.T) {
	got := nowPlayingTrackTextWidth(mpdclient.Song{Title: "Some Long Title Here", Artist: "Some Long Artist"}, 20)
	if strings.Count(got, "[") != strings.Count(got, "]") {
		t.Errorf("%q has unbalanced style-tag brackets", got)
	}
	if !strings.HasSuffix(got, "[-:-:-]") {
		t.Errorf("%q, want it to still end with a closing style tag", got)
	}
}

// TestNowPlayingTrackTextWidthDropsArtistWhenVeryNarrow: below the
// track minimum there is no room for both halves, so only the title
// (truncated) survives -- no " - " with two stubs around it.
func TestNowPlayingTrackTextWidthDropsArtistWhenVeryNarrow(t *testing.T) {
	got := nowPlayingTrackTextWidth(mpdclient.Song{Title: "Vaat Disu De", Artist: "Ajay-Atul"}, 8)
	if strings.Contains(got, "Ajay") {
		t.Errorf("%q, want the artist dropped at a budget below %d", got, nowPlayingTrackMinWidth)
	}
	if plain := stripStyleTags(got); len([]rune(plain)) > 8 {
		t.Errorf("visible text %q, want at most 8 cells", plain)
	}
}

// TestNowPlayingTrackTextWidthGivesSlackToTheLongerHalf: a short title
// must not leave its share of the budget unused while the artist is
// truncated.
func TestNowPlayingTrackTextWidthGivesSlackToTheLongerHalf(t *testing.T) {
	got := stripStyleTags(nowPlayingTrackTextWidth(mpdclient.Song{Title: "Om", Artist: "A Rather Long Artist Name"}, 23))
	if !strings.HasPrefix(got, "Om - ") {
		t.Errorf("got %q, want the short title kept whole", got)
	}
	if len([]rune(got)) > 23 {
		t.Errorf("got %q (%d cells), want at most 23", got, len([]rune(got)))
	}
	// 23 - 3 (" - ") - 2 ("Om") == 18 cells for the artist, well past
	// its own 3/5 share of 12.
	if !strings.HasPrefix(got, "Om - A Rather Long") {
		t.Errorf("got %q, want the title's unused share handed to the artist", got)
	}
}

// TestNowPlayingTrackTextWidthUnboundedMatchesUntruncated pins the
// max <= 0 case to the original, untruncated rendering.
func TestNowPlayingTrackTextWidthUnboundedMatchesUntruncated(t *testing.T) {
	song := mpdclient.Song{Title: "Vaat Disu De", Artist: "Ajay-Atul"}
	if got, want := nowPlayingTrackTextWidth(song, 0), nowPlayingTrackText(song); got != want {
		t.Errorf("width 0 = %q, want %q", got, want)
	}
}

// stripStyleTags removes tview style tags ("[green::b]", "[-:-:-]")
// from s, turning an escaped "[[" back into a literal "[" -- what the
// terminal actually shows, which is what the width budgets are about.
func stripStyleTags(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); {
		if s[i] == '[' {
			if i+1 < len(s) && s[i+1] == '[' {
				b.WriteByte('[')
				i += 2
				continue
			}
			if j := strings.IndexByte(s[i:], ']'); j >= 0 {
				i += j + 1
				continue
			}
		}
		b.WriteByte(s[i])
		i++
	}
	return b.String()
}
