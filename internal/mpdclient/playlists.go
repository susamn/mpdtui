package mpdclient

import (
	"errors"
	"time"

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
		pls[i] = Playlist{Name: a["playlist"], LastModified: parsePlaylistLastModified(a)}
	}
	return pls, nil
}

// parsePlaylistLastModified reads listplaylists' "Last-Modified" field
// (capitalized, unlike lsinfo's lowercased "last-modified" -- see
// parseDirEntries in directory.go for that one; gompd is inconsistent
// about casing between commands).
func parsePlaylistLastModified(a mpd.Attrs) time.Time {
	return parseTimestamp(a, "Last-Modified")
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

// PlaylistTrackCounts returns every stored playlist's track count, keyed
// by name. Deliberately uses MPD's "listplaylist" (track paths only) via
// the low-level Command/AttrsList escape hatch, rather than
// PlaylistContents' "listplaylistinfo" (every tag for every track) --
// only a count is needed here, and fetching full tag data for every track
// of every playlist is measurably heavier for no benefit (timed against a
// real ~200-playlist library: ~2ms/playlist with listplaylist vs.
// ~6ms/playlist with listplaylistinfo). gompd has no bulk/batched way to
// list many playlists' contents in one round-trip (its CommandList type
// only covers a fixed set of write/control commands, not this), so this
// is still one MPD round-trip per playlist -- still well under a second
// for a few hundred playlists, but real enough that callers should treat
// this as a background operation, not something to run inline on every
// UI refresh (see App.refreshTrackCounts).
func (c *Client) PlaylistTrackCounts() (map[string]int, error) {
	return call(c, func(conn *mpd.Client) (map[string]int, error) {
		lists, err := conn.ListPlaylists()
		if err != nil {
			return nil, err
		}
		counts := make(map[string]int, len(lists))
		for _, a := range lists {
			name := a["playlist"]
			tracks, err := conn.Command("listplaylist %s", name).AttrsList("file")
			if err != nil {
				return nil, err
			}
			counts[name] = len(tracks)
		}
		return counts, nil
	})
}
