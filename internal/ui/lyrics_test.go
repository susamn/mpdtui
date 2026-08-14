package ui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"mpdtui/internal/mpdclient"
)

func TestLyricsViewerRectSpansYearThroughTypeColumns(t *testing.T) {
	cases := []struct {
		queueY, queueHeight, yearX, durationX int
		wantX, wantY, wantW, wantH            int
	}{
		// y = queueY + 1 (the table's own top border row) + 1
		// (queueHeaderRows), landing on the first data row, past the
		// header. height = queueHeight - queueHeaderRows -
		// lyricsViewerBottomMargin (2) - 2 (both the table's own border
		// rows), so it stops lyricsViewerBottomMargin data rows short of
		// the table's own bottom border: 30 - 1 - 2 - 2 = 25.
		{5, 30, 60, 90, 60, 7, 30, 25},
		// A pathologically short queueHeight (shorter than just the two
		// border rows + header row + margin) clamps height to 0 rather
		// than negative.
		{0, 0, 0, 0, 0, 2, 0, 0},
	}
	for _, tc := range cases {
		gotX, gotY, gotW, gotH := lyricsViewerRect(tc.queueY, tc.queueHeight, tc.yearX, tc.durationX)
		if gotX != tc.wantX || gotY != tc.wantY || gotW != tc.wantW || gotH != tc.wantH {
			t.Errorf("lyricsViewerRect(%d,%d,%d,%d) = (%d,%d,%d,%d), want (%d,%d,%d,%d)",
				tc.queueY, tc.queueHeight, tc.yearX, tc.durationX, gotX, gotY, gotW, gotH, tc.wantX, tc.wantY, tc.wantW, tc.wantH)
		}
	}
}

func TestLyricsViewerRenderNothingPlaying(t *testing.T) {
	a := newTestApp()
	a.lyricsViewer.render(mpdclient.Song{})
	if got := a.lyricsViewer.GetText(false); !strings.Contains(got, "Nothing playing") {
		t.Errorf("render(zero Song) = %q, want it to contain %q", got, "Nothing playing")
	}
}

func TestLyricsViewerRenderNoMusicDirConfigured(t *testing.T) {
	a := newTestApp() // musicDir == ""
	a.lyricsViewer.render(mpdclient.Song{Title: "Track", Artist: "Artist"})
	got := a.lyricsViewer.GetText(false)
	if !strings.Contains(got, "No music directory configured") {
		t.Errorf("render(...) with no musicDir = %q, want it to mention no music directory is configured", got)
	}
}

func TestLyricsViewerRenderNoMatchingLyrics(t *testing.T) {
	a := newTestAppWithMusicDir(t.TempDir())
	song := mpdclient.Song{Title: "Track", Artist: "Artist", File: "artist/Track.mp3"}
	a.lyricsViewer.render(song)
	got := a.lyricsViewer.GetText(false)
	if !strings.Contains(got, "No lyrics found for "+song.DisplayName()) {
		t.Errorf("render(...) with no matching lyrics = %q, want it to say so", got)
	}
}

func TestLyricsViewerRenderShowsLyricsContent(t *testing.T) {
	dir := t.TempDir()
	trackDir := filepath.Join(dir, "artist")
	if err := os.MkdirAll(trackDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(filepath.Join(trackDir, "Track.txt"), []byte("line one\nline two"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	a := newTestAppWithMusicDir(dir)
	a.lyricsViewer.render(mpdclient.Song{Title: "Track", Artist: "Artist", File: "artist/Track.mp3"})
	got := a.lyricsViewer.GetText(false)
	if !strings.Contains(got, "line one") || !strings.Contains(got, "line two") {
		t.Errorf("render(...) = %q, want it to contain the lyrics file's content", got)
	}
}

func TestOpenLyricsViewerUsesCurrentSongWithoutFetching(t *testing.T) {
	a := newTestApp() // no MPD client at all -- proves this never calls client.CurrentSong
	a.tv.SetFocus(a.library.tree)
	a.currentSong = mpdclient.Song{Title: "Track", Artist: "Artist"}

	a.openLyricsViewer()

	if a.tv.GetFocus() != a.lyricsViewer {
		t.Fatalf("focus after openLyricsViewer = %T, want the lyrics viewer", a.tv.GetFocus())
	}
	if a.mode != modeOverlay {
		t.Error("mode after openLyricsViewer should be modeOverlay")
	}
	if got := a.lyricsViewer.GetText(false); !strings.Contains(got, "No music directory configured") {
		t.Errorf("viewer text = %q, want it to reflect a.currentSong (no musicDir configured in this test)", got)
	}
}

func TestOpenLyricsViewerTogglesClosedOnSecondYPress(t *testing.T) {
	a := newTestApp()
	a.tv.SetFocus(a.queue.table)
	a.openLyricsViewer()

	if a.mode != modeOverlay {
		t.Fatal("setup: mode after openLyricsViewer should be modeOverlay")
	}

	yKey := tcell.NewEventKey(tcell.KeyRune, 'y', tcell.ModNone)
	if result := a.globalInputCapture(yKey); result != nil {
		t.Errorf("'y' while the lyrics viewer is open should be consumed, got %v", result)
	}
	if a.mode != modeNormal {
		t.Error("mode after a second 'y' press should be modeNormal (viewer toggled closed)")
	}
	if a.tv.GetFocus() != a.queue.table {
		t.Errorf("focus after toggling closed = %T, want the originally-focused Queue table", a.tv.GetFocus())
	}
}

func TestQKeyWhileLyricsViewerOpenIsConsumed(t *testing.T) {
	a := newTestApp()
	a.tv.SetFocus(a.queue.table)
	a.openLyricsViewer()

	qKey := tcell.NewEventKey(tcell.KeyRune, 'q', tcell.ModNone)
	if result := a.globalInputCapture(qKey); result != nil {
		t.Errorf("'q' while the lyrics viewer is open should be consumed (quit), got %v", result)
	}
}

func TestYKeyWhileAnotherOverlayOpenDoesNotToggleLyricsViewer(t *testing.T) {
	a := newTestApp()
	a.tv.SetFocus(a.queue.table)
	a.openHelp()

	yKey := tcell.NewEventKey(tcell.KeyRune, 'y', tcell.ModNone)
	a.globalInputCapture(yKey)

	if a.mode != modeOverlay {
		t.Error("'y' while a different overlay (help) is open should not close it")
	}
}

func TestOpenLyricsViewerEscRestoresOriginalFocus(t *testing.T) {
	a := newTestApp()
	a.tv.SetFocus(a.queue.table)
	a.openLyricsViewer()

	esc := tcell.NewEventKey(tcell.KeyEscape, 0, tcell.ModNone)
	if result := a.globalInputCapture(esc); result != nil {
		t.Errorf("Escape while the lyrics viewer is open should be consumed, got %v", result)
	}
	if a.mode != modeNormal {
		t.Error("mode after Escape should be modeNormal")
	}
	if a.tv.GetFocus() != a.queue.table {
		t.Errorf("focus after Escape = %T, want the originally-focused Queue table", a.tv.GetFocus())
	}
}

// TestMaybeRefreshLyricsViewerOnlyWhenOpenAndTrackChanged is a pure,
// offline test of the same gating maybeRefreshLyricsViewer applies inside
// refreshNowPlaying (which itself needs a live client to test directly):
// it must never touch the viewer while it isn't the open overlay, and
// never re-render for the same track repeating on every ~500ms tick, only
// on an actual change.
func TestMaybeRefreshLyricsViewerOnlyWhenOpenAndTrackChanged(t *testing.T) {
	// musicDir must be configured here (unlike this file's other tests)
	// so the rendered text actually reflects which song was passed in --
	// with no musicDir, render's "No music directory configured" message
	// doesn't mention the track at all, which would make old-vs-new
	// track content indistinguishable in this test's assertions.
	a := newTestAppWithMusicDir(t.TempDir())
	a.tv.SetFocus(a.queue.table)

	a.maybeRefreshLyricsViewer(mpdclient.Song{Title: "New"}, true)
	if got := a.lyricsViewer.GetText(false); got != "" {
		t.Fatalf("viewer rendered while not the open overlay: %q", got)
	}

	a.currentSong = mpdclient.Song{Title: "Old Track", Artist: "Old Artist"}
	a.openLyricsViewer()

	a.maybeRefreshLyricsViewer(mpdclient.Song{Title: "New Track", Artist: "New Artist"}, false)
	if got := a.lyricsViewer.GetText(false); !strings.Contains(got, "Old Track") {
		t.Errorf("viewer re-rendered even though trackChanged was false: %q", got)
	}

	a.maybeRefreshLyricsViewer(mpdclient.Song{Title: "New Track", Artist: "New Artist"}, true)
	got := a.lyricsViewer.GetText(false)
	if strings.Contains(got, "Old Track") {
		t.Errorf("viewer still shows the old track after trackChanged: %q", got)
	}
	if !strings.Contains(got, "New Track") {
		t.Errorf("viewer after trackChanged = %q, want it updated to the new track", got)
	}
}

// TestHandleTransportKeyUnrecognizedRuneReturnsFalseWithoutTouchingClient
// proves the "not a transport key" path never reaches a.client (which is
// nil in this offline test -- any of the real handlers, e.g.
// togglePlayPause, would panic if mistakenly called) -- '#' is neither a
// transport key nor any other bound key in this app.
func TestHandleTransportKeyUnrecognizedRuneReturnsFalseWithoutTouchingClient(t *testing.T) {
	a := newTestApp() // nil a.client
	if a.handleTransportKey('#') {
		t.Error("handleTransportKey('#') = true, want false")
	}
}

// TestTransportKeysNotConsumedWhileAnotherOverlayOpen proves the "stays
// live" behavior is scoped to the lyrics viewer specifically, not a
// blanket rule for every overlay -- offline-safe (no a.client call
// happens, since focus isn't a.lyricsViewer, so handleTransportKey is
// never reached): a search/filter/save-playlist input still needs
// Space to stay literal typed text, so Space must fall through
// unconsumed (to whatever's focused) while e.g. help is open.
func TestTransportKeysNotConsumedWhileAnotherOverlayOpen(t *testing.T) {
	a := newTestApp()
	a.tv.SetFocus(a.queue.table)
	a.openHelp()

	space := tcell.NewEventKey(tcell.KeyRune, ' ', tcell.ModNone)
	if result := a.globalInputCapture(space); result == nil {
		t.Error("Space while help (not the lyrics viewer) is open should not be consumed by the transport-key passthrough")
	}
}

// TestTransportKeysStayLiveWhileLyricsViewerOpenNeedsLiveMPD needs a real
// client, since handleTransportKey's whole point is calling one -- but
// keeps the actual side effect minimal and reversible, the same way
// TestVolumeClamped (internal/mpdclient/tests) already accepts a small
// live side effect as the cost of testing a real transport action:
// toggling play/pause twice restores whatever state playback was already
// in, rather than leaving it changed.
func TestTransportKeysStayLiveWhileLyricsViewerOpenNeedsLiveMPD(t *testing.T) {
	c := dialOrSkip(t)
	a := &App{tv: tview.NewApplication(), client: c}
	a.build()
	a.tv.SetFocus(a.queue.table)
	a.openLyricsViewer()

	space := tcell.NewEventKey(tcell.KeyRune, ' ', tcell.ModNone)
	if result := a.globalInputCapture(space); result != nil {
		t.Errorf("Space while the lyrics viewer is open should be consumed (routed to togglePlayPause), got %v", result)
	}
	a.globalInputCapture(space) // toggle back, restoring whatever state playback was already in

	if a.mode != modeOverlay {
		t.Error("mode after a transport key should stay modeOverlay -- the lyrics viewer itself must stay open")
	}
	if a.tv.GetFocus() != a.lyricsViewer {
		t.Errorf("focus after a transport key = %T, want to stay on the lyrics viewer", a.tv.GetFocus())
	}
}
