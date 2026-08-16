package mini

import (
	"path/filepath"
	"testing"
	"time"

	"mpdtui/internal/config"
	"mpdtui/internal/metadata"
	"mpdtui/internal/mpdclient"
)

// dialOrSkip mirrors internal/ui/keys_test.go's own helper of the same
// name -- duplicated, not imported (internal/ui and internal/mini don't
// depend on each other, see DEPENDENCY.md), for the one test here that
// needs a real MPD connection.
func dialOrSkip(t *testing.T) *mpdclient.Client {
	t.Helper()
	c, err := mpdclient.Dial(config.Load())
	if err != nil {
		t.Skipf("no MPD server reachable, skipping: %v", err)
	}
	t.Cleanup(func() { c.Close() })
	return c
}

func openTestMetaDB(t *testing.T) *metadata.DB {
	t.Helper()
	db, err := metadata.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("metadata.Open: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func TestStateGlyph(t *testing.T) {
	cases := []struct {
		state mpdclient.State
		want  string
	}{
		{mpdclient.StatePlay, ">"},
		{mpdclient.StatePause, "||"},
		{mpdclient.StateStop, "[]"},
		{mpdclient.State("unknown"), "?"},
	}
	for _, tc := range cases {
		if got := stateGlyph(tc.state); got != tc.want {
			t.Errorf("stateGlyph(%q) = %q, want %q", tc.state, got, tc.want)
		}
	}
}

func TestStatsLine(t *testing.T) {
	got := statsLine(mpdclient.Status{PlaylistLength: 3}, 7)
	want := "3 track(s) in queue  ·  7 playlist(s)"
	if got != want {
		t.Errorf("statsLine = %q, want %q", got, want)
	}
}

func TestNowPlayingLineFallsBackWhenNothingPlaying(t *testing.T) {
	got := nowPlayingLine(mpdclient.Status{State: mpdclient.StateStop}, mpdclient.Song{})
	want := "[] (nothing playing)"
	if got != want {
		t.Errorf("nowPlayingLine(empty song) = %q, want %q", got, want)
	}
}

func TestNowPlayingLineShowsTrack(t *testing.T) {
	got := nowPlayingLine(mpdclient.Status{State: mpdclient.StatePlay}, mpdclient.Song{Title: "Track"})
	want := "> Track"
	if got != want {
		t.Errorf("nowPlayingLine = %q, want %q", got, want)
	}
}

func TestProgressLineUnknownVolume(t *testing.T) {
	got := progressLine(mpdclient.Status{Elapsed: 30 * time.Second, Duration: 60 * time.Second, Volume: -1})
	if got == "" {
		t.Fatal("progressLine returned empty string")
	}
	// -1 means "unknown" (see mpdclient.Status), rendered as "?" rather
	// than a nonsensical "-1%".
	want := "[======>      ] 0:30/1:00  vol ?%"
	if got != want {
		t.Errorf("progressLine = %q, want %q", got, want)
	}
}

func TestMetaLineUnratedUnmarked(t *testing.T) {
	got := metaLine(metadata.Track{})
	want := "☆☆☆☆☆  played 0x"
	if got != want {
		t.Errorf("metaLine(zero-value Track) = %q, want %q", got, want)
	}
}

func TestMetaLineRatedAndMarked(t *testing.T) {
	got := metaLine(metadata.Track{Rating: 4, PlayCount: 12, Mark: &metadata.MarkReason{Reason: "mark for deletion"}})
	want := "★★★★☆  played 12x  marked: mark for deletion"
	if got != want {
		t.Errorf("metaLine = %q, want %q", got, want)
	}
}

func TestMaybeTrackPlayCountIncrementsAtHalfway(t *testing.T) {
	db := openTestMetaDB(t)
	song := mpdclient.Song{File: "artist/track.mp3"}
	playCountedSongID := -1
	total := 200 * time.Second

	maybeTrackPlayCount(db, mpdclient.Status{SongID: 1, Duration: total, Elapsed: total * 3 / 10}, song, &playCountedSongID) // 30%
	track, err := db.Get(song.File)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if track.PlayCount != 0 {
		t.Fatalf("play count at 30%% = %d, want 0 (not counted yet)", track.PlayCount)
	}

	maybeTrackPlayCount(db, mpdclient.Status{SongID: 1, Duration: total, Elapsed: total / 2}, song, &playCountedSongID) // 50%
	track, err = db.Get(song.File)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if track.PlayCount != 1 {
		t.Errorf("play count at 50%% = %d, want 1", track.PlayCount)
	}

	// Continuing to tick past halfway must not keep incrementing.
	maybeTrackPlayCount(db, mpdclient.Status{SongID: 1, Duration: total, Elapsed: total * 9 / 10}, song, &playCountedSongID)
	track, err = db.Get(song.File)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if track.PlayCount != 1 {
		t.Errorf("play count after continuing past halfway = %d, want still 1", track.PlayCount)
	}
}

func TestMaybeTrackPlayCountCountsAgainForADifferentSongID(t *testing.T) {
	db := openTestMetaDB(t)
	song := mpdclient.Song{File: "artist/track.mp3"}
	playCountedSongID := -1
	total := 100 * time.Second

	maybeTrackPlayCount(db, mpdclient.Status{SongID: 1, Duration: total, Elapsed: total}, song, &playCountedSongID)
	maybeTrackPlayCount(db, mpdclient.Status{SongID: 2, Duration: total, Elapsed: total}, song, &playCountedSongID)

	track, err := db.Get(song.File)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if track.PlayCount != 2 {
		t.Errorf("play count across two distinct song ids = %d, want 2", track.PlayCount)
	}
}

func TestMaybeTrackPlayCountNoopWithoutMetaDB(t *testing.T) {
	playCountedSongID := -1
	// Must not panic with a nil *metadata.DB.
	maybeTrackPlayCount(nil, mpdclient.Status{SongID: 1, Duration: time.Second, Elapsed: time.Second}, mpdclient.Song{File: "x"}, &playCountedSongID)
}

// TestRateCurrentTrackSavesRatingForWhateversPlayingNeedsLiveMPD is the
// one test here that needs a real MPD connection (rateCurrentTrack calls
// client.CurrentSong()) -- skipped automatically if none is reachable,
// same convention as internal/ui's own live-gated tests.
func TestRateCurrentTrackSavesRatingForWhateversPlayingNeedsLiveMPD(t *testing.T) {
	c := dialOrSkip(t)
	db := openTestMetaDB(t)

	song, err := c.CurrentSong()
	if err != nil {
		t.Fatalf("CurrentSong: %v", err)
	}
	if song.File == "" {
		t.Skip("nothing currently playing/queued on the reachable MPD server, skipping")
	}

	rateCurrentTrack(c, db, 4)

	track, err := db.Get(song.File)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if track.Rating != 4 {
		t.Errorf("rating after rateCurrentTrack(4) = %d, want 4", track.Rating)
	}
}

func TestRateCurrentTrackNoopWithoutMetaDB(t *testing.T) {
	// Must not panic with a nil client -- metaDB nil short-circuits
	// before ever touching it.
	rateCurrentTrack(nil, nil, 4)
}
