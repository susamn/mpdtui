package ui

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/rivo/tview"

	"mpdtui/internal/mpdclient"
)

// newTestAppWithMusicDir mirrors newTestApp (keys_test.go), but with the
// lyrics feature active against musicDir -- needed for any test that
// exercises hasLyrics/lyricsViewer beyond the "feature inactive" default
// newTestApp's empty musicDir already covers.
func newTestAppWithMusicDir(musicDir string) *App {
	a := &App{tv: tview.NewApplication(), musicDir: musicDir}
	a.build()
	return a
}

func TestHasLyricsFeatureInactiveWithoutMusicDir(t *testing.T) {
	a := newTestApp() // musicDir == ""
	if a.queue.hasLyrics("artist/track.mp3", map[string]map[string]string{}) {
		t.Error("hasLyrics with no musicDir configured = true, want false (feature inactive)")
	}
}

func TestHasLyricsTrueForMatchingSidecar(t *testing.T) {
	dir := t.TempDir()
	trackDir := filepath.Join(dir, "artist")
	if err := os.MkdirAll(trackDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(filepath.Join(trackDir, "Some Track [84934].txt"), []byte("la la"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	a := newTestAppWithMusicDir(dir)
	if !a.queue.hasLyrics("artist/some_track-84934.mp3", map[string]map[string]string{}) {
		t.Error("hasLyrics for a track with a matching (normalized) sidecar = false, want true")
	}
}

func TestHasLyricsFalseWithoutMatchingSidecar(t *testing.T) {
	dir := t.TempDir()
	a := newTestAppWithMusicDir(dir)
	if a.queue.hasLyrics("artist/track.mp3", map[string]map[string]string{}) {
		t.Error("hasLyrics with no sidecar present = true, want false")
	}
}

func TestQueueRenderPrefixesLyricsIconOnlyWhenAvailable(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "artist"), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "artist", "With Lyrics.txt"), []byte("la la"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	a := newTestAppWithMusicDir(dir)
	a.queue.songs = []mpdclient.Song{
		{ID: 1, Title: "With Lyrics", File: "artist/With Lyrics.mp3"},
		{ID: 2, Title: "Without Lyrics", File: "artist/Without Lyrics.mp3"},
	}
	a.queue.render(-1)

	withLyrics := a.queue.table.GetCell(queueHeaderRows, 2).Text
	if want := lyricsIcon + " With Lyrics" + queueColumnGap; withLyrics != want {
		t.Errorf("title cell for the track with lyrics = %q, want %q", withLyrics, want)
	}

	without := a.queue.table.GetCell(queueHeaderRows+1, 2).Text
	if want := "Without Lyrics" + queueColumnGap; without != want {
		t.Errorf("title cell for the track without lyrics = %q, want %q (no icon)", without, want)
	}
}

// TestQueueRenderRechecksLyricsOnEveryRender is the "as I may add more
// lyrics" requirement: a track queued before its lyrics file existed
// picks up the icon on the very next render, no restart or requeue
// needed, since hasLyrics always reads the real directory contents (only
// caching within one render pass, see render's lyricsDirs).
func TestQueueRenderRechecksLyricsOnEveryRender(t *testing.T) {
	dir := t.TempDir()
	trackDir := filepath.Join(dir, "artist")
	if err := os.MkdirAll(trackDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	a := newTestAppWithMusicDir(dir)
	a.queue.songs = []mpdclient.Song{{ID: 1, Title: "Track", File: "artist/Track.mp3"}}
	a.queue.render(-1)

	if got := a.queue.table.GetCell(queueHeaderRows, 2).Text; got != "Track"+queueColumnGap {
		t.Fatalf("setup: title cell = %q, want no lyrics icon yet", got)
	}

	if err := os.WriteFile(filepath.Join(trackDir, "Track.txt"), []byte("now it exists"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	a.queue.render(-1)

	if want := lyricsIcon + " Track" + queueColumnGap; a.queue.table.GetCell(queueHeaderRows, 2).Text != want {
		t.Errorf("title cell after adding the lyrics file = %q, want %q", a.queue.table.GetCell(queueHeaderRows, 2).Text, want)
	}
}
