package lyricsline

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"mpdtui/internal/mpdclient"
)

type fakeSource struct {
	song    mpdclient.Song
	status  mpdclient.Status
	songErr error
	statErr error
}

func (f fakeSource) CurrentSong() (mpdclient.Song, error) { return f.song, f.songErr }
func (f fakeSource) Status() (mpdclient.Status, error)    { return f.status, f.statErr }

// TestPrintAlwaysWritesTheWholeWindow is the contract external tools
// depend on: conky and friends print one line each, so the output must
// always be exactly windowSize lines whether or not anything matched.
func TestPrintAlwaysWritesTheWholeWindow(t *testing.T) {
	var b strings.Builder
	if err := Print(fakeSource{}, "", &b); err != nil {
		t.Fatalf("Print: %v", err)
	}
	if got := strings.Count(b.String(), "\n"); got != windowSize {
		t.Errorf("printed %d lines with nothing playing, want %d", got, windowSize)
	}
}

func TestPrintWindowsAroundTheCurrentLine(t *testing.T) {
	musicDir := t.TempDir()
	dir := filepath.Join(musicDir, "artist", "album")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	lrc := "[00:00.00]first line\n[00:10.00]second line\n[00:20.00]third line\n[00:30.00]fourth line\n"
	if err := os.WriteFile(filepath.Join(dir, "01.lrc"), []byte(lrc), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	var b strings.Builder
	err := Print(fakeSource{
		song:   mpdclient.Song{File: "artist/album/01.mp3"},
		status: mpdclient.Status{Elapsed: 12 * time.Second},
	}, musicDir, &b)
	if err != nil {
		t.Fatalf("Print: %v", err)
	}

	out := b.String()
	if !strings.Contains(out, "second line") {
		t.Errorf("output %q does not contain the line playing at 12s", out)
	}
	if got := strings.Count(out, "\n"); got != windowSize {
		t.Errorf("printed %d lines, want %d", got, windowSize)
	}
}

// TestPrintWithNoSidecarStaysBlank covers a track with no .lrc: the
// window is still printed, just empty, so an external tool's layout
// does not jump around.
func TestPrintWithNoSidecarStaysBlank(t *testing.T) {
	var b strings.Builder
	err := Print(fakeSource{song: mpdclient.Song{File: "nothing/here.mp3"}}, t.TempDir(), &b)
	if err != nil {
		t.Fatalf("Print: %v", err)
	}
	if strings.TrimSpace(b.String()) != "" {
		t.Errorf("output = %q, want blank lines", b.String())
	}
	if got := strings.Count(b.String(), "\n"); got != windowSize {
		t.Errorf("printed %d lines, want %d", got, windowSize)
	}
}

func TestPrintReportsClientFailures(t *testing.T) {
	var b strings.Builder
	if err := Print(fakeSource{songErr: errors.New("offline")}, "", &b); err == nil {
		t.Error("a failed CurrentSong was swallowed")
	}

	musicDir := t.TempDir()
	dir := filepath.Join(musicDir, "a")
	os.MkdirAll(dir, 0o755)
	os.WriteFile(filepath.Join(dir, "1.lrc"), []byte("[00:00.00]x\n"), 0o644)

	b.Reset()
	err := Print(fakeSource{
		song:    mpdclient.Song{File: "a/1.mp3"},
		statErr: errors.New("offline"),
	}, musicDir, &b)
	if err == nil {
		t.Error("a failed Status was swallowed")
	}
}

// failingWriter stands in for a closed pipe, which is what an external
// poller disappearing mid-print looks like.
type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) { return 0, errors.New("broken pipe") }

func TestPrintReportsAWriteFailure(t *testing.T) {
	if err := Print(fakeSource{}, "", failingWriter{}); err == nil {
		t.Error("a failed write was swallowed")
	}
}
