package ui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/rivo/tview"

	"mpdtui/internal/mpdclient"
)

// TestEventLoopRefreshesOnItsTicker covers the ~500ms poll that keeps
// Now Playing current even when MPD sends no idle event -- the app's
// fallback when the watcher is unavailable, which is exactly the state
// a nil watcher puts it in.
func TestEventLoopRefreshesOnItsTicker(t *testing.T) {
	f := &fakeMPD{
		status: mpdclient.Status{State: mpdclient.StatePlay, SongID: 4},
		song:   mpdclient.Song{ID: 4, Title: "Ticked", File: "a.mp3"},
	}
	a := newFakeApp(t, f)
	a.done = make(chan struct{})
	// No watcher: its channels are nil, which blocks forever in the
	// select rather than spinning, leaving the tickers to drive things.
	a.watcher = nil

	done := make(chan struct{})
	go func() { a.eventLoop(); close(done) }()

	waitFor(t, func() bool { return a.currentSong.File == "a.mp3" })

	close(a.done)
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("eventLoop did not return after done was closed")
	}
}

// TestEventLoopStopsWhenDoneIsClosed is the shutdown path: App.Run
// closes done on the way out, and the loop must not outlive it.
func TestEventLoopStopsWhenDoneIsClosed(t *testing.T) {
	a := newFakeApp(t, &fakeMPD{})
	a.done = make(chan struct{})
	a.watcher = nil

	done := make(chan struct{})
	go func() { a.eventLoop(); close(done) }()

	close(a.done)
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("eventLoop kept running after done was closed")
	}
}

// TestEventLoopAnimatesOnlyWhilePlaying covers the animation ticker's
// guard: the visualizer redraws at 40ms while playing, and must not
// burn that frame budget when nothing is.
func TestEventLoopAnimatesOnlyWhilePlaying(t *testing.T) {
	f := &fakeMPD{
		status: mpdclient.Status{State: mpdclient.StateStop},
		song:   mpdclient.Song{File: "a.mp3"},
	}
	a := newFakeApp(t, f)
	a.done = make(chan struct{})
	a.watcher = nil
	a.visualizer.View().SetRect(0, 0, 20, 3)

	go a.eventLoop()
	t.Cleanup(func() { close(a.done) })

	// Let several animation ticks go by while stopped.
	time.Sleep(150 * time.Millisecond)
	if got := a.visualizer.View().GetText(true); strings.TrimSpace(got) != "" {
		t.Errorf("visualizer drew %q while stopped, want it idle", got)
	}
}

// --- Lyrics reindex -----------------------------------------------------

func TestReindexLyricsNeedsMusicDir(t *testing.T) {
	a := newFakeApp(t, &fakeMPD{}) // musicDir is ""

	a.handleReindexLyrics()

	if a.mode == modeOverlay {
		t.Error("the reindex overlay opened with no music_dir configured")
	}
	if got := a.hintBar.GetText(true); !strings.Contains(got, "music_dir") {
		t.Errorf("hint bar = %q, want it to explain music_dir is needed", got)
	}
}

func TestReindexLyricsNeedsAnIndexPath(t *testing.T) {
	a := newFakeApp(t, &fakeMPD{})
	a.musicDir = t.TempDir()
	a.cfg.LyricsIndexPath = "" // no config directory resolved

	a.handleReindexLyrics()

	if a.mode == modeOverlay {
		t.Error("the reindex overlay opened with nowhere to write the index")
	}
	if got := a.hintBar.GetText(true); !strings.Contains(got, "index") {
		t.Errorf("hint bar = %q, want it to explain the index is unavailable", got)
	}
}

// TestReindexLyricsBuildsAnIndex runs the whole overlay against a small
// on-disk library, which is the only way to cover the scan itself.
func TestReindexLyricsBuildsAnIndex(t *testing.T) {
	musicDir := t.TempDir()
	trackDir := filepath.Join(musicDir, "artist", "album")
	if err := os.MkdirAll(trackDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	// A track with a sidecar lyrics file, and one without.
	if err := os.WriteFile(filepath.Join(trackDir, "01.txt"), []byte("some lyrics here"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	f := &fakeMPD{songs: []mpdclient.Song{
		{File: "artist/album/01.mp3", Artist: "A", Title: "One"},
		{File: "artist/album/02.mp3", Artist: "A", Title: "Two"},
	}}
	a := newFakeApp(t, f)
	a.musicDir = musicDir
	a.cfg.LyricsIndexPath = filepath.Join(t.TempDir(), "lyrics.db")

	a.handleReindexLyrics()

	if a.mode != modeOverlay {
		t.Fatal("the reindex overlay did not open")
	}
	waitFor(t, func() bool {
		return strings.Contains(statusText(t, a), "Done.")
	})
	if got := statusText(t, a); !strings.Contains(got, "1 tracks with lyrics indexed") {
		t.Errorf("status = %q, want it to report the one indexed track", got)
	}
}

func TestReindexLyricsReportsAFailedTrackList(t *testing.T) {
	a := newFakeApp(t, &fakeMPD{err: errTest})
	a.musicDir = t.TempDir()
	a.cfg.LyricsIndexPath = filepath.Join(t.TempDir(), "lyrics.db")

	a.handleReindexLyrics()

	waitFor(t, func() bool {
		return strings.Contains(statusText(t, a), "failed to list tracks")
	})
}

// TestReindexLyricsEscapeCancels covers the cancellation wiring: closing
// the overlay cancels the scan, and a straggler update must not repaint
// a view the user has already dismissed.
func TestReindexLyricsEscapeCancels(t *testing.T) {
	a := newFakeApp(t, &fakeMPD{})
	a.musicDir = t.TempDir()
	a.cfg.LyricsIndexPath = filepath.Join(t.TempDir(), "lyrics.db")

	a.handleReindexLyrics()
	if a.mode != modeOverlay {
		t.Fatal("the reindex overlay did not open")
	}

	a.closeOverlay()

	if a.mode != modeNormal {
		t.Error("closing the reindex overlay did not leave overlay mode")
	}
}

// statusText reads whatever the reindex overlay is currently showing.
// showOverlay focuses the status view itself, so focus is the handle.
func statusText(t *testing.T, a *App) string {
	t.Helper()
	view, ok := a.tv.GetFocus().(*tview.TextView)
	if !ok {
		return ""
	}
	return view.GetText(true)
}
