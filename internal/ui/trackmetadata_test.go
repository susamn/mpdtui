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
