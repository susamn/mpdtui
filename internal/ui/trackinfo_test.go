package ui

import (
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
			// Quadrant (50x30) roomier than the card: top-left anchor.
			name: "fits in the quadrant",
			px:   0, py: 0, pw: 100, ph: 60, want: 18,
			wantX: 50, wantY: 30, wantW: trackInfoCardWidth, wantH: 18,
		},
		{
			// Quadrant narrower than the card's fixed width.
			name: "narrow panel clamps the width",
			px:   0, py: 0, pw: 82, ph: 42, want: 18,
			wantX: 41, wantY: 21, wantW: 41, wantH: 18,
		},
		{
			// Taller than the quadrant (20) but not the panel (40):
			// bottom edge stays put, the card grows upwards.
			name: "expanded card grows upwards, not past the panel",
			px:   0, py: 0, pw: 100, ph: 40, want: 30,
			wantX: 50, wantY: 10, wantW: trackInfoCardWidth, wantH: 30,
		},
		{
			// Asking for more than the whole panel: clamped to it, and
			// pinned to the panel's top rather than above it.
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

func TestOpenTrackInfoTakesFocusAndPositionsInBottomRightQuadrant(t *testing.T) {
	a := newTestApp()
	a.tv.SetFocus(a.library.tree)
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
	if x != 50 || y != 30 || w != trackInfoCardWidth || h != a.trackInfo.height() {
		t.Errorf("card rect after Draw = (%d,%d,%d,%d), want (50,30,%d,%d) -- compact size, floating at the quadrant's top-left corner",
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
