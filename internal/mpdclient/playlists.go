package mpdclient

import (
	"time"

	"github.com/fhs/gompd/v2/mpd"
)

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

// PlaylistDelete deletes the stored playlist name.
func (c *Client) PlaylistDelete(name string) error {
	return callErr(c, func(conn *mpd.Client) error { return conn.PlaylistRemove(name) })
}

// SaveQueueAsPlaylist saves the current queue as a new stored playlist
// named name.
func (c *Client) SaveQueueAsPlaylist(name string) error {
	return callErr(c, func(conn *mpd.Client) error { return conn.PlaylistSave(name) })
}

// ReplacePlaylist overwrites the stored playlist name so it ends up
// containing exactly uris, in order -- regardless of whether a playlist
// by that name already existed. Used for playlists mpdtui itself
// generates (see the "Recently Added" playlist in internal/ui), which
// need to be fully regenerated from scratch each time rather than
// appended to.
//
// Deletes any existing playlist by that name first; the delete's error is
// deliberately ignored (MPD reports "No such playlist" the first time
// name doesn't exist yet, which is expected, not a failure) rather than
// distinguishing that from other failure modes -- if something's
// genuinely wrong (e.g. a permissions issue), the PlaylistAdd calls right
// after it will surface their own error anyway. The adds themselves run
// as one batched command list instead of one MPD round-trip per track.
func (c *Client) ReplacePlaylist(name string, uris []string) error {
	return callErr(c, func(conn *mpd.Client) error {
		_ = conn.PlaylistRemove(name)
		cl := conn.BeginCommandList()
		for _, uri := range uris {
			cl.PlaylistAdd(name, uri)
		}
		return cl.End()
	})
}
