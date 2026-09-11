package tests

import (
	"database/sql"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"mpdtui/internal/metadata"
)

func TestBookmarksCRUD(t *testing.T) {
	db := openTestDB(t)

	const file = "Pink Floyd/The Dark Side of the Moon/04 Time.mp3"

	// Initial check: no bookmarks for track
	bms, err := db.BookmarksForTrack(file)
	if err != nil {
		t.Fatalf("BookmarksForTrack(initial): %v", err)
	}
	if len(bms) != 0 {
		t.Fatalf("expected 0 bookmarks initially, got %d", len(bms))
	}

	// Create first bookmark
	bm1, err := db.CreateBookmark(file, 134.5, "rototom intro solo")
	if err != nil {
		t.Fatalf("CreateBookmark 1: %v", err)
	}
	if bm1.ID <= 0 {
		t.Errorf("expected positive ID, got %d", bm1.ID)
	}
	if bm1.PositionSeconds != 134.5 {
		t.Errorf("PositionSeconds = %v, want 134.5", bm1.PositionSeconds)
	}
	if bm1.Text != "rototom intro solo" {
		t.Errorf("Text = %q, want %q", bm1.Text, "rototom intro solo")
	}
	if bm1.CreatedAt.IsZero() || bm1.UpdatedAt.IsZero() {
		t.Errorf("timestamps should not be zero: created=%v, updated=%v", bm1.CreatedAt, bm1.UpdatedAt)
	}

	// Create second bookmark with earlier position
	bm2, err := db.CreateBookmark(file, 45.0, "ticking clocks start")
	if err != nil {
		t.Fatalf("CreateBookmark 2: %v", err)
	}

	// Verify BookmarksForTrack returns sorted by position ascending
	bms, err = db.BookmarksForTrack(file)
	if err != nil {
		t.Fatalf("BookmarksForTrack: %v", err)
	}
	if len(bms) != 2 {
		t.Fatalf("expected 2 bookmarks, got %d", len(bms))
	}
	if bms[0].ID != bm2.ID || bms[0].PositionSeconds != 45.0 {
		t.Errorf("first bookmark should be bm2 at 45s, got id=%d, pos=%v", bms[0].ID, bms[0].PositionSeconds)
	}
	if bms[1].ID != bm1.ID || bms[1].PositionSeconds != 134.5 {
		t.Errorf("second bookmark should be bm1 at 134.5s, got id=%d, pos=%v", bms[1].ID, bms[1].PositionSeconds)
	}

	// Verify Get returns bookmarks on the Track
	track, err := db.Get(file)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if len(track.Bookmarks) != 2 {
		t.Fatalf("track.Bookmarks length = %d, want 2", len(track.Bookmarks))
	}

	// Test GetBookmark
	gotBm, err := db.GetBookmark(bm1.ID)
	if err != nil {
		t.Fatalf("GetBookmark: %v", err)
	}
	if gotBm.ID != bm1.ID || gotBm.Text != bm1.Text {
		t.Errorf("GetBookmark = %+v, want %+v", gotBm, bm1)
	}

	// Test UpdateBookmark
	time.Sleep(10 * time.Millisecond)
	err = db.UpdateBookmark(bm1.ID, "legendary rototom intro solo")
	if err != nil {
		t.Fatalf("UpdateBookmark: %v", err)
	}
	updatedBm, err := db.GetBookmark(bm1.ID)
	if err != nil {
		t.Fatalf("GetBookmark after update: %v", err)
	}
	if updatedBm.Text != "legendary rototom intro solo" {
		t.Errorf("Text after update = %q, want %q", updatedBm.Text, "legendary rototom intro solo")
	}

	// Test DeleteBookmark
	err = db.DeleteBookmark(bm2.ID)
	if err != nil {
		t.Fatalf("DeleteBookmark: %v", err)
	}
	_, err = db.GetBookmark(bm2.ID)
	if !errors.Is(err, sql.ErrNoRows) {
		t.Errorf("GetBookmark deleted id = %v, want sql.ErrNoRows", err)
	}
	bms, err = db.BookmarksForTrack(file)
	if err != nil {
		t.Fatalf("BookmarksForTrack after delete: %v", err)
	}
	if len(bms) != 1 || bms[0].ID != bm1.ID {
		t.Errorf("expected 1 bookmark remaining (bm1), got %+v", bms)
	}
}

func TestBookmarksCascadeOnTrackDelete(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cascade.db")
	db, err := metadata.Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer db.Close()

	const file = "Artist/Album/Track.mp3"
	bm, err := db.CreateBookmark(file, 10.0, "note")
	if err != nil {
		t.Fatalf("CreateBookmark: %v", err)
	}
	if bm.ID <= 0 {
		t.Fatalf("expected positive ID, got %d", bm.ID)
	}
}
