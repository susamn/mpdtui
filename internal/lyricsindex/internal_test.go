package lyricsindex

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// TestStatMtimeNSOnAMissingFile covers the sidecar-freshness probe for a
// file that is not there: it reports zero rather than failing, which is
// what makes "no sidecar" and "sidecar unchanged" comparable.
func TestStatMtimeNSOnAMissingFile(t *testing.T) {
	if got := statMtimeNS(filepath.Join(t.TempDir(), "nope.txt")); got != 0 {
		t.Errorf("statMtimeNS of a missing file = %d, want 0", got)
	}
}

func TestStatMtimeNSOnARealFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "lyrics.txt")
	if err := os.WriteFile(path, []byte("words"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if got := statMtimeNS(path); got == 0 {
		t.Error("statMtimeNS of a real file = 0, want its modification time")
	}
}

func TestMaxInt64(t *testing.T) {
	cases := []struct{ a, b, want int64 }{
		{1, 2, 2},
		{2, 1, 2},
		{5, 5, 5},
		{-3, -1, -1},
		{0, -1, 0},
	}
	for _, tc := range cases {
		if got := maxInt64(tc.a, tc.b); got != tc.want {
			t.Errorf("maxInt64(%d, %d) = %d, want %d", tc.a, tc.b, got, tc.want)
		}
	}
}

func TestOpenRejectsAnEmptyPath(t *testing.T) {
	if _, err := open(""); err == nil {
		t.Error("open(\"\") succeeded, want an error -- there is nowhere to write")
	}
}

func TestOpenRejectsAnUnusablePath(t *testing.T) {
	// A directory where a file is expected.
	if _, err := open(t.TempDir()); err == nil {
		t.Error("open succeeded on a directory path, want an error")
	}
}

// TestOpenDropsTheTableOnASchemaChange covers the derived-cache
// decision: the index is rebuildable, so a schema bump drops the table
// rather than carrying migration code. Simulated by writing an older
// schema_version into a real index.
func TestOpenDropsTheTableOnASchemaChange(t *testing.T) {
	path := filepath.Join(t.TempDir(), "lyrics.db")

	db, err := open(path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if _, err := db.Exec(
		`INSERT INTO entries (file, artist, title, kinds, mtime_ns, text, text_folded)
		 VALUES ('a.mp3', 'A', 'T', 'txt', 1, 'words', 'words')`); err != nil {
		t.Fatalf("seeding an entry: %v", err)
	}
	// Pretend it was written by an older build.
	if _, err := db.Exec(`UPDATE meta SET value = '0' WHERE key = 'schema_version'`); err != nil {
		t.Fatalf("rewinding the schema version: %v", err)
	}
	db.Close()

	db, err = open(path)
	if err != nil {
		t.Fatalf("reopening: %v", err)
	}
	defer db.Close()

	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM entries`).Scan(&n); err != nil {
		t.Fatalf("counting entries: %v", err)
	}
	if n != 0 {
		t.Errorf("%d entries survived a schema change, want the table dropped", n)
	}
	if got := metaValue(db, "schema_version"); got != fmt.Sprint(schemaVersion) {
		t.Errorf("schema_version = %q, want %q", got, fmt.Sprint(schemaVersion))
	}
}

// TestMetaValueMissingKey covers the "absent reads as empty" choice,
// which is what lets ReadInfo report a never-built index without
// distinguishing error from absence.
func TestMetaValueMissingKey(t *testing.T) {
	db, err := open(filepath.Join(t.TempDir(), "lyrics.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()

	if got := metaValue(db, "no-such-key"); got != "" {
		t.Errorf("metaValue of a missing key = %q, want empty", got)
	}
}
