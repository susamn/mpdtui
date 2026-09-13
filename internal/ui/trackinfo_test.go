package ui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gdamore/tcell/v2"

	"mpdtui/internal/metadata"
	"mpdtui/internal/mpdclient"
)

func TestQuadrantRectBottomRight(t *testing.T) {
	cases := []struct {
		x, y, w, h                 int
		wantX, wantY, wantW, wantH int
	}{
		{0, 0, 100, 60, 50, 30, 50, 30},
		{10, 5, 41, 21, 31, 16, 20, 10}, // odd dimensions round the quadrant size down
		{0, 0, 0, 0, 0, 0, 0, 0},
	}
	for _, tc := range cases {
		gotX, gotY, gotW, gotH := quadrantRect(tc.x, tc.y, tc.w, tc.h)
		if gotX != tc.wantX || gotY != tc.wantY || gotW != tc.wantW || gotH != tc.wantH {
			t.Errorf("quadrantRect(%d,%d,%d,%d) = (%d,%d,%d,%d), want (%d,%d,%d,%d)",
				tc.x, tc.y, tc.w, tc.h, gotX, gotY, gotW, gotH, tc.wantX, tc.wantY, tc.wantW, tc.wantH)
		}
	}
}

func TestCardRectFixedSizeClampedToQuadrant(t *testing.T) {
	// cardRect now takes the Queue panel's own rect and derives the
	// quadrant itself, so these cases are written in panel terms.
	cases := []struct {
		name                       string
		px, py, pw, ph, want       int
		wantX, wantY, wantW, wantH int
	}{
		{
			// Top-right anchor at 40% from panel's top (py=0).
			name: "fits in the quadrant",
			px:   0, py: 0, pw: 100, ph: 60, want: 18,
			wantX: 50, wantY: 24, wantW: trackInfoCardWidth, wantH: 18,
		},
		{
			// Quadrant narrower than the card's fixed width.
			name: "narrow panel clamps the width",
			px:   0, py: 0, pw: 82, ph: 42, want: 18,
			wantX: 41, wantY: 16, wantW: 41, wantH: 18,
		},
		{
			// Anchored at 40% of panel's height, but moves up to fit.
			name: "anchored at 40% of panel height, grows upwards",
			px:   0, py: 0, pw: 100, ph: 40, want: 30,
			wantX: 50, wantY: 10, wantW: trackInfoCardWidth, wantH: 30,
		},
		{
			// Asking for more than the whole panel: clamped to it, and
			// moves up to top of panel.
			name: "never taller than the panel itself",
			px:   0, py: 5, pw: 100, ph: 20, want: 100,
			wantX: 50, wantY: 5, wantW: trackInfoCardWidth, wantH: 20,
		},
		{name: "degenerate", px: 0, py: 0, pw: 2, ph: 2, want: 18, wantX: 1, wantY: 0, wantW: 1, wantH: 2},
		{name: "empty", px: 0, py: 0, pw: 0, ph: 0, want: 18, wantX: 0, wantY: 0, wantW: 0, wantH: 0},
	}
	for _, tc := range cases {
		gotX, gotY, gotW, gotH := cardRect(tc.px, tc.py, tc.pw, tc.ph, tc.want)
		if gotX != tc.wantX || gotY != tc.wantY || gotW != tc.wantW || gotH != tc.wantH {
			t.Errorf("%s: cardRect(%d,%d,%d,%d, want=%d) = (%d,%d,%d,%d), want (%d,%d,%d,%d)",
				tc.name, tc.px, tc.py, tc.pw, tc.ph, tc.want, gotX, gotY, gotW, gotH, tc.wantX, tc.wantY, tc.wantW, tc.wantH)
		}
	}
}

func TestTrackInfoCardRenderNothingPlaying(t *testing.T) {
	a := newTestApp()
	a.trackInfo.render(mpdclient.Song{}, mpdclient.Status{})
	if got := a.trackInfo.identity.GetText(false); !strings.Contains(got, "Nothing playing") {
		t.Errorf("render(zero Song) = %q, want it to contain %q", got, "Nothing playing")
	}
}

func TestTrackInfoCardRenderFullSong(t *testing.T) {
	a := newTestApp()
	song := mpdclient.Song{
		Title:  "Bohemian Rhapsody",
		Album:  "A Night at the Opera",
		Artist: "Queen",
		Genre:  "Rock",
		Date:   "1975-11-21",
	}
	a.trackInfo.render(song, mpdclient.Status{})
	got := a.trackInfo.identity.GetText(true)

	for _, want := range []string{"Bohemian Rhapsody", "A Night at the Opera", "Queen", "Rock", "1975"} {
		if !strings.Contains(got, want) {
			t.Errorf("render(%+v) text = %q, missing %q", song, got, want)
		}
	}
	if strings.Contains(got, "1975-11-21") {
		t.Errorf("render(%+v) text = %q, want the Date tag truncated to a 4-digit year", song, got)
	}
}

func TestTrackInfoCardRenderFallsBackToFilename(t *testing.T) {
	a := newTestApp()
	a.trackInfo.render(mpdclient.Song{File: "music/artist/track.mp3"}, mpdclient.Status{})
	got := a.trackInfo.identity.GetText(true)
	if !strings.Contains(got, "track.mp3") {
		t.Errorf("render(untagged Song) text = %q, want it to contain the filename %q", got, "track.mp3")
	}
}

// --- Audio quality line ---

func TestTrackInfoCardRenderShowsAudioQuality(t *testing.T) {
	a := newTestApp()
	song := mpdclient.Song{Title: "Track", Artist: "Artist"}
	st := mpdclient.Status{Bitrate: 128, AudioFormat: "44100:16:2"}
	a.trackInfo.render(song, st)
	got := a.trackInfo.identity.GetText(true)
	if !strings.Contains(got, "128kbps 44.1kHz/16-bit/2ch") {
		t.Errorf("render(...) text = %q, want it to contain the audio quality line", got)
	}
}

func TestTrackInfoCardRenderAudioQualityBlankWhenUnknown(t *testing.T) {
	a := newTestApp()
	song := mpdclient.Song{Title: "Track", Artist: "Artist"}
	a.trackInfo.render(song, mpdclient.Status{}) // stopped: no bitrate/audio format
	got := a.trackInfo.identity.GetText(true)
	if !strings.Contains(got, "🎚️") {
		t.Errorf("render(...) text = %q, want the audio quality line (icon) present even with a blank value", got)
	}
}

// --- Lyrics tick line ---

func TestTrackInfoCardRenderOmitsLyricsLineWithoutMusicDir(t *testing.T) {
	a := newTestApp() // musicDir == ""
	a.trackInfo.render(mpdclient.Song{Title: "Track", Artist: "Artist", File: "artist/track.mp3"}, mpdclient.Status{})
	got := a.trackInfo.identity.GetText(false)
	if strings.Contains(got, "📝") {
		t.Errorf("render(...) with no musicDir configured = %q, want the lyrics line omitted entirely", got)
	}
}

func TestTrackInfoCardRenderShowsTxtBadge(t *testing.T) {
	dir := t.TempDir()
	trackDir := filepath.Join(dir, "artist")
	if err := os.MkdirAll(trackDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(filepath.Join(trackDir, "Track.txt"), []byte("la la la"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	a := newTestAppWithMusicDir(dir)
	a.trackInfo.render(mpdclient.Song{Title: "Track", Artist: "Artist", File: "artist/Track.mp3"}, mpdclient.Status{})
	got := a.trackInfo.identity.GetText(true)
	if !strings.Contains(got, "TXT") {
		t.Errorf("render(...) with a matching .txt file = %q, want the TXT badge present", got)
	}
	if strings.Contains(got, "LRC") {
		t.Errorf("render(...) with only a .txt file = %q, want no LRC badge", got)
	}
}

func TestTrackInfoCardRenderShowsLRCBadge(t *testing.T) {
	dir := t.TempDir()
	trackDir := filepath.Join(dir, "artist")
	if err := os.MkdirAll(trackDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(filepath.Join(trackDir, "Track.lrc"), []byte("[00:01.00]la la la"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	a := newTestAppWithMusicDir(dir)
	a.trackInfo.render(mpdclient.Song{Title: "Track", Artist: "Artist", File: "artist/Track.mp3"}, mpdclient.Status{})
	got := a.trackInfo.identity.GetText(true)
	if !strings.Contains(got, "LRC") {
		t.Errorf("render(...) with a matching .lrc file = %q, want the LRC badge present", got)
	}
	if strings.Contains(got, "TXT") {
		t.Errorf("render(...) with only a .lrc file = %q, want no TXT badge", got)
	}
}

// TestTrackInfoCardRenderShowsBothBadges covers the explicit request:
// "we need to add same in track info card, rather than tick we can have
// colored text: LRC, TXT" -- both shown together when both exist.
func TestTrackInfoCardRenderShowsBothBadges(t *testing.T) {
	dir := t.TempDir()
	trackDir := filepath.Join(dir, "artist")
	if err := os.MkdirAll(trackDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(filepath.Join(trackDir, "Track.lrc"), []byte("[00:01.00]la la la"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if err := os.WriteFile(filepath.Join(trackDir, "Track.txt"), []byte("la la la"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	a := newTestAppWithMusicDir(dir)
	a.trackInfo.render(mpdclient.Song{Title: "Track", Artist: "Artist", File: "artist/Track.mp3"}, mpdclient.Status{})
	got := a.trackInfo.identity.GetText(true)
	if !strings.Contains(got, "LRC") || !strings.Contains(got, "TXT") {
		t.Errorf("render(...) with both .lrc and .txt = %q, want both badges present", got)
	}
}

func TestTrackInfoCardRenderNoBadgeWhenNoMatch(t *testing.T) {
	a := newTestAppWithMusicDir(t.TempDir())
	a.trackInfo.render(mpdclient.Song{Title: "Track", Artist: "Artist", File: "artist/Track.mp3"}, mpdclient.Status{})
	got := a.trackInfo.identity.GetText(true)
	if strings.Contains(got, "LRC") || strings.Contains(got, "TXT") {
		t.Errorf("render(...) with no matching lyrics = %q, want no format badge", got)
	}
	if !strings.Contains(got, "📝") {
		t.Errorf("render(...) = %q, want the lyrics line's icon still present (just no badge)", got)
	}
}

// --- Metadata table: presence gated on metaDB ---

func TestTrackInfoCardMetaTableNilWithoutMetaDB(t *testing.T) {
	a := newTestApp()
	if a.trackInfo.meta != nil {
		t.Error("trackInfo.meta should be nil when metaDB is inactive")
	}
}

func TestTrackInfoCardMetaTableBuiltWithMetaDB(t *testing.T) {
	a := newTestAppWithMetaDB(t)
	if a.trackInfo.meta == nil {
		t.Fatal("trackInfo.meta should be non-nil when metaDB is active")
	}
}

func metaCellText(a *App, row, col int) string {
	return a.trackInfo.meta.GetCell(row, col).Text
}

func TestTrackInfoCardRenderMetaShowsZeroOpinionPlaceholders(t *testing.T) {
	a := newTestAppWithMetaDB(t)
	a.trackInfo.render(mpdclient.Song{Title: "Track", File: "artist/track.mp3"}, mpdclient.Status{})

	if got := metaCellText(a, 0, 1); got != ratingStars(0) {
		t.Errorf("Rating cell = %q, want %q (all-empty stars)", got, ratingStars(0))
	}
	if got := metaCellText(a, 1, 1); got != "0" {
		t.Errorf("Plays cell = %q, want %q", got, "0")
	}
	// Marks and tags are their own sections now, not rows here.
	if got := a.trackInfo.marks.GetText(true); !strings.Contains(got, "none") {
		t.Errorf("Marks section = %q, want it to say none", got)
	}
	if got := a.trackInfo.tags.GetText(true); !strings.Contains(got, "none") {
		t.Errorf("Tags section = %q, want it to say none", got)
	}
	if got := a.trackInfo.bookmarks.GetText(true); !strings.Contains(got, "none") {
		t.Errorf("Bookmarks section = %q, want it to say none", got)
	}
}

func TestTrackInfoCardRenderMetaShowsRealValues(t *testing.T) {
	a := newTestAppWithMetaDB(t)
	file := "artist/track.mp3"

	if err := a.metaDB.Rate(file, 4); err != nil {
		t.Fatalf("Rate: %v", err)
	}
	if err := a.metaDB.IncrementPlayCount(file); err != nil {
		t.Fatalf("IncrementPlayCount: %v", err)
	}
	if err := a.metaDB.IncrementPlayCount(file); err != nil {
		t.Fatalf("IncrementPlayCount: %v", err)
	}
	markID := int64(1) // seeded "mark for deletion"
	if err := a.metaDB.SetMarks(file, []int64{markID}); err != nil {
		t.Fatalf("SetMark: %v", err)
	}
	if err := a.metaDB.SetTags(file, []int64{1, 2}); err != nil { // seeded bengali, hindi
		t.Fatalf("SetTags: %v", err)
	}
	if _, err := a.metaDB.CreateBookmark(file, 75.0, "guitar solo"); err != nil {
		t.Fatalf("CreateBookmark: %v", err)
	}

	a.trackInfo.render(mpdclient.Song{Title: "Track", File: file}, mpdclient.Status{})

	if got := metaCellText(a, 0, 1); got != ratingStars(4) {
		t.Errorf("Rating cell = %q, want %q", got, ratingStars(4))
	}
	if got := metaCellText(a, 1, 1); got != "2" {
		t.Errorf("Plays cell = %q, want %q", got, "2")
	}
	// Tags moved out of the table into their own list section, one per
	// line.
	tags := stripColorTags(a.trackInfo.tags.GetText(false))
	for _, want := range []string{"bengali", "hindi"} {
		if !strings.Contains(tags, want) {
			t.Errorf("Tags section = %q, want it to list %q", tags, want)
		}
	}
	// Marks moved out of the table into their own list section, one per
	// line, each in its own color.
	marks := a.trackInfo.marks.GetText(false)
	if !strings.Contains(stripColorTags(marks), "mark for deletion") {
		t.Errorf("Marks section = %q, want it to list the mark", marks)
	}
	if !strings.Contains(marks, markColor(metadata.MarkReason{ID: 1}).String()) {
		t.Errorf("Marks section = %q, want the mark in its own color", marks)
	}
	// Bookmarks section
	bookmarks := a.trackInfo.bookmarks.GetText(false)
	if !strings.Contains(bookmarks, "[1:15] guitar solo") {
		t.Errorf("Bookmarks section = %q, want it to list '[1:15] guitar solo'", bookmarks)
	}
}

func TestTrackInfoCardRenderMetaClearedWhenNothingPlaying(t *testing.T) {
	a := newTestAppWithMetaDB(t)
	a.trackInfo.render(mpdclient.Song{Title: "Track", File: "artist/track.mp3"}, mpdclient.Status{})
	if got := metaCellText(a, 1, 1); got != "0" {
		t.Fatalf("setup: Plays cell = %q, want %q", got, "0")
	}

	a.trackInfo.render(mpdclient.Song{}, mpdclient.Status{}) // nothing playing
	if got := a.trackInfo.meta.GetRowCount(); got != 0 {
		t.Errorf("meta table row count with nothing playing = %d, want 0 (cleared)", got)
	}
}

// --- Positioning/overlay behavior (unaffected by the metadata addition) ---

func TestOpenTrackInfoTakesFocusAndPositionsAtFortyPercent(t *testing.T) {
	a := newTestApp()
	a.queue.table.SetRect(0, 0, 100, 60)

	a.openTrackInfo()

	if a.tv.GetFocus() != a.trackInfo {
		t.Fatalf("focus after openTrackInfo = %T, want the track info card", a.tv.GetFocus())
	}
	if a.mode != modeOverlay {
		t.Error("mode after openTrackInfo should be modeOverlay")
	}

	// positionOverQueue is the part of Draw that computes the card's rect
	// from the Queue table's current rect -- exercise it directly rather
	// than Draw itself, which needs a real tcell.Screen to paint into.
	a.trackInfo.positionOverQueue()
	x, y, w, h := a.trackInfo.GetRect()
	if x != 50 || y != 24 || w != trackInfoCardWidth || h != a.trackInfo.height() {
		t.Errorf("card rect after Draw = (%d,%d,%d,%d), want (50,24,%d,%d) -- compact size, 40%% from panel top",
			x, y, w, h, trackInfoCardWidth, a.trackInfo.height())
	}
}

func TestOpenTrackInfoTogglesClosedOnSecondIPress(t *testing.T) {
	a := newTestApp()
	a.tv.SetFocus(a.queue.table)
	a.openTrackInfo()

	if a.mode != modeOverlay {
		t.Fatal("setup: mode after openTrackInfo should be modeOverlay")
	}

	iKey := tcell.NewEventKey(tcell.KeyRune, 'i', tcell.ModNone)
	if result := a.globalInputCapture(iKey); result != nil {
		t.Errorf("'i' while the track info card is open should be consumed, got %v", result)
	}
	if a.mode != modeNormal {
		t.Error("mode after a second 'i' press should be modeNormal (card toggled closed)")
	}
	if a.tv.GetFocus() != a.queue.table {
		t.Errorf("focus after toggling closed = %T, want the originally-focused Queue table", a.tv.GetFocus())
	}
}

func TestQKeyWhileTrackInfoOpenIsConsumed(t *testing.T) {
	a := newTestApp()
	a.tv.SetFocus(a.queue.table)
	a.openTrackInfo()

	qKey := tcell.NewEventKey(tcell.KeyRune, 'q', tcell.ModNone)
	if result := a.globalInputCapture(qKey); result != nil {
		t.Errorf("'q' while the track info card is open should be consumed (quit), got %v", result)
	}
}

func TestQKeyWhileAnotherOverlayOpenIsNotConsumed(t *testing.T) {
	a := newTestApp()
	a.tv.SetFocus(a.queue.table)
	a.openHelp()

	qKey := tcell.NewEventKey(tcell.KeyRune, 'q', tcell.ModNone)
	if result := a.globalInputCapture(qKey); result == nil {
		t.Error("'q' while a different overlay (help) is open should not quit -- only the track info card scopes 'q' to quit")
	}
}

func TestIKeyWhileAnotherOverlayOpenDoesNotToggleTrackInfo(t *testing.T) {
	a := newTestApp()
	a.tv.SetFocus(a.queue.table)
	a.openHelp()

	iKey := tcell.NewEventKey(tcell.KeyRune, 'i', tcell.ModNone)
	a.globalInputCapture(iKey)

	if a.mode != modeOverlay {
		t.Error("'i' while a different overlay (help) is open should not close it")
	}
}

func TestOpenTrackInfoEscRestoresOriginalFocus(t *testing.T) {
	a := newTestApp()
	a.tv.SetFocus(a.queue.table)
	a.openTrackInfo()

	esc := tcell.NewEventKey(tcell.KeyEscape, 0, tcell.ModNone)
	if result := a.globalInputCapture(esc); result != nil {
		t.Errorf("Escape while the track info card is open should be consumed, got %v", result)
	}
	if a.mode != modeNormal {
		t.Error("mode after Escape should be modeNormal")
	}
	if a.tv.GetFocus() != a.queue.table {
		t.Errorf("focus after Escape = %T, want the originally-focused Queue table", a.tv.GetFocus())
	}
}

func TestRefreshNowPlayingUpdatesTrackInfoLive(t *testing.T) {
	a := newTestApp()
	a.trackInfo.render(mpdclient.Song{Title: "Old Track", Artist: "Old Artist"}, mpdclient.Status{})
	if got := a.trackInfo.identity.GetText(true); !strings.Contains(got, "Old Track") {
		t.Fatalf("setup: expected initial render to contain %q, got %q", "Old Track", got)
	}

	// refreshNowPlaying itself needs a live client; exercise the same
	// call it makes so this stays a pure/no-MPD test.
	a.trackInfo.render(mpdclient.Song{Title: "New Track", Artist: "New Artist"}, mpdclient.Status{})
	got := a.trackInfo.identity.GetText(true)
	if strings.Contains(got, "Old Track") {
		t.Errorf("card still shows the old track after re-render: %q", got)
	}
	if !strings.Contains(got, "New Track") {
		t.Errorf("card = %q, want it to contain the newly rendered track %q", got, "New Track")
	}
}

// TestRenderTrackInfoShowsPlayingTrackNotSelection: the 'i' card follows
// App.targetSong like every other track-level Queue action.
func TestRenderTrackInfoShowsPlayingTrackNotSelection(t *testing.T) {
	a := newTestApp()
	a.queue.songs = []mpdclient.Song{
		{ID: 1, Title: "Playing", File: "artist/playing.mp3"},
		{ID: 2, Title: "Other", File: "artist/other.mp3"},
	}
	a.queue.render(1)
	a.queue.table.Select(queueHeaderRows+1, 0)
	a.currentSong = a.queue.songs[0]
	a.currentStatus = mpdclient.Status{State: mpdclient.StatePlay, SongID: 1, Bitrate: 320}

	a.renderTrackInfo()

	got := a.trackInfo.identity.GetText(true)
	if !strings.Contains(got, "Playing") {
		t.Errorf("card = %q, want it to show the playing track", got)
	}
	if !strings.Contains(got, "320kbps") {
		t.Errorf("card = %q, want the live audio quality for the playing track", got)
	}
}

// TestRenderTrackInfoFallsBackToSelectionWhenStopped: with playback
// stopped the card describes whatever you've scrolled to, instead of the
// bare "Nothing playing" it used to show -- but without attributing any
// live decoder numbers to it, since nothing is decoding.
func TestRenderTrackInfoFallsBackToSelectionWhenStopped(t *testing.T) {
	a := newTestApp()
	a.queue.songs = []mpdclient.Song{
		{ID: 1, Title: "Resume point", File: "artist/resume.mp3"},
		{ID: 2, Title: "Selected", File: "artist/selected.mp3"},
	}
	a.queue.render(-1)
	a.queue.table.Select(queueHeaderRows+1, 0)
	a.currentSong = a.queue.songs[0]
	a.currentStatus = mpdclient.Status{State: mpdclient.StateStop, SongID: 1, Bitrate: 320}

	a.renderTrackInfo()

	got := a.trackInfo.identity.GetText(true)
	if !strings.Contains(got, "Selected") {
		t.Errorf("card = %q, want it to show the selected track while stopped", got)
	}
	if strings.Contains(got, "kbps") {
		t.Errorf("card = %q, want no live audio quality for a merely-selected track", got)
	}
}

// TestRenderTrackInfoNothingPlayingWithEmptyQueue: no playback and no
// selection is still the original "Nothing playing" card.
func TestRenderTrackInfoNothingPlayingWithEmptyQueue(t *testing.T) {
	a := newTestApp()
	a.currentStatus = mpdclient.Status{State: mpdclient.StateStop}

	a.renderTrackInfo()

	if got := a.trackInfo.identity.GetText(true); !strings.Contains(got, "Nothing playing") {
		t.Errorf("card = %q, want \"Nothing playing\"", got)
	}
}

func TestTrackInfoNavigation(t *testing.T) {
	a := newTestApp()
	a.queue.songs = []mpdclient.Song{
		{ID: 1, Title: "Track One", File: "artist/track1.mp3"},
		{ID: 2, Title: "Track Two", File: "artist/track2.mp3"},
		{ID: 3, Title: "Track Three", File: "artist/track3.mp3"},
	}
	a.queue.render(1)
	a.currentSong = a.queue.songs[0]
	a.currentStatus = mpdclient.Status{State: mpdclient.StatePlay, SongID: 1}

	a.openTrackInfo()

	// Nothing has navigated yet, so the card follows the playing track
	// through targetSong rather than pinning a snapshot of it.
	if a.trackInfo.inspectSong != nil {
		t.Fatalf("inspectSong = %v on open, want nil until j/k moves", a.trackInfo.inspectSong)
	}
	if song, ok := a.inspectedSong(); !ok || song.Title != "Track One" {
		t.Fatalf("inspectedSong() = %v, %v on open, want Track One", song, ok)
	}

	// Navigate down with j
	evJ := tcell.NewEventKey(tcell.KeyRune, 'j', tcell.ModNone)
	if ret := a.globalInputCapture(evJ); ret != nil {
		t.Errorf("globalInputCapture('j') returned %v, want nil", ret)
	}
	if a.trackInfo.inspectSong == nil || a.trackInfo.inspectSong.Title != "Track Two" {
		t.Errorf("expected inspected track to be Track Two after 'j', got %v", a.trackInfo.inspectSong)
	}
	if row, _ := a.queue.table.GetSelection(); row != queueHeaderRows+1 {
		t.Errorf("queue selection row = %d, want %d", row, queueHeaderRows+1)
	}

	// Navigate down with KeyDown
	evDown := tcell.NewEventKey(tcell.KeyDown, 0, tcell.ModNone)
	if ret := a.globalInputCapture(evDown); ret != nil {
		t.Errorf("globalInputCapture(KeyDown) returned %v, want nil", ret)
	}
	if a.trackInfo.inspectSong == nil || a.trackInfo.inspectSong.Title != "Track Three" {
		t.Errorf("expected inspected track to be Track Three after KeyDown, got %v", a.trackInfo.inspectSong)
	}

	// Navigate up with k
	evK := tcell.NewEventKey(tcell.KeyRune, 'k', tcell.ModNone)
	if ret := a.globalInputCapture(evK); ret != nil {
		t.Errorf("globalInputCapture('k') returned %v, want nil", ret)
	}
	if a.trackInfo.inspectSong == nil || a.trackInfo.inspectSong.Title != "Track Two" {
		t.Errorf("expected inspected track to be Track Two after 'k', got %v", a.trackInfo.inspectSong)
	}

	// Navigate up with KeyUp
	evUp := tcell.NewEventKey(tcell.KeyUp, 0, tcell.ModNone)
	if ret := a.globalInputCapture(evUp); ret != nil {
		t.Errorf("globalInputCapture(KeyUp) returned %v, want nil", ret)
	}
	if a.trackInfo.inspectSong == nil || a.trackInfo.inspectSong.Title != "Track One" {
		t.Errorf("expected inspected track to be Track One after KeyUp, got %v", a.trackInfo.inspectSong)
	}

	// Close overlay with Esc
	evEsc := tcell.NewEventKey(tcell.KeyEscape, 0, tcell.ModNone)
	if ret := a.globalInputCapture(evEsc); ret != nil {
		t.Errorf("globalInputCapture(Esc) returned %v, want nil", ret)
	}
	// The stale inspectSong pointer is deliberately not cleared on close
	// -- it is simply no longer consulted, which no close path can
	// forget to do. What matters is that the card is back to following
	// the playing track.
	if song, ok := a.inspectedSong(); !ok || song.Title != "Track One" {
		t.Errorf("inspectedSong() = %v, %v after close, want the playing track (Track One)", song, ok)
	}
	if a.mode != modeNormal {
		t.Errorf("expected mode to be modeNormal after close, got %v", a.mode)
	}
}

// TestCardStaysInsideQueueBorders pins the fix for the card painting
// over the Queue panel's own border: positionOverQueue clamps against
// the panel's inner rect, not its outer one, so the card's last row can
// no longer land on the bottom border row (nor its last column on the
// right border column, which is what a narrow panel used to do).
func TestCardStaysInsideQueueBorders(t *testing.T) {
	cases := []struct{ x, y, w, h int }{
		{0, 0, 100, 40},  // roomy: the 40% anchor has somewhere to go
		{50, 3, 100, 40}, // offset panel, the case that exposed this
		{0, 0, 100, 24},  // short: card taller than the panel
		{0, 0, 60, 40},   // narrow: card wider than the right half
		{0, 0, 30, 12},   // both
		{0, 0, 10, 6},    // degenerate
	}
	for _, tc := range cases {
		a := newTestAppWithMetaDB(t)
		a.queue.table.SetRect(tc.x, tc.y, tc.w, tc.h)
		a.trackInfo.positionOverQueue()

		ix, iy, iw, ih := a.queue.table.GetInnerRect()
		cx, cy, cw, ch := a.trackInfo.GetRect()
		if ch == 0 || cw == 0 {
			continue // nothing drawn, nothing to overlap
		}
		if cx < ix || cx+cw > ix+iw {
			t.Errorf("panel %v: card x %d..%d escapes the interior %d..%d",
				tc, cx, cx+cw-1, ix, ix+iw-1)
		}
		if cy < iy || cy+ch > iy+ih {
			t.Errorf("panel %v: card y %d..%d escapes the interior %d..%d",
				tc, cy, cy+ch-1, iy, iy+ih-1)
		}
	}
}

// TestOpenTrackInfoLeavesQueueCursorAlone: pressing 'i' inspects, it
// does not navigate. It used to snap the Queue cursor onto the playing
// track and never put it back, losing the user's place in the queue.
func TestOpenTrackInfoLeavesQueueCursorAlone(t *testing.T) {
	a := newTestApp()
	a.queue.songs = make([]mpdclient.Song, 20)
	for i := range a.queue.songs {
		a.queue.songs[i] = mpdclient.Song{ID: i + 1, Title: "T", File: fmt.Sprintf("f%d.mp3", i)}
	}
	a.queue.render(3)
	a.queue.table.Select(15+queueHeaderRows, 0) // user is browsing row 15
	a.currentSong = a.queue.songs[2]            // track 3 is playing
	a.currentStatus = mpdclient.Status{State: mpdclient.StatePlay, SongID: 3}

	a.openTrackInfo()

	if row, _ := a.queue.table.GetSelection(); row != 15+queueHeaderRows {
		t.Errorf("queue selection row = %d after 'i', want it left at %d", row, 15+queueHeaderRows)
	}
	// The card still shows the playing track, as it always did.
	if song, ok := a.inspectedSong(); !ok || song.ID != 3 {
		t.Errorf("inspectedSong() = %v, %v, want the playing track (id 3)", song, ok)
	}
}

// TestTrackInfoFollowsPlaybackUntilNavigated: with the card open and
// nothing navigated, a track change still moves the card on. A snapshot
// taken at open time froze it on the track that had been playing then.
func TestTrackInfoFollowsPlaybackUntilNavigated(t *testing.T) {
	a := newTestApp()
	a.queue.songs = []mpdclient.Song{
		{ID: 1, Title: "First", File: "a.mp3"},
		{ID: 2, Title: "Second", File: "b.mp3"},
	}
	a.queue.render(1)
	a.currentSong = a.queue.songs[0]
	a.currentStatus = mpdclient.Status{State: mpdclient.StatePlay, SongID: 1}
	a.openTrackInfo()

	// MPD advances to the next track.
	a.currentSong = a.queue.songs[1]
	a.currentStatus = mpdclient.Status{State: mpdclient.StatePlay, SongID: 2}
	a.renderTrackInfo()
	if got := a.trackInfo.identity.GetText(true); !strings.Contains(got, "Second") {
		t.Errorf("card = %q, want it to have followed playback to Second", got)
	}

	// Once navigated, it pins: playback moving on must not yank the card
	// away from the track being inspected.
	a.trackInfo.handleNav(-1)
	if song, _ := a.inspectedSong(); song.Title != "First" {
		t.Fatalf("after k, inspected = %q, want First", song.Title)
	}
	a.currentSong = a.queue.songs[1]
	a.renderTrackInfo()
	if got := a.trackInfo.identity.GetText(true); !strings.Contains(got, "First") {
		t.Errorf("card = %q, want it pinned to the inspected track", got)
	}
}

// TestTrackInfoNavStartsFromShownTrack: the first j steps off the track
// the card is showing, not off wherever the Queue cursor was parked.
func TestTrackInfoNavStartsFromShownTrack(t *testing.T) {
	a := newTestApp()
	a.queue.songs = []mpdclient.Song{
		{ID: 1, Title: "One", File: "a.mp3"},
		{ID: 2, Title: "Two", File: "b.mp3"},
		{ID: 3, Title: "Three", File: "c.mp3"},
		{ID: 4, Title: "Four", File: "d.mp3"},
	}
	a.queue.render(2)
	a.queue.table.Select(3+queueHeaderRows, 0) // cursor parked on "Four"
	a.currentSong = a.queue.songs[1]           // but "Two" is playing
	a.currentStatus = mpdclient.Status{State: mpdclient.StatePlay, SongID: 2}
	a.openTrackInfo()

	a.trackInfo.handleNav(1)
	if song, _ := a.inspectedSong(); song.Title != "Three" {
		t.Errorf("after j, inspected = %q, want Three (one past the shown track)", song.Title)
	}
}

// TestTrackInfoScrollStaysInRange: j/k while expanded may not run
// scrollOffset outside [0, maxScroll], even held down past either end.
func TestTrackInfoScrollStaysInRange(t *testing.T) {
	a := newTestAppWithMetaDB(t)
	a.queue.table.SetRect(0, 0, 120, 44)
	a.queue.songs = []mpdclient.Song{{ID: 1, Title: "T", File: "a.mp3"}}
	a.queue.render(1)
	a.openTrackInfo()
	a.trackInfo.positionOverQueue()
	_, _, _, innerH := a.trackInfo.GetInnerRect()

	a.trackInfo.toggleExpanded()
	for i := 0; i < 200; i++ {
		a.trackInfo.handleNav(1)
	}
	max := a.trackInfo.maxScroll(innerH)
	if a.trackInfo.scrollOffset != max {
		t.Errorf("scrollOffset after scrolling to the end = %d, want %d", a.trackInfo.scrollOffset, max)
	}
	for i := 0; i < 200; i++ {
		a.trackInfo.handleNav(-1)
	}
	if a.trackInfo.scrollOffset != 0 {
		t.Errorf("scrollOffset after scrolling back = %d, want 0", a.trackInfo.scrollOffset)
	}

	// Collapsing resets it, so reopening never starts mid-list.
	a.trackInfo.handleNav(1)
	a.trackInfo.toggleExpanded()
	if a.trackInfo.scrollOffset != 0 {
		t.Errorf("scrollOffset after collapsing = %d, want 0", a.trackInfo.scrollOffset)
	}
}

// TestQueueIndexOfPrefersID: the same file can sit at several queue
// positions, so the id is what identifies an entry; the path is only a
// fallback for a song that has no id (a Library selection).
func TestQueueIndexOfPrefersID(t *testing.T) {
	a := newTestApp()
	a.queue.songs = []mpdclient.Song{
		{ID: 1, File: "dup.mp3"},
		{ID: 2, File: "other.mp3"},
		{ID: 3, File: "dup.mp3"},
	}
	if got := a.queue.indexOf(mpdclient.Song{ID: 3, File: "dup.mp3"}); got != 2 {
		t.Errorf("indexOf(id 3) = %d, want 2 (not the first row sharing its path)", got)
	}
	if got := a.queue.indexOf(mpdclient.Song{File: "other.mp3"}); got != 1 {
		t.Errorf("indexOf(no id) = %d, want 1 via the path fallback", got)
	}
	if got := a.queue.indexOf(mpdclient.Song{File: "absent.mp3"}); got != -1 {
		t.Errorf("indexOf(absent) = %d, want -1", got)
	}
}
