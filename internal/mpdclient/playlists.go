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

// parsePlaylistLastModified parses MPD's "Last-Modified" field (ISO
// 8601/RFC 3339, e.g. "2026-08-05T03:46:30Z") from a listplaylists
// response row. Returns the zero time if the field is missing or
// unparseable, rather than erroring the whole listing over it.
func parsePlaylistLastModified(a mpd.Attrs) time.Time {
	v, ok := a["Last-Modified"]
	if !ok {
		return time.Time{}
	}
	t, err := time.Parse(time.RFC3339, v)
	if err != nil {
		return time.Time{}
	}
	return t
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
