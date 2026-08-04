package mpdclient

import "github.com/fhs/gompd/v2/mpd"

// Artists returns every distinct artist name in the library.
func (c *Client) Artists() ([]string, error) {
	return call(c, func(conn *mpd.Client) ([]string, error) { return conn.List("Artist") })
}

// Albums returns every distinct album name, optionally restricted to one
// artist (empty string means all artists).
func (c *Client) Albums(artist string) ([]string, error) {
	if artist == "" {
		return call(c, func(conn *mpd.Client) ([]string, error) { return conn.List("Album") })
	}
	return call(c, func(conn *mpd.Client) ([]string, error) { return conn.List("Album", "Artist", artist) })
}

// ArtistTracks returns every track by artist, across all their albums.
func (c *Client) ArtistTracks(artist string) ([]Song, error) {
	list, err := call(c, func(conn *mpd.Client) ([]mpd.Attrs, error) { return conn.Find("Artist", artist) })
	if err != nil {
		return nil, err
	}
	return parseSongs(list), nil
}

// Tracks returns every track for the given artist+album, in track order.
func (c *Client) Tracks(artist, album string) ([]Song, error) {
	list, err := call(c, func(conn *mpd.Client) ([]mpd.Attrs, error) {
		return conn.Find("Artist", artist, "Album", album)
	})
	if err != nil {
		return nil, err
	}
	return parseSongs(list), nil
}

// Search performs a case-insensitive free-text search across all tags.
func (c *Client) Search(query string) ([]Song, error) {
	list, err := call(c, func(conn *mpd.Client) ([]mpd.Attrs, error) { return conn.Search("any", query) })
	if err != nil {
		return nil, err
	}
	return parseSongs(list), nil
}
