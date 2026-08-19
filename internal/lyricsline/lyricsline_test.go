package lyricsline

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"mpdtui/internal/config"
	"mpdtui/internal/lyrics"
	"mpdtui/internal/mpdclient"
)

// dialOrSkip mirrors internal/mini/mini_test.go's own helper of the same
// name -- duplicated, not imported, for the one test here that needs a
// real MPD connection.
func dialOrSkip(t *testing.T) *mpdclient.Client {
	t.Helper()
	c, err := mpdclient.Dial(config.Load())
	if err != nil {
		t.Skipf("no MPD server reachable, skipping: %v", err)
	}
	t.Cleanup(func() { c.Close() })
	return c
}

func TestWindowMidSongHasContextOnBothSides(t *testing.T) {
	lines := []lyrics.LyricLine{
		{Time: 10 * time.Second, Text: "one"},
		{Time: 20 * time.Second, Text: "two"},
		{Time: 30 * time.Second, Text: "three"},
		{Time: 40 * time.Second, Text: "four"},
	}
	got := Window(lines, lyrics.CurrentLineIndex(lines, 25*time.Second)) // idx 1, "two"
	want := [windowSize]string{"one", "two", "three", "four"}
	if got != want {
		t.Errorf("Window(...) = %v, want %v", got, want)
	}
}

// TestWindowBeforeFirstLineLeavesAboveSlotBlank covers
// lyrics.CurrentLineIndex's own -1 ("before the first timestamp, e.g. an
// instrumental intro") case: there's no current line and nothing above
// it, but the two lines below must still show as upcoming context.
func TestWindowBeforeFirstLineLeavesAboveSlotBlank(t *testing.T) {
	lines := []lyrics.LyricLine{
		{Time: 10 * time.Second, Text: "one"},
		{Time: 20 * time.Second, Text: "two"},
	}
	got := Window(lines, lyrics.CurrentLineIndex(lines, 0)) // idx -1
	want := [windowSize]string{"", "", "one", "two"}
	if got != want {
		t.Errorf("Window(...) = %v, want %v", got, want)
	}
}

// TestWindowNearEndOfSongLeavesBelowSlotsBlank covers the current line
// being close enough to the end of the track that there's nothing left
// to show for one or both of the below-context slots.
func TestWindowNearEndOfSongLeavesBelowSlotsBlank(t *testing.T) {
	lines := []lyrics.LyricLine{
		{Time: 10 * time.Second, Text: "one"},
		{Time: 20 * time.Second, Text: "two"},
		{Time: 30 * time.Second, Text: "three"},
	}
	got := Window(lines, lyrics.CurrentLineIndex(lines, 35*time.Second)) // idx 2, "three", last line
	want := [windowSize]string{"two", "three", "", ""}
	if got != want {
		t.Errorf("Window(...) = %v, want %v", got, want)
	}
}

func TestWindowEmptyLinesAllBlank(t *testing.T) {
	got := Window(nil, -1)
	want := [windowSize]string{"", "", "", ""}
	if got != want {
		t.Errorf("Window(nil, -1) = %v, want %v", got, want)
	}
}

// TestPrintAlwaysWritesExactlyWindowSizeLines is the fixed-line-count
// contract Print exists for -- an external layout (conky) must never see
// a variable number of lines, whatever MPD's actual state is right now
// (stopped, playing something with no .lrc, mid-song with synced
// lyrics, ...). Needs a live MPD connection since Print's whole job is
// the round-trip; musicDir is deliberately left unset here so this stays
// offline-safe regardless of what's configured for the real music
// library -- the point being tested is the line count holding even
// without a musicDir, not any particular lyrics content.
func TestPrintAlwaysWritesExactlyWindowSizeLines(t *testing.T) {
	c := dialOrSkip(t)
	var buf bytes.Buffer
	if err := Print(c, "", &buf); err != nil {
		t.Fatalf("Print: %v", err)
	}
	// Count newlines rather than strings.Split, which would collapse to
	// a single (misleadingly non-empty) element once every line is
	// blank -- exactly the all-blank case this test needs to also cover
	// correctly (no musicDir configured here).
	if got := strings.Count(buf.String(), "\n"); got != windowSize {
		t.Errorf("Print wrote %d lines (%q), want exactly %d", got, buf.String(), windowSize)
	}
}

// TestPrintWithoutMusicDirPutsNoSyncedLyricsOnCurrentLineOnly covers the
// no-.lrc case (guaranteed deterministically here by leaving musicDir
// unset, same trick as the test above): the message lands only on the
// current-line slot (line linesAbove+1 in 1-indexed output), every other
// slot stays blank -- still exactly windowSize lines, not collapsed to
// just the message, so an external tool reading any other line number
// never gets nothing.
func TestPrintWithoutMusicDirPutsNoSyncedLyricsOnCurrentLineOnly(t *testing.T) {
	c := dialOrSkip(t)
	var buf bytes.Buffer
	if err := Print(c, "", &buf); err != nil {
		t.Fatalf("Print: %v", err)
	}
	var want [windowSize]string
	want[linesAbove] = noSyncedLyrics
	if got := buf.String(); got != strings.Join(want[:], "\n")+"\n" {
		t.Errorf("Print wrote %q, want %q", got, strings.Join(want[:], "\n")+"\n")
	}
}

// TestPrintExactAlwaysWritesExactlyOneLine is PrintExact's own
// fixed-line-count contract -- always exactly 1 line, whatever MPD's
// state. Same offline-safe musicDir="" trick as the Print tests above.
func TestPrintExactAlwaysWritesExactlyOneLine(t *testing.T) {
	c := dialOrSkip(t)
	var buf bytes.Buffer
	if err := PrintExact(c, "", &buf); err != nil {
		t.Fatalf("PrintExact: %v", err)
	}
	if got := strings.Count(buf.String(), "\n"); got != 1 {
		t.Errorf("PrintExact wrote %d lines (%q), want exactly 1", got, buf.String())
	}
}

// TestPrintExactWithoutMusicDirWritesNoSyncedLyrics covers PrintExact's
// no-.lrc case (deterministic via musicDir="", same as Print's).
func TestPrintExactWithoutMusicDirWritesNoSyncedLyrics(t *testing.T) {
	c := dialOrSkip(t)
	var buf bytes.Buffer
	if err := PrintExact(c, "", &buf); err != nil {
		t.Fatalf("PrintExact: %v", err)
	}
	if got := strings.TrimRight(buf.String(), "\n"); got != noSyncedLyrics {
		t.Errorf("PrintExact wrote %q, want %q", got, noSyncedLyrics)
	}
}
