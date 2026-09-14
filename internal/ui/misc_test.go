package ui

import (
	"errors"
	"strings"
	"testing"
	"time"

	"mpdtui/internal/mpdclient"
)

// TestFlashRevertsToTheHintBar covers the transient-message timer: a
// flash replaces the hint bar and then puts it back, so feedback does
// not sit there forever pretending to be the key hints.
func TestFlashRevertsToTheHintBar(t *testing.T) {
	a := newFakeApp(t, &fakeMPD{})
	a.updateHintBar()
	hints := a.hintBar.GetText(true)

	a.flash("[red]something happened[-]")
	if got := a.hintBar.GetText(true); !strings.Contains(got, "something happened") {
		t.Fatalf("hint bar = %q, want the flashed message", got)
	}

	waitForUpTo(t, 5*time.Second, func() bool { return a.hintBar.GetText(true) == hints })
}

// TestFlashSupersedesAnEarlierOne is what msgSeq exists for: a second
// flash while the first is still up must not have the first one's timer
// wipe it early.
func TestFlashSupersedesAnEarlierOne(t *testing.T) {
	a := newFakeApp(t, &fakeMPD{})

	a.flash("first")
	first := a.msgSeq
	a.flash("second")

	if a.msgSeq == first {
		t.Error("the second flash did not take a new sequence number")
	}
	if got := a.hintBar.GetText(true); !strings.Contains(got, "second") {
		t.Errorf("hint bar = %q, want the newer message", got)
	}

	// The sequence number is the mechanism: the older timer compares
	// against it when it fires and leaves the newer message alone.
}

// TestRunAsyncDefaultAppliesOnSuccessAndReportsFailure covers the real
// background-work helper, as opposed to the synchronous stand-in the
// rest of these tests use.
func TestRunAsyncDefaultAppliesOnSuccessAndReportsFailure(t *testing.T) {
	a := newFakeApp(t, &fakeMPD{})

	t.Run("success", func(t *testing.T) {
		applied := make(chan struct{})
		a.runAsyncDefault(func() error { return nil }, func() { close(applied) })
		select {
		case <-applied:
		case <-time.After(time.Second):
			t.Fatal("onSuccess never ran")
		}
	})

	t.Run("failure", func(t *testing.T) {
		ran := false
		a.runAsyncDefault(func() error { return errTest }, func() { ran = true })
		waitFor(t, func() bool { return strings.Contains(a.hintBar.GetText(true), errTest.Error()) })
		if ran {
			t.Error("onSuccess ran despite the work failing")
		}
	})
}

// TestReloadTrackMeta covers the refresh after a metadata write: the
// Queue row and the Track Info card both have to pick up the new value.
func TestReloadTrackMeta(t *testing.T) {
	f := &fakeMPD{}
	a := newFakeAppWithMetaDB(t, f)
	seedQueue(a, f, mpdclient.Song{ID: 1, Pos: 0, Title: "Track", File: "a/b.mp3"})

	if err := a.metaDB.Rate("a/b.mp3", 5); err != nil {
		t.Fatalf("Rate: %v", err)
	}
	a.reloadTrackMeta("a/b.mp3")

	meta, ok := a.queue.metaCache["a/b.mp3"]
	if !ok {
		t.Fatal("the Queue has no cached metadata for the track after reloading")
	}
	if meta.Rating != 5 {
		t.Errorf("cached rating = %d, want 5", meta.Rating)
	}
}

// TestReloadTrackMetaWithoutMetadataIsANoOp covers the gate: with the
// feature off there is nothing to read, and it must not reach for a nil
// database.
func TestReloadTrackMetaWithoutMetadataIsANoOp(t *testing.T) {
	a := newFakeApp(t, &fakeMPD{}) // no metaDB
	a.reloadTrackMeta("a/b.mp3")   // must not panic
}

// --- 'A' add-all-results ------------------------------------------------

func TestAddAllResultsQueuesEverySearchResult(t *testing.T) {
	f := &fakeMPD{songs: []mpdclient.Song{
		{Title: "One", Artist: "Rahman", File: "a/1.mp3"},
		{Title: "Two", Artist: "Rahman", File: "a/2.mp3"},
		{Title: "Other", Artist: "Someone", File: "b/1.mp3"},
	}}
	a := newFakeApp(t, f)
	if n := a.library.showSearch("rahman"); n != 2 {
		t.Fatalf("setup: search matched %d tracks, want 2", n)
	}
	a.tv.SetFocus(a.library.tree)

	a.handleAddAll()

	if !f.did("queueadd:a/1.mp3") || !f.did("queueadd:a/2.mp3") {
		t.Errorf("did %v, want every search result queued", f.commands())
	}
	if a.tv.GetFocus() != a.queue.table {
		t.Errorf("focus = %T, want it moved to the Queue after adding", a.tv.GetFocus())
	}
}

// TestAddAllStopsAtTheFirstFailure covers the error arm: the count
// reported is what actually landed, not what was attempted.
func TestAddAllStopsAtTheFirstFailure(t *testing.T) {
	f := &fakeMPD{
		songs: []mpdclient.Song{
			{Title: "One", Artist: "Rahman", File: "a/1.mp3"},
			{Title: "Two", Artist: "Rahman", File: "a/2.mp3"},
		},
	}
	a := newFakeApp(t, f)
	if n := a.library.showSearch("rahman"); n != 2 {
		t.Fatalf("setup: search matched %d tracks, want 2", n)
	}
	a.tv.SetFocus(a.library.tree)
	f.err = errTest // start failing only now that the results are built

	added, err := a.library.addAllResults()

	if !errors.Is(err, errTest) {
		t.Errorf("err = %v, want the client failure", err)
	}
	if added != 0 {
		t.Errorf("reported %d added, want 0 -- the first add already failed", added)
	}
}

// TestAddAllRequiresASearchFirst covers the guard: in browse mode there
// is no result set, so 'A' explains itself rather than silently doing
// nothing.
func TestAddAllRequiresASearchFirst(t *testing.T) {
	a := newFakeApp(t, &fakeMPD{})
	a.tv.SetFocus(a.library.tree) // browse mode, not search

	a.handleAddAll()

	if got := a.hintBar.GetText(true); !strings.Contains(got, "search first") {
		t.Errorf("hint bar = %q, want it to say to search first", got)
	}
}

func TestAddAllOnlyFromTheLibrary(t *testing.T) {
	a := newFakeApp(t, &fakeMPD{})
	a.tv.SetFocus(a.queue.table)

	a.handleAddAll()

	if got := a.hintBar.GetText(true); !strings.Contains(got, "A") {
		t.Errorf("hint bar = %q, want an invalid-key flash", got)
	}
}

// TestAddAllWithNoResultsSaysSo covers a search that matched nothing.
func TestAddAllWithNoResultsSaysSo(t *testing.T) {
	a := newFakeApp(t, &fakeMPD{})
	a.library.showSearch("matches nothing at all")
	a.tv.SetFocus(a.library.tree)

	a.handleAddAll()

	if got := a.hintBar.GetText(true); !strings.Contains(got, "nothing to add") {
		t.Errorf("hint bar = %q, want it to report nothing to add", got)
	}
}

// --- 'R' refresh playlist counts ---------------------------------------

func TestRefreshCountsKeyOnlyFromThePlaylistsPanel(t *testing.T) {
	f := &fakeMPD{plIndex: mpdclient.PlaylistIndex{Counts: map[string]int{"A": 1}}}
	a := newFakeApp(t, f)

	a.tv.SetFocus(a.queue.table)
	a.handleRefreshPlaylistCounts()
	if got := a.hintBar.GetText(true); !strings.Contains(got, "R") {
		t.Errorf("hint bar = %q, want an invalid-key flash from the wrong panel", got)
	}

	a.tv.SetFocus(a.playlists.table)
	a.handleRefreshPlaylistCounts()
	waitFor(t, func() bool { return strings.Contains(a.hintBar.GetText(true), "refreshed") })
}

// --- loadPlaylist -------------------------------------------------------

func TestLoadPlaylistReportsFailure(t *testing.T) {
	f := &fakeMPD{err: errTest}
	a := newFakeApp(t, f)

	a.loadPlaylist("Road Trip")

	if got := a.hintBar.GetText(true); !strings.Contains(got, errTest.Error()) {
		t.Errorf("hint bar = %q, want the failure reported", got)
	}
}
