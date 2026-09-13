package lyricsindex

import (
	"context"
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

// TestReindexReportsProgress covers the Progress callback, which the 'I'
// overlay uses to show a live count. It fires every progressEvery
// tracks plus once at the end, coarse enough not to swamp a redraw
// queue on a large library.
func TestReindexReportsProgress(t *testing.T) {
	musicDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(musicDir, "a"), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	tracks := make([]Track, progressEvery*2+5)
	for i := range tracks {
		tracks[i] = Track{File: fmt.Sprintf("a/%04d.mp3", i), Artist: "A", Title: fmt.Sprintf("T%d", i)}
		// Every track has a sidecar here, so progress fires on the
		// regular cadence -- see TestReindexProgressOnlyCountsTracksWithLyrics
		// for what happens when most do not.
		name := filepath.Join(musicDir, "a", fmt.Sprintf("%04d.txt", i))
		if err := os.WriteFile(name, []byte("words"), 0o644); err != nil {
			t.Fatalf("WriteFile: %v", err)
		}
	}

	path := filepath.Join(t.TempDir(), "lyrics.db")
	var calls [][2]int
	p := func(done, total int) { calls = append(calls, [2]int{done, total}) }

	if _, err := Reindex(context.Background(), path, musicDir, tracks, p); err != nil {
		t.Fatalf("Reindex: %v", err)
	}

	if len(calls) < 3 {
		t.Fatalf("Progress fired %d times over %d tracks, want at least 3", len(calls), len(tracks))
	}
	last := calls[len(calls)-1]
	if last[0] != len(tracks) || last[1] != len(tracks) {
		t.Errorf("final Progress = %v, want (%d, %d)", last, len(tracks), len(tracks))
	}
	for _, c := range calls {
		if c[1] != len(tracks) {
			t.Errorf("Progress reported a total of %d, want %d", c[1], len(tracks))
		}
	}

	// A second pass over the same, unchanged library takes the
	// "unchanged" branch, which reports progress separately.
	calls = nil
	stats, err := Reindex(context.Background(), path, musicDir, tracks, p)
	if err != nil {
		t.Fatalf("second Reindex: %v", err)
	}
	if stats.Unchanged == 0 {
		t.Error("a second pass over an unchanged library read everything again")
	}
	if len(calls) < 3 {
		t.Errorf("Progress fired %d times on the unchanged pass, want at least 3", len(calls))
	}
}

// TestReindexProgressOnlyCountsTracksWithLyrics pins a limitation of the
// progress display rather than an intended behavior.
//
// A track with no .txt and no .lrc is skipped before the progress
// callback, so the counter advances per *track that has lyrics*, not per
// track examined -- while the total it is reported against is the whole
// library. On a library where few tracks have sidecars, the 'I' overlay
// therefore sits near "0 / 8198" for the whole scan and then jumps
// straight to complete, which reads as a hang.
//
// Left as it is: moving the callback above the skip is a one-line change
// but it makes the scan fire progress far more often on exactly the
// libraries where each step is cheapest, and the throttle in
// handleReindexLyrics already assumes the current cadence. Worth knowing
// before anyone reports the overlay as stuck.
func TestReindexProgressOnlyCountsTracksWithLyrics(t *testing.T) {
	musicDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(musicDir, "a"), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	// One sidecar among many tracks.
	if err := os.WriteFile(filepath.Join(musicDir, "a", "0000.txt"), []byte("words"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	tracks := make([]Track, progressEvery*2+5)
	for i := range tracks {
		tracks[i] = Track{File: fmt.Sprintf("a/%04d.mp3", i)}
	}

	var calls [][2]int
	_, err := Reindex(context.Background(), filepath.Join(t.TempDir(), "lyrics.db"),
		musicDir, tracks, func(done, total int) { calls = append(calls, [2]int{done, total}) })
	if err != nil {
		t.Fatalf("Reindex: %v", err)
	}

	if len(calls) != 1 {
		t.Errorf("Progress fired %d times over %d tracks with one sidecar, want only the final call",
			len(calls), len(tracks))
	}
}

// TestReindexUpdatesNamesWithoutRereadingLyrics covers the narrow path
// where a track's sidecar is untouched but its tags changed: the row's
// artist/title are refreshed without re-reading the file.
func TestReindexUpdatesNamesWithoutRereadingLyrics(t *testing.T) {
	musicDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(musicDir, "a"), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(filepath.Join(musicDir, "a", "1.txt"), []byte("words"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	path := filepath.Join(t.TempDir(), "lyrics.db")

	tracks := []Track{{File: "a/1.mp3", Artist: "Old", Title: "Name"}}
	if _, err := Reindex(context.Background(), path, musicDir, tracks, nil); err != nil {
		t.Fatalf("Reindex: %v", err)
	}

	tracks[0].Artist, tracks[0].Title = "New", "Title"
	stats, err := Reindex(context.Background(), path, musicDir, tracks, nil)
	if err != nil {
		t.Fatalf("second Reindex: %v", err)
	}
	if stats.Read != 0 {
		t.Errorf("Read = %d, want 0 -- the sidecar did not change", stats.Read)
	}

	entries, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(entries) != 1 || entries[0].Artist != "New" || entries[0].Title != "Title" {
		t.Errorf("entry = %+v, want the refreshed tags", entries)
	}
}

// TestReindexRemovesVanishedTracks covers the prune pass: a track no
// longer in the library must not linger in the index and keep turning
// up in search.
func TestReindexRemovesVanishedTracks(t *testing.T) {
	musicDir := t.TempDir()
	os.MkdirAll(filepath.Join(musicDir, "a"), 0o755)
	os.WriteFile(filepath.Join(musicDir, "a", "1.txt"), []byte("words"), 0o644)
	os.WriteFile(filepath.Join(musicDir, "a", "2.txt"), []byte("more words"), 0o644)
	path := filepath.Join(t.TempDir(), "lyrics.db")

	both := []Track{{File: "a/1.mp3", Title: "One"}, {File: "a/2.mp3", Title: "Two"}}
	if _, err := Reindex(context.Background(), path, musicDir, both, nil); err != nil {
		t.Fatalf("Reindex: %v", err)
	}

	stats, err := Reindex(context.Background(), path, musicDir, both[:1], nil)
	if err != nil {
		t.Fatalf("second Reindex: %v", err)
	}
	if stats.Removed != 1 {
		t.Errorf("Removed = %d, want 1", stats.Removed)
	}
	entries, _ := Load(path)
	if len(entries) != 1 {
		t.Errorf("%d entries after the prune, want 1", len(entries))
	}
}

func TestReindexRejectsAnUnusablePath(t *testing.T) {
	if _, err := Reindex(context.Background(), "", t.TempDir(), nil, nil); err == nil {
		t.Error("Reindex with no index path succeeded")
	}
	if _, err := Reindex(context.Background(), t.TempDir(), t.TempDir(), nil, nil); err == nil {
		t.Error("Reindex with a directory as its index path succeeded")
	}
}

// TestReindexHonoursCancellation covers the ctx check: the 'I' overlay
// cancels the scan when the user closes it, and a large library must
// not keep working afterwards.
func TestReindexHonoursCancellation(t *testing.T) {
	musicDir := t.TempDir()
	os.MkdirAll(filepath.Join(musicDir, "a"), 0o755)

	tracks := make([]Track, progressEvery*4)
	for i := range tracks {
		tracks[i] = Track{File: fmt.Sprintf("a/%04d.mp3", i)}
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // already cancelled before it starts

	if _, err := Reindex(ctx, filepath.Join(t.TempDir(), "lyrics.db"), musicDir, tracks, nil); err == nil {
		t.Error("a cancelled Reindex reported success")
	}
}

func TestReadInfoWithNoPathOrNoFile(t *testing.T) {
	info, err := ReadInfo("")
	if err != nil {
		t.Fatalf("ReadInfo(\"\"): %v", err)
	}
	if info.Exists {
		t.Error("ReadInfo with no configured path reports an existing index")
	}

	info, err = ReadInfo(filepath.Join(t.TempDir(), "never-built.db"))
	if err != nil {
		t.Fatalf("ReadInfo on a missing file: %v", err)
	}
	if info.Exists {
		t.Error("ReadInfo reports an index that was never built")
	}
}
