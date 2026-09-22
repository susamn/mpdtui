package mpdclient

import (
	"errors"
	"sort"

	"github.com/fhs/gompd/v2/mpd"
)

// ErrTrackAlreadyInPlaylist is returned by AddTrackToPlaylist when uri is
// already present in the target playlist.
var ErrTrackAlreadyInPlaylist = errors.New("track already in playlist")

// Playlists returns every stored (saved) playlist.
func (c *Client) Playlists() ([]Playlist, error) {
	list, err := call(c, func(conn *mpd.Client) ([]mpd.Attrs, error) { return conn.ListPlaylists() })
	if err != nil {
		return nil, err
	}
	pls := make([]Playlist, len(list))
	for i, a := range list {
		pls[i] = Playlist{Name: a["playlist"], LastModified: ParseLastModified(a)}
	}
	return pls, nil
}

// PlaylistTracks returns the tracks stored in playlist name.
func (c *Client) PlaylistTracks(name string) ([]Song, error) {
	list, err := call(c, func(conn *mpd.Client) ([]mpd.Attrs, error) { return conn.PlaylistContents(name) })
	if err != nil {
		return nil, err
	}
	return parseSongs(list), nil
}

// PlaylistLoad replaces the queue with the contents of playlist name and
// starts playback from its first track.
func (c *Client) PlaylistLoad(name string) error {
	return callErr(c, func(conn *mpd.Client) error {
		if err := conn.Clear(); err != nil {
			return err
		}
		if err := conn.PlaylistLoad(name, -1, -1); err != nil {
			return err
		}
		return conn.Play(-1)
	})
}

// PlaylistAppend appends playlist name's contents to the current queue
// without clearing it or starting playback.
func (c *Client) PlaylistAppend(name string) error {
	return callErr(c, func(conn *mpd.Client) error { return conn.PlaylistLoad(name, -1, -1) })
}

// AddTrackToPlaylist appends uri to the stored playlist name, writing
// directly into that playlist's own .m3u file via MPD's "playlistadd"
// command -- unlike PlaylistAppend/PlaylistLoad, which only ever read a
// stored playlist's contents into the queue, never touching the file
// itself. Returns ErrTrackAlreadyInPlaylist, without touching the file at
// all, if uri is already one of the playlist's tracks -- "playlistadd"
// has no dedup of its own and would otherwise happily write a second line
// for the same track. The duplicate check is deliberately its own call()
// (a plain "listplaylist" read), not folded into the same closure as the
// mutation: call()'s reconnect-and-retry-once logic re-invokes fn on any
// non-nil error, and ErrTrackAlreadyInPlaylist is an outcome, not a
// connection failure worth retrying. name not existing yet is treated as
// zero existing tracks rather than an error -- "playlistadd" creates a
// brand-new NAME.m3u the same as it would for any other add, so adding
// the very first track to a not-yet-existing playlist must still work.
func (c *Client) AddTrackToPlaylist(name, uri string) error {
	existing, err := call(c, func(conn *mpd.Client) ([]string, error) {
		attrs, err := conn.Command("listplaylist %s", name).AttrsList("file")
		if err != nil {
			var mpdErr mpd.Error
			if errors.As(err, &mpdErr) && mpdErr.Code == mpd.ErrorNoExist {
				return nil, nil
			}
			return nil, err
		}
		files := make([]string, len(attrs))
		for i, a := range attrs {
			files[i] = a["file"]
		}
		return files, nil
	})
	if err != nil {
		return err
	}
	for _, f := range existing {
		if f == uri {
			return ErrTrackAlreadyInPlaylist
		}
	}
	return callErr(c, func(conn *mpd.Client) error { return conn.PlaylistAdd(name, uri) })
}

// PlaylistDelete deletes the stored playlist name.
func (c *Client) PlaylistDelete(name string) error {
	return callErr(c, func(conn *mpd.Client) error { return conn.PlaylistRemove(name) })
}

// SaveQueueAsPlaylist saves the current queue as a new stored playlist
// named name.
func (c *Client) SaveQueueAsPlaylist(name string) error {
	return callErr(c, func(conn *mpd.Client) error { return conn.PlaylistSave(name) })
}

// PlaylistIndex is one snapshot of every stored playlist's contents,
// reduced to the two views the UI needs: how many tracks each playlist
// holds, and which playlists a given track appears in.
//
// Both come from the same scan on purpose. Building the membership map
// costs nothing extra -- listing a playlist already returns its track
// paths, and the count is just their length -- so the alternative,
// answering "which playlists is this track in?" with its own pass over
// every playlist, would double the work to learn something already in
// hand.
type PlaylistIndex struct {
	// Counts maps a playlist name to its track count.
	Counts map[string]int
	// Membership maps a track URI to the names of every playlist
	// containing it, sorted, with duplicates collapsed -- a playlist
	// that lists the same track twice appears once.
	Membership map[string][]string
}

// PlaylistIndex reads every stored playlist once and returns both views
// of it (see PlaylistIndex).
//
// Deliberately uses MPD's "listplaylist" (track paths only) via the
// low-level Command/AttrsList escape hatch, rather than
// PlaylistContents' "listplaylistinfo" (every tag for every track) --
// paths are all either view needs, and fetching full tag data for every
// track of every playlist is measurably heavier for no benefit (timed
// against a real ~200-playlist library: ~2ms/playlist with listplaylist
// vs. ~6ms/playlist with listplaylistinfo). gompd has no bulk/batched
// way to list many playlists' contents in one round-trip (its
// CommandList type only covers a fixed set of write/control commands,
// not this), so this is still one MPD round-trip per playlist -- still
// well under a second for a few hundred playlists, but real enough that
// callers should treat this as a background operation, not something to
// run inline on every UI refresh (see App.refreshTrackCounts).
func (c *Client) PlaylistIndex() (PlaylistIndex, error) {
	return call(c, func(conn *mpd.Client) (PlaylistIndex, error) {
		lists, err := conn.ListPlaylists()
		if err != nil {
			return PlaylistIndex{}, err
		}
		idx := PlaylistIndex{
			Counts:     make(map[string]int, len(lists)),
			Membership: make(map[string][]string),
		}
		for _, a := range lists {
			name := a["playlist"]
			tracks, err := conn.Command("listplaylist %s", name).AttrsList("file")
			if err != nil {
				return PlaylistIndex{}, err
			}
			idx.Counts[name] = len(tracks)
			for _, t := range tracks {
				uri := t["file"]
				if uri == "" {
					continue
				}
				// A playlist listing the same track twice must still
				// name that playlist only once.
				if m := idx.Membership[uri]; len(m) > 0 && m[len(m)-1] == name {
					continue
				}
				idx.Membership[uri] = append(idx.Membership[uri], name)
			}
		}
		for _, names := range idx.Membership {
			sort.Strings(names)
		}
		return idx, nil
	})
}

// PlaylistTrackCounts returns every stored playlist's track count, keyed
// by name -- the Counts half of PlaylistIndex, which see for the cost of
// the underlying scan.
func (c *Client) PlaylistTrackCounts() (map[string]int, error) {
	idx, err := c.PlaylistIndex()
	if err != nil {
		return nil, err
	}
	return idx.Counts, nil
}

// PlaylistFallout is one stored playlist's unresolvable entries -- the
// paths it lists that MPD's database has no track for.
//
// This is what a playlist imported from somewhere else leaves behind: a
// .m3u written against another machine's layout, or against files that
// were since renamed, moved or never copied across. MPD loads such a
// playlist without complaint and simply skips those lines, so a
// playlist that is half missing looks identical to a short one from
// inside the app. Counting the gap is the only way to see it.
type PlaylistFallout struct {
	Name string
	// Total is how many entries the playlist file lists, including the
	// missing ones.
	Total int
	// Missing is every listed path the library does not have, in the
	// order the playlist lists them.
	Missing []string
}

// PlaylistFalloutReport is PlaylistFallouts' whole answer: the totals
// across every stored playlist, and the affected playlists themselves.
type PlaylistFalloutReport struct {
	// Playlists is how many stored playlists were scanned, Entries the
	// total number of lines across all of them, and Missing how many of
	// those the library could not resolve.
	Playlists int
	Entries   int
	Missing   int

	// Affected holds only the playlists with at least one missing
	// entry, worst first, ties broken by name. A report on 200
	// playlists of which 3 have holes is a report about those 3.
	Affected []PlaylistFallout
}

// PlaylistFallouts reads every stored playlist and reports which of
// their entries the library cannot resolve (see PlaylistFallout).
//
// Costs one "list file" for the whole library plus one "listplaylist"
// per stored playlist -- the same per-playlist scan PlaylistIndex makes,
// which see for why that is a background operation and not something to
// run inline on a UI refresh. Deliberately a separate scan rather than
// another field on PlaylistIndex: that one runs every ten minutes to
// keep the Playlists panel's counts fresh, and it should not start
// failing, or paying for a full library listing, because of a summary
// the user opens occasionally.
func (c *Client) PlaylistFallouts() (PlaylistFalloutReport, error) {
	return call(c, func(conn *mpd.Client) (PlaylistFalloutReport, error) {
		// "list file" rather than listallinfo: only the paths are being
		// compared, and this is the cheap way to ask for exactly those.
		files, err := conn.GetFiles()
		if err != nil {
			return PlaylistFalloutReport{}, err
		}
		known := make(map[string]struct{}, len(files))
		for _, f := range files {
			known[f] = struct{}{}
		}

		lists, err := conn.ListPlaylists()
		if err != nil {
			return PlaylistFalloutReport{}, err
		}

		contents := make([]playlistEntries, 0, len(lists))
		for _, a := range lists {
			name := a["playlist"]
			entries, err := conn.Command("listplaylist %s", name).AttrsList("file")
			if err != nil {
				return PlaylistFalloutReport{}, err
			}
			uris := make([]string, 0, len(entries))
			for _, e := range entries {
				uris = append(uris, e["file"])
			}
			contents = append(contents, playlistEntries{Name: name, URIs: uris})
		}
		return falloutReport(known, contents), nil
	})
}

// playlistEntries is one stored playlist as read off the wire: its name
// and the paths it lists, verbatim.
type playlistEntries struct {
	Name string
	URIs []string
}

// falloutReport assembles the report from playlists already read.
//
// Split from PlaylistFallouts so the counting and ordering can be
// tested against a library with holes in it. The integration test can
// only assert the shape of whatever the developer's own collection
// happens to be, and a collection whose playlists all resolve -- the
// good case -- exercises none of this.
func falloutReport(known map[string]struct{}, playlists []playlistEntries) PlaylistFalloutReport {
	report := PlaylistFalloutReport{Playlists: len(playlists)}
	for _, pl := range playlists {
		fallout := PlaylistFallout{Name: pl.Name, Total: len(pl.URIs)}
		for _, uri := range pl.URIs {
			if uri == "" {
				continue
			}
			if _, ok := known[uri]; ok {
				continue
			}
			fallout.Missing = append(fallout.Missing, uri)
		}
		report.Entries += fallout.Total
		report.Missing += len(fallout.Missing)
		if len(fallout.Missing) > 0 {
			report.Affected = append(report.Affected, fallout)
		}
	}
	sort.Slice(report.Affected, func(i, j int) bool {
		if a, b := len(report.Affected[i].Missing), len(report.Affected[j].Missing); a != b {
			return a > b
		}
		return report.Affected[i].Name < report.Affected[j].Name
	})
	return report
}
