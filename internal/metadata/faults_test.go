package metadata

import (
	"os"
	"path/filepath"
	"testing"
)

// Every write here is a sequence of steps that each return the first
// error they see. A closed database covers the first step of each; these
// cover the later ones, by removing exactly the table a given step needs
// while leaving the rest of the schema intact. Without this the arms are
// unreachable, and a swallowed error in the middle of a multi-step write
// would look identical to success.

func newDB(t *testing.T) *DB {
	t.Helper()
	db, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

// drop removes a table so the next statement touching it fails.
func drop(t *testing.T, db *DB, table string) {
	t.Helper()
	if _, err := db.sql.Exec("DROP TABLE " + table); err != nil {
		t.Fatalf("dropping %s: %v", table, err)
	}
}

func TestSetMarksReportsAFailedJoinWrite(t *testing.T) {
	db := newDB(t)
	if err := db.SetMarks("a.mp3", []int64{1}); err != nil {
		t.Fatalf("setup: %v", err)
	}

	drop(t, db, "track_marks")
	if err := db.SetMarks("a.mp3", []int64{1}); err == nil {
		t.Error("SetMarks succeeded with no track_marks table")
	}
}

func TestSetTagsReportsAFailedJoinWrite(t *testing.T) {
	db := newDB(t)
	if err := db.SetTags("a.mp3", []int64{1}); err != nil {
		t.Fatalf("setup: %v", err)
	}

	drop(t, db, "track_tags")
	if err := db.SetTags("a.mp3", []int64{1}); err == nil {
		t.Error("SetTags succeeded with no track_tags table")
	}
}

func TestToggleMarkReportsAFailedRead(t *testing.T) {
	db := newDB(t)
	if _, err := db.ToggleMark("a.mp3", 1); err != nil {
		t.Fatalf("setup: %v", err)
	}

	drop(t, db, "track_marks")
	if _, err := db.ToggleMark("a.mp3", 1); err == nil {
		t.Error("ToggleMark succeeded with no track_marks table")
	}
}

func TestToggleTagReportsAFailedRead(t *testing.T) {
	db := newDB(t)
	if _, err := db.ToggleTag("a.mp3", 1); err != nil {
		t.Fatalf("setup: %v", err)
	}

	drop(t, db, "track_tags")
	if _, err := db.ToggleTag("a.mp3", 1); err == nil {
		t.Error("ToggleTag succeeded with no track_tags table")
	}
}

// TestDeleteMarkReasonReportsAFailedCascade covers the arm that matters
// most here: the delete also clears every track still referencing the
// row, so a failure there must surface rather than leave a dangling
// reference behind.
func TestDeleteMarkReasonReportsAFailedCascade(t *testing.T) {
	db := newDB(t)
	drop(t, db, "track_marks")

	if err := db.DeleteMarkReason(1); err == nil {
		t.Error("DeleteMarkReason succeeded with no track_marks table to clear")
	}
}

func TestDeleteTagReportsAFailedCascade(t *testing.T) {
	db := newDB(t)
	drop(t, db, "track_tags")

	if err := db.DeleteTag(1); err == nil {
		t.Error("DeleteTag succeeded with no track_tags table to clear")
	}
}

func TestGetReportsAFailedMarksRead(t *testing.T) {
	db := newDB(t)
	if err := db.Rate("a.mp3", 3); err != nil {
		t.Fatalf("setup: %v", err)
	}

	drop(t, db, "track_marks")
	if _, err := db.Get("a.mp3"); err == nil {
		t.Error("Get succeeded with no track_marks table")
	}
}

func TestGetReportsAFailedTagsRead(t *testing.T) {
	db := newDB(t)
	if err := db.Rate("a.mp3", 3); err != nil {
		t.Fatalf("setup: %v", err)
	}

	drop(t, db, "track_tags")
	if _, err := db.Get("a.mp3"); err == nil {
		t.Error("Get succeeded with no track_tags table")
	}
}

func TestBookmarkWritesReportFailures(t *testing.T) {
	db := newDB(t)
	bm, err := db.CreateBookmark("a.mp3", 10, "note")
	if err != nil {
		t.Fatalf("setup: %v", err)
	}

	drop(t, db, "bookmarks")

	if _, err := db.CreateBookmark("a.mp3", 20, "another"); err == nil {
		t.Error("CreateBookmark succeeded with no bookmarks table")
	}
	if err := db.UpdateBookmark(bm.ID, "edited"); err == nil {
		t.Error("UpdateBookmark succeeded with no bookmarks table")
	}
	if err := db.DeleteBookmark(bm.ID); err == nil {
		t.Error("DeleteBookmark succeeded with no bookmarks table")
	}
	if _, err := db.GetBookmark(bm.ID); err == nil {
		t.Error("GetBookmark succeeded with no bookmarks table")
	}
	if _, err := db.BookmarksForTrack("a.mp3"); err == nil {
		t.Error("BookmarksForTrack succeeded with no bookmarks table")
	}
}

func TestIncrementPlayCountReportsAFailure(t *testing.T) {
	db := newDB(t)
	if err := db.Rate("a.mp3", 3); err != nil {
		t.Fatalf("setup: %v", err)
	}

	drop(t, db, "tracks")
	if err := db.IncrementPlayCount("a.mp3"); err == nil {
		t.Error("IncrementPlayCount succeeded with no tracks table")
	}
}

func TestListsReportAFailedRead(t *testing.T) {
	db := newDB(t)
	drop(t, db, "mark_reason")
	if _, err := db.ListMarkReasons(); err == nil {
		t.Error("ListMarkReasons succeeded with no mark_reason table")
	}

	db2 := newDB(t)
	drop(t, db2, "tags")
	if _, err := db2.ListTags(); err == nil {
		t.Error("ListTags succeeded with no tags table")
	}
}

// TestMigrateMarksToJoinTableOnAFreshSchema covers the migration's
// no-op path -- a database created by this build already has the join
// table and no legacy column, which is every run after the first.
func TestMigrateMarksToJoinTableOnAFreshSchema(t *testing.T) {
	db := newDB(t)
	if err := migrateMarksToJoinTable(db.sql); err != nil {
		t.Errorf("re-running the migration on a current schema failed: %v", err)
	}
}

// TestMigrateMarksToJoinTableMovesLegacyRows covers the migration
// itself: a database from before marks became a many-to-many relation
// has a `mark` column on tracks, whose values have to move into
// track_marks before that column is dropped.
func TestMigrateMarksToJoinTableMovesLegacyRows(t *testing.T) {
	db := newDB(t)
	if err := db.Rate("a.mp3", 3); err != nil {
		t.Fatalf("setup: %v", err)
	}

	// Re-create the pre-migration shape.
	if _, err := db.sql.Exec(`ALTER TABLE tracks ADD COLUMN mark INTEGER`); err != nil {
		t.Fatalf("adding the legacy column: %v", err)
	}
	if _, err := db.sql.Exec(`UPDATE tracks SET mark = 1`); err != nil {
		t.Fatalf("seeding the legacy value: %v", err)
	}

	if err := migrateMarksToJoinTable(db.sql); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	track, err := db.Get("a.mp3")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if len(track.Marks) != 1 {
		t.Errorf("track has %d marks after the migration, want the legacy one moved across", len(track.Marks))
	}
	gone, err := columnExists(db.sql, "tracks", "mark")
	if err != nil {
		t.Fatalf("columnExists: %v", err)
	}
	if gone {
		t.Error("the legacy mark column survived a successful migration")
	}
}

// TestMigrateMarksRefusesToLoseData covers the guard on the one
// irreversible step in the package. It runs unattended on the first
// launch after an upgrade, so it counts the marks reachable through the
// join table and refuses to drop the old column unless that matches
// what was there. Here the legacy value points at a mark_reason that no
// longer exists, so the foreign key stops the copy and the count comes
// up short.
func TestMigrateMarksRefusesToLoseData(t *testing.T) {
	db := newDB(t)
	if err := db.Rate("a.mp3", 3); err != nil {
		t.Fatalf("setup: %v", err)
	}
	if _, err := db.sql.Exec(`ALTER TABLE tracks ADD COLUMN mark INTEGER`); err != nil {
		t.Fatalf("adding the legacy column: %v", err)
	}
	// 999 is not a real mark_reason id.
	if _, err := db.sql.Exec(`UPDATE tracks SET mark = 999`); err != nil {
		t.Fatalf("seeding the legacy value: %v", err)
	}
	if _, err := db.sql.Exec(`DROP TABLE track_marks`); err != nil {
		t.Fatalf("removing the destination: %v", err)
	}

	err := migrateMarksToJoinTable(db.sql)
	if err == nil {
		t.Fatal("the migration reported success despite being unable to copy")
	}

	// The old column and its data must still be there.
	still, cerr := columnExists(db.sql, "tracks", "mark")
	if cerr != nil {
		t.Fatalf("columnExists: %v", cerr)
	}
	if !still {
		t.Error("the legacy column was dropped despite the migration failing")
	}
}

func TestColumnExists(t *testing.T) {
	db := newDB(t)

	got, err := columnExists(db.sql, "tracks", "rating")
	if err != nil {
		t.Fatalf("columnExists: %v", err)
	}
	if !got {
		t.Error("columnExists says tracks.rating is missing")
	}

	got, err = columnExists(db.sql, "tracks", "no_such_column")
	if err != nil {
		t.Fatalf("columnExists: %v", err)
	}
	if got {
		t.Error("columnExists found a column that does not exist")
	}

	// A table that does not exist reads as "no such column" rather than
	// erroring, which is what makes the migration check safe to run
	// against any schema version.
	got, err = columnExists(db.sql, "no_such_table", "anything")
	if err != nil {
		t.Fatalf("columnExists on a missing table: %v", err)
	}
	if got {
		t.Error("columnExists found a column in a nonexistent table")
	}

	// A closed database is a real failure and must be reported.
	db.Close()
	if _, err := columnExists(db.sql, "tracks", "rating"); err == nil {
		t.Error("columnExists on a closed database reported no error")
	}
}

// TestOpenReportsAFailedSchema covers Open's own setup arms. Each runs
// on every launch, and a failure there has to come back as an error
// rather than a DB that fails later on first use.
func TestOpenReportsAFailedSchema(t *testing.T) {
	// A path inside a directory that does not exist.
	if _, err := Open(filepath.Join(t.TempDir(), "no-such-dir", "x.db")); err == nil {
		t.Error("Open succeeded with a nonexistent parent directory")
	}

	// A file that exists but is not a database.
	path := filepath.Join(t.TempDir(), "notadb")
	if err := os.WriteFile(path, []byte("this is not sqlite"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if _, err := Open(path); err == nil {
		t.Error("Open succeeded on a file that is not a database")
	}
}

// The migration's own read arms (the before-count and the copy) are
// deliberately not tested. Reaching them needs the legacy `mark` column
// to exist while the statements over it fail, and dropping the tracks
// table takes the column with it, so the migration short-circuits on
// columnExists before it gets there. Its two decisions that matter --
// moving the rows across, and refusing to drop the column when the copy
// comes up short -- are both covered above.
