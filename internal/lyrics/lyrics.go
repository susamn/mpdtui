// Package lyrics finds and reads sidecar lyrics files that sit alongside
// a track's audio file on the local filesystem -- plain text (.txt) or
// timestamped/synced (.lrc).
//
// MPD's protocol has no command to serve an arbitrary sibling file's
// content -- unlike album art, which MPD serves natively via its own
// albumart/readpicture commands (see mpdclient.FetchAlbumArt), there's no
// equivalent for a plain sidecar file. So this package requires direct
// filesystem access to the same tree MPD itself reads from, rooted at a
// musicDir the caller supplies (see internal/config.LoadMusicDir) -- it
// works only when mpdtui runs somewhere that can actually see those files
// (typically the same host as MPD, or a mounted/synced path).
package lyrics

import (
	"os"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
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
	return candidatesWithExt(dir, ".txt")
}

// LRCCandidates is Candidates' counterpart for synced (.lrc) lyrics --
// kept as its own function rather than an optional parameter on
// Candidates, since callers specifically checking for synced lyrics
// availability (see FindLRC) need a distinct list, not a mixed one.
func LRCCandidates(dir string) map[string]string {
	return candidatesWithExt(dir, ".lrc")
}

func candidatesWithExt(dir, ext string) map[string]string {
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
		if !strings.EqualFold(path.Ext(name), ext) {
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

// FindLRC returns the absolute local path to file's matching .lrc
// sidecar under musicDir, and true, or ("", false) if there isn't one --
// mirrors Find exactly, just matched against LRCCandidates instead of
// Candidates.
func FindLRC(musicDir, file string) (string, bool) {
	dir := Dir(musicDir, file)
	name, ok := Match(file, LRCCandidates(dir))
	if !ok {
		return "", false
	}
	return filepath.Join(dir, name), true
}

// ReadLRC reads and parses file's matching .lrc sidecar under musicDir
// into timed lines sorted by timestamp, and true, or (nil, false) if
// there's no matching .lrc, it can't be read, or it has no lines with a
// recognizable leading timestamp tag (an empty, non-LRC, or entirely
// malformed file).
func ReadLRC(musicDir, file string) ([]LyricLine, bool) {
	lrcPath, ok := FindLRC(musicDir, file)
	if !ok {
		return nil, false
	}
	data, err := os.ReadFile(lrcPath)
	if err != nil {
		return nil, false
	}
	lines := ParseLRC(string(data))
	if len(lines) == 0 {
		return nil, false
	}
	return lines, true
}

// LyricLine is one timestamped line of synced (.lrc) lyrics.
type LyricLine struct {
	Time time.Duration
	Text string
}

// ParseLRC parses raw LRC-format text into timed lines, sorted by
// timestamp. Recognizes the standard "[mm:ss]", "[mm:ss.hh]" (hundredths),
// and "[mm:ss.hhh]" (milliseconds) tag forms, and a line may carry more
// than one leading timestamp tag (e.g. "[00:12.00][00:45.00]text", the
// LRC convention for a repeated line like a chorus) -- one LyricLine is
// emitted per timestamp found on that line, all sharing its text.
// Metadata tags ("[ar:Artist]", "[ti:Title]", "[offset:...]", etc.),
// blank lines, and any line with no recognizable leading timestamp tag
// at all are silently skipped rather than treated as errors -- a handful
// of unrecognized lines in an otherwise valid LRC file shouldn't sink
// the whole thing.
func ParseLRC(raw string) []LyricLine {
	var lines []LyricLine
	for _, rawLine := range strings.Split(raw, "\n") {
		times, text := parseLRCLine(rawLine)
		for _, t := range times {
			lines = append(lines, LyricLine{Time: t, Text: text})
		}
	}
	sort.SliceStable(lines, func(i, j int) bool { return lines[i].Time < lines[j].Time })
	return lines
}

// parseLRCLine extracts every leading timestamp tag from line (see
// parseLRCTag) and returns them alongside the remaining text, trimmed of
// surrounding whitespace and any trailing \r from CRLF line endings.
// Returns (nil, "") if line has no leading timestamp tag at all (a
// metadata tag, a blank line, or plain text with no tag).
func parseLRCLine(line string) ([]time.Duration, string) {
	line = strings.TrimRight(line, "\r")
	var times []time.Duration
	for {
		d, rest, ok := parseLRCTag(line)
		if !ok {
			break
		}
		times = append(times, d)
		line = rest
	}
	return times, strings.TrimSpace(line)
}

// parseLRCTag parses one leading "[mm:ss]"/"[mm:ss.hh]"/"[mm:ss.hhh]" tag
// off the front of line, returning the parsed duration, the remainder of
// line past the tag's closing "]", and whether a valid timestamp tag was
// found. ok is false -- without consuming anything -- for a line that
// doesn't start with "[", has no closing "]", or whose bracketed content
// isn't a numeric mm:ss(.fraction) timestamp (e.g. a metadata tag like
// "ar:Some Artist", where "ar" fails to parse as minutes).
func parseLRCTag(line string) (d time.Duration, rest string, ok bool) {
	if !strings.HasPrefix(line, "[") {
		return 0, line, false
	}
	end := strings.IndexByte(line, ']')
	if end < 0 {
		return 0, line, false
	}
	mm, ss, frac, tagOK := parseLRCTimestamp(line[1:end])
	if !tagOK {
		return 0, line, false
	}
	d = time.Duration(mm)*time.Minute + time.Duration(ss)*time.Second + frac
	return d, line[end+1:], true
}

// parseLRCTimestamp parses tag ("mm:ss", "mm:ss.hh", or "mm:ss.hhh") into
// minutes, seconds, and a fractional-second duration -- the fraction's
// own digit count decides its scale (2 digits = hundredths, 3 =
// milliseconds, matching the two LRC precisions in real-world use), so
// "34" and "340" both mean 340ms, just written to different precision.
// ok is false for anything that doesn't parse as digits:digits(.digits)?,
// which is what lets callers tell a real timestamp apart from a
// metadata tag sharing the same "[...]" bracket syntax.
func parseLRCTimestamp(tag string) (mm, ss int, frac time.Duration, ok bool) {
	minPart, rest, found := strings.Cut(tag, ":")
	if !found {
		return 0, 0, 0, false
	}
	secPart, fracPart, _ := strings.Cut(rest, ".")

	m, err1 := strconv.Atoi(minPart)
	s, err2 := strconv.Atoi(secPart)
	if err1 != nil || err2 != nil || m < 0 || s < 0 {
		return 0, 0, 0, false
	}
	if fracPart != "" {
		f, err := strconv.Atoi(fracPart)
		if err != nil || f < 0 {
			return 0, 0, 0, false
		}
		scale := time.Second
		for i := 0; i < len(fracPart); i++ {
			scale /= 10
		}
		frac = time.Duration(f) * scale
	}
	return m, s, frac, true
}

// CurrentLineIndex returns the index into lines (sorted by Time, as
// ParseLRC/ReadLRC already return them) of whichever line is current at
// elapsed -- the last line whose Time is <= elapsed -- or -1 if lines is
// empty or elapsed is before the first line's own timestamp (e.g. during
// an instrumental intro, before any lyrics have started).
func CurrentLineIndex(lines []LyricLine, elapsed time.Duration) int {
	idx := -1
	for i, l := range lines {
		if l.Time > elapsed {
			break
		}
		idx = i
	}
	return idx
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
