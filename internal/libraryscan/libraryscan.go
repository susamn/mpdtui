// Package libraryscan counts what the music directory holds beside the
// audio: lyrics sidecars and fetched track stories.
//
// MPD knows nothing about either. Both are plain files living next to
// the tracks (see internal/lyrics and internal/trackwiki), so the only
// way to total them is to look, which is what this does -- once, over
// the directories MPD's own track list says are in use, rather than by
// walking the whole tree. That bounds the work by album count rather
// than file count and means a stray folder of downloads inside the
// music directory cannot inflate the numbers.
//
// The caller supplies MPD's track paths, so nothing here has to guess
// which extensions count as audio. That guess is the usual way a
// summary like this ends up disagreeing with the player it sits in.
package libraryscan

import (
	"os"
	"path"
	"path/filepath"
	"strings"

	"mpdtui/internal/lyrics"
	"mpdtui/internal/trackwiki"
)

// Counts is one scan's result.
type Counts struct {
	// Tracks is how many of the supplied paths were examined, and Dirs
	// how many distinct directories they live in.
	Tracks int
	Dirs   int

	// Synced and Plain are tracks with a matching .lrc / .txt sidecar,
	// Both tracks with one of each, and WithAny tracks with at least
	// one. Synced and Plain overlap by exactly Both, so
	// Synced+Plain-Both == WithAny.
	Synced  int
	Plain   int
	Both    int
	WithAny int

	// Orphans is sidecars in those directories that match no track --
	// a lyrics file left behind by a rename, or one whose name drifted
	// far enough that internal/lyrics no longer pairs it. They are
	// invisible everywhere else in the app, which is the point of
	// counting them here.
	Orphans int

	// Stories is story directories holding a wiki.json this version can
	// actually open, and Unreadable ones holding a manifest it refuses
	// (a newer schema, malformed JSON, or nothing worth a modal). A
	// story the card would not show is not a story the summary should
	// claim, so the two are counted apart.
	Stories    int
	Unreadable int

	// Images is every file sitting in a story directory beside its
	// manifest.
	Images int
}

// Scan examines the directories of files under musicDir and totals
// their sidecars and stories. files are MPD's own forward-slash
// relative track paths.
//
// Never returns an error: an unreadable directory contributes nothing
// and the rest of the scan still answers. A partial count is worth more
// here than a failed one -- this is a summary the user opened, not a
// step in an operation that has to be all-or-nothing -- and a directory
// that cannot be read is already visible as a track with no lyrics
// everywhere else in the app.
func Scan(musicDir string, files []string) Counts {
	var c Counts
	if musicDir == "" {
		return c
	}

	byDir := make(map[string][]string)
	for _, f := range files {
		if f == "" {
			continue
		}
		byDir[path.Dir(f)] = append(byDir[path.Dir(f)], f)
	}
	c.Tracks = len(files)
	c.Dirs = len(byDir)

	for dir, tracks := range byDir {
		local := filepath.Join(musicDir, dir)
		txt, lrc, hasWiki := readSidecars(local)

		// claimed is the set of sidecar keys some track matched.
		// Collected rather than deducted as we go, because two tracks
		// whose names normalize alike share one sidecar: removing it
		// from the map on the first track would make the second look
		// like it had no lyrics.
		claimed := make(map[string]struct{}, len(tracks))
		for _, f := range tracks {
			_, plain := lyrics.Match(f, txt)
			_, synced := lyrics.Match(f, lrc)
			switch {
			case plain && synced:
				c.Both++
				c.Plain++
				c.Synced++
				c.WithAny++
			case plain:
				c.Plain++
				c.WithAny++
			case synced:
				c.Synced++
				c.WithAny++
			}
			if plain || synced {
				claimed[lyrics.Normalize(stem(f))] = struct{}{}
			}
		}
		for _, side := range []map[string]string{txt, lrc} {
			for key := range side {
				if _, ok := claimed[key]; !ok {
					c.Orphans++
				}
			}
		}

		if hasWiki {
			stories, unreadable, images := scanStories(filepath.Join(local, trackwiki.DirName))
			c.Stories += stories
			c.Unreadable += unreadable
			c.Images += images
		}
	}
	return c
}

// readSidecars lists one directory once and reports its .txt and .lrc
// files (keyed as internal/lyrics keys them) plus whether it holds a
// story folder.
//
// One os.ReadDir for all three rather than lyrics.Candidates and
// lyrics.LRCCandidates back to back: those list the directory once
// each, which is fine for the one album a card is showing and is three
// times the syscalls when the whole collection is being counted.
func readSidecars(dir string) (txt, lrc map[string]string, hasWiki bool) {
	txt, lrc = map[string]string{}, map[string]string{}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return txt, lrc, false
	}
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() {
			if name == trackwiki.DirName {
				hasWiki = true
			}
			continue
		}
		key := lyrics.Normalize(strings.TrimSuffix(name, path.Ext(name)))
		switch strings.ToLower(path.Ext(name)) {
		case ".txt":
			txt[key] = name
		case ".lrc":
			lrc[key] = name
		}
	}
	return txt, lrc, hasWiki
}

// scanStories counts one album's wiki/ folder: each child directory is
// one track's story (see music-tui docs/wiki.md).
func scanStories(wikiDir string) (stories, unreadable, images int) {
	entries, err := os.ReadDir(wikiDir)
	if err != nil {
		return 0, 0, 0
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		storyDir := filepath.Join(wikiDir, e.Name())
		if _, ok := trackwiki.LoadDir(storyDir); ok {
			stories++
		} else {
			unreadable++
		}
		images += countImages(storyDir)
	}
	return stories, unreadable, images
}

// countImages counts everything in a story directory that is not the
// manifest. Counted by exclusion rather than by extension for the same
// reason Scan takes MPD's file list: a whitelist that misses a format
// undercounts silently, while the one filename that is definitely not a
// picture is known.
func countImages(storyDir string) int {
	entries, err := os.ReadDir(storyDir)
	if err != nil {
		return 0
	}
	n := 0
	for _, e := range entries {
		if e.IsDir() || e.Name() == trackwiki.FileName {
			continue
		}
		n++
	}
	return n
}

// stem is a track path's base name without its extension -- what
// internal/lyrics normalizes when pairing a sidecar to a track.
func stem(file string) string {
	base := path.Base(file)
	return strings.TrimSuffix(base, path.Ext(base))
}
