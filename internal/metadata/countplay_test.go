package metadata_test

import (
	"path/filepath"
	"sync"
	"testing"

	"mpdtui/internal/metadata"
)

// These are the tests that would have caught the double-counting: the
// guard that makes a play count once used to be a field on the running
// app, so nothing here could reach it, and every test necessarily had
// exactly one "instance". The guard now lives beside play_count itself,
// so it can be tested the way it actually fails -- several watchers,
// one database.

func TestCountPlayCountsOnce(t *testing.T) {
	db := openTestDB(t)
	const file, songID = "artist/track.mp3", 7

	counted, err := db.CountPlay(file, songID)
	if err != nil {
		t.Fatalf("CountPlay: %v", err)
	}
	if !counted {
		t.Fatal("the first CountPlay reported it did not count")
	}

	// Every later call for the same queue entry is a no-op, however
	// many times it happens -- this is the ~500ms tick of a second
	// instance still watching the same track.
	for i := 0; i < 5; i++ {
		counted, err = db.CountPlay(file, songID)
		if err != nil {
			t.Fatalf("CountPlay: %v", err)
		}
		if counted {
			t.Errorf("repeat call %d counted again", i+1)
		}
	}

	track, err := db.Get(file)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if track.PlayCount != 1 {
		t.Errorf("play count = %d, want 1", track.PlayCount)
	}
}

// TestCountPlayIsOnceAcrossConcurrentWatchers is the actual bug: two
// mpdtui instances watching one MPD both decide the track passed its
// halfway point at the same moment and both try to record it.
func TestCountPlayIsOnceAcrossConcurrentWatchers(t *testing.T) {
	path := filepath.Join(t.TempDir(), "shared.db")
	const file, songID = "artist/track.mp3", 7

	// Separate handles, as separate processes would have.
	const instances = 4
	dbs := make([]*metadata.DB, instances)
	for i := range dbs {
		db, err := metadata.Open(path)
		if err != nil {
			t.Fatalf("Open: %v", err)
		}
		t.Cleanup(func() { db.Close() })
		dbs[i] = db
	}

	var wg sync.WaitGroup
	results := make([]bool, instances)
	errs := make([]error, instances)
	start := make(chan struct{})
	for i, db := range dbs {
		wg.Add(1)
		go func(i int, db *metadata.DB) {
			defer wg.Done()
			<-start // pile up, then go at once
			results[i], errs[i] = db.CountPlay(file, songID)
		}(i, db)
	}
	close(start)
	wg.Wait()

	wins := 0
	for i := range results {
		if errs[i] != nil {
			t.Fatalf("instance %d: %v", i, errs[i])
		}
		if results[i] {
			wins++
		}
	}
	if wins != 1 {
		t.Errorf("%d of %d instances counted the play, want exactly 1", wins, instances)
	}

	track, err := dbs[0].Get(file)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if track.PlayCount != 1 {
		t.Errorf("play count after %d instances saw one play-through = %d, want 1",
			instances, track.PlayCount)
	}
}

// TestCountPlayCountsEachQueueEntrySeparately covers a genuine second
// listen: MPD hands a re-added track a fresh song id.
func TestCountPlayCountsEachQueueEntrySeparately(t *testing.T) {
	db := openTestDB(t)
	const file = "artist/track.mp3"

	for _, songID := range []int{7, 8, 9} {
		counted, err := db.CountPlay(file, songID)
		if err != nil {
			t.Fatalf("CountPlay(%d): %v", songID, err)
		}
		if !counted {
			t.Errorf("song id %d did not count", songID)
		}
	}

	track, _ := db.Get(file)
	if track.PlayCount != 3 {
		t.Errorf("play count across three queue entries = %d, want 3", track.PlayCount)
	}
}

// TestRearmPlayLetsARepeatCountAgain covers repeat mode, which reuses
// the same song id rather than getting a fresh one.
func TestRearmPlayLetsARepeatCountAgain(t *testing.T) {
	db := openTestDB(t)
	const file, songID = "artist/track.mp3", 7

	if counted, _ := db.CountPlay(file, songID); !counted {
		t.Fatal("the first play did not count")
	}
	if counted, _ := db.CountPlay(file, songID); counted {
		t.Fatal("the same play counted twice")
	}

	// The track restarts from the beginning under the same song id.
	if err := db.RearmPlay(file, songID); err != nil {
		t.Fatalf("RearmPlay: %v", err)
	}

	if counted, _ := db.CountPlay(file, songID); !counted {
		t.Error("a repeat play did not count after re-arming")
	}

	track, _ := db.Get(file)
	if track.PlayCount != 2 {
		t.Errorf("play count after a repeat = %d, want 2", track.PlayCount)
	}
}

// TestRearmPlayIsIdempotent covers several instances all observing the
// same restart: they clear the same marker and the outcome is identical.
func TestRearmPlayIsIdempotent(t *testing.T) {
	db := openTestDB(t)
	const file, songID = "artist/track.mp3", 7

	db.CountPlay(file, songID)
	for i := 0; i < 4; i++ {
		if err := db.RearmPlay(file, songID); err != nil {
			t.Fatalf("RearmPlay: %v", err)
		}
	}

	// Still exactly one re-arm's worth: one more play, not four.
	if counted, _ := db.CountPlay(file, songID); !counted {
		t.Fatal("the repeat play did not count")
	}
	if counted, _ := db.CountPlay(file, songID); counted {
		t.Error("the repeat play counted twice")
	}

	track, _ := db.Get(file)
	if track.PlayCount != 2 {
		t.Errorf("play count = %d, want 2", track.PlayCount)
	}
}

// TestRearmPlayOnlyClearsItsOwnSongID guards against a stale re-arm
// clearing a marker that has since moved on to a different queue entry.
func TestRearmPlayOnlyClearsItsOwnSongID(t *testing.T) {
	db := openTestDB(t)
	const file = "artist/track.mp3"

	db.CountPlay(file, 7)
	db.CountPlay(file, 8) // the queue moved on; 8 is what is counted now

	if err := db.RearmPlay(file, 7); err != nil { // a late re-arm for the old entry
		t.Fatalf("RearmPlay: %v", err)
	}

	if counted, _ := db.CountPlay(file, 8); counted {
		t.Error("a stale re-arm for a previous song id unblocked the current one")
	}
}

// TestCountPlayCreatesTheRow covers a track with no row yet, which is
// every track the first time it is played.
func TestCountPlayCreatesTheRow(t *testing.T) {
	db := openTestDB(t)

	counted, err := db.CountPlay("brand/new.mp3", 1)
	if err != nil {
		t.Fatalf("CountPlay: %v", err)
	}
	if !counted {
		t.Error("the first play of a new track did not count")
	}
	track, _ := db.Get("brand/new.mp3")
	if track.PlayCount != 1 {
		t.Errorf("play count = %d, want 1", track.PlayCount)
	}
}

// TestCountPlayOnAnUpgradedDatabase covers the migration: a row that
// predates the marker column has NULL there, which must read as
// "nothing counted yet" rather than blocking.
func TestCountPlayOnAnUpgradedDatabase(t *testing.T) {
	db := openTestDB(t)
	const file = "artist/track.mp3"

	// A pre-upgrade row: a play count, no marker.
	if err := db.IncrementPlayCount(file); err != nil {
		t.Fatalf("IncrementPlayCount: %v", err)
	}

	counted, err := db.CountPlay(file, 7)
	if err != nil {
		t.Fatalf("CountPlay: %v", err)
	}
	if !counted {
		t.Error("a row carried over from before the marker existed refused to count")
	}
	track, _ := db.Get(file)
	if track.PlayCount != 2 {
		t.Errorf("play count = %d, want 2", track.PlayCount)
	}
}
