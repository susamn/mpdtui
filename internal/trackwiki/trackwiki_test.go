package trackwiki

import (
	"os"
	"path/filepath"
	"testing"
)

// write lays out a track's wiki directory the way wiki-push does and
// returns the musicDir it sits under.
func write(t *testing.T, file, contents string) string {
	t.Helper()
	music := t.TempDir()
	dir := Dir(music, file)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if contents != "" {
		if err := os.WriteFile(filepath.Join(dir, "wiki.json"), []byte(contents), 0o644); err != nil {
			t.Fatalf("write: %v", err)
		}
	}
	return music
}

const valid = `{
  "schema": 1,
  "track": {"title": "Smooth Operator", "artist": "Sade"},
  "fetched_at": "2026-09-16T20:55:00Z",
  "story": {"summary": "A song about a con man."},
  "bootlegs": [{"title": "Hammersmith Odeon"}],
  "images": [{"file": "cover.jpg", "role": "cover"}, {"file": "scene-01.jpg", "role": "scene"}]
}`

// TestDirInsertsTheWikiLevel pins the one place the metadata tree and the
// music tree differ. wiki-push inserts this level on the way out, so
// getting it wrong here means never finding a story that was pushed
// perfectly correctly.
func TestDirInsertsTheWikiLevel(t *testing.T) {
	got := Dir("/music", "sade/the-best-of-sade/1-03-smooth-operator.m4a")
	want := filepath.Join("/music", "sade", "the-best-of-sade", "wiki", "1-03-smooth-operator")
	if got != want {
		t.Errorf("Dir = %q, want %q", got, want)
	}
}

// TestDirDropsOnlyTheExtension covers a filename with dots of its own:
// the directory is the base name without its *extension*, not up to the
// first dot.
func TestDirDropsOnlyTheExtension(t *testing.T) {
	got := Dir("/music", "a/b/01-song-feat.-someone.mp3")
	want := filepath.Join("/music", "a", "b", "wiki", "01-song-feat.-someone")
	if got != want {
		t.Errorf("Dir = %q, want %q", got, want)
	}
}

func TestDirEmptyWhenFeatureInactive(t *testing.T) {
	for _, tc := range []struct{ name, music, file string }{
		{"no music dir", "", "a/b.mp3"},
		{"no file", "/music", ""},
	} {
		if got := Dir(tc.music, tc.file); got != "" {
			t.Errorf("%s: Dir = %q, want empty", tc.name, got)
		}
	}
}

func TestLoadReadsAStory(t *testing.T) {
	file := "sade/best/01-smooth.m4a"
	music := write(t, file, valid)

	w, ok := Load(music, file)
	if !ok {
		t.Fatal("Load found nothing, want the story it just wrote")
	}
	if w.Track.Title != "Smooth Operator" {
		t.Errorf("title = %q", w.Track.Title)
	}
	if w.Story.Summary == "" {
		t.Error("summary is empty")
	}
	if len(w.Bootlegs) != 1 || len(w.Images) != 2 {
		t.Errorf("bootlegs=%d images=%d, want 1 and 2", len(w.Bootlegs), len(w.Images))
	}
}

// TestLoadRefusesWhatItCannotTrust covers every way a file is declined.
// All of them read as "no story" rather than an error: this is a card the
// user opened out of curiosity, and there is nothing they could do about
// a bad file from inside the player.
func TestLoadRefusesWhatItCannotTrust(t *testing.T) {
	for _, tc := range []struct{ name, body string }{
		{"missing file", ""},
		{"malformed json", `{"schema": 1, `},
		{"a newer schema", `{"schema": 2, "story": {"summary": "x"}}`},
		{"an older schema", `{"schema": 0, "story": {"summary": "x"}}`},
		{"nothing worth showing", `{"schema": 1, "track": {"title": "x", "artist": "y"}}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			file := "a/b/track.mp3"
			music := write(t, file, tc.body)
			if _, ok := Load(music, file); ok {
				t.Errorf("Load accepted %s", tc.name)
			}
		})
	}
}

// TestPathRefusesAnythingButABareName is the security-relevant one.
// wiki.json is written by a script and could name anything; the
// directory is documented as flat, so a manifest entry must never become
// a read outside it.
func TestPathRefusesAnythingButABareName(t *testing.T) {
	file := "a/b/track.mp3"
	music := write(t, file, valid)
	w, ok := Load(music, file)
	if !ok {
		t.Fatal("setup: Load failed")
	}

	if got := w.Path(Image{File: "cover.jpg"}); got == "" {
		t.Error("a bare filename was refused")
	} else if filepath.Dir(got) != Dir(music, file) {
		t.Errorf("resolved outside the track dir: %q", got)
	}

	for _, bad := range []string{
		"../cover.jpg",
		"../../etc/passwd",
		"sub/cover.jpg",
		"/etc/passwd",
		".hidden.jpg",
		"",
	} {
		if got := w.Path(Image{File: bad}); got != "" {
			t.Errorf("Path(%q) = %q, want it refused", bad, got)
		}
	}
}

func TestImagesWithRoleKeepsManifestOrder(t *testing.T) {
	w := Wiki{Images: []Image{
		{File: "scene-01.jpg", Role: "scene"},
		{File: "cover.jpg", Role: "cover"},
		{File: "scene-02.jpg", Role: "scene"},
	}}
	got := w.ImagesWithRole("scene")
	if len(got) != 2 || got[0].File != "scene-01.jpg" || got[1].File != "scene-02.jpg" {
		t.Errorf("ImagesWithRole(scene) = %+v", got)
	}
	if len(w.ImagesWithRole("bootleg")) != 0 {
		t.Error("a role with no images returned something")
	}
}
