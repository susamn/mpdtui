package ui

import (
	"fmt"
	"sync"
	"time"

	"mpdtui/internal/mpdclient"
)

// fakeMPD is an in-memory stand-in for the MPD server, satisfying
// mpdConn. It records every command it was asked to perform so a test
// can assert on what a keypress actually did, without needing a live
// server and -- more to the point -- without stopping, skipping or
// clearing whatever the developer is actually listening to.
//
// Zero value is usable: an idle server with an empty queue and library.
type fakeMPD struct {
	mu sync.Mutex

	// calls records every mutating command in order, as a short string
	// ("stop", "volume+5", "seek+5s"), which is what most assertions
	// here want to look at.
	calls []string

	status       mpdclient.Status
	song         mpdclient.Song
	queue        []mpdclient.Song
	pls          []mpdclient.Playlist
	plIndex      mpdclient.PlaylistIndex
	plIndexCalls int
	artists      []string
	albums       map[string][]string
	songs        []mpdclient.Song
	dirs         map[string][]mpdclient.DirEntry
	stats        mpdclient.LibraryStats
	files        []string
	recent       []mpdclient.RecentTrack
	fallouts     mpdclient.PlaylistFalloutReport
	art          []byte

	// err, when set, is returned by every command that can fail, for
	// exercising the error paths.
	err error
}

func (f *fakeMPD) record(format string, args ...any) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, fmt.Sprintf(format, args...))
}

// did reports whether the named command was issued.
func (f *fakeMPD) did(call string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, c := range f.calls {
		if c == call {
			return true
		}
	}
	return false
}

func (f *fakeMPD) commands() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.calls...)
}

func (f *fakeMPD) Status() (mpdclient.Status, error)        { return f.status, f.err }
func (f *fakeMPD) CurrentSong() (mpdclient.Song, error)     { return f.song, f.err }
func (f *fakeMPD) TogglePlayPause() error                   { f.record("toggle"); return f.err }
func (f *fakeMPD) Stop() error                              { f.record("stop"); return f.err }
func (f *fakeMPD) Next() error                              { f.record("next"); return f.err }
func (f *fakeMPD) Previous() error                          { f.record("previous"); return f.err }
func (f *fakeMPD) PlayID(id int) error                      { f.record("play:%d", id); return f.err }
func (f *fakeMPD) ChangeVolume(delta int) error             { f.record("volume%+d", delta); return f.err }
func (f *fakeMPD) SetRandom(on bool) error                  { f.record("random:%v", on); return f.err }
func (f *fakeMPD) SetRepeat(on bool) error                  { f.record("repeat:%v", on); return f.err }
func (f *fakeMPD) SetSingle(on bool) error                  { f.record("single:%v", on); return f.err }
func (f *fakeMPD) SetConsume(on bool) error                 { f.record("consume:%v", on); return f.err }
func (f *fakeMPD) QueueAdd(uri string) error                { f.record("queueadd:%s", uri); return f.err }
func (f *fakeMPD) QueueClear() error                        { f.record("queueclear"); return f.err }
func (f *fakeMPD) QueueMoveID(id, pos int) error            { f.record("move:%d->%d", id, pos); return f.err }
func (f *fakeMPD) QueueRemoveID(id int) error               { f.record("remove:%d", id); return f.err }
func (f *fakeMPD) PlaylistLoad(name string) error           { f.record("plload:%s", name); return f.err }
func (f *fakeMPD) PlaylistAppend(name string) error         { f.record("plappend:%s", name); return f.err }
func (f *fakeMPD) PlaylistDelete(name string) error         { f.record("pldelete:%s", name); return f.err }
func (f *fakeMPD) SaveQueueAsPlaylist(name string) error    { f.record("plsave:%s", name); return f.err }
func (f *fakeMPD) FetchAlbumArt(uri string) ([]byte, error) { return f.art, f.err }

func (f *fakeMPD) SeekCur(d time.Duration, relative bool) error {
	f.record("seek%+ds:rel=%v", int(d.Seconds()), relative)
	return f.err
}

func (f *fakeMPD) SeekSongID(id int, d time.Duration) error {
	f.record("seekid:%d:%ds", id, int(d.Seconds()))
	return f.err
}

func (f *fakeMPD) QueueAddID(uri string) (int, error) {
	f.record("queueaddid:%s", uri)
	if f.err != nil {
		return 0, f.err
	}
	return 1, nil
}

func (f *fakeMPD) AddTrackToPlaylist(name, uri string) error {
	f.record("pladd:%s:%s", name, uri)
	return f.err
}

func (f *fakeMPD) Queue() ([]mpdclient.Song, error) { return f.queue, f.err }

func (f *fakeMPD) Playlists() ([]mpdclient.Playlist, error) { return f.pls, f.err }

func (f *fakeMPD) PlaylistIndex() (mpdclient.PlaylistIndex, error) {
	f.mu.Lock()
	f.plIndexCalls++
	f.mu.Unlock()
	return f.plIndex, f.err
}

// playlistIndexCalls is how many full rescans were asked for.
func (f *fakeMPD) playlistIndexCalls() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.plIndexCalls
}

func (f *fakeMPD) Artists() ([]string, error) { return f.artists, f.err }

func (f *fakeMPD) Albums(artist string) ([]string, error) { return f.albums[artist], f.err }

func (f *fakeMPD) AlbumArtists() (map[string]string, error) { return nil, f.err }

func (f *fakeMPD) AllSongs() ([]mpdclient.Song, error) { return f.songs, f.err }

func (f *fakeMPD) ListDirectory(path string) ([]mpdclient.DirEntry, error) {
	return f.dirs[path], f.err
}

func (f *fakeMPD) LibraryStats() (mpdclient.LibraryStats, error) { return f.stats, f.err }

func (f *fakeMPD) LibraryFiles() ([]string, error) { return f.files, f.err }

func (f *fakeMPD) RecentTracks(n int) ([]mpdclient.RecentTrack, error) {
	f.record("RecentTracks(%d)", n)
	if f.err != nil {
		return nil, f.err
	}
	if n < len(f.recent) {
		return f.recent[:n], nil
	}
	return f.recent, nil
}

func (f *fakeMPD) PlaylistFallouts() (mpdclient.PlaylistFalloutReport, error) {
	return f.fallouts, f.err
}

func (f *fakeMPD) Watch(subsystems ...string) (*mpdclient.Watcher, error) { return nil, f.err }

// Compile-time proof the fake stays in step with the interface.
var _ mpdConn = (*fakeMPD)(nil)
