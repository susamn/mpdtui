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

func TestQueueRenderShowsLyricsIconInLyrColumnOnlyWhenAvailable(t *testing.T) {
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
	lyrCol := newQueueColumns(true, false).lyr

	if got := a.queue.table.GetCell(queueHeaderRows, lyrCol).Text; got != lyricsIcon {
		t.Errorf("Lyr cell for the track with lyrics = %q, want %q", got, lyricsIcon)
	}
	if got := a.queue.table.GetCell(queueHeaderRows, 2).Text; got != "With Lyrics"+queueColumnGap {
		t.Errorf("title cell for the track with lyrics = %q, want it unprefixed (icon lives in its own column now)", got)
	}

	if got := a.queue.table.GetCell(queueHeaderRows+1, lyrCol).Text; got != "" {
		t.Errorf("Lyr cell for the track without lyrics = %q, want empty", got)
	}
	if got := a.queue.table.GetCell(queueHeaderRows+1, 2).Text; got != "Without Lyrics"+queueColumnGap {
		t.Errorf("title cell for the track without lyrics = %q, want %q", got, "Without Lyrics"+queueColumnGap)
	}
}

// TestQueueRenderRechecksLyricsOnEveryRender is the "as I may add more
// lyrics" requirement: a track queued before its lyrics file existed
// picks up the Lyr icon on the very next render, no restart or requeue
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
	lyrCol := newQueueColumns(true, false).lyr

	if got := a.queue.table.GetCell(queueHeaderRows, lyrCol).Text; got != "" {
		t.Fatalf("setup: Lyr cell = %q, want empty (no lyrics file yet)", got)
	}

	if err := os.WriteFile(filepath.Join(trackDir, "Track.txt"), []byte("now it exists"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	a.queue.render(-1)

	if got := a.queue.table.GetCell(queueHeaderRows, lyrCol).Text; got != lyricsIcon {
		t.Errorf("Lyr cell after adding the lyrics file = %q, want %q", got, lyricsIcon)
	}
}
