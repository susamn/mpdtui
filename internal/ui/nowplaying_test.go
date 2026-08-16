package ui

import (
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
	a.renderNowPlaying(mpdclient.Status{}, mpdclient.Song{File: "artist/track.mp3", Title: "Track"})
	got := a.nowPlaying.GetText(true)
	if !strings.Contains(got, "rating") {
		t.Errorf("Now Playing text = %q, want it to mention rating", got)
	}
	if !strings.Contains(got, "★★★★☆") {
		t.Errorf("Now Playing text = %q, want the 4-star rating shown", got)
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
