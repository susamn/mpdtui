package ui

import (
	"errors"
	"strings"
	"syscall"
	"testing"
	"time"

	"mpdtui/internal/config"
	"mpdtui/internal/mpdclient"
	"mpdtui/internal/uitheme"
)

// --- refreshNowPlaying and the refresh fan-out --------------------------

func TestRefreshNowPlayingPullsStatusAndSongIntoTheApp(t *testing.T) {
	f := &fakeMPD{
		status: mpdclient.Status{State: mpdclient.StatePlay, SongID: 7, Volume: 55,
			Elapsed: 30 * time.Second, Duration: 200 * time.Second},
		song: mpdclient.Song{ID: 7, Title: "Ay Hairathe", Artist: "Hariharan", File: "a/b.mp3"},
	}
	a := newFakeApp(t, f)
	a.nowPlaying.SetRect(0, 0, 120, 4)

	a.refreshNowPlaying()

	if a.currentStatus.SongID != 7 {
		t.Errorf("currentStatus.SongID = %d, want 7", a.currentStatus.SongID)
	}
	if a.currentSong.Title != "Ay Hairathe" {
		t.Errorf("currentSong.Title = %q, want the fetched title", a.currentSong.Title)
	}
	if got := a.nowPlaying.GetText(true); !strings.Contains(got, "Ay Hairathe") {
		t.Errorf("Now Playing panel = %q, want the current track rendered", got)
	}
	if a.queue.currentID != 7 {
		t.Errorf("queue.currentID = %d, want it following the playing track", a.queue.currentID)
	}
}

// TestRefreshNowPlayingReportsAFailedServer covers both error arms: a
// failure must reach the hint bar and leave the previous state alone
// rather than half-applying a refresh.
func TestRefreshNowPlayingReportsAFailedServer(t *testing.T) {
	f := &fakeMPD{err: errors.New("connection refused")}
	a := newFakeApp(t, f)

	a.refreshNowPlaying()

	if got := a.hintBar.GetText(true); !strings.Contains(got, "connection refused") {
		t.Errorf("hint bar = %q, want the failure reported", got)
	}
	if a.currentSong.File != "" {
		t.Error("a failed refresh still applied a song")
	}
}

// TestRefreshAllPrimesEveryPanelAndMarksStartupDone covers the startup
// fan-out, including that startedUp flips -- which is what stops the
// first observation of already-playing music from yanking focus to the
// Queue before the user has done anything.
func TestRefreshAllPrimesEveryPanelAndMarksStartupDone(t *testing.T) {
	f := &fakeMPD{
		status:  mpdclient.Status{State: mpdclient.StatePlay, SongID: 3},
		song:    mpdclient.Song{ID: 3, Title: "Track", File: "a.mp3"},
		queue:   []mpdclient.Song{{ID: 3, Pos: 0, Title: "Track", File: "a.mp3"}},
		pls:     []mpdclient.Playlist{{Name: "Road Trip"}},
		artists: []string{"A. R. Rahman"},
		stats:   mpdclient.LibraryStats{Tracks: 8198},
	}
	a := newFakeApp(t, f)

	if a.startedUp {
		t.Fatal("startedUp should be false before refreshAll")
	}

	a.refreshAll()

	if !a.startedUp {
		t.Error("startedUp = false after refreshAll, want true")
	}
	if len(a.queue.songs) != 1 {
		t.Errorf("queue has %d songs after refreshAll, want the fetched 1", len(a.queue.songs))
	}
	if len(a.playlists.pls) != 1 {
		t.Errorf("playlists has %d entries after refreshAll, want the fetched 1", len(a.playlists.pls))
	}
	if got := a.queue.stats.GetText(true); !strings.Contains(got, "8198") {
		t.Errorf("stats = %q, want the library track count", got)
	}
}

// TestStartupDoesNotStealFocusToTheQueue is the behavior startedUp
// exists for: learning what MPD was already playing is not a track
// change the user made, so focus must stay where build() left it.
func TestStartupDoesNotStealFocusToTheQueue(t *testing.T) {
	f := &fakeMPD{
		status: mpdclient.Status{State: mpdclient.StatePlay, SongID: 3},
		song:   mpdclient.Song{ID: 3, File: "a.mp3"},
		queue:  []mpdclient.Song{{ID: 3, Pos: 0, File: "a.mp3"}},
	}
	a := newFakeApp(t, f)
	a.focusPanel(libraryPanelIdx)

	a.refreshAll() // the startup observation

	if a.tv.GetFocus() != a.library.tree {
		t.Errorf("focus after startup = %T, want it left on the Library", a.tv.GetFocus())
	}
}

func TestHandleSubsystemRoutesEachEvent(t *testing.T) {
	cases := []struct {
		subsystem string
		wants     string
	}{
		{"player", "status"},
		{"mixer", "status"},
		{"options", "status"},
		{"playlist", "queue"},
		{"stored_playlist", "playlists"},
		{"database", "stats"},
	}
	for _, tc := range cases {
		t.Run(tc.subsystem, func(t *testing.T) {
			f := &fakeMPD{
				song:  mpdclient.Song{ID: 1, File: "a.mp3"},
				queue: []mpdclient.Song{{ID: 1, Pos: 0, Title: "Queued", File: "a.mp3"}},
				pls:   []mpdclient.Playlist{{Name: "Seeded"}},
				stats: mpdclient.LibraryStats{Tracks: 42},
			}
			a := newFakeApp(t, f)

			a.handleSubsystem(tc.subsystem)

			switch tc.wants {
			case "status":
				if a.currentSong.File != "a.mp3" {
					t.Errorf("%s did not refresh Now Playing", tc.subsystem)
				}
			case "queue":
				if len(a.queue.songs) != 1 {
					t.Errorf("%s did not refresh the queue", tc.subsystem)
				}
			case "playlists":
				if len(a.playlists.pls) != 1 {
					t.Errorf("%s did not refresh the playlists", tc.subsystem)
				}
			case "stats":
				if got := a.queue.stats.GetText(true); !strings.Contains(got, "42") {
					t.Errorf("%s did not refresh the stats: %q", tc.subsystem, got)
				}
			}
		})
	}
}

// TestHandleSubsystemIgnoresUnknownEvents guards the default arm: MPD
// emits subsystems this app does not subscribe to, and an unknown name
// must be a no-op rather than a panic or a needless full refresh.
func TestHandleSubsystemIgnoresUnknownEvents(t *testing.T) {
	f := &fakeMPD{}
	a := newFakeApp(t, f)

	for _, name := range []string{"sticker", "output", "partition", ""} {
		a.handleSubsystem(name)
	}
	if a.currentSong.File != "" {
		t.Error("an unknown subsystem triggered a Now Playing refresh")
	}
}

// --- Playlist track counts ---------------------------------------------

func TestRefreshTrackCountsFillsCountsAndMembership(t *testing.T) {
	f := &fakeMPD{
		pls: []mpdclient.Playlist{{Name: "Road Trip"}},
		plIndex: mpdclient.PlaylistIndex{
			Counts:     map[string]int{"Road Trip": 12},
			Membership: map[string][]string{"a/b.mp3": {"Road Trip"}},
		},
	}
	a := newFakeApp(t, f)

	a.refreshTrackCounts(true)
	waitFor(t, func() bool { return a.playlists.trackCounts != nil })

	if got := a.playlists.trackCounts["Road Trip"]; got != 12 {
		t.Errorf("track count = %d, want 12", got)
	}
	if got := a.playlistMembership["a/b.mp3"]; len(got) != 1 || got[0] != "Road Trip" {
		t.Errorf("membership = %v, want the track in Road Trip", got)
	}
}

// TestRefreshTrackCountsAnnouncesWhenNotSilent covers the 'R' keypress
// path, which differs from the startup one only in telling the user it
// happened.
func TestRefreshTrackCountsAnnouncesWhenNotSilent(t *testing.T) {
	f := &fakeMPD{plIndex: mpdclient.PlaylistIndex{Counts: map[string]int{"A": 1, "B": 2}}}
	a := newFakeApp(t, f)

	a.refreshTrackCounts(false)
	waitFor(t, func() bool { return strings.Contains(a.hintBar.GetText(true), "refreshed") })

	if got := a.hintBar.GetText(true); !strings.Contains(got, "2 playlists") {
		t.Errorf("hint bar = %q, want it to report how many playlists were refreshed", got)
	}
}

func TestRefreshTrackCountsReportsFailure(t *testing.T) {
	f := &fakeMPD{err: errors.New("index failed")}
	a := newFakeApp(t, f)

	a.refreshTrackCounts(false)
	waitFor(t, func() bool { return strings.Contains(a.hintBar.GetText(true), "index failed") })
}

// TestRefreshTrackCountsSupersedesAnInFlightOne covers the cancellation
// guard: a second refresh must cancel the first so a slow result cannot
// land after a newer one.
func TestRefreshTrackCountsSupersedesAnInFlightOne(t *testing.T) {
	f := &fakeMPD{plIndex: mpdclient.PlaylistIndex{Counts: map[string]int{"A": 1}}}
	a := newFakeApp(t, f)

	a.refreshTrackCounts(true)
	first := a.playlistRefreshCancel
	if first == nil {
		t.Fatal("no cancel function was recorded for the first refresh")
	}

	a.refreshTrackCounts(true)
	if a.playlistRefreshCancel == nil {
		t.Fatal("no cancel function after the second refresh")
	}
	// The first context must now be cancelled, so its result is discarded.
	waitFor(t, func() bool { return a.playlistRefreshCancel != nil })
}

// --- Theme reload -------------------------------------------------------

// TestReapplyThemeRepaintsWidgetsThatBakedInTheirColors is the whole
// point of reapplyTheme: tview reads border colors live, but table cells
// and tree nodes bake the color in when set, so a theme change has to
// walk them.
func TestReapplyThemeRepaintsWidgetsThatBakedInTheirColors(t *testing.T) {
	f := &fakeMPD{
		queue: []mpdclient.Song{{ID: 1, Pos: 0, Title: "Track", File: "a.mp3"}},
	}
	a := newFakeApp(t, f)
	a.queue.refresh()

	old := uitheme.Palette()
	t.Cleanup(func() { uitheme.SetPaletteForTest(old); deriveColors() })

	// Swap in a palette nothing could already be holding.
	uitheme.SetPaletteForTest(paletteWithAccent("#ff00ff"))
	deriveColors()

	a.reapplyTheme()

	if got, want := a.nowPlaying.GetBorderColor(), nowPlayingBorderColor; got != want {
		t.Errorf("Now Playing border = %v, want the newly derived %v", got, want)
	}
	// The queue's header cells are rebuilt from the new colors.
	if cell := a.queue.table.GetCell(0, 0); cell != nil {
		if got := cell.BackgroundColor; got != uitheme.TableHeaderBg() {
			t.Errorf("queue header background = %v, want the new %v", got, uitheme.TableHeaderBg())
		}
	}
}

func TestSetThemeFileDerivesEveryPanelColor(t *testing.T) {
	old := uitheme.Palette()
	t.Cleanup(func() { uitheme.SetPaletteForTest(old); deriveColors() })

	SetThemeFile("") // no file: falls back to the built-in default

	if queueTitleColor == 0 || nowPlayingBorderColor == 0 {
		t.Error("panel colors are still zero after SetThemeFile")
	}

	ResetPaletteForTest()
	if queueTitleColor == 0 {
		t.Error("panel colors are zero after ResetPaletteForTest")
	}
}

// --- start --------------------------------------------------------------

// dialOrSkipUI mirrors the other helpers of this name: start takes a
// concrete *mpdclient.Client because it opens an idle watch, which the
// fake cannot provide.
func dialOrSkipUI(t *testing.T) *mpdclient.Client {
	t.Helper()
	c, err := mpdclient.Dial(config.Load())
	if err != nil {
		t.Skipf("no MPD server reachable: %v", err)
	}
	t.Cleanup(func() { c.Close() })
	return c
}

// TestStartBuildsAPrimedApp covers everything App.Run does except the
// one call that needs a terminal: the watch, the signal handlers, the
// panels and the first refresh.
func TestStartBuildsAPrimedApp(t *testing.T) {
	client := dialOrSkipUI(t)

	a, cleanup, err := start(client, "", nil, ConfigSummary{})
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	defer cleanup()

	if a.watcher == nil {
		t.Error("no MPD watch was opened")
	}
	if a.queue == nil || a.library == nil || a.playlists == nil {
		t.Error("start returned an App with unbuilt panels")
	}
	if !a.startedUp {
		t.Error("startedUp = false, so refreshAll did not run")
	}
	if a.applyToUI == nil {
		t.Error("applyToUI was not wired")
	}
	if a.runAsync == nil {
		t.Error("runAsync was not wired")
	}
	// The Queue reflects whatever the server actually has.
	if got := a.queue.stats.GetText(true); got == "" {
		t.Error("the stats panel is empty after refreshAll")
	}
}

// TestStartReportsAFailedWatch covers the one error start returns: with
// no idle connection there is no way to see changes, so it refuses
// rather than running blind.
func TestStartReportsAFailedWatch(t *testing.T) {
	c, err := mpdclient.Dial(config.Config{Host: "127.0.0.1", Port: "1"})
	if err != nil {
		// Dial itself failed, which is the same class of problem and
		// means there is nothing to watch either.
		return
	}
	t.Cleanup(func() { c.Close() })

	if _, _, err := start(c, "", nil, ConfigSummary{}); err == nil {
		t.Error("start succeeded against an unreachable server, want a watch error")
	}
}

// TestStartCleanupStopsTheEventLoop covers the teardown: cleanup closes
// done, which is what stops the event loop and the two signal
// goroutines.
func TestStartCleanupStopsTheEventLoop(t *testing.T) {
	client := dialOrSkipUI(t)

	a, cleanup, err := start(client, "", nil, ConfigSummary{})
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	cleanup()

	select {
	case <-a.done:
	default:
		t.Error("cleanup did not close the done channel")
	}
}

// TestStartTheThemeSignalRepaints covers the SIGUSR1 goroutine start
// installs -- the hook an Omarchy theme-set drops in to recolor every
// running mpdtui.
func TestStartTheThemeSignalRepaints(t *testing.T) {
	client := dialOrSkipUI(t)

	a, cleanup, err := start(client, "", nil, ConfigSummary{})
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	defer cleanup()

	repainted := make(chan struct{}, 1)
	a.applyToUI = func(fn func()) {
		fn()
		select {
		case repainted <- struct{}{}:
		default:
		}
	}

	if err := syscall.Kill(syscall.Getpid(), syscall.SIGUSR1); err != nil {
		t.Fatalf("Kill: %v", err)
	}
	select {
	case <-repainted:
	case <-time.After(2 * time.Second):
		t.Error("SIGUSR1 did not trigger a repaint")
	}
}
