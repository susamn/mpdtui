package ui

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/rivo/tview"

	"mpdtui/internal/mpdclient"
)

// newTestAppWithMusicDir mirrors newTestApp (keys_test.go), but with the
// lyrics feature active against musicDir -- needed for any test that
// exercises lyricsPresence/lyricsViewer beyond the "feature inactive"
// default newTestApp's empty musicDir already covers.
func newTestAppWithMusicDir(musicDir string) *App {
	a := &App{tv: tview.NewApplication(), musicDir: musicDir}
	a.build()
	a.queue.table.SetRect(0, 0, 150, 40)
	return a
}

// lrcTickText/txtTickText build the exact colored-tag cell content
// lyricsCellText produces for one format alone -- computed independently
// here (not by calling lyricsCellText itself) so these tests genuinely
// check the expected tag format, not just that the function agrees with
// itself.
func lrcTickText() string { return fmt.Sprintf("[%s]%s[-]", lyricsLRCColor, lyricsTick) }
func txtTickText() string { return fmt.Sprintf("[%s]%s[-]", lyricsTxtColor, lyricsTick) }

func TestLyricsPresenceFeatureInactiveWithoutMusicDir(t *testing.T) {
	a := newTestApp() // musicDir == ""
	hasLRC, hasTxt := a.queue.lyricsPresence("artist/track.mp3", map[string]map[string]string{}, map[string]map[string]string{})
	if hasLRC || hasTxt {
		t.Errorf("lyricsPresence with no musicDir configured = (%v, %v), want (false, false) (feature inactive)", hasLRC, hasTxt)
	}
}

func TestLyricsPresenceTrueForMatchingTxtSidecar(t *testing.T) {
	dir := t.TempDir()
	trackDir := filepath.Join(dir, "artist")
	if err := os.MkdirAll(trackDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(filepath.Join(trackDir, "Some Track [84934].txt"), []byte("la la"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	a := newTestAppWithMusicDir(dir)
	hasLRC, hasTxt := a.queue.lyricsPresence("artist/some_track-84934.mp3", map[string]map[string]string{}, map[string]map[string]string{})
	if hasLRC {
		t.Error("lyricsPresence with only a .txt sidecar: hasLRC = true, want false")
	}
	if !hasTxt {
		t.Error("lyricsPresence for a track with a matching (normalized) .txt sidecar: hasTxt = false, want true")
	}
}

func TestLyricsPresenceTrueForMatchingLRCSidecar(t *testing.T) {
	dir := t.TempDir()
	trackDir := filepath.Join(dir, "artist")
	if err := os.MkdirAll(trackDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(filepath.Join(trackDir, "Track.lrc"), []byte("[00:01.00]la la"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	a := newTestAppWithMusicDir(dir)
	hasLRC, hasTxt := a.queue.lyricsPresence("artist/Track.mp3", map[string]map[string]string{}, map[string]map[string]string{})
	if !hasLRC {
		t.Error("lyricsPresence for a track with a matching .lrc sidecar: hasLRC = false, want true")
	}
	if hasTxt {
		t.Error("lyricsPresence with only a .lrc sidecar: hasTxt = true, want false")
	}
}

func TestLyricsPresenceBothFormats(t *testing.T) {
	dir := t.TempDir()
	trackDir := filepath.Join(dir, "artist")
	if err := os.MkdirAll(trackDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(filepath.Join(trackDir, "Track.lrc"), []byte("[00:01.00]la la"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if err := os.WriteFile(filepath.Join(trackDir, "Track.txt"), []byte("la la"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	a := newTestAppWithMusicDir(dir)
	hasLRC, hasTxt := a.queue.lyricsPresence("artist/Track.mp3", map[string]map[string]string{}, map[string]map[string]string{})
	if !hasLRC || !hasTxt {
		t.Errorf("lyricsPresence with both sidecars present = (%v, %v), want (true, true)", hasLRC, hasTxt)
	}
}

func TestLyricsPresenceFalseWithoutMatchingSidecar(t *testing.T) {
	dir := t.TempDir()
	a := newTestAppWithMusicDir(dir)
	hasLRC, hasTxt := a.queue.lyricsPresence("artist/track.mp3", map[string]map[string]string{}, map[string]map[string]string{})
	if hasLRC || hasTxt {
		t.Errorf("lyricsPresence with no sidecar present = (%v, %v), want (false, false)", hasLRC, hasTxt)
	}
}

// --- lyricsCellText ---

func TestLyricsCellTextNeitherFormat(t *testing.T) {
	if got := lyricsCellText(false, false); got != "" {
		t.Errorf("lyricsCellText(false, false) = %q, want empty", got)
	}
}

func TestLyricsCellTextLRCOnly(t *testing.T) {
	if got, want := lyricsCellText(true, false), lrcTickText(); got != want {
		t.Errorf("lyricsCellText(true, false) = %q, want %q", got, want)
	}
}

func TestLyricsCellTextTxtOnly(t *testing.T) {
	if got, want := lyricsCellText(false, true), txtTickText(); got != want {
		t.Errorf("lyricsCellText(false, true) = %q, want %q", got, want)
	}
}

func TestLyricsCellTextBothFormats(t *testing.T) {
	got := lyricsCellText(true, true)
	want := lrcTickText() + txtTickText()
	if got != want {
		t.Errorf("lyricsCellText(true, true) = %q, want %q (LRC tick, then TXT tick)", got, want)
	}
}

// --- Queue render wiring ---

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
	lyrCol := a.queue.cols.lyr

	if got, want := a.queue.table.GetCell(queueHeaderRows, lyrCol).Text, txtTickText(); got != want {
		t.Errorf("Lyr cell for the track with a .txt = %q, want %q", got, want)
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

// TestQueueRenderShowsBothTicksWhenBothFormatsPresent covers the
// explicit request: "can we have multi tick of diff colors... green for
// LRC and Orange for TXT" -- both present at once, not just one or the
// other.
func TestQueueRenderShowsBothTicksWhenBothFormatsPresent(t *testing.T) {
	dir := t.TempDir()
	trackDir := filepath.Join(dir, "artist")
	if err := os.MkdirAll(trackDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(filepath.Join(trackDir, "Track.lrc"), []byte("[00:01.00]la la"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if err := os.WriteFile(filepath.Join(trackDir, "Track.txt"), []byte("la la"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	a := newTestAppWithMusicDir(dir)
	a.queue.songs = []mpdclient.Song{{ID: 1, Title: "Track", File: "artist/Track.mp3"}}
	a.queue.render(-1)
	lyrCol := a.queue.cols.lyr

	got := a.queue.table.GetCell(queueHeaderRows, lyrCol).Text
	want := lrcTickText() + txtTickText()
	if got != want {
		t.Errorf("Lyr cell with both .lrc and .txt present = %q, want %q", got, want)
	}
}

// TestQueueRenderRechecksLyricsOnEveryRender is the "as I may add more
// lyrics" requirement: a track queued before its lyrics file existed
// picks up the Lyr icon on the very next render, no restart or requeue
// needed, since lyricsPresence always reads the real directory contents
// (only caching within one render pass, see render's lrcDirs/txtDirs).
func TestQueueRenderRechecksLyricsOnEveryRender(t *testing.T) {
	dir := t.TempDir()
	trackDir := filepath.Join(dir, "artist")
	if err := os.MkdirAll(trackDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	a := newTestAppWithMusicDir(dir)
	a.queue.songs = []mpdclient.Song{{ID: 1, Title: "Track", File: "artist/Track.mp3"}}
	a.queue.render(-1)
	lyrCol := a.queue.cols.lyr

	if got := a.queue.table.GetCell(queueHeaderRows, lyrCol).Text; got != "" {
		t.Fatalf("setup: Lyr cell = %q, want empty (no lyrics file yet)", got)
	}

	if err := os.WriteFile(filepath.Join(trackDir, "Track.txt"), []byte("now it exists"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	a.queue.render(-1)

	if got, want := a.queue.table.GetCell(queueHeaderRows, lyrCol).Text, txtTickText(); got != want {
		t.Errorf("Lyr cell after adding the lyrics file = %q, want %q", got, want)
	}
}
