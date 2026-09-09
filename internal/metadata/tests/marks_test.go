package tests

import (
	"database/sql"
	"path/filepath"
	"testing"

	_ "modernc.org/sqlite"

	"mpdtui/internal/metadata"
)

// legacySchema is the schema as it stood before marks became
// many-to-many: a single nullable tracks.mark foreign key. Databases in
// this shape exist on real machines, so the migration is tested against
// one built here rather than against an assumption about it.
const legacySchema = `
CREATE TABLE mark_reason (id INTEGER PRIMARY KEY, reason TEXT NOT NULL UNIQUE);
CREATE TABLE tags (id INTEGER PRIMARY KEY, tagname TEXT NOT NULL UNIQUE);
CREATE TABLE tracks (
	id              INTEGER PRIMARY KEY AUTOINCREMENT,
	normalized_path TEXT NOT NULL UNIQUE,
	real_path       TEXT NOT NULL,
	play_count      INTEGER NOT NULL DEFAULT 0,
	rating          INTEGER NOT NULL DEFAULT 0,
	mark            INTEGER REFERENCES mark_reason(id),
	updated_at      TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE TABLE track_tags (
	track_id INTEGER NOT NULL REFERENCES tracks(id) ON DELETE CASCADE,
	tag_id   INTEGER NOT NULL REFERENCES tags(id),
	PRIMARY KEY (track_id, tag_id)
);
INSERT INTO mark_reason (id, reason) VALUES (1, 'mark for deletion'), (2, 'bad rip'), (3, 'wrong lyrics');
`

// writeLegacyDB builds a pre-migration database at path containing the
// given (normalized_path, mark) pairs, with a NULL mark written as 0.
//
// The paths used here are deliberately already in normalized form --
// lowercase, letters and digits only per segment -- so that looking the
// same string back up through Get, which folds what it is given, finds
// the row. The migration itself never looks at paths; it copies (id,
// mark) pairs, so this costs the test nothing.
func writeLegacyDB(t *testing.T, path string, marks map[string]int64) {
	t.Helper()
	sqlDB, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("open legacy db: %v", err)
	}
	defer sqlDB.Close()
	if _, err := sqlDB.Exec(legacySchema); err != nil {
		t.Fatalf("legacy schema: %v", err)
	}
	for file, mark := range marks {
		var m any
		if mark != 0 {
			m = mark
		}
		if _, err := sqlDB.Exec(
			`INSERT INTO tracks (normalized_path, real_path, rating, mark) VALUES (?, ?, 3, ?)`,
			file, file, m); err != nil {
			t.Fatalf("insert legacy track %q: %v", file, err)
		}
	}
}

// TestMigrationKeepsExistingMarks is the one that matters: an upgrade
// runs this unattended on the next launch, and it drops the column the
// old marks live in. Every mark that was there has to still be there
// afterwards.
func TestMigrationKeepsExistingMarks(t *testing.T) {
	path := filepath.Join(t.TempDir(), "legacy.db")
	writeLegacyDB(t, path, map[string]int64{
		"artist/one":   1,
		"artist/two":   2,
		"artist/three": 3,
		"artist/four":  1,
		"artist/plain": 0, // no mark
	})

	db, err := metadata.Open(path)
	if err != nil {
		t.Fatalf("Open (runs the migration): %v", err)
	}
	defer db.Close()

	want := map[string]string{
		"artist/one":   "mark for deletion",
		"artist/two":   "bad rip",
		"artist/three": "wrong lyrics",
		"artist/four":  "mark for deletion",
	}
	for file, reason := range want {
		track, err := db.Get(file)
		if err != nil {
			t.Fatalf("Get(%q): %v", file, err)
		}
		if len(track.Marks) != 1 || track.Marks[0].Reason != reason {
			t.Errorf("%s: marks after migration = %+v, want exactly [%q]", file, track.Marks, reason)
		}
	}

	plain, err := db.Get("artist/plain")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if len(plain.Marks) != 0 {
		t.Errorf("unmarked track gained marks in the migration: %+v", plain.Marks)
	}
}

// TestMigrationKeepsEverythingElse guards the blunt instrument the
// migration uses -- ALTER TABLE ... DROP COLUMN rewrites the table, so
// the rest of each row has to survive it too.
func TestMigrationKeepsEverythingElse(t *testing.T) {
	path := filepath.Join(t.TempDir(), "legacy.db")
	writeLegacyDB(t, path, map[string]int64{"artist/one": 2})

	db, err := metadata.Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer db.Close()

	track, err := db.Get("artist/one")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if track.Rating != 3 {
		t.Errorf("rating after migration = %d, want the 3 it had", track.Rating)
	}
	if track.RealPath != "artist/one" {
		t.Errorf("real_path after migration = %q, want it unchanged", track.RealPath)
	}
}

// TestMigrationIsIdempotent covers the fact that it runs on every Open,
// not once: a second launch must not re-copy, duplicate, or fail.
func TestMigrationIsIdempotent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "legacy.db")
	writeLegacyDB(t, path, map[string]int64{"artist/one": 1, "artist/two": 2})

	for i := 1; i <= 3; i++ {
		db, err := metadata.Open(path)
		if err != nil {
			t.Fatalf("Open #%d: %v", i, err)
		}
		track, err := db.Get("artist/one")
		if err != nil {
			t.Fatalf("Get on open #%d: %v", i, err)
		}
		if len(track.Marks) != 1 {
			t.Errorf("open #%d: marks = %+v, want exactly one", i, track.Marks)
		}
		db.Close()
	}
}

// TestMigrationDropsTheOldColumn: leaving it behind would be a second,
// silently diverging source of truth for the same fact.
func TestMigrationDropsTheOldColumn(t *testing.T) {
	path := filepath.Join(t.TempDir(), "legacy.db")
	writeLegacyDB(t, path, map[string]int64{"artist/one": 1})

	db, err := metadata.Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	db.Close()

	sqlDB, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer sqlDB.Close()
	var n int
	if err := sqlDB.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('tracks') WHERE name = 'mark'`).Scan(&n); err != nil {
		t.Fatalf("pragma: %v", err)
	}
	if n != 0 {
		t.Error("tracks.mark still present after the migration")
	}
}

// TestFreshDatabaseNeedsNoMigration: a database created by this version
// has no mark column to begin with, so the migration must be a no-op
// rather than an error.
func TestFreshDatabaseNeedsNoMigration(t *testing.T) {
	db := openTestDB(t)
	if err := db.SetMarks("artist/one", []int64{1}); err != nil {
		t.Fatalf("SetMarks: %v", err)
	}
	track, err := db.Get("artist/one")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if len(track.Marks) != 1 {
		t.Errorf("marks = %+v, want one", track.Marks)
	}
}

// --- many-to-many behaviour ---

func TestTrackCanHoldSeveralMarks(t *testing.T) {
	db := openTestDB(t)
	id2, err := db.AddMarkReason("bad rip")
	if err != nil {
		t.Fatalf("AddMarkReason: %v", err)
	}

	if err := db.SetMarks("artist/one", []int64{1, id2}); err != nil {
		t.Fatalf("SetMarks: %v", err)
	}
	track, err := db.Get("artist/one")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if len(track.Marks) != 2 {
		t.Fatalf("marks = %+v, want two", track.Marks)
	}
	// Ordered by catalog id, so the same track reads the same way
	// everywhere it is shown.
	if track.Marks[0].ID > track.Marks[1].ID {
		t.Errorf("marks not ordered by id: %+v", track.Marks)
	}
}

func TestMarkAppliesToSeveralTracks(t *testing.T) {
	db := openTestDB(t)
	for _, f := range []string{"artist/one", "artist/two", "artist/three"} {
		if err := db.SetMarks(f, []int64{1}); err != nil {
			t.Fatalf("SetMarks(%q): %v", f, err)
		}
	}
	for _, f := range []string{"artist/one", "artist/two", "artist/three"} {
		track, err := db.Get(f)
		if err != nil {
			t.Fatalf("Get(%q): %v", f, err)
		}
		if len(track.Marks) != 1 || track.Marks[0].ID != 1 {
			t.Errorf("%s: marks = %+v, want the shared mark", f, track.Marks)
		}
	}
}

func TestSetMarksReplacesRatherThanAdds(t *testing.T) {
	db := openTestDB(t)
	id2, _ := db.AddMarkReason("bad rip")

	if err := db.SetMarks("artist/one", []int64{1, id2}); err != nil {
		t.Fatalf("SetMarks: %v", err)
	}
	if err := db.SetMarks("artist/one", []int64{id2}); err != nil {
		t.Fatalf("SetMarks: %v", err)
	}
	track, _ := db.Get("artist/one")
	if len(track.Marks) != 1 || track.Marks[0].ID != id2 {
		t.Errorf("marks after replacing = %+v, want only the second", track.Marks)
	}
}

func TestSetMarksIgnoresDuplicates(t *testing.T) {
	db := openTestDB(t)
	if err := db.SetMarks("artist/one", []int64{1, 1, 1}); err != nil {
		t.Fatalf("SetMarks: %v", err)
	}
	track, _ := db.Get("artist/one")
	if len(track.Marks) != 1 {
		t.Errorf("marks = %+v, want the duplicate collapsed", track.Marks)
	}
}

func TestToggleMarkAddsThenRemoves(t *testing.T) {
	db := openTestDB(t)

	on, err := db.ToggleMark("artist/one", 1)
	if err != nil {
		t.Fatalf("ToggleMark: %v", err)
	}
	if !on {
		t.Error("first toggle reported the mark as unset, want set")
	}
	track, _ := db.Get("artist/one")
	if len(track.Marks) != 1 {
		t.Fatalf("marks after first toggle = %+v, want one", track.Marks)
	}

	on, err = db.ToggleMark("artist/one", 1)
	if err != nil {
		t.Fatalf("ToggleMark: %v", err)
	}
	if on {
		t.Error("second toggle reported the mark as set, want cleared")
	}
	track, _ = db.Get("artist/one")
	if len(track.Marks) != 0 {
		t.Errorf("marks after second toggle = %+v, want none", track.Marks)
	}
}

func TestToggleMarkLeavesOtherMarksAlone(t *testing.T) {
	db := openTestDB(t)
	id2, _ := db.AddMarkReason("bad rip")
	if err := db.SetMarks("artist/one", []int64{1, id2}); err != nil {
		t.Fatalf("SetMarks: %v", err)
	}

	if _, err := db.ToggleMark("artist/one", 1); err != nil {
		t.Fatalf("ToggleMark: %v", err)
	}
	track, _ := db.Get("artist/one")
	if len(track.Marks) != 1 || track.Marks[0].ID != id2 {
		t.Errorf("marks = %+v, want only the untouched one", track.Marks)
	}
}

// TestDeleteMarkReasonRemovesItFromEveryTrack: the catalog row going
// away must not leave tracks pointing at something that no longer
// exists.
func TestDeleteMarkReasonRemovesItFromEveryTrack(t *testing.T) {
	db := openTestDB(t)
	doomed, _ := db.AddMarkReason("temporary")
	for _, f := range []string{"artist/one", "artist/two"} {
		if err := db.SetMarks(f, []int64{1, doomed}); err != nil {
			t.Fatalf("SetMarks: %v", err)
		}
	}

	if err := db.DeleteMarkReason(doomed); err != nil {
		t.Fatalf("DeleteMarkReason: %v", err)
	}
	for _, f := range []string{"artist/one", "artist/two"} {
		track, err := db.Get(f)
		if err != nil {
			t.Fatalf("Get(%q): %v", f, err)
		}
		if len(track.Marks) != 1 || track.Marks[0].ID != 1 {
			t.Errorf("%s: marks after deleting one reason = %+v, want only the surviving one", f, track.Marks)
		}
	}
}

// --- tags: the same relation, exercised the same way ---

func TestToggleTagAddsThenRemoves(t *testing.T) {
	db := openTestDB(t)

	on, err := db.ToggleTag("artist/one", 1)
	if err != nil {
		t.Fatalf("ToggleTag: %v", err)
	}
	if !on {
		t.Error("first toggle reported the tag as unset, want set")
	}
	track, _ := db.Get("artist/one")
	if len(track.Tags) != 1 {
		t.Fatalf("tags after first toggle = %+v, want one", track.Tags)
	}

	on, err = db.ToggleTag("artist/one", 1)
	if err != nil {
		t.Fatalf("ToggleTag: %v", err)
	}
	if on {
		t.Error("second toggle reported the tag as set, want cleared")
	}
	track, _ = db.Get("artist/one")
	if len(track.Tags) != 0 {
		t.Errorf("tags after second toggle = %+v, want none", track.Tags)
	}
}

func TestToggleTagLeavesOtherTagsAlone(t *testing.T) {
	db := openTestDB(t)
	if err := db.SetTags("artist/one", []int64{1, 2}); err != nil {
		t.Fatalf("SetTags: %v", err)
	}
	if _, err := db.ToggleTag("artist/one", 1); err != nil {
		t.Fatalf("ToggleTag: %v", err)
	}
	track, _ := db.Get("artist/one")
	if len(track.Tags) != 1 || track.Tags[0].ID != 2 {
		t.Errorf("tags = %+v, want only the untouched one", track.Tags)
	}
}

func TestSetTagsIgnoresDuplicates(t *testing.T) {
	db := openTestDB(t)
	if err := db.SetTags("artist/one", []int64{2, 2, 2}); err != nil {
		t.Fatalf("SetTags: %v", err)
	}
	track, _ := db.Get("artist/one")
	if len(track.Tags) != 1 {
		t.Errorf("tags = %+v, want the duplicate collapsed", track.Tags)
	}
}

func TestTagAppliesToSeveralTracksAndTrackHoldsSeveralTags(t *testing.T) {
	db := openTestDB(t)
	if err := db.SetTags("artist/one", []int64{1, 2, 3}); err != nil {
		t.Fatalf("SetTags: %v", err)
	}
	if err := db.SetTags("artist/two", []int64{1}); err != nil {
		t.Fatalf("SetTags: %v", err)
	}

	one, _ := db.Get("artist/one")
	if len(one.Tags) != 3 {
		t.Errorf("first track tags = %+v, want three", one.Tags)
	}
	two, _ := db.Get("artist/two")
	if len(two.Tags) != 1 || two.Tags[0].ID != 1 {
		t.Errorf("second track tags = %+v, want the shared one", two.Tags)
	}
}
