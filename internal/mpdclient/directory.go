package mpdclient

import (
	"strconv"
	"time"

	"github.com/fhs/gompd/v2/mpd"
)

// EntryType distinguishes what kind of database entry ListDirectory
// returned: MPD's lsinfo command mixes subdirectories, playable files, and
// stored playlists together at each level.
type EntryType int

const (
	EntryDirectory EntryType = iota
	EntryFile
	EntryPlaylist
)

// DirEntry is one child of a directory listing (see Client.ListDirectory).
type DirEntry struct {
	Type EntryType
	Path string // MPD's own path, usable as-is for a further ListDirectory call, QueueAdd, PlaylistAppend, etc.
	Song Song   // populated only when Type == EntryFile
	// LastModified is MPD's last-modified timestamp for this entry, or
	// the zero time if MPD didn't report one. Files reliably report a
	// real mtime; directories are inconsistent (verified against a live
	// server -- root-level directories reported none, nested ones did),
	// so any code sorting/filtering by this must handle the zero value
	// as "unknown", not "very old".
	LastModified time.Time
}

// ListDirectory lists the immediate children of path (empty string means
// the library root) using MPD's lsinfo command -- the actual filesystem
// layout of the music directory, as opposed to the tag-derived Artist/
// Album/Track view Artists/Albums/Tracks already provide.
func (c *Client) ListDirectory(path string) ([]DirEntry, error) {
	attrs, err := call(c, func(conn *mpd.Client) ([]mpd.Attrs, error) {
		return conn.ListInfo(path)
	})
	if err != nil {
		return nil, err
	}
	return parseDirEntries(attrs), nil
}

// parseDirEntries classifies each attribute set lsinfo returned. Split out
// from ListDirectory so it's testable without a live MPD connection.
func parseDirEntries(attrs []mpd.Attrs) []DirEntry {
	entries := make([]DirEntry, 0, len(attrs))
	for _, a := range attrs {
		switch {
		case a["directory"] != "":
			entries = append(entries, DirEntry{Type: EntryDirectory, Path: a["directory"], LastModified: parseLibraryLastModified(a)})
		case a["playlist"] != "":
			entries = append(entries, DirEntry{Type: EntryPlaylist, Path: a["playlist"], LastModified: parseLibraryLastModified(a)})
		case a["file"] != "":
			entries = append(entries, DirEntry{Type: EntryFile, Path: a["file"], Song: parseLibrarySong(a), LastModified: parseLibraryLastModified(a)})
		}
	}
	return entries
}

// parseLibrarySong builds a Song from an lsinfo file entry. Unlike
// parseSong (used for queue/playlist entries via PlaylistInfo/CurrentSong,
// which preserve MPD's original key casing), gompd's ListInfo lowercases
// every key, and there's no Id/Pos -- these aren't queue entries.
func parseLibrarySong(a mpd.Attrs) Song {
	return Song{
		ID:       -1,
		Pos:      -1,
		File:     a["file"],
		Artist:   a["artist"],
		Album:    a["album"],
		Title:    a["title"],
		Duration: parseLibraryDuration(a),
	}
}

// parseLibraryLastModified reads lsinfo's lowercased "last-modified" field
// (gompd's ListInfo lowercases every key -- unlike ListPlaylists, which
// preserves "Last-Modified" capitalized; see parsePlaylistLastModified in
// playlists.go).
func parseLibraryLastModified(a mpd.Attrs) time.Time {
	return parseTimestamp(a, "last-modified")
}

func parseLibraryDuration(a mpd.Attrs) time.Duration {
	if v, ok := a["duration"]; ok {
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			return time.Duration(f * float64(time.Second))
		}
	}
	if v, ok := a["time"]; ok {
		if i, err := strconv.Atoi(v); err == nil {
			return time.Duration(i) * time.Second
		}
	}
	return 0
}
