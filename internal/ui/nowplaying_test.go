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
