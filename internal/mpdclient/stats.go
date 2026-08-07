package mpdclient

import (
	"strconv"

	"github.com/fhs/gompd/v2/mpd"
)

// LibraryStats summarizes the library: total tracks and artists (from
// MPD's stats command), plus stored playlist count -- stats doesn't
// include that, so it's a separate call.
type LibraryStats struct {
	Tracks    int
	Artists   int
	Playlists int
}

// LibraryStats fetches a fresh snapshot of the library totals.
func (c *Client) LibraryStats() (LibraryStats, error) {
	attrs, err := call(c, func(conn *mpd.Client) (mpd.Attrs, error) {
		return conn.Stats()
	})
	if err != nil {
		return LibraryStats{}, err
	}

	playlists, err := c.Playlists()
	if err != nil {
		return LibraryStats{}, err
	}

	tracks, _ := strconv.Atoi(attrs["songs"])
	artists, _ := strconv.Atoi(attrs["artists"])
	return LibraryStats{
		Tracks:    tracks,
		Artists:   artists,
		Playlists: len(playlists),
	}, nil
}
