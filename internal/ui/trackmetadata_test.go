package ui

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"mpdtui/internal/metadata"
	"mpdtui/internal/mpdclient"
)

// newTestAppWithMetaDB mirrors newTestApp/newTestAppWithMusicDir, but
// with the track-metadata feature active against a real (temp-file)
// SQLite database. runAsync is overridden to run its work synchronously
// (see runAsyncDefault) -- in production it hands off to a goroutine and
// applies the result via tv.QueueUpdateDraw, but nothing drains that
// queue without tv.Run() actually running, so tests need a deterministic
// stand-in rather than racing a background goroutine.
func newTestAppWithMetaDB(t *testing.T) *App {
	t.Helper()
	db, err := metadata.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("metadata.Open: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	a := &App{tv: tview.NewApplication(), metaDB: db, playCountedSongID: -1}
	a.build()
	a.queue.table.SetRect(0, 0, 150, 40)
	a.runAsync = func(work func() error, onSuccess func()) {
		if err := work(); err != nil {
			a.showError(err)
			return
		}
		onSuccess()
	}
	return a
}

func TestRatingStars(t *testing.T) {
	cases := []struct {
		rating int
		want   string
	}{
		{0, "☆☆☆☆☆"},
		{3, "★★★☆☆"},
		{5, "★★★★★"},
	}
	for _, tc := range cases {
		if got := ratingStars(tc.rating); got != tc.want {
			t.Errorf("ratingStars(%d) = %q, want %q", tc.rating, got, tc.want)
		}
	}
}

func TestHandleRateSelectedTrackNotEnabledFlashesWithoutTouchingClient(t *testing.T) {
	a := newTestApp() // nil metaDB, nil client -- would panic if either were touched
	a.queue.songs = []mpdclient.Song{{ID: 1, File: "artist/track.mp3"}}
	a.queue.render(-1)
	a.queue.table.Select(queueHeaderRows, 0)

	a.handleRateSelectedTrack(4)

	if got := a.hintBar.GetText(true); !strings.Contains(got, "track metadata not enabled") {
		t.Errorf("hint bar = %q, want it to mention track metadata isn't enabled", got)
	}
}

func TestHandleRateSelectedTrackSavesRating(t *testing.T) {
	a := newTestAppWithMetaDB(t)
	a.queue.songs = []mpdclient.Song{{ID: 1, Title: "Track", File: "artist/track.mp3"}}
	a.queue.render(-1)
	a.queue.table.Select(queueHeaderRows, 0)

	a.handleRateSelectedTrack(4)

	track, err := a.metaDB.Get("artist/track.mp3")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if track.Rating != 4 {
		t.Errorf("rating = %d, want 4", track.Rating)
	}
	if got := a.hintBar.GetText(true); !strings.Contains(got, "★★★★☆") {
		t.Errorf("hint bar = %q, want it to show the new rating", got)
	}
}

// TestHandleRateSelectedTrackUpdatesQueueRatingCell is the "refreshing
// that metadata in the queue panel" requirement: rating a track repaints
// its Rating cell in the already-rendered Queue table, not just the
// database row.
func TestHandleRateSelectedTrackUpdatesQueueRatingCell(t *testing.T) {
	a := newTestAppWithMetaDB(t)
	a.queue.songs = []mpdclient.Song{{ID: 1, Title: "Track", File: "artist/track.mp3"}}
	a.queue.render(-1)
	a.queue.table.Select(queueHeaderRows, 0)

	a.handleRateSelectedTrack(4)

	want := ratingStars(4) + queueColumnGap
	if got := a.queue.table.GetCell(queueHeaderRows, a.queue.cols.rating).Text; got != want {
		t.Errorf("Rating cell after rating 4 = %q, want %q", got, want)
	}
}

func TestHandleRateSelectedTrackNoopWhenNothingSelected(t *testing.T) {
	a := newTestAppWithMetaDB(t)
	// No songs in the queue at all -- selectedSong() must return false.

	a.handleRateSelectedTrack(3) // must not panic

	if got := a.hintBar.GetText(true); strings.Contains(got, "rated") {
		t.Errorf("hint bar = %q, want no rating confirmation (nothing was selected)", got)
	}
}

// TestNumberKeysRateInQueueButJumpPanelsElsewhere is the actual
// keybinding-conflict resolution: 1-5 rate the selected track only while
// Queue is focused; from any other panel, 1/2/3 keep their original
// panel-jump meaning and 4/5 are simply invalid there.
func TestNumberKeysRateInQueueButJumpPanelsElsewhere(t *testing.T) {
	a := newTestAppWithMetaDB(t)
	a.queue.songs = []mpdclient.Song{{ID: 1, Title: "Track", File: "artist/track.mp3"}}
	a.queue.render(-1)
	a.queue.table.Select(queueHeaderRows, 0)
	a.tv.SetFocus(a.queue.table)

	fourKey := tcell.NewEventKey(tcell.KeyRune, '4', tcell.ModNone)
	if result := a.globalInputCapture(fourKey); result != nil {
		t.Errorf("'4' while Queue is focused should be consumed (rate), got %v", result)
	}
	track, err := a.metaDB.Get("artist/track.mp3")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if track.Rating != 4 {
		t.Errorf("rating after '4' in Queue = %d, want 4", track.Rating)
	}

	// From Library (not Queue), '2' must still jump to Playlists, same as
	// before this feature existed.
	a.tv.SetFocus(a.library.tree)
	twoKey := tcell.NewEventKey(tcell.KeyRune, '2', tcell.ModNone)
	a.globalInputCapture(twoKey)
	if a.tv.GetFocus() != a.playlists.table {
		t.Errorf("focus after '2' from Library = %T, want the Playlists table", a.tv.GetFocus())
	}
}

func TestMaybeTrackPlayCountIncrementsAtHalfway(t *testing.T) {
	a := newTestAppWithMetaDB(t)
	song := mpdclient.Song{File: "artist/track.mp3"}
	status := func(elapsedFrac float64) mpdclient.Status {
		total := 200 * time.Second
		return mpdclient.Status{SongID: 7, Duration: total, Elapsed: time.Duration(elapsedFrac * float64(total))}
	}

	a.maybeTrackPlayCount(status(0.3), song) // 30% -- below halfway
	track, err := a.metaDB.Get(song.File)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if track.PlayCount != 0 {
		t.Fatalf("play count at 30%% = %d, want 0 (not counted yet)", track.PlayCount)
	}

	a.maybeTrackPlayCount(status(0.5), song) // exactly halfway -- should count
	track, err = a.metaDB.Get(song.File)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if track.PlayCount != 1 {
		t.Errorf("play count at 50%% = %d, want 1", track.PlayCount)
	}

	// Continuing to tick past halfway (e.g. every ~500ms refresh) must
	// not keep incrementing.
	a.maybeTrackPlayCount(status(0.9), song)
	track, err = a.metaDB.Get(song.File)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if track.PlayCount != 1 {
		t.Errorf("play count after continuing past halfway = %d, want still 1 (counted once per play-through)", track.PlayCount)
	}
}

// TestMaybeTrackPlayCountUpdatesQueuePlaysCell is the Plays-column
// counterpart to TestHandleRateSelectedTrackUpdatesQueueRatingCell:
// crossing the halfway threshold repaints the Queue table's Plays cell,
// not just the database row.
func TestMaybeTrackPlayCountUpdatesQueuePlaysCell(t *testing.T) {
	a := newTestAppWithMetaDB(t)
	song := mpdclient.Song{File: "artist/track.mp3"}
	a.queue.songs = []mpdclient.Song{{ID: 1, File: song.File}}
	a.queue.render(-1)
	total := 100 * time.Second

	a.maybeTrackPlayCount(mpdclient.Status{SongID: 1, Duration: total, Elapsed: total}, song)

	if got, want := a.queue.table.GetCell(queueHeaderRows, a.queue.cols.playcount).Text, "1"+queueColumnGap; got != want {
		t.Errorf("Plays cell after one play-through = %q, want %q", got, want)
	}
}

func TestMaybeTrackPlayCountCountsAgainForADifferentSongID(t *testing.T) {
	a := newTestAppWithMetaDB(t)
	song := mpdclient.Song{File: "artist/track.mp3"}
	total := 100 * time.Second

	a.maybeTrackPlayCount(mpdclient.Status{SongID: 1, Duration: total, Elapsed: total}, song)
	a.maybeTrackPlayCount(mpdclient.Status{SongID: 2, Duration: total, Elapsed: total}, song) // a fresh play-through, different queue id

	track, err := a.metaDB.Get(song.File)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if track.PlayCount != 2 {
		t.Errorf("play count across two distinct song ids = %d, want 2", track.PlayCount)
	}
}

// TestMaybeTrackPlayCountCountsAgainAfterRestartFromBeginning covers the
// bug reported live: repeat mode (or replaying the same still-queued
// entry) reuses the SAME SongID rather than getting a fresh one like a
// re-add does, so the count must re-arm once Elapsed is observed back
// near the start rather than staying permanently blocked for that id.
func TestMaybeTrackPlayCountCountsAgainAfterRestartFromBeginning(t *testing.T) {
	a := newTestAppWithMetaDB(t)
	song := mpdclient.Song{File: "artist/track.mp3"}
	total := 200 * time.Second

	a.maybeTrackPlayCount(mpdclient.Status{SongID: 7, Duration: total, Elapsed: total}, song)
	// Restarted from the beginning on the same SongID (repeat-mode loop,
	// or the user replaying the same queue entry) -- must not count yet.
	a.maybeTrackPlayCount(mpdclient.Status{SongID: 7, Duration: total, Elapsed: 0}, song)
	a.maybeTrackPlayCount(mpdclient.Status{SongID: 7, Duration: total, Elapsed: total}, song)

	track, err := a.metaDB.Get(song.File)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if track.PlayCount != 2 {
		t.Errorf("play count after a same-SongID restart-and-replay = %d, want 2", track.PlayCount)
	}
}

// TestMaybeTrackPlayCountDoesNotRearmOnASmallBackwardSeek keeps the
// original stated goal intact: seeking backward across the halfway
// point without going all the way back to the beginning is normal
// scrubbing, not a restart, and must not re-arm the count.
func TestMaybeTrackPlayCountDoesNotRearmOnASmallBackwardSeek(t *testing.T) {
	a := newTestAppWithMetaDB(t)
	song := mpdclient.Song{File: "artist/track.mp3"}
	total := 200 * time.Second

	a.maybeTrackPlayCount(mpdclient.Status{SongID: 7, Duration: total, Elapsed: total}, song)
	// Seeks back near the midpoint, well short of the beginning, then
	// forward past halfway again -- still the same play-through.
	a.maybeTrackPlayCount(mpdclient.Status{SongID: 7, Duration: total, Elapsed: total / 3}, song)
	a.maybeTrackPlayCount(mpdclient.Status{SongID: 7, Duration: total, Elapsed: total}, song)

	track, err := a.metaDB.Get(song.File)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if track.PlayCount != 1 {
		t.Errorf("play count after a small backward seek = %d, want still 1", track.PlayCount)
	}
}

func TestMaybeTrackPlayCountNoopWithoutMetaDB(t *testing.T) {
	a := newTestApp() // nil metaDB, nil client -- would panic if metaDB were touched
	song := mpdclient.Song{File: "artist/track.mp3"}
	total := 100 * time.Second

	a.maybeTrackPlayCount(mpdclient.Status{SongID: 1, Duration: total, Elapsed: total}, song) // must not panic
}

func TestHandleOpenMarkPickerRequiresQueueFocus(t *testing.T) {
	a := newTestAppWithMetaDB(t)
	a.queue.songs = []mpdclient.Song{{ID: 1, File: "artist/track.mp3"}}
	a.queue.render(-1)
	a.tv.SetFocus(a.library.tree)

	a.handleOpenMarkPicker()

	if a.mode != modeNormal {
		t.Error("mode after 'm' outside Queue should stay modeNormal (invalid key, not opened)")
	}
}

func TestHandleOpenMarkPickerNotEnabledFlashesWithoutTouchingClient(t *testing.T) {
	a := newTestApp() // nil metaDB, nil client
	a.queue.songs = []mpdclient.Song{{ID: 1, File: "artist/track.mp3"}}
	a.queue.render(-1)
	a.tv.SetFocus(a.queue.table)

	a.handleOpenMarkPicker()

	if got := a.hintBar.GetText(true); !strings.Contains(got, "track metadata not enabled") {
		t.Errorf("hint bar = %q, want it to mention track metadata isn't enabled", got)
	}
}

func TestMarkPickerListsClearMarkPlusCatalogAndApplies(t *testing.T) {
	a := newTestAppWithMetaDB(t)
	a.queue.songs = []mpdclient.Song{{ID: 1, Title: "Track", File: "artist/track.mp3"}}
	a.queue.render(-1)
	a.queue.table.Select(queueHeaderRows, 0)
	a.tv.SetFocus(a.queue.table)

	a.handleOpenMarkPicker()
	if a.mode != modeOverlay {
		t.Fatal("setup: mode after handleOpenMarkPicker should be modeOverlay")
	}
	if a.tv.GetFocus() != a.markPicker {
		t.Fatalf("setup: focus = %T, want the mark picker", a.tv.GetFocus())
	}
	// "(clear mark)" plus the one seeded mark_reason row.
	if got := a.markPicker.GetItemCount(); got != 2 {
		t.Fatalf("mark picker item count = %d, want 2", got)
	}

	a.markPicker.SetCurrentItem(1) // the real "mark for deletion" reason, not the synthetic clear entry
	a.markPicker.apply(1)

	if a.mode != modeNormal {
		t.Error("mode after applying a mark should be modeNormal (popup closed)")
	}
	track, err := a.metaDB.Get("artist/track.mp3")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if track.Mark == nil || track.Mark.Reason != "mark for deletion" {
		t.Fatalf("Mark after applying = %+v, want {mark for deletion}", track.Mark)
	}
}

func TestMarkPickerClearMarkEntry(t *testing.T) {
	a := newTestAppWithMetaDB(t)
	if err := a.metaDB.Rate("artist/track.mp3", 0); err != nil { // ensure a row exists
		t.Fatalf("Rate: %v", err)
	}
	reasonID := int64(1)
	if err := a.metaDB.SetMark("artist/track.mp3", &reasonID); err != nil {
		t.Fatalf("SetMark: %v", err)
	}

	a.queue.songs = []mpdclient.Song{{ID: 1, Title: "Track", File: "artist/track.mp3"}}
	a.queue.render(-1)
	a.queue.table.Select(queueHeaderRows, 0)
	a.tv.SetFocus(a.queue.table)

	a.handleOpenMarkPicker()
	a.markPicker.apply(0) // the synthetic "(clear mark)" entry

	track, err := a.metaDB.Get("artist/track.mp3")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if track.Mark != nil {
		t.Errorf("Mark after applying the clear entry = %+v, want nil", track.Mark)
	}
}

// TestMarkPickerApplyUpdatesQueueMarkCell is the Mark-column counterpart
// to TestHandleRateSelectedTrackUpdatesQueueRatingCell: applying a mark
// (and clearing it) repaints the Queue table's Mark cell, not just the
// database row.
func TestMarkPickerApplyUpdatesQueueMarkCell(t *testing.T) {
	a := newTestAppWithMetaDB(t)
	a.queue.songs = []mpdclient.Song{{ID: 1, Title: "Track", File: "artist/track.mp3"}}
	a.queue.render(-1)
	a.queue.table.Select(queueHeaderRows, 0)
	a.tv.SetFocus(a.queue.table)

	a.handleOpenMarkPicker()
	a.markPicker.apply(1) // "mark for deletion"

	if got, want := a.queue.table.GetCell(queueHeaderRows, a.queue.cols.mark).Text, queueMarkTick+queueColumnGap; got != want {
		t.Errorf("Mark cell after marking = %q, want %q", got, want)
	}

	a.handleOpenMarkPicker()
	a.markPicker.apply(0) // "(clear mark)"

	if got, want := a.queue.table.GetCell(queueHeaderRows, a.queue.cols.mark).Text, queueColumnGap; got != want {
		t.Errorf("Mark cell after clearing = %q, want %q (blank)", got, want)
	}
}

// TestRunAsyncDefaultDoesNotBlockCaller is the core requirement behind
// every metaDB write going through App.runAsync: work runs on its own
// goroutine, so the caller (a keypress handler, on the UI goroutine)
// gets control back immediately regardless of how long work takes --
// proven here with a work func that blocks until the test explicitly
// releases it.
func TestRunAsyncDefaultDoesNotBlockCaller(t *testing.T) {
	a := newTestApp() // runAsync == runAsyncDefault (not overridden, unlike newTestAppWithMetaDB)
	release := make(chan struct{})
	callerReturned := make(chan struct{})

	go func() {
		a.runAsync(func() error {
			<-release // only unblocks once this test says so
			return nil
		}, func() {})
		close(callerReturned)
	}()

	select {
	case <-callerReturned:
	case <-time.After(2 * time.Second):
		t.Fatal("runAsync blocked its caller until work finished, want it to return immediately")
	}
	close(release) // let the still-running background goroutine finish, best-effort cleanup
}

func TestQKeyWhileMarkPickerOpenIsConsumed(t *testing.T) {
	a := newTestAppWithMetaDB(t)
	a.queue.songs = []mpdclient.Song{{ID: 1, File: "artist/track.mp3"}}
	a.queue.render(-1)
	a.queue.table.Select(queueHeaderRows, 0)
	a.tv.SetFocus(a.queue.table)
	a.handleOpenMarkPicker()

	qKey := tcell.NewEventKey(tcell.KeyRune, 'q', tcell.ModNone)
	if result := a.globalInputCapture(qKey); result != nil {
		t.Errorf("'q' while the mark picker is open should be consumed (quit), got %v", result)
	}
}

func TestTransportKeysStayLiveWhileMarkPickerOpenNeedsLiveMPD(t *testing.T) {
	c := dialOrSkip(t)
	db, err := metadata.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("metadata.Open: %v", err)
	}
	defer db.Close()

	a := &App{tv: tview.NewApplication(), client: c, metaDB: db, playCountedSongID: -1}
	a.build()
	a.queue.songs = []mpdclient.Song{{ID: 1, File: "artist/track.mp3"}}
	a.queue.render(-1)
	a.queue.table.Select(queueHeaderRows, 0)
	a.tv.SetFocus(a.queue.table)
	a.handleOpenMarkPicker()
	if a.mode != modeOverlay {
		t.Fatal("setup: mode after handleOpenMarkPicker should be modeOverlay")
	}

	space := tcell.NewEventKey(tcell.KeyRune, ' ', tcell.ModNone)
	if result := a.globalInputCapture(space); result != nil {
		t.Errorf("Space while the mark picker is open should be consumed (routed to togglePlayPause), got %v", result)
	}
	a.globalInputCapture(space) // toggle back, restoring whatever state playback was already in

	if a.mode != modeOverlay {
		t.Error("mode after a transport key should stay modeOverlay -- the mark picker itself must stay open")
	}
}

func TestMarkPickerEscRestoresOriginalFocus(t *testing.T) {
	a := newTestAppWithMetaDB(t)
	a.queue.songs = []mpdclient.Song{{ID: 1, File: "artist/track.mp3"}}
	a.queue.render(-1)
	a.queue.table.Select(queueHeaderRows, 0)
	a.tv.SetFocus(a.queue.table)
	a.handleOpenMarkPicker()

	esc := tcell.NewEventKey(tcell.KeyEscape, 0, tcell.ModNone)
	if result := a.globalInputCapture(esc); result != nil {
		t.Errorf("Escape while the mark picker is open should be consumed, got %v", result)
	}
	if a.mode != modeNormal {
		t.Error("mode after Escape should be modeNormal")
	}
	if a.tv.GetFocus() != a.queue.table {
		t.Errorf("focus after Escape = %T, want the originally-focused Queue table", a.tv.GetFocus())
	}
}

// TestHandleRateSelectedTrackRatesPlayingTrackNotSelection is the whole
// point of ratingTarget: scrolling the Queue away from the playing track
// (to look at what's coming up, say) and then pressing 1-5 must rate what
// is actually playing, not the row the cursor was left on.
func TestHandleRateSelectedTrackRatesPlayingTrackNotSelection(t *testing.T) {
	a := newTestAppWithMetaDB(t)
	a.queue.songs = []mpdclient.Song{
		{ID: 1, Title: "Playing", File: "artist/playing.mp3"},
		{ID: 2, Title: "Other", File: "artist/other.mp3"},
	}
	a.queue.render(1)
	a.queue.table.Select(queueHeaderRows+1, 0) // selection scrolled to "Other"
	a.currentStatus = mpdclient.Status{State: mpdclient.StatePlay, SongID: 1}
	a.currentSong = a.queue.songs[0]

	a.handleRateSelectedTrack(4)

	playing, err := a.metaDB.Get("artist/playing.mp3")
	if err != nil {
		t.Fatalf("Get(playing): %v", err)
	}
	if playing.Rating != 4 {
		t.Errorf("playing track rating = %d, want 4", playing.Rating)
	}
	other, err := a.metaDB.Get("artist/other.mp3")
	if err != nil {
		t.Fatalf("Get(other): %v", err)
	}
	if other.Rating != 0 {
		t.Errorf("selected-but-not-playing track rating = %d, want 0 (untouched)", other.Rating)
	}
	if got := a.hintBar.GetText(true); !strings.Contains(got, "Playing") {
		t.Errorf("hint bar = %q, want it to name the playing track", got)
	}
}

// TestHandleRateSelectedTrackRatesPlayingTrackWhilePaused: paused is
// still "the track you're listening to", so it targets the same way play
// does -- only a stop hands the target back to the selection.
func TestHandleRateSelectedTrackRatesPlayingTrackWhilePaused(t *testing.T) {
	a := newTestAppWithMetaDB(t)
	a.queue.songs = []mpdclient.Song{
		{ID: 1, Title: "Paused", File: "artist/paused.mp3"},
		{ID: 2, Title: "Other", File: "artist/other.mp3"},
	}
	a.queue.render(1)
	a.queue.table.Select(queueHeaderRows+1, 0)
	a.currentStatus = mpdclient.Status{State: mpdclient.StatePause, SongID: 1}
	a.currentSong = a.queue.songs[0]

	a.handleRateSelectedTrack(2)

	paused, err := a.metaDB.Get("artist/paused.mp3")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if paused.Rating != 2 {
		t.Errorf("paused track rating = %d, want 2", paused.Rating)
	}
}

// TestHandleRateSelectedTrackFallsBackToSelectionWhenStopped: MPD still
// reports a current song while stopped (the resume position), but nobody
// is listening to it, so the Queue selection is what gets rated.
func TestHandleRateSelectedTrackFallsBackToSelectionWhenStopped(t *testing.T) {
	a := newTestAppWithMetaDB(t)
	a.queue.songs = []mpdclient.Song{
		{ID: 1, Title: "Resume point", File: "artist/resume.mp3"},
		{ID: 2, Title: "Selected", File: "artist/selected.mp3"},
	}
	a.queue.render(-1)
	a.queue.table.Select(queueHeaderRows+1, 0)
	a.currentStatus = mpdclient.Status{State: mpdclient.StateStop, SongID: 1}
	a.currentSong = a.queue.songs[0]

	a.handleRateSelectedTrack(5)

	selected, err := a.metaDB.Get("artist/selected.mp3")
	if err != nil {
		t.Fatalf("Get(selected): %v", err)
	}
	if selected.Rating != 5 {
		t.Errorf("selected track rating = %d, want 5", selected.Rating)
	}
	resume, err := a.metaDB.Get("artist/resume.mp3")
	if err != nil {
		t.Fatalf("Get(resume): %v", err)
	}
	if resume.Rating != 0 {
		t.Errorf("stopped-at track rating = %d, want 0 (untouched)", resume.Rating)
	}
}

// TestBuildFocusesQueuePanelOnStartup: the Queue is where a session
// starts, and panelIdx has to agree with it so the first Tab cycles from
// Queue rather than from Library.
func TestBuildFocusesQueuePanelOnStartup(t *testing.T) {
	a := newTestApp()

	if a.tv.GetFocus() != a.queue.table {
		t.Errorf("startup focus = %T, want the Queue table", a.tv.GetFocus())
	}
	if a.panelIdx != queuePanelIdx {
		t.Errorf("panelIdx = %d, want queuePanelIdx (%d)", a.panelIdx, queuePanelIdx)
	}
}

// setPlayingForTest puts the app in the state refreshNowPlaying would
// leave it in with song actually playing -- the precondition for
// App.targetSong preferring it over the Queue selection.
func setPlayingForTest(a *App, song mpdclient.Song) {
	a.currentSong = song
	a.currentStatus = mpdclient.Status{State: mpdclient.StatePlay, SongID: song.ID}
}

// TestMarkPickerMarksPlayingTrackNotSelection: 'm' follows the same
// target rule as rating -- the popup is about the track you're listening
// to, not the row the cursor was left on.
func TestMarkPickerMarksPlayingTrackNotSelection(t *testing.T) {
	a := newTestAppWithMetaDB(t)
	a.queue.songs = []mpdclient.Song{
		{ID: 1, Title: "Playing", File: "artist/playing.mp3"},
		{ID: 2, Title: "Other", File: "artist/other.mp3"},
	}
	a.queue.render(1)
	a.queue.table.Select(queueHeaderRows+1, 0)
	setPlayingForTest(a, a.queue.songs[0])
	a.tv.SetFocus(a.queue.table)

	a.handleOpenMarkPicker()

	if a.markPicker.song.File != "artist/playing.mp3" {
		t.Fatalf("mark picker target = %q, want the playing track", a.markPicker.song.File)
	}
	if got := a.markPicker.GetTitle(); !strings.Contains(got, "Playing") {
		t.Errorf("mark picker title = %q, want it to name the playing track", got)
	}

	a.markPicker.apply(1) // first real mark reason (index 0 is "(clear mark)")

	playing, err := a.metaDB.Get("artist/playing.mp3")
	if err != nil {
		t.Fatalf("Get(playing): %v", err)
	}
	if playing.Mark == nil {
		t.Error("playing track should have been marked")
	}
	other, err := a.metaDB.Get("artist/other.mp3")
	if err != nil {
		t.Fatalf("Get(other): %v", err)
	}
	if other.Mark != nil {
		t.Errorf("selected-but-not-playing track mark = %+v, want nil (untouched)", other.Mark)
	}
}

// TestMarkPickerAppliesToTheTrackItWasOpenedFor is why markPicker
// captures its target at open time: transport controls stay live while
// the popup is up, so the track can auto-advance between opening it and
// pressing Enter -- the mark must still land on the track the title
// named, not on whatever happens to be playing by then.
func TestMarkPickerAppliesToTheTrackItWasOpenedFor(t *testing.T) {
	a := newTestAppWithMetaDB(t)
	a.queue.songs = []mpdclient.Song{
		{ID: 1, Title: "First", File: "artist/first.mp3"},
		{ID: 2, Title: "Second", File: "artist/second.mp3"},
	}
	a.queue.render(1)
	a.queue.table.Select(queueHeaderRows, 0)
	setPlayingForTest(a, a.queue.songs[0])
	a.tv.SetFocus(a.queue.table)

	a.handleOpenMarkPicker()
	setPlayingForTest(a, a.queue.songs[1]) // track auto-advanced under the open popup
	a.markPicker.apply(1)

	first, err := a.metaDB.Get("artist/first.mp3")
	if err != nil {
		t.Fatalf("Get(first): %v", err)
	}
	if first.Mark == nil {
		t.Error("the track the popup was opened for should have been marked")
	}
	second, err := a.metaDB.Get("artist/second.mp3")
	if err != nil {
		t.Fatalf("Get(second): %v", err)
	}
	if second.Mark != nil {
		t.Errorf("the newly-advanced-to track mark = %+v, want nil (untouched)", second.Mark)
	}
}

// TestMarkPickerFallsBackToSelectionWhenStopped mirrors rating's own
// stopped-playback fallback.
func TestMarkPickerFallsBackToSelectionWhenStopped(t *testing.T) {
	a := newTestAppWithMetaDB(t)
	a.queue.songs = []mpdclient.Song{
		{ID: 1, Title: "Resume point", File: "artist/resume.mp3"},
		{ID: 2, Title: "Selected", File: "artist/selected.mp3"},
	}
	a.queue.render(-1)
	a.queue.table.Select(queueHeaderRows+1, 0)
	a.currentStatus = mpdclient.Status{State: mpdclient.StateStop, SongID: 1}
	a.currentSong = a.queue.songs[0]
	a.tv.SetFocus(a.queue.table)

	a.handleOpenMarkPicker()

	if a.markPicker.song.File != "artist/selected.mp3" {
		t.Errorf("mark picker target = %q, want the selected track (playback stopped)", a.markPicker.song.File)
	}
}

// --- App.targetSong itself ---

func TestTargetSong(t *testing.T) {
	playing := mpdclient.Song{ID: 1, Title: "Playing", File: "artist/playing.mp3"}
	selected := mpdclient.Song{ID: 2, Title: "Selected", File: "artist/selected.mp3"}

	cases := []struct {
		name  string
		state mpdclient.State
		want  string
	}{
		{"playing wins over the selection", mpdclient.StatePlay, playing.File},
		{"paused still counts as playing", mpdclient.StatePause, playing.File},
		{"stopped falls back to the selection", mpdclient.StateStop, selected.File},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			a := newTestApp()
			a.queue.songs = []mpdclient.Song{playing, selected}
			a.queue.render(1)
			a.queue.table.Select(queueHeaderRows+1, 0)
			a.currentSong = playing
			a.currentStatus = mpdclient.Status{State: tc.state, SongID: playing.ID}

			got, ok := a.targetSong()
			if !ok {
				t.Fatal("targetSong returned ok=false, want a target")
			}
			if got.File != tc.want {
				t.Errorf("targetSong = %q, want %q", got.File, tc.want)
			}
		})
	}
}

func TestTargetSongNoneWhenStoppedAndQueueEmpty(t *testing.T) {
	a := newTestApp()
	a.currentSong = mpdclient.Song{File: "artist/resume.mp3"}
	a.currentStatus = mpdclient.Status{State: mpdclient.StateStop}

	if _, ok := a.targetSong(); ok {
		t.Error("targetSong should report no target with playback stopped and an empty queue")
	}
}
