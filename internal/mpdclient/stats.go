package mpdclient

import (
	"sort"
	"strconv"
	"time"

	"github.com/fhs/gompd/v2/mpd"
)

// LibraryStats summarizes the library: totals from MPD's stats command,
// plus stored playlist count -- stats doesn't include that, so it's a
// separate call.
type LibraryStats struct {
	Tracks    int
	Artists   int
	Albums    int
	Playlists int

	// Playtime is how long it would take to play the whole database
	// once (MPD's db_playtime), zero when MPD didn't report it.
	Playtime time.Duration

	// Updated is when MPD last finished a database update (db_update,
	// a Unix timestamp), zero when MPD didn't report it. A library that
	// gained files MPD has not been told about looks complete
	// everywhere else in the app; this is the field that gives it away.
	Updated time.Time
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
	albums, _ := strconv.Atoi(attrs["albums"])
	return LibraryStats{
		Tracks:    tracks,
		Artists:   artists,
		Albums:    albums,
		Playlists: len(playlists),
		Playtime:  parseSeconds(attrs["db_playtime"]),
		Updated:   parseUnix(attrs["db_update"]),
	}, nil
}

// parseSeconds reads one of stats' whole-second durations. An absent or
// unparseable value is zero rather than an error: a missing total is
// one blank field on a summary, not a reason to fail the whole command.
func parseSeconds(v string) time.Duration {
	n, err := strconv.ParseInt(v, 10, 64)
	if err != nil || n <= 0 {
		return 0
	}
	return time.Duration(n) * time.Second
}

// parseUnix reads one of stats' Unix timestamps, zero on anything
// unparseable -- see parseSeconds.
func parseUnix(v string) time.Time {
	n, err := strconv.ParseInt(v, 10, 64)
	if err != nil || n <= 0 {
		return time.Time{}
	}
	return time.Unix(n, 0)
}

// RecentTrack is a library track together with the modification time
// MPD recorded for its file.
//
// "Recently added" is this field sorted: MPD has no notion of when a
// track entered the library, only the file's own mtime, which is what
// its Last-Modified reports. For a collection that is copied in rather
// than edited in place those are the same thing; for one where files
// get re-tagged after the fact they are not, which is why the card
// labels this by what it actually is.
type RecentTrack struct {
	Song
	LastModified time.Time
}

// RecentTracks returns the n most recently modified library tracks,
// newest first.
//
// One listallinfo round-trip for the whole library -- the same call
// AllSongs makes -- rather than walking the music directory, because
// this needs each track's tags to be readable in a list and the
// filesystem only has paths. Tracks whose Last-Modified MPD did not
// report are left out entirely: a zero timestamp sorted as "very old"
// is wrong, and sorted as "now" is worse.
func (c *Client) RecentTracks(n int) ([]RecentTrack, error) {
	if n <= 0 {
		return nil, nil
	}
	list, err := call(c, func(conn *mpd.Client) ([]mpd.Attrs, error) {
		return conn.ListAllInfo("")
	})
	if err != nil {
		return nil, err
	}

	recent := make([]RecentTrack, 0, len(list))
	for _, a := range list {
		mod := ParseLastModified(a)
		if mod.IsZero() {
			continue
		}
		recent = append(recent, RecentTrack{Song: parseSong(a), LastModified: mod})
	}
	sort.SliceStable(recent, func(i, j int) bool {
		return recent[i].LastModified.After(recent[j].LastModified)
	})
	if len(recent) > n {
		recent = recent[:n]
	}
	return recent, nil
}
