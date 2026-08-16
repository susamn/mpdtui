package tests

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
	if track.PlayCount != 0 || track.Rating != 0 || track.Mark != nil || len(track.Tags) != 0 {
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
	if err := db.SetMark(file, &reasonID); err != nil {
		t.Fatalf("SetMark: %v", err)
	}
	track, err := db.Get(file)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if track.Mark == nil || track.Mark.Reason != "mark for deletion" {
		t.Fatalf("Mark after SetMark = %+v, want {1 mark for deletion}", track.Mark)
	}

	if err := db.SetMark(file, nil); err != nil {
		t.Fatalf("SetMark(nil): %v", err)
	}
	track, err = db.Get(file)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if track.Mark != nil {
		t.Errorf("Mark after clearing = %+v, want nil", track.Mark)
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
