package mpdclient

import "github.com/fhs/gompd/v2/mpd"

// FetchAlbumArt attempts to retrieve album art for the given track URI.
// It first tries to read directory-level art (e.g., cover.jpg in the same
// folder), and if that fails, falls back to reading embedded picture tags.
func (c *Client) FetchAlbumArt(uri string) ([]byte, error) {
	// Try MPD's "albumart" command first (fetches from directory)
	b, err := call(c, func(conn *mpd.Client) ([]byte, error) {
		return conn.AlbumArt(uri)
	})
	if err == nil && len(b) > 0 {
		return b, nil
	}

	// Fall back to MPD's "readpicture" command (fetches embedded tags)
	return call(c, func(conn *mpd.Client) ([]byte, error) {
		return conn.ReadPicture(uri)
	})
}
