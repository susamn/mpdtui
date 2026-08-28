package tests

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"mpdtui/internal/lyricsindex"
)

// buildLibrary lays out a small music tree and returns its root plus the
// track list to feed Reindex.
func buildLibrary(t *testing.T) (musicDir string, tracks []lyricsindex.Track) {
	t.Helper()
	musicDir = t.TempDir()

	mk := func(rel, content string) {
		full := filepath.Join(musicDir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	mk("Simon & Garfunkel/Sounds of Silence/The Sound of Silence.txt",
		"Hello darkness my old friend\nI've come to talk with you again")
	mk("Beatles/Abbey Road/Here Comes the Sun.lrc",
		"[ti:Here Comes the Sun]\n[00:12.00]Here comes the sun\n[00:15.00]doo doo doo doo")
	mk("Björk/Post/Army of Me.txt", "Stand up, you've got to manage")

	tracks = []lyricsindex.Track{
		{File: "Simon & Garfunkel/Sounds of Silence/The Sound of Silence.flac", Display: "Simon & Garfunkel - The Sound of Silence"},
		{File: "Beatles/Abbey Road/Here Comes the Sun.mp3", Display: "The Beatles - Here Comes the Sun"},
		{File: "Björk/Post/Army of Me.mp3", Display: "Björk - Army of Me"},
		{File: "Beatles/Abbey Road/Something.mp3", Display: "The Beatles - Something"}, // no sidecar
	}
	return musicDir, tracks
}

func TestReindexAndLoad(t *testing.T) {
	musicDir, tracks := buildLibrary(t)
	dbPath := filepath.Join(t.TempDir(), "lyrics_index.db")

	stats, err := lyricsindex.Reindex(context.Background(), dbPath, musicDir, tracks, nil)
	if err != nil {
		t.Fatalf("Reindex: %v", err)
	}
	if stats.Indexed != 3 || stats.Read != 3 || stats.Unchanged != 0 || stats.Removed != 0 {
		t.Fatalf("stats = %+v, want Indexed 3 / Read 3 / Unchanged 0 / Removed 0", stats)
	}

	entries, err := lyricsindex.Load(dbPath)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(entries) != 3 {
		t.Fatalf("Load returned %d entries, want 3", len(entries))
	}

	byFile := map[string]lyricsindex.Entry{}
	for _, e := range entries {
		byFile[e.File] = e
	}

	sos := byFile["Simon & Garfunkel/Sounds of Silence/The Sound of Silence.flac"]
	if sos.Display != "Simon & Garfunkel - The Sound of Silence" {
		t.Errorf("display = %q", sos.Display)
	}
	if want := "hello darkness my old friend"; !contains(sos.TextFolded, want) {
		t.Errorf("folded text %q missing %q", sos.TextFolded, want)
	}

	// .lrc is flattened: line text kept, timestamps and [ti:] tag gone.
	sun := byFile["Beatles/Abbey Road/Here Comes the Sun.mp3"]
	if !contains(sun.TextFolded, "here comes the sun") || contains(sun.TextFolded, "00:12") || contains(sun.TextFolded, "[ti:") {
		t.Errorf("lrc folded text not flattened cleanly: %q", sun.TextFolded)
	}

	// Diacritics folded on the way in.
	army := byFile["Björk/Post/Army of Me.mp3"]
	if !contains(army.TextFolded, "manage") {
		t.Errorf("army folded text = %q", army.TextFolded)
	}
}

func TestReindexIsIncremental(t *testing.T) {
	musicDir, tracks := buildLibrary(t)
	dbPath := filepath.Join(t.TempDir(), "idx.db")

	if _, err := lyricsindex.Reindex(context.Background(), dbPath, musicDir, tracks, nil); err != nil {
		t.Fatal(err)
	}

	// Touch one sidecar into the future; leave the others alone.
	changed := filepath.Join(musicDir, "Björk/Post/Army of Me.txt")
	future := time.Now().Add(2 * time.Hour)
	if err := os.Chtimes(changed, future, future); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(changed, []byte("i'm alright"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(changed, future, future); err != nil {
		t.Fatal(err)
	}

	stats, err := lyricsindex.Reindex(context.Background(), dbPath, musicDir, tracks, nil)
	if err != nil {
		t.Fatal(err)
	}
	if stats.Read != 1 || stats.Unchanged != 2 || stats.Indexed != 3 {
		t.Fatalf("second run stats = %+v, want Read 1 / Unchanged 2 / Indexed 3", stats)
	}

	entries, _ := lyricsindex.Load(dbPath)
	for _, e := range entries {
		if e.File == "Björk/Post/Army of Me.mp3" && !contains(e.TextFolded, "i'm alright") {
			t.Errorf("changed sidecar not re-read: %q", e.TextFolded)
		}
	}
}

func TestReindexRemovesDroppedTracks(t *testing.T) {
	musicDir, tracks := buildLibrary(t)
	dbPath := filepath.Join(t.TempDir(), "idx.db")

	if _, err := lyricsindex.Reindex(context.Background(), dbPath, musicDir, tracks, nil); err != nil {
		t.Fatal(err)
	}

	// Library shrinks to just the first track.
	stats, err := lyricsindex.Reindex(context.Background(), dbPath, musicDir, tracks[:1], nil)
	if err != nil {
		t.Fatal(err)
	}
	if stats.Removed != 2 || stats.Indexed != 1 {
		t.Fatalf("stats = %+v, want Removed 2 / Indexed 1", stats)
	}
	if entries, _ := lyricsindex.Load(dbPath); len(entries) != 1 {
		t.Fatalf("Load returned %d entries after shrink, want 1", len(entries))
	}
}

func TestReindexCancellation(t *testing.T) {
	musicDir, tracks := buildLibrary(t)
	dbPath := filepath.Join(t.TempDir(), "idx.db")

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := lyricsindex.Reindex(ctx, dbPath, musicDir, tracks, nil)
	if err == nil {
		t.Fatal("Reindex with a cancelled context returned nil error")
	}
	// The previous (nonexistent) index must not have been left half-written.
	if entries, _ := lyricsindex.Load(dbPath); len(entries) != 0 {
		t.Fatalf("cancelled Reindex committed %d entries", len(entries))
	}
}

func TestReindexNeedsMusicDir(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "idx.db")
	if _, err := lyricsindex.Reindex(context.Background(), dbPath, "", nil, nil); err == nil {
		t.Fatal("Reindex with empty musicDir returned nil error")
	}
}

func TestLoadMissingIndexIsEmptyNotError(t *testing.T) {
	entries, err := lyricsindex.Load(filepath.Join(t.TempDir(), "never-created.db"))
	if err != nil {
		t.Fatalf("Load of a missing index: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("got %d entries", len(entries))
	}
}

func TestReadInfo(t *testing.T) {
	musicDir, tracks := buildLibrary(t)
	dbPath := filepath.Join(t.TempDir(), "idx.db")

	if info, err := lyricsindex.ReadInfo(dbPath); err != nil || info.Exists {
		t.Fatalf("ReadInfo before build: info=%+v err=%v", info, err)
	}

	if _, err := lyricsindex.Reindex(context.Background(), dbPath, musicDir, tracks, nil); err != nil {
		t.Fatal(err)
	}

	info, err := lyricsindex.ReadInfo(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	if !info.Exists || info.Count != 3 || info.MusicDir != musicDir || info.IndexedAt.IsZero() {
		t.Fatalf("ReadInfo = %+v", info)
	}
}

func TestFold(t *testing.T) {
	cases := map[string]string{
		"Bublé":       "buble",
		"HELLO World": "hello world",
		"Björk":       "bjork",
		"Rock & Roll": "rock & roll",
	}
	for in, want := range cases {
		if got := lyricsindex.Fold(in); got != want {
			t.Errorf("Fold(%q) = %q, want %q", in, got, want)
		}
	}
}

func contains(haystack, needle string) bool { return strings.Contains(haystack, needle) }
