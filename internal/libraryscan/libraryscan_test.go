package libraryscan_test

import (
	"os"
	"path/filepath"
	"testing"

	"mpdtui/internal/libraryscan"
)

// tree builds a music directory from a path -> contents map. A path
// ending in "/" is an empty directory.
func tree(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	for rel, body := range files {
		full := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatalf("MkdirAll: %v", err)
		}
		if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
			t.Fatalf("WriteFile %s: %v", rel, err)
		}
	}
	return root
}

// wikiJSON is a manifest with enough content that trackwiki will open
// it (a file carrying only its track block is refused as empty).
const wikiJSON = `{"schema":1,"track":{"title":"T"},"story":{"summary":"a story"}}`

func TestScanCountsSidecarsStoriesAndOrphans(t *testing.T) {
	root := tree(t, map[string]string{
		// Both formats.
		"queen/opera/bohemian rhapsody.mp3": "",
		"queen/opera/bohemian rhapsody.txt": "lyrics",
		"queen/opera/Bohemian-Rhapsody.lrc": "[00:01.00] lyrics",
		// Synced only, and the sidecar's name only matches after
		// internal/lyrics normalizes it.
		"queen/opera/love of my life.mp3": "",
		"queen/opera/Love_Of_My_Life.lrc": "[00:01.00] x",
		// Plain only.
		"queen/opera/39.mp3": "",
		"queen/opera/39.txt": "x",
		// Neither.
		"queen/opera/seaside rendezvous.mp3": "",
		// An orphan in each format.
		"queen/opera/a track that left.txt": "x",
		"queen/opera/another one gone.lrc":  "x",
		// Two stories, one image each, plus a manifest this version
		// refuses.
		"queen/opera/wiki/bohemian rhapsody/wiki.json": wikiJSON,
		"queen/opera/wiki/bohemian rhapsody/cover.jpg": "",
		"queen/opera/wiki/39/wiki.json":                wikiJSON,
		"queen/opera/wiki/39/cover.jpg":                "",
		"queen/opera/wiki/39/live.jpg":                 "",
		"queen/opera/wiki/love of my life/wiki.json":   `{"schema":99}`,
		"queen/opera/wiki/love of my life/stray.txt":   "",
		// A second directory, with nothing beside the audio.
		"abba/arrival/dancing queen.mp3": "",
	})

	got := libraryscan.Scan(root, []string{
		"queen/opera/bohemian rhapsody.mp3",
		"queen/opera/love of my life.mp3",
		"queen/opera/39.mp3",
		"queen/opera/seaside rendezvous.mp3",
		"abba/arrival/dancing queen.mp3",
	})

	want := libraryscan.Counts{
		Tracks: 5, Dirs: 2,
		Synced: 2, Plain: 2, Both: 1, WithAny: 3,
		Orphans:    2,
		Stories:    2,
		Unreadable: 1,
		Images:     4,
	}
	if got != want {
		t.Errorf("Scan() = %+v, want %+v", got, want)
	}
}

// Scan must not read directories MPD has no track in: a folder of
// downloads dropped inside the music directory would otherwise inflate
// every sidecar total on the card.
func TestScanIgnoresDirectoriesWithNoTracks(t *testing.T) {
	root := tree(t, map[string]string{
		"queen/opera/39.mp3":             "",
		"queen/opera/39.txt":             "x",
		"inbox/something.mp3":            "",
		"inbox/something.txt":            "x",
		"inbox/wiki/something/wiki.json": wikiJSON,
	})

	got := libraryscan.Scan(root, []string{"queen/opera/39.mp3"})

	if got.Dirs != 1 || got.Tracks != 1 {
		t.Errorf("Scan() examined %d track(s) in %d dir(s), want 1 in 1", got.Tracks, got.Dirs)
	}
	if got.Plain != 1 || got.Orphans != 0 || got.Stories != 0 {
		t.Errorf("Scan() = %+v, want only queen/opera counted", got)
	}
}

// Two tracks whose names normalize alike share one sidecar. Both count
// as having lyrics, and the shared file must not also be reported as an
// orphan -- the bug a count-the-matches implementation would have.
func TestScanSharedSidecarIsNotAlsoAnOrphan(t *testing.T) {
	root := tree(t, map[string]string{
		"a/b/Track One.mp3": "",
		"a/b/track-one.mp3": "",
		"a/b/track_one.txt": "x",
	})

	got := libraryscan.Scan(root, []string{"a/b/Track One.mp3", "a/b/track-one.mp3"})

	if got.Plain != 2 {
		t.Errorf("Plain = %d, want both tracks matched to the one sidecar", got.Plain)
	}
	if got.Orphans != 0 {
		t.Errorf("Orphans = %d, want 0", got.Orphans)
	}
}

func TestScanWithoutMusicDirCountsNothing(t *testing.T) {
	if got := (libraryscan.Scan("", []string{"a/b.mp3"})); got != (libraryscan.Counts{}) {
		t.Errorf("Scan(\"\", ...) = %+v, want the zero Counts", got)
	}
}

// An unreadable directory contributes nothing and does not stop the
// rest of the scan -- Scan has no error to return, by design.
func TestScanSkipsUnreadableDirectories(t *testing.T) {
	root := tree(t, map[string]string{
		"good/39.mp3": "",
		"good/39.txt": "x",
	})

	got := libraryscan.Scan(root, []string{"good/39.mp3", "missing/gone.mp3", ""})

	if got.Plain != 1 || got.WithAny != 1 {
		t.Errorf("Scan() = %+v, want the readable directory still counted", got)
	}
	if got.Tracks != 3 {
		t.Errorf("Tracks = %d, want all 3 supplied paths reported as examined", got.Tracks)
	}
	if got.Dirs != 2 {
		t.Errorf("Dirs = %d, want 2 (the empty path is skipped)", got.Dirs)
	}
}

// A wiki/ folder holding loose files rather than per-track directories
// is the pre-push layout. Nothing in it is a story.
func TestScanIgnoresLooseFilesInAWikiFolder(t *testing.T) {
	root := tree(t, map[string]string{
		"a/b/39.mp3":         "",
		"a/b/wiki/wiki.json": wikiJSON,
		"a/b/wiki/cover.jpg": "",
	})

	got := libraryscan.Scan(root, []string{"a/b/39.mp3"})

	if got.Stories != 0 || got.Unreadable != 0 || got.Images != 0 {
		t.Errorf("Scan() = %+v, want nothing counted from a flat wiki folder", got)
	}
}

// A story directory that cannot be listed still counts as a story if
// its manifest read -- the image count is what goes missing, not the
// story itself.
func TestScanStoryWithNoImages(t *testing.T) {
	root := tree(t, map[string]string{
		"a/b/39.mp3":            "",
		"a/b/wiki/39/wiki.json": wikiJSON,
	})

	got := libraryscan.Scan(root, []string{"a/b/39.mp3"})

	if got.Stories != 1 || got.Images != 0 {
		t.Errorf("Scan() = %+v, want 1 story and 0 images", got)
	}
}

// An unreadable wiki/ folder, and an unreadable story directory inside
// a readable one, are the two places the scan can be stopped by
// permissions. Neither may fail the scan.
func TestScanSkipsUnreadableStoryDirectories(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("running as root: mode 0 does not deny anything")
	}

	t.Run("wiki folder", func(t *testing.T) {
		root := tree(t, map[string]string{
			"a/b/39.mp3":            "",
			"a/b/wiki/39/wiki.json": wikiJSON,
		})
		denyDir(t, filepath.Join(root, "a", "b", "wiki"))

		got := libraryscan.Scan(root, []string{"a/b/39.mp3"})

		if got.Stories != 0 || got.Unreadable != 0 || got.Images != 0 {
			t.Errorf("Scan() = %+v, want nothing counted from an unreadable wiki folder", got)
		}
	})

	t.Run("story directory", func(t *testing.T) {
		root := tree(t, map[string]string{
			"a/b/39.mp3":            "",
			"a/b/wiki/39/wiki.json": wikiJSON,
			"a/b/wiki/39/cover.jpg": "",
		})
		denyDir(t, filepath.Join(root, "a", "b", "wiki", "39"))

		got := libraryscan.Scan(root, []string{"a/b/39.mp3"})

		if got.Stories != 0 || got.Unreadable != 1 || got.Images != 0 {
			t.Errorf("Scan() = %+v, want the story reported unreadable and no images", got)
		}
	})
}

// denyDir makes dir unreadable for the rest of the test, restoring it
// afterwards so t.TempDir's own cleanup can still remove the tree.
func denyDir(t *testing.T, dir string) {
	t.Helper()
	if err := os.Chmod(dir, 0o000); err != nil {
		t.Fatalf("Chmod: %v", err)
	}
	t.Cleanup(func() { os.Chmod(dir, 0o755) })
}
