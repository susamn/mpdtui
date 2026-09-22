package metadata_test

import (
	"testing"

	"mpdtui/internal/metadata"
)

func TestTotalsOnAnEmptyDatabaseCountsOnlyTheSeededCatalogs(t *testing.T) {
	db := openTestDB(t)

	got, err := db.Totals()
	if err != nil {
		t.Fatalf("Totals: %v", err)
	}

	// Open seeds one mark reason and three tags (see the schema), and
	// nothing else -- so every per-track field must be zero rather
	// than, say, SUM() coming back NULL and failing the scan.
	want := metadata.Totals{MarkReasons: 1, Tags: 3}
	if got != want {
		t.Errorf("Totals() = %+v, want %+v", got, want)
	}
}

func TestTotalsCountsEverythingRecorded(t *testing.T) {
	db := openTestDB(t)

	// Two rated tracks, one of them also played twice and carrying a
	// mark, a tag and two bookmarks; a third track with a play and
	// nothing else.
	if err := db.Rate("a/one.mp3", 5); err != nil {
		t.Fatalf("Rate: %v", err)
	}
	if err := db.Rate("a/two.mp3", 3); err != nil {
		t.Fatalf("Rate: %v", err)
	}
	for range 2 {
		if err := db.IncrementPlayCount("a/one.mp3"); err != nil {
			t.Fatalf("IncrementPlayCount: %v", err)
		}
	}
	if err := db.IncrementPlayCount("a/three.mp3"); err != nil {
		t.Fatalf("IncrementPlayCount: %v", err)
	}
	if _, err := db.ToggleMark("a/one.mp3", 1); err != nil {
		t.Fatalf("ToggleMark: %v", err)
	}
	if _, err := db.ToggleTag("a/one.mp3", 1); err != nil {
		t.Fatalf("ToggleTag: %v", err)
	}
	if _, err := db.ToggleTag("a/one.mp3", 2); err != nil {
		t.Fatalf("ToggleTag: %v", err)
	}
	for _, at := range []float64{10, 90} {
		if _, err := db.CreateBookmark("a/one.mp3", at, "note"); err != nil {
			t.Fatalf("CreateBookmark: %v", err)
		}
	}

	got, err := db.Totals()
	if err != nil {
		t.Fatalf("Totals: %v", err)
	}

	want := metadata.Totals{
		Tracks: 3,
		Rated:  2, Stars: 8,
		Played: 2, Plays: 3,
		// One track, two tags: Tagged counts tracks, not assignments.
		Marked: 1, Tagged: 1,
		Bookmarks: 2, BookmarkedTracks: 1,
		MarkReasons: 1, Tags: 3,
	}
	if got != want {
		t.Errorf("Totals() = %+v, want %+v", got, want)
	}
}

func TestTotalsOnAClosedDatabaseErrors(t *testing.T) {
	db := openTestDB(t)
	if err := db.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	if _, err := db.Totals(); err == nil {
		t.Error("Totals() on a closed database returned no error")
	}
}
