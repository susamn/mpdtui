package mini

import (
	"path/filepath"
	"strings"
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

// plainText concatenates segs' plain (uncolored) text, for content
// assertions that don't care which parts are colored.
func plainText(segs []segment) string {
	s := ""
	for _, seg := range segs {
		s += seg.text
	}
	return s
}

func TestStatsSegmentsIsAllSkyBlue(t *testing.T) {
	segs := statsSegments(mpdclient.Status{PlaylistLength: 3}, 7)
	want := "3 track(s) in queue  ·  7 playlist(s)"
	if got := plainText(segs); got != want {
		t.Errorf("statsSegments text = %q, want %q", got, want)
	}
	for _, seg := range segs {
		if seg.fg != ansiStatsSkyBlue {
			t.Errorf("statsSegments segment %q fg = %q, want %q", seg.text, seg.fg, ansiStatsSkyBlue)
		}
	}
}

func TestNowPlayingSegmentsFallsBackWhenNothingPlaying(t *testing.T) {
	segs := nowPlayingSegments(mpdclient.Status{State: mpdclient.StateStop}, mpdclient.Song{})
	want := "[] (nothing playing)"
	if got := plainText(segs); got != want {
		t.Errorf("nowPlayingSegments(empty song) text = %q, want %q", got, want)
	}
}

func TestNowPlayingSegmentsColorsOnlyTheTrackGreen(t *testing.T) {
	segs := nowPlayingSegments(mpdclient.Status{State: mpdclient.StatePlay}, mpdclient.Song{Title: "Track"})
	want := "> Track"
	if got := plainText(segs); got != want {
		t.Errorf("nowPlayingSegments text = %q, want %q", got, want)
	}
	for _, seg := range segs {
		switch seg.text {
		case "Track":
			if seg.fg != ansiTrackGreen {
				t.Errorf("track segment fg = %q, want %q", seg.fg, ansiTrackGreen)
			}
		default:
			if seg.fg != "" {
				t.Errorf("non-track segment %q fg = %q, want plain (no color)", seg.text, seg.fg)
			}
		}
	}
}

func TestProgressSegmentsUnknownVolume(t *testing.T) {
	segs := progressSegments(mpdclient.Status{Elapsed: 30 * time.Second, Duration: 60 * time.Second, Volume: -1})
	// -1 means "unknown" (see mpdclient.Status), rendered as "?" rather
	// than a nonsensical "-1%".
	want := "[██████░░░░░░] 0:30/1:00  vol ?%"
	if got := plainText(segs); got != want {
		t.Errorf("progressSegments text = %q, want %q", got, want)
	}
	var barFg string
	for _, seg := range segs {
		if strings.Contains(seg.text, "█") || strings.Contains(seg.text, "░") {
			barFg = seg.fg
		}
	}
	if barFg != ansiBarCyan {
		t.Errorf("progress bar segment fg = %q, want %q", barFg, ansiBarCyan)
	}
}

func TestMetaSegmentsUnratedUnmarked(t *testing.T) {
	segs := metaSegments(metadata.Track{})
	want := "☆☆☆☆☆  played 0x"
	if got := plainText(segs); got != want {
		t.Errorf("metaSegments(zero-value Track) text = %q, want %q", got, want)
	}
}

func TestMetaSegmentsRatedAndMarkedColorsOnlyTheStarsGold(t *testing.T) {
	segs := metaSegments(metadata.Track{Rating: 4, PlayCount: 12, Mark: &metadata.MarkReason{Reason: "mark for deletion"}})
	want := "★★★★☆  played 12x  marked: mark for deletion"
	if got := plainText(segs); got != want {
		t.Errorf("metaSegments text = %q, want %q", got, want)
	}
	if segs[0].fg != ansiRatingGold {
		t.Errorf("rating segment fg = %q, want %q", segs[0].fg, ansiRatingGold)
	}
	for _, seg := range segs[1:] {
		if seg.fg != "" {
			t.Errorf("non-rating segment %q fg = %q, want plain (no color)", seg.text, seg.fg)
		}
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

// TestMaybeTrackPlayCountCountsAgainAfterRestartFromBeginning mirrors
// internal/ui's own test of the same name -- see its doc comment.
func TestMaybeTrackPlayCountCountsAgainAfterRestartFromBeginning(t *testing.T) {
	db := openTestMetaDB(t)
	song := mpdclient.Song{File: "artist/track.mp3"}
	playCountedSongID := -1
	total := 200 * time.Second

	maybeTrackPlayCount(db, mpdclient.Status{SongID: 7, Duration: total, Elapsed: total}, song, &playCountedSongID)
	maybeTrackPlayCount(db, mpdclient.Status{SongID: 7, Duration: total, Elapsed: 0}, song, &playCountedSongID)
	maybeTrackPlayCount(db, mpdclient.Status{SongID: 7, Duration: total, Elapsed: total}, song, &playCountedSongID)

	track, err := db.Get(song.File)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if track.PlayCount != 2 {
		t.Errorf("play count after a same-SongID restart-and-replay = %d, want 2", track.PlayCount)
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
