// Package lyrics finds and reads sidecar lyrics (.txt) files that sit
// alongside a track's audio file on the local filesystem.
//
// MPD's protocol has no command to serve an arbitrary sibling file's
// content -- unlike album art, which MPD serves natively via its own
// albumart/readpicture commands (see mpdclient.FetchAlbumArt), there's no
// equivalent for a plain .txt file dropped next to a track. So this
// package requires direct filesystem access to the same tree MPD itself
// reads from, rooted at a musicDir the caller supplies (see
// internal/config.LoadMusicDir) -- it works only when mpdtui runs
// somewhere that can actually see those files (typically the same host as
// MPD, or a mounted/synced path).
package lyrics

import (
	"os"
	"path"
	"path/filepath"
	"strings"
	"unicode"
)

// Dir returns the local directory that would contain file's lyrics, or
// "" if musicDir is unset (the feature is inactive). file is MPD's own
// forward-slash-relative path (e.g. "Queen/A Night at the Opera/
// Bohemian Rhapsody.mp3"), so its directory is computed with the "path"
// package (always "/"-based, matching MPD's convention) before joining
// onto musicDir with "path/filepath" (OS-appropriate) to get a real local
// path.
func Dir(musicDir, file string) string {
	if musicDir == "" {
		return ""
	}
	return filepath.Join(musicDir, path.Dir(file))
}

// Candidates lists every .txt file in dir, keyed by its normalized base
// name (see Normalize) mapping to the file's actual on-disk name --
// split out from Find so a caller checking several tracks that share a
// directory (e.g. every song in an album queued together) only has to
// list it once. Returns nil if dir is "" or can't be read (doesn't
// exist, no permission, not a directory).
func Candidates(dir string) map[string]string {
	if dir == "" {
		return nil
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	out := make(map[string]string)
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if !strings.EqualFold(path.Ext(name), ".txt") {
			continue
		}
		base := strings.TrimSuffix(name, path.Ext(name))
		out[Normalize(base)] = name
	}
	return out
}

// Match reports file's matching lyrics filename (just the name, not a
// full path) from candidates (as returned by Candidates for file's own
// directory), and true, or ("", false) if there's no match. Both sides
// are normalized (Normalize) before comparing, so e.g. "Some Track
// [84934].mp3" matches a sidecar named "some_track-84934.txt" despite the
// two filenames differing in punctuation and spacing.
func Match(file string, candidates map[string]string) (string, bool) {
	base := strings.TrimSuffix(path.Base(file), path.Ext(file))
	name, ok := candidates[Normalize(base)]
	return name, ok
}

// Find returns the absolute local path to file's matching lyrics file
// under musicDir, and true, or ("", false) if there isn't one -- musicDir
// is unset, file's directory can't be read, or no .txt in it normalizes
// to the same name as file.
func Find(musicDir, file string) (string, bool) {
	dir := Dir(musicDir, file)
	name, ok := Match(file, Candidates(dir))
	if !ok {
		return "", false
	}
	return filepath.Join(dir, name), true
}

// Read reads and returns the full lyrics text for file under musicDir,
// and true, or ("", false) if there's no matching lyrics file or it
// can't be read.
func Read(musicDir, file string) (string, bool) {
	lyricsPath, ok := Find(musicDir, file)
	if !ok {
		return "", false
	}
	data, err := os.ReadFile(lyricsPath)
	if err != nil {
		return "", false
	}
	return string(data), true
}

// Normalize folds s to just its letters and digits, lowercased -- e.g.
// "Some Track [84934]" -> "sometrack84934" -- so a track's filename and
// its lyrics sidecar still match despite differing punctuation, spacing,
// or case.
func Normalize(s string) string {
	var b strings.Builder
	for _, r := range s {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(unicode.ToLower(r))
		}
	}
	return b.String()
}
