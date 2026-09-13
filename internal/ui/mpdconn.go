package ui

import (
	"time"

	"mpdtui/internal/mpdclient"
)

// mpdConn is everything internal/ui asks of the MPD server, as an
// interface rather than the concrete *mpdclient.Client.
//
// The point is testability. Nearly every command in this package ends in
// a client call, and while an MPD server is easy enough to reach in a
// test, the interesting commands are the destructive ones: proving that
// 'd' removes the selected track, that 'c' clears the queue, or that 's'
// stops playback should not require doing those things to whatever the
// developer happens to be listening to. Before this existed, the suite
// covered exactly one live command -- togglePlayPause, toggled straight
// back afterwards -- and left the rest untested.
//
// Run still takes a *mpdclient.Client; it satisfies this interface, and
// nothing outside this package needs to know the field is narrowed.
type mpdConn interface {
	// Playback and transport.
	Status() (mpdclient.Status, error)
	CurrentSong() (mpdclient.Song, error)
	TogglePlayPause() error
	Stop() error
	Next() error
	Previous() error
	PlayID(id int) error
	SeekCur(d time.Duration, relative bool) error
	SeekSongID(id int, d time.Duration) error
	ChangeVolume(delta int) error

	// Playback options.
	SetRandom(on bool) error
	SetRepeat(on bool) error
	SetSingle(on bool) error
	SetConsume(on bool) error

	// The queue.
	Queue() ([]mpdclient.Song, error)
	QueueAdd(uri string) error
	QueueAddID(uri string) (int, error)
	QueueClear() error
	QueueMoveID(id, pos int) error
	QueueRemoveID(id int) error
	SaveQueueAsPlaylist(name string) error

	// Stored playlists.
	Playlists() ([]mpdclient.Playlist, error)
	PlaylistIndex() (mpdclient.PlaylistIndex, error)
	PlaylistLoad(name string) error
	PlaylistAppend(name string) error
	PlaylistDelete(name string) error
	AddTrackToPlaylist(name, uri string) error

	// The library.
	Artists() ([]string, error)
	Albums(artist string) ([]string, error)
	AlbumArtists() (map[string]string, error)
	AllSongs() ([]mpdclient.Song, error)
	ListDirectory(path string) ([]mpdclient.DirEntry, error)
	LibraryStats() (mpdclient.LibraryStats, error)

	// Album art, for internal/albumart (which declares its own
	// one-method Fetcher; this satisfies it).
	FetchAlbumArt(uri string) ([]byte, error)

	// Change notification.
	Watch(subsystems ...string) (*mpdclient.Watcher, error)
}

// Compile-time proof that the real client still satisfies the interface,
// so adding a method here fails the build rather than at a call site.
var _ mpdConn = (*mpdclient.Client)(nil)
