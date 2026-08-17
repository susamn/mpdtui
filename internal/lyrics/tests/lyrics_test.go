package tests

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"mpdtui/internal/lyrics"
)

func TestNormalizeStripsSpecialCharactersAndFoldsCase(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"Some Track [84934]", "sometrack84934"},
		{"some_track-84934", "sometrack84934"},
		{"SOME TRACK (84934)!", "sometrack84934"},
		{"", ""},
	}
	for _, tc := range cases {
		if got := lyrics.Normalize(tc.in); got != tc.want {
			t.Errorf("Normalize(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestDirEmptyMusicDirMeansInactive(t *testing.T) {
	if got := lyrics.Dir("", "artist/track.mp3"); got != "" {
		t.Errorf("Dir(\"\", ...) = %q, want empty (feature inactive)", got)
	}
}

func TestDirJoinsMusicDirWithTracksDirectory(t *testing.T) {
	got := lyrics.Dir("/music", "Queen/A Night at the Opera/Bohemian Rhapsody.mp3")
	want := filepath.Join("/music", "Queen/A Night at the Opera")
	if got != want {
		t.Errorf("Dir(...) = %q, want %q", got, want)
	}
}

func TestCandidatesOnMissingDirReturnsNil(t *testing.T) {
	if got := lyrics.Candidates(filepath.Join(t.TempDir(), "does-not-exist")); got != nil {
		t.Errorf("Candidates(missing dir) = %v, want nil", got)
	}
	if got := lyrics.Candidates(""); got != nil {
		t.Errorf("Candidates(\"\") = %v, want nil", got)
	}
}

func TestCandidatesOnlyListsTxtFilesNormalized(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "Some Track [84934].txt", "la la la")
	writeFile(t, dir, "Some Track [84934].mp3", "not lyrics")
	writeFile(t, dir, "cover.jpg", "not lyrics either")

	got := lyrics.Candidates(dir)
	want := "sometrack84934"
	name, ok := got[want]
	if !ok {
		t.Fatalf("Candidates(%q) = %v, want a %q entry", dir, got, want)
	}
	if name != "Some Track [84934].txt" {
		t.Errorf("Candidates(%q)[%q] = %q, want %q", dir, want, name, "Some Track [84934].txt")
	}
	if len(got) != 1 {
		t.Errorf("Candidates(%q) = %v, want exactly 1 entry (non-.txt files excluded)", dir, got)
	}
}

// TestFindMatchesDespiteDifferingPunctuation is the scenario the user
// described: the track and its lyrics sidecar have the same name once
// special characters are stripped from both, even though the raw
// filenames differ (brackets vs. underscore/hyphen here).
func TestFindMatchesDespiteDifferingPunctuation(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "some_track-84934.txt", "la la la")

	musicDir := filepath.Dir(dir)
	relDir := filepath.Base(dir)
	file := relDir + "/Some Track [84934].mp3"

	got, ok := lyrics.Find(musicDir, file)
	if !ok {
		t.Fatalf("Find(%q, %q) = not found, want a match", musicDir, file)
	}
	want := filepath.Join(dir, "some_track-84934.txt")
	if got != want {
		t.Errorf("Find(...) = %q, want %q", got, want)
	}
}

func TestFindNoMatchingLyrics(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "Unrelated.txt", "la la la")

	musicDir := filepath.Dir(dir)
	relDir := filepath.Base(dir)
	file := relDir + "/Some Track.mp3"

	if _, ok := lyrics.Find(musicDir, file); ok {
		t.Error("Find(...) = found a match, want none")
	}
}

func TestFindEmptyMusicDirNeverMatches(t *testing.T) {
	if _, ok := lyrics.Find("", "artist/track.mp3"); ok {
		t.Error("Find(\"\", ...) = found a match, want none (feature inactive)")
	}
}

func TestReadReturnsLyricsContent(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "Track.txt", "line one\nline two\n")

	musicDir := filepath.Dir(dir)
	relDir := filepath.Base(dir)
	file := relDir + "/Track.mp3"

	got, ok := lyrics.Read(musicDir, file)
	if !ok {
		t.Fatalf("Read(%q, %q) = not found, want the file's content", musicDir, file)
	}
	if want := "line one\nline two\n"; got != want {
		t.Errorf("Read(...) = %q, want %q", got, want)
	}
}

func TestReadNoMatchReturnsFalse(t *testing.T) {
	dir := t.TempDir()
	musicDir := filepath.Dir(dir)
	relDir := filepath.Base(dir)
	file := relDir + "/Track.mp3"

	if _, ok := lyrics.Read(musicDir, file); ok {
		t.Error("Read(...) with no lyrics file present = found content, want false")
	}
}

// TestFindRechecksLiveState is the "as I may add more lyrics" scenario:
// a track with no lyrics yet, then a lyrics file appears later -- Find
// must reflect the current directory state on every call, not a cached
// snapshot from an earlier one.
func TestFindRechecksLiveState(t *testing.T) {
	dir := t.TempDir()
	musicDir := filepath.Dir(dir)
	relDir := filepath.Base(dir)
	file := relDir + "/Track.mp3"

	if _, ok := lyrics.Find(musicDir, file); ok {
		t.Fatal("setup: expected no lyrics file yet")
	}

	writeFile(t, dir, "Track.txt", "now it exists")

	if _, ok := lyrics.Find(musicDir, file); !ok {
		t.Error("Find(...) after adding the lyrics file = not found, want a match")
	}
}

func writeFile(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
		t.Fatalf("writeFile(%q): %v", name, err)
	}
}

// --- ParseLRC ---

func TestParseLRCBasicTimestamps(t *testing.T) {
	raw := "[00:12.50]line one\n[00:15.30]line two\n[01:02.00]line three\n"
	got := lyrics.ParseLRC(raw)

	want := []lyrics.LyricLine{
		{Time: 12*time.Second + 500*time.Millisecond, Text: "line one"},
		{Time: 15*time.Second + 300*time.Millisecond, Text: "line two"},
		{Time: time.Minute + 2*time.Second, Text: "line three"},
	}
	if len(got) != len(want) {
		t.Fatalf("ParseLRC(...) = %+v, want %d lines", got, len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("line %d = %+v, want %+v", i, got[i], want[i])
		}
	}
}

func TestParseLRCMillisecondPrecision(t *testing.T) {
	got := lyrics.ParseLRC("[00:12.345]line")
	want := 12*time.Second + 345*time.Millisecond
	if len(got) != 1 || got[0].Time != want {
		t.Errorf("ParseLRC(3-digit fraction) = %+v, want Time = %v", got, want)
	}
}

func TestParseLRCNoFraction(t *testing.T) {
	got := lyrics.ParseLRC("[02:03]line")
	want := 2*time.Minute + 3*time.Second
	if len(got) != 1 || got[0].Time != want || got[0].Text != "line" {
		t.Errorf("ParseLRC(no fraction) = %+v, want [{%v line}]", got, want)
	}
}

func TestParseLRCSkipsMetadataTagsAndBlankLines(t *testing.T) {
	raw := "[ar:Some Artist]\n[ti:Some Title]\n[offset:0]\n\n[00:01.00]real line\n"
	got := lyrics.ParseLRC(raw)
	if len(got) != 1 || got[0].Text != "real line" {
		t.Errorf("ParseLRC(with metadata tags) = %+v, want exactly the one real timestamped line", got)
	}
}

func TestParseLRCSkipsPlainUntaggedLines(t *testing.T) {
	got := lyrics.ParseLRC("not a timestamp line\n[00:01.00]real line\n")
	if len(got) != 1 || got[0].Text != "real line" {
		t.Errorf("ParseLRC(with a plain untagged line) = %+v, want exactly the one real timestamped line", got)
	}
}

// TestParseLRCMultipleTimestampsPerLine covers the LRC convention for a
// repeated line (e.g. a chorus): several leading timestamp tags sharing
// one line of text, each producing its own LyricLine.
func TestParseLRCMultipleTimestampsPerLine(t *testing.T) {
	got := lyrics.ParseLRC("[00:12.00][00:45.00]chorus line\n")
	if len(got) != 2 {
		t.Fatalf("ParseLRC(repeated line) = %+v, want 2 entries", got)
	}
	if got[0].Text != "chorus line" || got[1].Text != "chorus line" {
		t.Errorf("ParseLRC(repeated line) = %+v, want both entries to share the same text", got)
	}
	if got[0].Time != 12*time.Second || got[1].Time != 45*time.Second {
		t.Errorf("ParseLRC(repeated line) times = %v, %v, want 12s, 45s", got[0].Time, got[1].Time)
	}
}

func TestParseLRCSortsByTimestampRegardlessOfFileOrder(t *testing.T) {
	raw := "[00:45.00]second\n[00:12.00]first\n[01:00.00]third\n"
	got := lyrics.ParseLRC(raw)
	if len(got) != 3 || got[0].Text != "first" || got[1].Text != "second" || got[2].Text != "third" {
		t.Errorf("ParseLRC(out-of-order file) = %+v, want sorted first/second/third", got)
	}
}

func TestParseLRCHandlesCRLFLineEndings(t *testing.T) {
	got := lyrics.ParseLRC("[00:01.00]line one\r\n[00:02.00]line two\r\n")
	if len(got) != 2 || got[0].Text != "line one" || got[1].Text != "line two" {
		t.Errorf("ParseLRC(CRLF) = %+v, want the \\r stripped from both lines' text", got)
	}
}

func TestParseLRCEmptyInput(t *testing.T) {
	if got := lyrics.ParseLRC(""); len(got) != 0 {
		t.Errorf("ParseLRC(\"\") = %+v, want no lines", got)
	}
}

func TestParseLRCMalformedTimestampSkipped(t *testing.T) {
	got := lyrics.ParseLRC("[not-a-timestamp]line\n[00:01.00]real line\n")
	if len(got) != 1 || got[0].Text != "real line" {
		t.Errorf("ParseLRC(malformed timestamp) = %+v, want only the one valid line", got)
	}
}

// --- LRCCandidates / FindLRC / ReadLRC ---

func TestLRCCandidatesOnlyListsLrcFilesNormalized(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "Some Track [84934].lrc", "[00:01.00]la la la")
	writeFile(t, dir, "Some Track [84934].txt", "plain lyrics")
	writeFile(t, dir, "cover.jpg", "not lyrics")

	got := lyrics.LRCCandidates(dir)
	name, ok := got["sometrack84934"]
	if !ok {
		t.Fatalf("LRCCandidates(%q) = %v, want a %q entry", dir, got, "sometrack84934")
	}
	if name != "Some Track [84934].lrc" {
		t.Errorf("LRCCandidates(%q)[...] = %q, want %q", dir, name, "Some Track [84934].lrc")
	}
	if len(got) != 1 {
		t.Errorf("LRCCandidates(%q) = %v, want exactly 1 entry (.txt excluded)", dir, got)
	}
}

func TestFindLRCAndFindAreIndependent(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "Track.lrc", "[00:01.00]synced")
	musicDir := filepath.Dir(dir)
	file := filepath.Base(dir) + "/Track.mp3"

	if _, ok := lyrics.FindLRC(musicDir, file); !ok {
		t.Error("FindLRC(...) with a matching .lrc present = not found, want a match")
	}
	if _, ok := lyrics.Find(musicDir, file); ok {
		t.Error("Find(...) (plain .txt lookup) = found a match, want none -- only a .lrc file exists here")
	}
}

func TestReadLRCReturnsParsedSortedLines(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "Track.lrc", "[00:05.00]second\n[00:01.00]first\n")
	musicDir := filepath.Dir(dir)
	file := filepath.Base(dir) + "/Track.mp3"

	got, ok := lyrics.ReadLRC(musicDir, file)
	if !ok {
		t.Fatal("ReadLRC(...) = not found, want the parsed lines")
	}
	if len(got) != 2 || got[0].Text != "first" || got[1].Text != "second" {
		t.Errorf("ReadLRC(...) = %+v, want sorted [first, second]", got)
	}
}

func TestReadLRCNoMatchReturnsFalse(t *testing.T) {
	dir := t.TempDir()
	musicDir := filepath.Dir(dir)
	file := filepath.Base(dir) + "/Track.mp3"

	if _, ok := lyrics.ReadLRC(musicDir, file); ok {
		t.Error("ReadLRC(...) with no .lrc file present = found lines, want false")
	}
}

// TestReadLRCMalformedFileReturnsFalse covers a .lrc file that exists but
// parses to zero valid timestamped lines (e.g. someone renamed a plain
// .txt to .lrc by mistake) -- must report "not found", not an empty-but-
// present result, so the caller falls back to plain-text rendering.
func TestReadLRCMalformedFileReturnsFalse(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "Track.lrc", "just plain text, no timestamps at all")
	musicDir := filepath.Dir(dir)
	file := filepath.Base(dir) + "/Track.mp3"

	if _, ok := lyrics.ReadLRC(musicDir, file); ok {
		t.Error("ReadLRC(malformed .lrc) = found lines, want false")
	}
}

// --- CurrentLineIndex ---

func TestCurrentLineIndexBeforeFirstLine(t *testing.T) {
	lines := []lyrics.LyricLine{{Time: 10 * time.Second, Text: "a"}, {Time: 20 * time.Second, Text: "b"}}
	if got := lyrics.CurrentLineIndex(lines, 5*time.Second); got != -1 {
		t.Errorf("CurrentLineIndex(before first line) = %d, want -1", got)
	}
}

func TestCurrentLineIndexExactAndBetweenTimestamps(t *testing.T) {
	lines := []lyrics.LyricLine{
		{Time: 10 * time.Second, Text: "a"},
		{Time: 20 * time.Second, Text: "b"},
		{Time: 30 * time.Second, Text: "c"},
	}
	cases := map[time.Duration]int{
		10 * time.Second: 0, // exact match on a's own timestamp
		15 * time.Second: 0, // between a and b -- still a
		20 * time.Second: 1, // exact match on b
		25 * time.Second: 1, // between b and c -- still b
		30 * time.Second: 2,
	}
	for elapsed, want := range cases {
		if got := lyrics.CurrentLineIndex(lines, elapsed); got != want {
			t.Errorf("CurrentLineIndex(%v) = %d, want %d", elapsed, got, want)
		}
	}
}

func TestCurrentLineIndexAfterLastLine(t *testing.T) {
	lines := []lyrics.LyricLine{{Time: 10 * time.Second, Text: "a"}, {Time: 20 * time.Second, Text: "b"}}
	if got := lyrics.CurrentLineIndex(lines, time.Hour); got != 1 {
		t.Errorf("CurrentLineIndex(long after the last line) = %d, want 1 (the last line stays current)", got)
	}
}

func TestCurrentLineIndexEmptyLines(t *testing.T) {
	if got := lyrics.CurrentLineIndex(nil, time.Minute); got != -1 {
		t.Errorf("CurrentLineIndex(nil lines) = %d, want -1", got)
	}
}
