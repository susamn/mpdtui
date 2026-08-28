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

// AlbumArtists returns a map from album name to its AlbumArtist tag, for
// every album that has a non-empty one. Albums with no AlbumArtist tag
// are simply absent from the map -- the caller decides how to present
// "unknown". Built from `list albumartist group album`; an album
// credited to several album-artists reports whichever MPD lists last.
func (c *Client) AlbumArtists() (map[string]string, error) {
	groups, err := call(c, func(conn *mpd.Client) ([]mpd.Attrs, error) {
		return conn.Command("list albumartist group album").AttrsList("Album")
	})
	if err != nil {
		return nil, err
	}
	out := make(map[string]string, len(groups))
	for _, g := range groups {
		album := g["Album"]
		artist := g["AlbumArtist"]
		if album == "" || artist == "" {
			continue
		}
		out[album] = artist
	}
	return out, nil
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

// SearchAlbums performs a case-insensitive free-text search restricted to
// the Album tag, returning every track belonging to a matching album.
func (c *Client) SearchAlbums(query string) ([]Song, error) {
	list, err := call(c, func(conn *mpd.Client) ([]mpd.Attrs, error) { return conn.Search("album", query) })
	if err != nil {
		return nil, err
	}
	return parseSongs(list), nil
}

// AllSongs returns every track in the library.
func (c *Client) AllSongs() ([]Song, error) {
	list, err := call(c, func(conn *mpd.Client) ([]mpd.Attrs, error) { return conn.ListAllInfo("") })
	if err != nil {
		return nil, err
	}
	return parseSongs(list), nil
}
