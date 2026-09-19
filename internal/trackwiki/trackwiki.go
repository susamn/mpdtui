// Package trackwiki reads the per-track story that music-tui's wiki-fetch
// gathered ahead of time and wiki-push copied into the music collection:
// a wiki.json and its images, in a directory of their own beside the
// audio.
//
// Everything here is local. mpdtui never goes online -- fetching happens
// in a separate tool, on the user's own schedule, and this package only
// ever reads what is already on disk. That is what keeps opening the
// modal instant and keeps it working with no network at all.
//
// Like internal/lyrics, this needs direct filesystem access to the tree
// MPD itself reads from, rooted at a musicDir the caller supplies (see
// internal/config.LoadMusicDir): MPD's protocol has no command to serve
// an arbitrary sibling file.
package trackwiki

import (
	"encoding/json"
	"os"
	"path"
	"path/filepath"
	"strings"
)

// SchemaVersion is the only wiki.json version this understands.
//
// A file declaring anything else is refused rather than read as best it
// can be: the version is bumped only for a breaking change, so guessing
// at a newer one means showing the user something quietly wrong.
const SchemaVersion = 1

// DirName is the folder each album gains to hold its tracks' stories.
// The push inserts this level (see music-tui docs/wiki.md) so an album
// keeps one wiki folder rather than a directory per track among the
// audio files.
const DirName = "wiki"

// Prose is a headed block of plain text -- never HTML or Markdown, so it
// can be wrapped to the terminal as-is.
type Prose struct {
	Heading string `json:"heading"`
	Body    string `json:"body"`
	Source  string `json:"source"`
	URL     string `json:"url"`
}

// Bootleg is one unofficial recording: a live tape, an alternate take, a
// radio session.
type Bootleg struct {
	Title  string `json:"title"`
	Date   string `json:"date"`
	Venue  string `json:"venue"`
	City   string `json:"city"`
	Format string `json:"format"`
	Notes  string `json:"notes"`
	Source string `json:"source"`
	URL    string `json:"url"`
}

// Image is one picture in the track's directory. File is a bare
// filename: the directory is flat, and a path here would be a way out of
// it, so Path refuses anything that looks like one.
type Image struct {
	File    string `json:"file"`
	Role    string `json:"role"`
	Caption string `json:"caption"`
	Credit  string `json:"credit"`
	Source  string `json:"source"`
	URL     string `json:"url"`
}

// Link is further reading.
type Link struct {
	Label string `json:"label"`
	URL   string `json:"url"`
}

// Source is one service consulted, with the licence its text carries --
// Wikipedia's CC BY-SA has to be attributed where the text is shown.
type Source struct {
	Name      string `json:"name"`
	URL       string `json:"url"`
	License   string `json:"license"`
	FetchedAt string `json:"fetched_at"`
}

// Track is who the file is about, carried in the file itself so a
// directory that ended up in the wrong place can be noticed rather than
// shown against another track.
type Track struct {
	File   string `json:"file"`
	Title  string `json:"title"`
	Artist string `json:"artist"`
	Album  string `json:"album"`
	Year   string `json:"year"`
}

// Story is the main narrative: a summary shown first, then any sections.
type Story struct {
	Summary  string  `json:"summary"`
	Sections []Prose `json:"sections"`
}

// Wiki is one track's whole story.
type Wiki struct {
	Schema          int       `json:"schema"`
	Track           Track     `json:"track"`
	FetchedAt       string    `json:"fetched_at"`
	Story           Story     `json:"story"`
	BehindTheScenes []Prose   `json:"behind_the_scenes"`
	Bootlegs        []Bootleg `json:"bootlegs"`
	Images          []Image   `json:"images"`
	Links           []Link    `json:"links"`
	Sources         []Source  `json:"sources"`

	// dir is where this was loaded from, kept so Path can resolve the
	// images without the caller having to recompute it.
	dir string
}

// Dir returns the local directory that would hold file's story, or ""
// if musicDir is unset (the feature is inactive).
//
// file is MPD's own forward-slash-relative path, so its directory and
// base name are computed with "path" (always "/"-based, matching MPD's
// convention) before joining onto musicDir with "path/filepath"
// (OS-appropriate) -- the same split internal/lyrics.Dir makes, and for
// the same reason.
func Dir(musicDir, file string) string {
	if musicDir == "" || file == "" {
		return ""
	}
	base := path.Base(file)
	stem := strings.TrimSuffix(base, path.Ext(base))
	if stem == "" {
		return ""
	}
	return filepath.Join(musicDir, path.Dir(file), DirName, stem)
}

// Load reads the story for file. The bool reports whether there is one
// at all -- much the commoner answer, since a story has to have been
// fetched and pushed for a track before there is anything to read.
//
// A malformed or unknown-version file reads as "no story" rather than as
// an error: this is a passive card the user opened out of curiosity, and
// there is nothing they could usefully do about a bad file from inside
// the player.
func Load(musicDir, file string) (Wiki, bool) {
	dir := Dir(musicDir, file)
	if dir == "" {
		return Wiki{}, false
	}
	data, err := os.ReadFile(filepath.Join(dir, "wiki.json"))
	if err != nil {
		return Wiki{}, false
	}
	var w Wiki
	if err := json.Unmarshal(data, &w); err != nil {
		return Wiki{}, false
	}
	if w.Schema != SchemaVersion {
		return Wiki{}, false
	}
	w.dir = dir
	if w.Empty() {
		return Wiki{}, false
	}
	return w, true
}

// Empty reports whether a file parsed but has nothing worth opening a
// modal for. A wiki.json with only its track block is not a story.
func (w Wiki) Empty() bool {
	return w.Story.Summary == "" && len(w.Story.Sections) == 0 &&
		len(w.BehindTheScenes) == 0 && len(w.Bootlegs) == 0 && len(w.Images) == 0
}

// Path resolves an image to its file on disk, or "" if the name is not
// one this package will open.
//
// Only a bare filename is accepted. wiki.json is generated by a script
// and could name anything; a manifest entry of "../../../etc/passwd"
// must not become a read outside the track's own directory, and the
// directory is documented as flat anyway.
func (w Wiki) Path(img Image) string {
	if w.dir == "" || img.File == "" {
		return ""
	}
	if img.File != filepath.Base(img.File) || img.File != path.Base(img.File) {
		return ""
	}
	if strings.HasPrefix(img.File, ".") {
		return ""
	}
	return filepath.Join(w.dir, img.File)
}

// ImagesWithRole returns the images carrying one role, in manifest
// order.
func (w Wiki) ImagesWithRole(role string) []Image {
	var out []Image
	for _, img := range w.Images {
		if img.Role == role {
			out = append(out, img)
		}
	}
	return out
}
