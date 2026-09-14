package ui

import (
	"strings"
	"testing"
	"time"

	"mpdtui/internal/mpdclient"
)

// The Track Info card's "Playlists" section reads App.playlistMembership,
// which used to be refreshed only at startup, on a ten-minute ticker, or
// by pressing 'R' on the Playlists panel. Adding a track to a playlist
// and pressing 'i' therefore showed the old answer for up to ten
// minutes. These cover both halves of the fix: the immediate local
// record of our own edit, and the rescan the stored_playlist event
// schedules for edits made anywhere else.

func TestNotePlaylistMembershipAddsSortedAndDeduplicated(t *testing.T) {
	a := newFakeApp(t, &fakeMPD{})
	const file = "a/b.mp3"

	a.notePlaylistMembership(file, "Road Trip")
	a.notePlaylistMembership(file, "Chill")
	a.notePlaylistMembership(file, "Road Trip") // already there

	got := a.playlistMembership[file]
	want := []string{"Chill", "Road Trip"}
	if len(got) != len(want) {
		t.Fatalf("membership = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("membership = %v, want %v (sorted, as PlaylistIndex returns)", got, want)
		}
	}
}

func TestNotePlaylistMembershipIgnoresEmptyInput(t *testing.T) {
	a := newFakeApp(t, &fakeMPD{})
	a.notePlaylistMembership("", "Road Trip")
	a.notePlaylistMembership("a/b.mp3", "")
	if len(a.playlistMembership) != 0 {
		t.Errorf("membership = %v, want nothing recorded", a.playlistMembership)
	}
}

// TestAddingToAPlaylistShowsInTrackInfoImmediately is the reported bug,
// end to end: add the playing track to a playlist, press 'i', see it.
func TestAddingToAPlaylistShowsInTrackInfoImmediately(t *testing.T) {
	f := &fakeMPD{}
	a := newFakeApp(t, f)
	song := mpdclient.Song{ID: 1, Pos: 0, Title: "Ay Hairathe", Artist: "Hariharan", File: "a/b.mp3"}
	seedQueue(a, f, song)
	setPlaylistsForTest(a.playlists, []string{"Road Trip", "Chill"})
	a.tv.SetFocus(a.queue.table)
	a.queue.table.Select(queueHeaderRows, 0)

	// Add it to a playlist through the real picker.
	a.openAddToPlaylistPicker()
	field := openInputFieldOf(t, a)
	field.SetText("chill")
	sendKey(field, enterKey(), a)

	if !f.did("pladd:Chill:a/b.mp3") {
		t.Fatalf("did %v, want the track added to the playlist", f.commands())
	}

	// Now press 'i' -- no rescan has had a chance to run.
	a.tv.SetFocus(a.queue.table)
	a.queue.table.Select(queueHeaderRows, 0)
	a.openTrackInfo()

	if got := trackInfoPlaylists(a); !strings.Contains(got, "Chill") {
		t.Errorf("Track Info card's Playlists section = %q, want it to list the playlist just added to", got)
	}
}

// TestStoredPlaylistEventRefreshesMembership covers an edit made
// somewhere else entirely -- another client, or mpc -- which is the only
// thing that can update membership for a track this instance did not
// touch.
func TestStoredPlaylistEventRefreshesMembership(t *testing.T) {
	f := &fakeMPD{
		pls: []mpdclient.Playlist{{Name: "Road Trip"}},
		plIndex: mpdclient.PlaylistIndex{
			Counts:     map[string]int{"Road Trip": 1},
			Membership: map[string][]string{"a/b.mp3": {"Road Trip"}},
		},
	}
	a := newFakeApp(t, f)

	if len(a.playlistMembership) != 0 {
		t.Fatal("setup: membership should start empty")
	}

	a.handleSubsystem("stored_playlist")

	// Debounced: nothing yet.
	if len(a.playlistMembership) != 0 {
		t.Error("the rescan ran immediately, want it debounced")
	}

	waitForUpTo(t, 5*time.Second, func() bool {
		return len(a.playlistMembership["a/b.mp3"]) == 1
	})
	if got := a.playlistMembership["a/b.mp3"]; len(got) != 1 || got[0] != "Road Trip" {
		t.Errorf("membership after the event = %v, want [Road Trip]", got)
	}
}

// TestStoredPlaylistBurstCoalescesIntoOneRescan is why the debounce
// exists: the rescan is one round-trip per stored playlist and holds the
// client's connection mutex throughout, so a burst of edits must not
// mean a burst of scans.
func TestStoredPlaylistBurstCoalescesIntoOneRescan(t *testing.T) {
	f := &fakeMPD{
		plIndex: mpdclient.PlaylistIndex{Counts: map[string]int{"A": 1}},
	}
	a := newFakeApp(t, f)

	for i := 0; i < 12; i++ {
		a.handleSubsystem("stored_playlist")
	}

	waitForUpTo(t, 5*time.Second, func() bool { return a.playlists.trackCounts != nil })
	// Let any further timers fire.
	time.Sleep(playlistRefreshDebounce + 300*time.Millisecond)

	if got := f.playlistIndexCalls(); got != 1 {
		t.Errorf("twelve events caused %d rescans, want 1", got)
	}
}
