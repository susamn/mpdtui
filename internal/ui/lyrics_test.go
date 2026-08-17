package ui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

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

// TestLyricsViewerRenderColorsAndEscapesContent covers both the muted-
// yellow coloring and the reason it needs tview.Escape: a lyrics file
// containing a "[Chorus]"-style annotation (a real, common pattern in
// lyrics files) would otherwise be misparsed as a style tag by
// SetDynamicColors(true) and silently vanish instead of rendering as
// literal text.
func TestLyricsViewerRenderColorsAndEscapesContent(t *testing.T) {
	dir := t.TempDir()
	trackDir := filepath.Join(dir, "artist")
	if err := os.MkdirAll(trackDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(filepath.Join(trackDir, "Track.txt"), []byte("[Chorus]\nline one"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	a := newTestAppWithMusicDir(dir)
	a.lyricsViewer.render(mpdclient.Song{Title: "Track", Artist: "Artist", File: "artist/Track.mp3"})

	raw := a.lyricsViewer.GetText(false)
	if !strings.Contains(raw, "["+lyricsTextColor+"]") {
		t.Errorf("raw viewer text = %q, want it wrapped in the %q color tag", raw, lyricsTextColor)
	}

	stripped := a.lyricsViewer.GetText(true) // tags applied/stripped, as actually rendered
	if !strings.Contains(stripped, "[Chorus]") {
		t.Errorf("rendered viewer text = %q, want literal %q (not swallowed as a style tag)", stripped, "[Chorus]")
	}
	if !strings.Contains(stripped, "line one") {
		t.Errorf("rendered viewer text = %q, want it to contain the lyrics content", stripped)
	}
}

// --- Synced (.lrc) lyrics ---

func writeLRCFixture(t *testing.T, musicDir, relDir, name, content string) {
	t.Helper()
	trackDir := filepath.Join(musicDir, relDir)
	if err := os.MkdirAll(trackDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(filepath.Join(trackDir, name), []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
}

func TestLyricsViewerRenderPrefersLRCOverPlainText(t *testing.T) {
	dir := t.TempDir()
	writeLRCFixture(t, dir, "artist", "Track.lrc", "[00:01.00]synced line")
	writeLRCFixture(t, dir, "artist", "Track.txt", "plain line")

	a := newTestAppWithMusicDir(dir)
	a.lyricsViewer.render(mpdclient.Song{Title: "Track", File: "artist/Track.mp3"})

	if a.lyricsViewer.syncedLines == nil {
		t.Fatal("syncedLines is nil, want the .lrc content loaded (it should win over the .txt)")
	}
	got := a.lyricsViewer.GetText(true)
	if !strings.Contains(got, "synced line") {
		t.Errorf("viewer text = %q, want the .lrc content", got)
	}
	if strings.Contains(got, "plain line") {
		t.Errorf("viewer text = %q, want the .txt content NOT shown (.lrc should win)", got)
	}
	if got := a.lyricsViewer.GetTitle(); got != lyricsViewerSyncedTitle {
		t.Errorf("title = %q, want %q", got, lyricsViewerSyncedTitle)
	}
}

func TestLyricsViewerRenderFallsBackToPlainTextWithoutLRC(t *testing.T) {
	dir := t.TempDir()
	writeLRCFixture(t, dir, "artist", "Track.txt", "plain line")

	a := newTestAppWithMusicDir(dir)
	a.lyricsViewer.render(mpdclient.Song{Title: "Track", File: "artist/Track.mp3"})

	if a.lyricsViewer.syncedLines != nil {
		t.Errorf("syncedLines = %+v, want nil (no .lrc file exists)", a.lyricsViewer.syncedLines)
	}
	if got := a.lyricsViewer.GetText(true); !strings.Contains(got, "plain line") {
		t.Errorf("viewer text = %q, want the plain .txt content", got)
	}
	if got := a.lyricsViewer.GetTitle(); got != lyricsViewerTitle {
		t.Errorf("title = %q, want the plain (non-synced) title %q", got, lyricsViewerTitle)
	}
}

func TestLyricsViewerRenderResetsSyncedStateOnTrackChange(t *testing.T) {
	dir := t.TempDir()
	writeLRCFixture(t, dir, "artist", "Synced.lrc", "[00:01.00]line")
	writeLRCFixture(t, dir, "artist", "Plain.txt", "line")

	a := newTestAppWithMusicDir(dir)
	a.lyricsViewer.render(mpdclient.Song{Title: "Synced", File: "artist/Synced.mp3"})
	if a.lyricsViewer.syncedLines == nil {
		t.Fatal("setup: expected synced lines loaded for the first track")
	}

	a.lyricsViewer.render(mpdclient.Song{Title: "Plain", File: "artist/Plain.mp3"})
	if a.lyricsViewer.syncedLines != nil {
		t.Error("syncedLines after switching to a track with no .lrc = non-nil, want reset to nil")
	}
	if a.lyricsViewer.currentLine != -1 {
		t.Errorf("currentLine after a track change = %d, want reset to -1", a.lyricsViewer.currentLine)
	}
}

func TestLyricsViewerUpdateHighlightNoopWithoutSyncedLines(t *testing.T) {
	dir := t.TempDir()
	writeLRCFixture(t, dir, "artist", "Track.txt", "plain line")
	a := newTestAppWithMusicDir(dir)
	a.lyricsViewer.render(mpdclient.Song{Title: "Track", File: "artist/Track.mp3"})
	before := a.lyricsViewer.GetText(false)

	a.lyricsViewer.updateHighlight(time.Minute)

	if got := a.lyricsViewer.GetText(false); got != before {
		t.Errorf("updateHighlight with no synced lines changed the text: before %q, after %q", before, got)
	}
	if a.lyricsViewer.currentLine != -1 {
		t.Errorf("currentLine = %d, want -1 (never set for plain-text content)", a.lyricsViewer.currentLine)
	}
}

func TestLyricsViewerUpdateHighlightMovesToCurrentLine(t *testing.T) {
	dir := t.TempDir()
	writeLRCFixture(t, dir, "artist", "Track.lrc", "[00:10.00]first\n[00:20.00]second\n[00:30.00]third\n")
	a := newTestAppWithMusicDir(dir)
	a.lyricsViewer.render(mpdclient.Song{Title: "Track", File: "artist/Track.mp3"})

	a.lyricsViewer.updateHighlight(5 * time.Second) // before the first line
	if a.lyricsViewer.currentLine != -1 {
		t.Errorf("currentLine before the first timestamp = %d, want -1", a.lyricsViewer.currentLine)
	}

	a.lyricsViewer.updateHighlight(15 * time.Second) // between first and second -- still first
	if a.lyricsViewer.currentLine != 0 {
		t.Errorf("currentLine at 15s = %d, want 0 (\"first\")", a.lyricsViewer.currentLine)
	}
	rendered := a.lyricsViewer.GetText(true)
	if !strings.Contains(rendered, "first") {
		t.Errorf("rendered text = %q, want it to still contain the highlighted line's text", rendered)
	}

	a.lyricsViewer.updateHighlight(25 * time.Second) // between second and third -- now second
	if a.lyricsViewer.currentLine != 1 {
		t.Errorf("currentLine at 25s = %d, want 1 (\"second\")", a.lyricsViewer.currentLine)
	}
}

// TestLyricsViewerUpdateHighlightNoopWhenLineUnchanged checks the
// early-return path directly via currentLine rather than trying to detect
// a skipped SetText call -- two elapsed values landing on the same
// current line must leave currentLine (and by extension the rendered
// content) exactly as it was.
func TestLyricsViewerUpdateHighlightNoopWhenLineUnchanged(t *testing.T) {
	dir := t.TempDir()
	writeLRCFixture(t, dir, "artist", "Track.lrc", "[00:10.00]first\n[00:20.00]second\n")
	a := newTestAppWithMusicDir(dir)
	a.lyricsViewer.render(mpdclient.Song{Title: "Track", File: "artist/Track.mp3"})

	a.lyricsViewer.updateHighlight(12 * time.Second)
	if a.lyricsViewer.currentLine != 0 {
		t.Fatalf("setup: currentLine = %d, want 0", a.lyricsViewer.currentLine)
	}
	before := a.lyricsViewer.GetText(false)

	a.lyricsViewer.updateHighlight(15 * time.Second) // still within "first"'s window
	if a.lyricsViewer.currentLine != 0 {
		t.Errorf("currentLine after a same-line tick = %d, want unchanged 0", a.lyricsViewer.currentLine)
	}
	if got := a.lyricsViewer.GetText(false); got != before {
		t.Errorf("text changed on a same-line tick: before %q, after %q", before, got)
	}
}

func TestLyricsViewerScrollToCurrentLineKeepsLookbackContext(t *testing.T) {
	dir := t.TempDir()
	var raw strings.Builder
	for i := 0; i < 20; i++ {
		fmt.Fprintf(&raw, "[00:%02d.00]line %d\n", i, i)
	}
	writeLRCFixture(t, dir, "artist", "Track.lrc", raw.String())
	a := newTestAppWithMusicDir(dir)
	a.lyricsViewer.render(mpdclient.Song{Title: "Track", File: "artist/Track.mp3"})

	a.lyricsViewer.updateHighlight(10 * time.Second) // line index 10
	row, _ := a.lyricsViewer.GetScrollOffset()
	if want := 10 - lyricsSyncedScrollLookback; row != want {
		t.Errorf("scroll offset row = %d, want %d (currentLine - lookback)", row, want)
	}
}

func TestOpenLyricsViewerSeedsHighlightFromCurrentStatus(t *testing.T) {
	dir := t.TempDir()
	writeLRCFixture(t, dir, "artist", "Track.lrc", "[00:10.00]first\n[00:20.00]second\n")
	a := newTestAppWithMusicDir(dir)
	a.currentSong = mpdclient.Song{Title: "Track", File: "artist/Track.mp3"}
	a.currentStatus = mpdclient.Status{Elapsed: 25 * time.Second}

	a.openLyricsViewer()

	if a.lyricsViewer.currentLine != 1 {
		t.Errorf("currentLine after opening mid-track = %d, want 1 (seeded from currentStatus.Elapsed immediately, not left at -1 until the next tick)", a.lyricsViewer.currentLine)
	}
}

func TestMaybeUpdateLyricsHighlightOnlyWhenOpen(t *testing.T) {
	dir := t.TempDir()
	writeLRCFixture(t, dir, "artist", "Track.lrc", "[00:10.00]first\n[00:20.00]second\n")
	a := newTestAppWithMusicDir(dir)
	a.tv.SetFocus(a.queue.table)
	a.currentSong = mpdclient.Song{Title: "Track", File: "artist/Track.mp3"}
	a.lyricsViewer.render(a.currentSong)

	a.maybeUpdateLyricsHighlight(mpdclient.Status{Elapsed: 25 * time.Second})
	if a.lyricsViewer.currentLine != -1 {
		t.Errorf("currentLine after a tick while the viewer isn't open = %d, want untouched -1", a.lyricsViewer.currentLine)
	}

	a.openLyricsViewer() // focuses the viewer
	a.maybeUpdateLyricsHighlight(mpdclient.Status{Elapsed: 25 * time.Second})
	if a.lyricsViewer.currentLine != 1 {
		t.Errorf("currentLine after a tick while the viewer is open = %d, want 1", a.lyricsViewer.currentLine)
	}
}

// TestNewLyricsViewerBorderMatchesFocusedPanelColor covers the border
// (tview.Box exposes a GetBorderColor getter); the title color has no
// equivalent getter in tview's API, so lyricsColor's own value (set on
// both via the same var in newLyricsViewer) stands in as the assertion
// for "title matches too".
func TestNewLyricsViewerBorderMatchesFocusedPanelColor(t *testing.T) {
	a := newTestApp()
	if got := a.lyricsViewer.GetBorderColor(); got != colorActiveBorder {
		t.Errorf("lyrics viewer border color = %v, want colorActiveBorder (%v), matching a focused panel's own border", got, colorActiveBorder)
	}
	if lyricsColor != colorActiveBorder {
		t.Errorf("lyricsColor = %v, want colorActiveBorder (%v)", lyricsColor, colorActiveBorder)
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
