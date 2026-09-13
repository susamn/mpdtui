package ui

import (
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"mpdtui/internal/metadata"
	"mpdtui/internal/mpdclient"
)

// TestLiveLoopTrackInfoNav is the regression test for the freeze that
// j/k used to cause: the card's key handler called Application.Draw
// synchronously from inside the input capture, re-entering the very loop
// that was dispatching the key, and the app locked up.
//
// Nothing in the unit tests could catch that, because they call
// globalInputCapture and Draw directly and never start the event loop.
// This drives the real one on a simulation screen instead, so input
// capture, Draw and the QueueUpdateDraw queue all run through the same
// plumbing as the live app -- which is the only place a deadlock of that
// shape can appear.
//
// Liveness is probed by round-tripping an empty update through the
// queue: it returns only once the loop has drained everything queued
// before it, so it is both a barrier and a "still alive?" check. A
// frozen loop never returns, and the test fails on the timeout naming
// the step that did it. Worth running under -race, which also covers
// scrollOffset/inspectSong being touched from the loop goroutine.
func TestLiveLoopTrackInfoNav(t *testing.T) {
	db, err := metadata.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	a := &App{tv: tview.NewApplication(), metaDB: db, playCountedSongID: -1}
	a.build()
	a.runAsync = func(work func() error, onSuccess func()) {
		go func() {
			if err := work(); err == nil {
				a.tv.QueueUpdateDraw(onSuccess)
			}
		}()
	}
	a.queue.songs = make([]mpdclient.Song, 40)
	for i := range a.queue.songs {
		a.queue.songs[i] = mpdclient.Song{ID: i + 1, File: "d/f.mp3", Title: "Title", Artist: "Artist"}
	}
	var tagIDs []int64
	for i := 0; i < 12; i++ {
		id, err := db.AddTag(fmt.Sprintf("tag%d", i))
		if err != nil {
			t.Fatal(err)
		}
		tagIDs = append(tagIDs, id)
	}
	if err := db.SetTags("d/f.mp3", tagIDs); err != nil {
		t.Fatal(err)
	}
	a.queue.render(-1)

	screen := tcell.NewSimulationScreen("UTF-8")
	if err := screen.Init(); err != nil {
		t.Fatal(err)
	}
	screen.SetSize(150, 40)
	a.tv.SetScreen(screen)

	runDone := make(chan error, 1)
	go func() { runDone <- a.tv.Run() }()

	// A round-trip through the update queue: it only returns once the
	// loop has drained everything queued before it, so it doubles as a
	// liveness probe and a barrier.
	sync := func(t *testing.T, stage string) {
		t.Helper()
		done := make(chan struct{})
		go func() { a.tv.QueueUpdateDraw(func() {}); close(done) }()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Fatalf("event loop froze: %s", stage)
		}
	}
	sync(t, "startup")

	a.tv.QueueUpdateDraw(func() { a.openTrackInfo() })
	sync(t, "open card")

	for _, step := range []struct {
		stage string
		key   tcell.Key
		r     rune
		n     int
	}{
		{"j collapsed (queue nav)", tcell.KeyRune, 'j', 15},
		{"Tab expand", tcell.KeyTab, 0, 1},
		{"j expanded (scroll)", tcell.KeyRune, 'j', 30},
		{"k expanded (scroll back past 0)", tcell.KeyRune, 'k', 50},
		{"Down expanded", tcell.KeyDown, 0, 10},
		{"Tab collapse", tcell.KeyTab, 0, 1},
		{"Up collapsed", tcell.KeyUp, 0, 20},
		{"i close", tcell.KeyRune, 'i', 1},
	} {
		for i := 0; i < step.n; i++ {
			screen.InjectKey(step.key, step.r, tcell.ModNone)
		}
		sync(t, step.stage)
	}

	a.tv.Stop()
	select {
	case <-runDone:
	case <-time.After(5 * time.Second):
		t.Fatal("Application.Run did not exit")
	}
}
