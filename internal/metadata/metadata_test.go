package metadata_test

import (
	"path/filepath"
	"testing"

	"mpdtui/internal/metadata"
)

func openTestDB(t *testing.T) *metadata.DB {
	t.Helper()
	db, err := metadata.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func TestOpenSeedsMarkReasonsAndTags(t *testing.T) {
	db := openTestDB(t)

	reasons, err := db.ListMarkReasons()
	if err != nil {
		t.Fatalf("ListMarkReasons: %v", err)
	}
	if len(reasons) != 1 || reasons[0].ID != 1 || reasons[0].Reason != "mark for deletion" {
		t.Errorf("ListMarkReasons() = %+v, want [{1 mark for deletion}]", reasons)
	}

	tags, err := db.ListTags()
	if err != nil {
		t.Fatalf("ListTags: %v", err)
	}
	want := []struct {
		id   int64
		name string
	}{{1, "bengali"}, {2, "hindi"}, {3, "english"}}
	if len(tags) != len(want) {
		t.Fatalf("ListTags() = %+v, want %d entries", tags, len(want))
	}
	for i, w := range want {
		if tags[i].ID != w.id || tags[i].Tagname != w.name {
			t.Errorf("tags[%d] = %+v, want {%d %s}", i, tags[i], w.id, w.name)
		}
	}
}

// TestOpenIsIdempotent guards against re-running Open (as happens every
// app startup) duplicating or resetting the seed rows -- a real risk
// with a naive seed that doesn't use INSERT OR IGNORE, and the whole
// point of seeding by fixed id rather than auto-increment.
func TestOpenIsIdempotent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.db")

	db1, err := metadata.Open(path)
	if err != nil {
		t.Fatalf("Open (first): %v", err)
	}
	if err := db1.Rate("artist/track.mp3", 4); err != nil {
		t.Fatalf("Rate: %v", err)
	}
	db1.Close()

	db2, err := metadata.Open(path)
	if err != nil {
		t.Fatalf("Open (second): %v", err)
	}
	defer db2.Close()

	tags, err := db2.ListTags()
	if err != nil {
		t.Fatalf("ListTags: %v", err)
	}
	if len(tags) != 3 {
		t.Errorf("ListTags() after reopening = %d entries, want 3 (not duplicated)", len(tags))
	}

	track, err := db2.Get("artist/track.mp3")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if track.Rating != 4 {
		t.Errorf("rating after reopening = %d, want 4 (data survives)", track.Rating)
	}
}

func TestGetUnknownTrackReturnsZeroOpinionNotError(t *testing.T) {
	db := openTestDB(t)

	track, err := db.Get("artist/never-touched.mp3")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if track.PlayCount != 0 || track.Rating != 0 || len(track.Marks) != 0 || len(track.Tags) != 0 {
		t.Errorf("Get(unknown) = %+v, want all-zero-opinion", track)
	}
}

func TestRateSetsAndOverwritesRating(t *testing.T) {
	db := openTestDB(t)
	file := "artist/track.mp3"

	if err := db.Rate(file, 3); err != nil {
		t.Fatalf("Rate(3): %v", err)
	}
	track, err := db.Get(file)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if track.Rating != 3 {
		t.Errorf("rating = %d, want 3", track.Rating)
	}

	if err := db.Rate(file, 5); err != nil {
		t.Fatalf("Rate(5): %v", err)
	}
	track, err = db.Get(file)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if track.Rating != 5 {
		t.Errorf("rating after re-rating = %d, want 5 (overwritten, not summed)", track.Rating)
	}
}

func TestIncrementPlayCountAccumulates(t *testing.T) {
	db := openTestDB(t)
	file := "artist/track.mp3"

	for i := 0; i < 3; i++ {
		if err := db.IncrementPlayCount(file); err != nil {
			t.Fatalf("IncrementPlayCount: %v", err)
		}
	}

	track, err := db.Get(file)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if track.PlayCount != 3 {
		t.Errorf("play count = %d, want 3", track.PlayCount)
	}
}

func TestSetMarkAndClear(t *testing.T) {
	db := openTestDB(t)
	file := "artist/track.mp3"

	reasonID := int64(1)
	if err := db.SetMarks(file, []int64{reasonID}); err != nil {
		t.Fatalf("SetMark: %v", err)
	}
	track, err := db.Get(file)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if len(track.Marks) != 1 || track.Marks[0].Reason != "mark for deletion" {
		t.Fatalf("Marks after SetMarks = %+v, want [{1 mark for deletion}]", track.Marks)
	}

	if err := db.SetMarks(file, nil); err != nil {
		t.Fatalf("SetMarks(nil): %v", err)
	}
	track, err = db.Get(file)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if len(track.Marks) != 0 {
		t.Errorf("Marks after clearing = %+v, want none", track.Marks)
	}
}

func TestSetTagsReplacesFullSet(t *testing.T) {
	db := openTestDB(t)
	file := "artist/track.mp3"

	if err := db.SetTags(file, []int64{1, 2}); err != nil {
		t.Fatalf("SetTags([1,2]): %v", err)
	}
	track, err := db.Get(file)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if len(track.Tags) != 2 || track.Tags[0].Tagname != "bengali" || track.Tags[1].Tagname != "hindi" {
		t.Fatalf("Tags after SetTags([1,2]) = %+v, want [bengali hindi]", track.Tags)
	}

	if err := db.SetTags(file, []int64{3}); err != nil {
		t.Fatalf("SetTags([3]): %v", err)
	}
	track, err = db.Get(file)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if len(track.Tags) != 1 || track.Tags[0].Tagname != "english" {
		t.Fatalf("Tags after SetTags([3]) = %+v, want [english] (fully replaced, not appended)", track.Tags)
	}
}

// TestDifferentTracksSameNormalizedSegmentStayDistinctAcrossDirectories
// is the exact collision scenario normalizePath's own doc comment warns
// about: two tracks with the same generic filename in different artist
// folders must never share a row.
func TestDifferentTracksSameNormalizedSegmentStayDistinctAcrossDirectories(t *testing.T) {
	db := openTestDB(t)

	if err := db.Rate("Artist A/01 Intro.mp3", 2); err != nil {
		t.Fatalf("Rate A: %v", err)
	}
	if err := db.Rate("Artist B/01 Intro.mp3", 5); err != nil {
		t.Fatalf("Rate B: %v", err)
	}

	a, err := db.Get("Artist A/01 Intro.mp3")
	if err != nil {
		t.Fatalf("Get A: %v", err)
	}
	b, err := db.Get("Artist B/01 Intro.mp3")
	if err != nil {
		t.Fatalf("Get B: %v", err)
	}
	if a.Rating != 2 {
		t.Errorf("Artist A/01 Intro.mp3 rating = %d, want 2 (must not be clobbered by B)", a.Rating)
	}
	if b.Rating != 5 {
		t.Errorf("Artist B/01 Intro.mp3 rating = %d, want 5", b.Rating)
	}
}

// TestNormalizedMatchDespitePunctuationDifferences mirrors
// internal/lyrics' own matching guarantee: a path that differs only in
// special characters/case still resolves to the same row.
func TestNormalizedMatchDespitePunctuationDifferences(t *testing.T) {
	db := openTestDB(t)

	if err := db.Rate("50-cent/Get Rich or Die Tryin'/Candy Shop [84934].mp3", 4); err != nil {
		t.Fatalf("Rate: %v", err)
	}

	track, err := db.Get("50-CENT/get rich or die tryin/candy_shop_84934.mp3")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if track.Rating != 4 {
		t.Errorf("rating via a punctuation-different path = %d, want 4 (should still match the same normalized row)", track.Rating)
	}
}

func TestAddMarkReasonAppendsAfterSeededRow(t *testing.T) {
	db := openTestDB(t)

	id, err := db.AddMarkReason("mark for move")
	if err != nil {
		t.Fatalf("AddMarkReason: %v", err)
	}
	if id != 2 {
		t.Errorf("AddMarkReason id = %d, want 2 (after the seeded id 1)", id)
	}

	reasons, err := db.ListMarkReasons()
	if err != nil {
		t.Fatalf("ListMarkReasons: %v", err)
	}
	if len(reasons) != 2 || reasons[1].Reason != "mark for move" {
		t.Errorf("ListMarkReasons() = %+v, want the new reason appended", reasons)
	}
}

// TestAddMarkReasonRejectsDuplicate guards the UNIQUE constraint on
// mark_reason.reason -- callers (the Settings overlay) rely on this
// erroring rather than silently creating a second row for the same text.
func TestAddMarkReasonRejectsDuplicate(t *testing.T) {
	db := openTestDB(t)
	if _, err := db.AddMarkReason("mark for deletion"); err == nil {
		t.Error("AddMarkReason(duplicate of the seeded reason) = nil error, want a UNIQUE constraint error")
	}
}

func TestAddTagAppendsAfterSeededRows(t *testing.T) {
	db := openTestDB(t)

	id, err := db.AddTag("spanish")
	if err != nil {
		t.Fatalf("AddTag: %v", err)
	}
	if id != 4 {
		t.Errorf("AddTag id = %d, want 4 (after the seeded ids 1-3)", id)
	}

	tags, err := db.ListTags()
	if err != nil {
		t.Fatalf("ListTags: %v", err)
	}
	if len(tags) != 4 || tags[3].Tagname != "spanish" {
		t.Errorf("ListTags() = %+v, want the new tag appended", tags)
	}
}

func TestAddTagRejectsDuplicate(t *testing.T) {
	db := openTestDB(t)
	if _, err := db.AddTag("hindi"); err == nil {
		t.Error("AddTag(duplicate of a seeded tag) = nil error, want a UNIQUE constraint error")
	}
}

func TestDeleteMarkReasonRemovesRow(t *testing.T) {
	db := openTestDB(t)
	id, err := db.AddMarkReason("mark for move")
	if err != nil {
		t.Fatalf("AddMarkReason: %v", err)
	}
	if err := db.DeleteMarkReason(id); err != nil {
		t.Fatalf("DeleteMarkReason: %v", err)
	}
	reasons, err := db.ListMarkReasons()
	if err != nil {
		t.Fatalf("ListMarkReasons: %v", err)
	}
	for _, r := range reasons {
		if r.ID == id {
			t.Errorf("ListMarkReasons() still contains deleted id %d: %+v", id, reasons)
		}
	}
}

// TestDeleteMarkReasonClearsReferencingTracks guards the orphan-
// reference fix DeleteMarkReason exists for: without clearing tracks'
// mark column first, Get would start erroring (dangling FK) for any
// track that still had the deleted reason set.
func TestDeleteMarkReasonClearsReferencingTracks(t *testing.T) {
	db := openTestDB(t)
	seededID := int64(1) // "mark for deletion", seeded by Open

	if err := db.SetMarks("artist/track.mp3", []int64{seededID}); err != nil {
		t.Fatalf("SetMark: %v", err)
	}
	if err := db.DeleteMarkReason(seededID); err != nil {
		t.Fatalf("DeleteMarkReason: %v", err)
	}

	track, err := db.Get("artist/track.mp3")
	if err != nil {
		t.Fatalf("Get after deleting the referenced mark reason returned an error (dangling reference): %v", err)
	}
	if len(track.Marks) != 0 {
		t.Errorf("track.Marks after deleting the referenced reason = %+v, want none (cleared)", track.Marks)
	}
}

func TestDeleteTagRemovesRow(t *testing.T) {
	db := openTestDB(t)
	id, err := db.AddTag("spanish")
	if err != nil {
		t.Fatalf("AddTag: %v", err)
	}
	if err := db.DeleteTag(id); err != nil {
		t.Fatalf("DeleteTag: %v", err)
	}
	tags, err := db.ListTags()
	if err != nil {
		t.Fatalf("ListTags: %v", err)
	}
	for _, tg := range tags {
		if tg.ID == id {
			t.Errorf("ListTags() still contains deleted id %d: %+v", id, tags)
		}
	}
}

// TestDeleteTagClearsReferencingTrackTags mirrors
// TestDeleteMarkReasonClearsReferencingTracks for the tags/track_tags
// join table: a track's Tags list must no longer include the deleted
// tag afterward, not error or leave a dangling join row.
func TestDeleteTagClearsReferencingTrackTags(t *testing.T) {
	db := openTestDB(t)
	seededID := int64(1) // "bengali", seeded by Open

	if err := db.SetTags("artist/track.mp3", []int64{seededID}); err != nil {
		t.Fatalf("SetTags: %v", err)
	}
	if err := db.DeleteTag(seededID); err != nil {
		t.Fatalf("DeleteTag: %v", err)
	}

	track, err := db.Get("artist/track.mp3")
	if err != nil {
		t.Fatalf("Get after deleting a referenced tag returned an error: %v", err)
	}
	for _, tg := range track.Tags {
		if tg.ID == seededID {
			t.Errorf("track.Tags after deleting the referenced tag = %+v, still contains it", track.Tags)
		}
	}
}

// --- Error paths --------------------------------------------------------
//
// Every write below shares the same shape: do the work, return the first
// error. A closed database is the cheapest way to make each of those
// errors actually happen, which is what these cover -- the arms are
// otherwise unreachable and would hide a swallowed error.

func closedDB(t *testing.T) *metadata.DB {
	t.Helper()
	db, err := metadata.Open(filepath.Join(t.TempDir(), "closed.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	return db
}

func TestWritesReportErrorsOnAClosedDatabase(t *testing.T) {
	db := closedDB(t)

	cases := []struct {
		name string
		call func() error
	}{
		{"SetMarks", func() error { return db.SetMarks("a.mp3", []int64{1}) }},
		{"ToggleMark", func() error { _, err := db.ToggleMark("a.mp3", 1); return err }},
		{"SetTags", func() error { return db.SetTags("a.mp3", []int64{1}) }},
		{"ToggleTag", func() error { _, err := db.ToggleTag("a.mp3", 1); return err }},
		{"DeleteMarkReason", func() error { return db.DeleteMarkReason(1) }},
		{"DeleteTag", func() error { return db.DeleteTag(1) }},
		{"AddMarkReason", func() error { _, err := db.AddMarkReason("x"); return err }},
		{"AddTag", func() error { _, err := db.AddTag("x"); return err }},
		{"Rate", func() error { return db.Rate("a.mp3", 3) }},
		{"CreateBookmark", func() error { _, err := db.CreateBookmark("a.mp3", 0, "n"); return err }},
		{"UpdateBookmark", func() error { return db.UpdateBookmark(1, "n") }},
		{"DeleteBookmark", func() error { return db.DeleteBookmark(1) }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.call(); err == nil {
				t.Error("no error from a closed database")
			}
		})
	}
}

func TestReadsReportErrorsOnAClosedDatabase(t *testing.T) {
	db := closedDB(t)

	cases := []struct {
		name string
		call func() error
	}{
		{"Get", func() error { _, err := db.Get("a.mp3"); return err }},
		{"ListMarkReasons", func() error { _, err := db.ListMarkReasons(); return err }},
		{"ListTags", func() error { _, err := db.ListTags(); return err }},
		{"BookmarksForTrack", func() error { _, err := db.BookmarksForTrack("a.mp3"); return err }},
		{"GetBookmark", func() error { _, err := db.GetBookmark(1); return err }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.call(); err == nil {
				t.Error("no error from a closed database")
			}
		})
	}
}

// TestOpenRejectsAnUnusablePath covers Open's own failure arm: a path
// that cannot be created must come back as an error rather than a DB
// that fails later on first use.
func TestOpenRejectsAnUnusablePath(t *testing.T) {
	// A directory where a file is expected.
	dir := t.TempDir()
	if _, err := metadata.Open(dir); err == nil {
		t.Error("Open succeeded on a directory path, want an error")
	}
}

// TestOpenIsIdempotentAcrossReopens covers the schema and seed
// statements running a second time against a database that already has
// them -- the normal case on every launch after the first.
func TestOpenIsIdempotentAcrossReopens(t *testing.T) {
	path := filepath.Join(t.TempDir(), "reopen.db")

	first, err := metadata.Open(path)
	if err != nil {
		t.Fatalf("first Open: %v", err)
	}
	if _, err := first.AddMarkReason("keep me"); err != nil {
		t.Fatalf("AddMarkReason: %v", err)
	}
	reasonsBefore, err := first.ListMarkReasons()
	if err != nil {
		t.Fatalf("ListMarkReasons: %v", err)
	}
	first.Close()

	second, err := metadata.Open(path)
	if err != nil {
		t.Fatalf("reopening: %v", err)
	}
	t.Cleanup(func() { second.Close() })

	reasonsAfter, err := second.ListMarkReasons()
	if err != nil {
		t.Fatalf("ListMarkReasons after reopen: %v", err)
	}
	if len(reasonsAfter) != len(reasonsBefore) {
		t.Errorf("reopening changed the catalog: %d reasons before, %d after",
			len(reasonsBefore), len(reasonsAfter))
	}
	var found bool
	for _, r := range reasonsAfter {
		if r.Reason == "keep me" {
			found = true
		}
	}
	if !found {
		t.Error("a row added before the reopen is gone afterwards")
	}
}
