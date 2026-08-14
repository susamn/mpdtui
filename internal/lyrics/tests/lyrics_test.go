package tests

import (
	"os"
	"path/filepath"
	"testing"

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
