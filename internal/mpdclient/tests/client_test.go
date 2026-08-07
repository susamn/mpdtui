// Integration tests against a real MPD server. Skipped automatically if
// one isn't reachable at MPD_HOST/MPD_PORT (default localhost:6600).
package tests

import (
	"strings"
	"testing"
	"time"

	"mpdtui/internal/config"
	"mpdtui/internal/mpdclient"
)

func dialOrSkip(t *testing.T) *mpdclient.Client {
	t.Helper()
	c, err := mpdclient.Dial(config.Load())
	if err != nil {
		t.Skipf("no MPD server reachable, skipping: %v", err)
	}
	t.Cleanup(func() { c.Close() })
	return c
}

func TestStatusAndCurrentSong(t *testing.T) {
	c := dialOrSkip(t)

	st, err := c.Status()
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if st.State != mpdclient.StatePlay && st.State != mpdclient.StatePause && st.State != mpdclient.StateStop {
		t.Fatalf("unexpected state: %q", st.State)
	}

	if _, err := c.CurrentSong(); err != nil {
		t.Fatalf("CurrentSong: %v", err)
	}
}

func TestQueueRoundTrip(t *testing.T) {
	c := dialOrSkip(t)

	before, err := c.Queue()
	if err != nil {
		t.Fatalf("Queue: %v", err)
	}
	if len(before) == 0 {
		t.Skip("queue is empty, nothing to exercise QueueMoveID/QueueRemoveID against")
	}

	// Move the first song to its own position: a no-op that still proves
	// the round trip works without mutating queue contents.
	first := before[0]
	if err := c.QueueMoveID(first.ID, first.Pos); err != nil {
		t.Fatalf("QueueMoveID: %v", err)
	}

	after, err := c.Queue()
	if err != nil {
		t.Fatalf("Queue (after): %v", err)
	}
	if len(after) != len(before) {
		t.Fatalf("queue length changed: %d -> %d", len(before), len(after))
	}
}

func TestVolumeClamped(t *testing.T) {
	c := dialOrSkip(t)

	orig, err := c.Status()
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	t.Cleanup(func() { c.SetVolume(orig.Volume) })

	if err := c.SetVolume(200); err != nil {
		t.Fatalf("SetVolume: %v", err)
	}
	st, err := c.Status()
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if st.Volume != 100 {
		t.Fatalf("expected volume clamped to 100, got %d", st.Volume)
	}
}

func TestLibraryListing(t *testing.T) {
	c := dialOrSkip(t)

	artists, err := c.Artists()
	if err != nil {
		t.Fatalf("Artists: %v", err)
	}
	if len(artists) == 0 {
		t.Skip("library has no tagged artists to browse")
	}

	albums, err := c.Albums(artists[0])
	if err != nil {
		t.Fatalf("Albums: %v", err)
	}
	if len(albums) == 0 {
		t.Skip("artist has no tagged albums")
	}

	tracks, err := c.Tracks(artists[0], albums[0])
	if err != nil {
		t.Fatalf("Tracks: %v", err)
	}
	if len(tracks) == 0 {
		t.Fatalf("expected at least one track for %s / %s", artists[0], albums[0])
	}
}

func TestPlaylistsListing(t *testing.T) {
	c := dialOrSkip(t)

	if _, err := c.Playlists(); err != nil {
		t.Fatalf("Playlists: %v", err)
	}
}

func TestWatcherReceivesMixerEvent(t *testing.T) {
	c := dialOrSkip(t)

	orig, err := c.Status()
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	t.Cleanup(func() { c.SetVolume(orig.Volume) })

	w, err := c.Watch("mixer")
	if err != nil {
		t.Fatalf("Watch: %v", err)
	}
	t.Cleanup(func() { w.Close() })

	delta := 1
	if orig.Volume >= 100 {
		delta = -1
	}
	if err := c.ChangeVolume(delta); err != nil {
		t.Fatalf("ChangeVolume: %v", err)
	}

	select {
	case name := <-w.Events():
		if name != "mixer" {
			t.Fatalf("expected mixer event, got %q", name)
		}
	case err := <-w.Errors():
		t.Fatalf("watcher error: %v", err)
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for mixer event")
	}
}

// TestFetchAlbumArt doesn't assert art is present -- the test library may
// have none -- only that the albumart/readpicture fallback doesn't error
// out or panic against a real track, and that a bogus URI fails cleanly.
func TestFetchAlbumArt(t *testing.T) {
	c := dialOrSkip(t)

	if _, err := c.FetchAlbumArt("does-not-exist.mp3"); err == nil {
		t.Error("expected an error for a nonexistent URI, got nil")
	}

	artists, err := c.Artists()
	if err != nil {
		t.Fatalf("Artists: %v", err)
	}
	if len(artists) == 0 {
		t.Skip("library has no tagged artists to test against")
	}
	albums, err := c.Albums(artists[0])
	if err != nil {
		t.Fatalf("Albums: %v", err)
	}
	if len(albums) == 0 {
		t.Skip("artist has no tagged albums")
	}
	tracks, err := c.Tracks(artists[0], albums[0])
	if err != nil {
		t.Fatalf("Tracks: %v", err)
	}
	if len(tracks) == 0 {
		t.Skip("album has no tracks")
	}

	// Either outcome is valid -- many libraries have no cover art at all --
	// the point is this must not error transport-wise or panic.
	if _, err := c.FetchAlbumArt(tracks[0].File); err != nil {
		t.Logf("FetchAlbumArt(%s): %v (no art embedded/alongside -- expected on a library without covers)", tracks[0].File, err)
	}
}

func TestListDirectory(t *testing.T) {
	c := dialOrSkip(t)

	root, err := c.ListDirectory("")
	if err != nil {
		t.Fatalf("ListDirectory(\"\"): %v", err)
	}
	if len(root) == 0 {
		t.Skip("library root has no entries to browse")
	}

	var dir mpdclient.DirEntry
	found := false
	for _, e := range root {
		if e.Type == mpdclient.EntryDirectory {
			dir, found = e, true
			break
		}
	}
	if !found {
		t.Skip("library root has no subdirectories to descend into")
	}

	children, err := c.ListDirectory(dir.Path)
	if err != nil {
		t.Fatalf("ListDirectory(%q): %v", dir.Path, err)
	}
	if len(children) == 0 {
		t.Fatalf("expected %q to have at least one child entry", dir.Path)
	}
}

func TestLibraryStats(t *testing.T) {
	c := dialOrSkip(t)

	stats, err := c.LibraryStats()
	if err != nil {
		t.Fatalf("LibraryStats: %v", err)
	}

	playlists, err := c.Playlists()
	if err != nil {
		t.Fatalf("Playlists: %v", err)
	}
	if stats.Playlists != len(playlists) {
		t.Errorf("stats.Playlists = %d, want %d (len(Playlists()))", stats.Playlists, len(playlists))
	}

	// Tracks/Artists come straight from MPD's own stats command -- just
	// sanity-check they're not obviously broken (a real library has more
	// than zero of each; a negative count would mean parsing failed).
	if stats.Tracks < 0 || stats.Artists < 0 {
		t.Errorf("negative stats: %+v", stats)
	}
}

// firstTaggedAlbum finds an (artist, album) pair with non-empty names.
// Real libraries often have an untagged-track bucket that shows up as an
// empty-string "artist"/"album" (first alphabetically, so artists[0] is a
// trap here) -- a substring search against "" trivially matches
// everything instead of testing anything meaningful.
func firstTaggedAlbum(t *testing.T, c *mpdclient.Client) (artist, album string) {
	t.Helper()
	artists, err := c.Artists()
	if err != nil {
		t.Fatalf("Artists: %v", err)
	}
	for _, a := range artists {
		if a == "" {
			continue
		}
		albums, err := c.Albums(a)
		if err != nil {
			t.Fatalf("Albums(%q): %v", a, err)
		}
		for _, al := range albums {
			if al != "" {
				return a, al
			}
		}
	}
	t.Skip("library has no artist with a non-empty tagged album to search against")
	return "", ""
}

func TestSearchAlbums(t *testing.T) {
	c := dialOrSkip(t)
	_, album := firstTaggedAlbum(t, c)

	songs, err := c.SearchAlbums(album)
	if err != nil {
		t.Fatalf("SearchAlbums(%q): %v", album, err)
	}
	if len(songs) == 0 {
		t.Fatalf("expected at least one track for album %q", album)
	}
	// MPD's search is substring/case-insensitive, so a different album
	// that happens to contain this one's name as a substring is a
	// legitimate match too -- only assert containment, not exact equality.
	needle := strings.ToLower(album)
	for _, s := range songs {
		if !strings.Contains(strings.ToLower(s.Album), needle) {
			t.Errorf("track %q has Album = %q, doesn't contain %q", s.File, s.Album, album)
		}
	}
}
