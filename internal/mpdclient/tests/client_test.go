// Integration tests against a real MPD server. Skipped automatically if
// one isn't reachable at MPD_HOST/MPD_PORT (default localhost:6600).
package tests

import (
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
