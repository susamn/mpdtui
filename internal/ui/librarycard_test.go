package ui

import (
	"strings"
	"testing"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"mpdtui/internal/libraryscan"
	"mpdtui/internal/metadata"
	"mpdtui/internal/mpdclient"
)

// loadedSnapshot is a snapshot with every section in, so a test can
// change one thing and assert on that one thing.
func loadedSnapshot() *librarySnapshot {
	return &librarySnapshot{
		stats: mpdclient.LibraryStats{
			Tracks: 8261, Artists: 2197, Albums: 2069, Playlists: 215,
			Playtime: 38*24*time.Hour + 19*time.Hour + 3*time.Minute,
			Updated:  time.Date(2026, 9, 20, 22, 36, 0, 0, time.Local),
		},
		statsOK: true,
		totals: metadata.Totals{
			Tracks: 1781, Rated: 1300, Stars: 4550, Played: 1420, Plays: 1762,
			Marked: 380, Tagged: 1, Bookmarks: 38, BookmarkedTracks: 35,
			MarkReasons: 11, Tags: 3,
		},
		totalsOK: true,
		scan: libraryscan.Counts{
			Tracks: 8261, Dirs: 900, Synced: 5107, Plain: 5005,
			Both: 4773, WithAny: 5339, Orphans: 4, Stories: 137, Images: 412,
		},
		scanOK:     true,
		falloutsOK: true,
		recent: []mpdclient.RecentTrack{{
			Song:         mpdclient.Song{Artist: "Sade", Title: "Cherish the Day"},
			LastModified: time.Date(2026, 9, 19, 8, 0, 0, 0, time.Local),
		}},
		recentOK: true,
		takenAt:  time.Date(2026, 9, 21, 7, 51, 48, 0, time.Local),
	}
}

func TestRenderLibrarySnapshotShowsEverySection(t *testing.T) {
	got := renderLibrarySnapshot(loadedSnapshot(), "/music", true)

	for _, want := range []string{
		"Collection", "8,261", "2,069", "2,197", "215", "38d 19h 3m", "2026-09-20 22:36",
		"Lyrics and stories", "5,339 of 8,261", "(64%)", "5,107", "5,005", "4,773", "137", "412",
		"Local metadata", "1,300", "(avg 3.5)", "1,420", "1,762", "38", "on 35 track(s)",
		"380", "/ 11 reason(s)", "/ 3 tag(s)", "1,781",
		"Playlist fallouts", "Recently added", "2026-09-19", "Sade - Cherish the Day",
		"as of 07:51:48", "refresh",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("card is missing %q:\n%s", want, got)
		}
	}
}

func TestRenderLibrarySnapshotSaysWhatIsStillCounting(t *testing.T) {
	got := renderLibrarySnapshot(&librarySnapshot{inFlight: true}, "/music", true)

	if n := strings.Count(got, "counting..."); n < 4 {
		t.Errorf("card names %d sections as counting, want one per unfinished section:\n%s", n, got)
	}
	// No snapshot has ever completed, so there is no "as of" to show.
	if strings.Contains(got, "as of") {
		t.Errorf("card claims an age before the first scan finished:\n%s", got)
	}
}

func TestRenderLibrarySnapshotReportsAFailureInPlaceOfNumbers(t *testing.T) {
	got := renderLibrarySnapshot(&librarySnapshot{failure: "mpd is unreachable"}, "/music", true)

	if !strings.Contains(got, "mpd is unreachable") {
		t.Errorf("card does not show the failure:\n%s", got)
	}
	if strings.Contains(got, "counting...") {
		t.Errorf("card still claims to be counting after a failure:\n%s", got)
	}
}

func TestRenderLibrarySnapshotWithoutMusicDirOrMetadata(t *testing.T) {
	got := renderLibrarySnapshot(loadedSnapshot(), "", false)

	if !strings.Contains(got, "music_dir is not configured") {
		t.Errorf("card does not explain the missing music_dir:\n%s", got)
	}
	if !strings.Contains(got, "track_metadata is off") {
		t.Errorf("card does not explain the inactive database:\n%s", got)
	}
	// The sidecar numbers must not appear at all -- a scan that could
	// not run has no answer, which is not the same as zero.
	if strings.Contains(got, "5,339") {
		t.Errorf("card shows sidecar counts with no music_dir:\n%s", got)
	}
}

func TestRenderLibrarySnapshotFlagsUnreadableStories(t *testing.T) {
	s := loadedSnapshot()
	s.scan.Unreadable = 3
	got := renderLibrarySnapshot(s, "/music", true)

	if !strings.Contains(got, "Unreadable") || !strings.Contains(got, "3 story file(s)") {
		t.Errorf("card does not report unreadable stories:\n%s", got)
	}

	s.scan.Unreadable = 0
	if got := renderLibrarySnapshot(s, "/music", true); strings.Contains(got, "Unreadable") {
		t.Errorf("card shows an Unreadable row when there is nothing to report:\n%s", got)
	}
}

func TestRenderLibraryFalloutsNamesTheWorstAndCountsTheRest(t *testing.T) {
	s := loadedSnapshot()
	s.fallouts = mpdclient.PlaylistFalloutReport{Playlists: 215, Entries: 24669, Missing: 30}
	for i := range libraryCardFallouts + 2 {
		s.fallouts.Affected = append(s.fallouts.Affected, mpdclient.PlaylistFallout{
			Name: "list " + string(rune('a'+i)), Total: 100, Missing: make([]string, 3),
		})
	}

	got := renderLibrarySnapshot(s, "/music", true)

	if !strings.Contains(got, "30") || !strings.Contains(got, "24,669") || !strings.Contains(got, "215") {
		t.Errorf("card does not report the fallout totals:\n%s", got)
	}
	if !strings.Contains(got, "list a") {
		t.Errorf("card does not name the worst playlist:\n%s", got)
	}
	if strings.Contains(got, "list g") {
		t.Errorf("card names more than %d playlists:\n%s", libraryCardFallouts, got)
	}
	if !strings.Contains(got, "and 2 more playlist(s)") {
		t.Errorf("card does not count the playlists it left out:\n%s", got)
	}
}

func TestRenderLibraryFalloutsOnAnIntactCollection(t *testing.T) {
	s := loadedSnapshot()
	s.fallouts = mpdclient.PlaylistFalloutReport{Playlists: 215, Entries: 24669}

	got := renderLibrarySnapshot(s, "/music", true)

	if !strings.Contains(got, "every one of 24,669 entries across 215 playlists resolves") {
		t.Errorf("card does not say the playlists are intact:\n%s", got)
	}
}

func TestRenderLibraryRecentWhenMPDReportsNoTimes(t *testing.T) {
	s := loadedSnapshot()
	s.recent = nil

	got := renderLibrarySnapshot(s, "/music", true)

	if !strings.Contains(got, "MPD reported no modification times") {
		t.Errorf("card does not explain an empty recently-added list:\n%s", got)
	}
}

// A long track name must not push the card's own text past its border.
func TestRenderLibraryRecentClipsLongNames(t *testing.T) {
	s := loadedSnapshot()
	s.recent = []mpdclient.RecentTrack{{
		Song:         mpdclient.Song{Artist: strings.Repeat("artist ", 20), Title: "x"},
		LastModified: time.Now(),
	}}
	s.fallouts = mpdclient.PlaylistFalloutReport{
		Playlists: 1, Entries: 1, Missing: 1,
		Affected: []mpdclient.PlaylistFallout{
			{Name: strings.Repeat("name ", 30), Total: 1, Missing: []string{"gone.mp3"}},
		},
	}

	for i, line := range strings.Split(renderLibrarySnapshot(s, "/music", true), "\n") {
		if w := tview.TaggedStringWidth(line); w > libraryCardWidth-2 {
			t.Errorf("line %d is %d cells wide, wider than the card's %d:\n%s", i, w, libraryCardWidth-2, line)
		}
	}
}

func TestLibraryCardHeightIsBounded(t *testing.T) {
	if got := libraryCardHeight("one line", libraryCardMaxHeight); got != libraryCardMinHeight {
		t.Errorf("libraryCardHeight(short) = %d, want the floor %d", got, libraryCardMinHeight)
	}
	long := strings.Repeat("a line\n", 200)
	if got := libraryCardHeight(long, libraryCardMaxHeight); got != libraryCardMaxHeight {
		t.Errorf("libraryCardHeight(long) = %d, want the cap %d", got, libraryCardMaxHeight)
	}
	if got := libraryCardHeight(long, 12); got != 12 {
		t.Errorf("libraryCardHeight(long, 12) = %d, want the caller's cap", got)
	}
	// In between, the height is the content's -- counting the blank
	// lines a section break leaves behind.
	text := strings.Repeat("a line\n", 15) + "\n\n"
	if got, want := libraryCardHeight(text, libraryCardMaxHeight), 15+2; got != want {
		t.Errorf("libraryCardHeight(15 lines) = %d, want %d", got, want)
	}
}

// On a short terminal the card must stop two rows short of the screen
// rather than filling it, so its footer is not what falls off the
// bottom.
func TestLibraryCardCapFollowsTheTerminal(t *testing.T) {
	a, _ := newLibraryCardApp(t)

	// Nothing drawn yet: the static cap stands, so the card is sized
	// by its content rather than by a rect that means nothing.
	a.root.SetRect(0, 0, 0, 0)
	if got := a.libraryCardCap(); got != libraryCardMaxHeight {
		t.Errorf("libraryCardCap() undrawn = %d, want the static cap %d", got, libraryCardMaxHeight)
	}

	a.root.SetRect(0, 0, 100, 24)
	if got := a.libraryCardCap(); got != 22 {
		t.Errorf("libraryCardCap() on 24 rows = %d, want 22", got)
	}

	a.root.SetRect(0, 0, 100, 60)
	if got := a.libraryCardCap(); got != libraryCardMaxHeight {
		t.Errorf("libraryCardCap() on 60 rows = %d, want the static cap %d", got, libraryCardMaxHeight)
	}

	// A terminal too short even for the floor still gets the floor --
	// a card shorter than that is unreadable, and tview clips it.
	a.root.SetRect(0, 0, 100, 5)
	if got := a.libraryCardCap(); got != libraryCardMinHeight {
		t.Errorf("libraryCardCap() on 5 rows = %d, want the floor %d", got, libraryCardMinHeight)
	}
}

func TestLibraryCardNumberHelpers(t *testing.T) {
	for in, want := range map[int]string{0: "0", 7: "7", 999: "999", 1000: "1,000", 24669: "24,669", 1234567: "1,234,567", -4321: "-4,321"} {
		if got := comma(in); got != want {
			t.Errorf("comma(%d) = %q, want %q", in, got, want)
		}
	}
	if got := percent(0, 0); got != "" {
		t.Errorf("percent(0, 0) = %q, want nothing rather than a misleading 0%%", got)
	}
	if got := percent(1, 2); !strings.Contains(got, "50%") {
		t.Errorf("percent(1, 2) = %q, want 50%%", got)
	}
	if got := average(0, 0); got != "" {
		t.Errorf("average(0, 0) = %q, want nothing", got)
	}
	if got := average(9, 2); !strings.Contains(got, "4.5") {
		t.Errorf("average(9, 2) = %q, want 4.5", got)
	}
	if got := bookmarkCount(metadata.Totals{}); got != "0" {
		t.Errorf("bookmarkCount(none) = %q, want a bare 0", got)
	}
	if got := bookmarkCount(metadata.Totals{Bookmarks: 3, BookmarkedTracks: 2}); !strings.Contains(got, "on 2 track(s)") {
		t.Errorf("bookmarkCount = %q, want the track count alongside", got)
	}
	if got := catalogCount(0, 5, "tag"); !strings.Contains(got, "/ 5 tag(s)") {
		t.Errorf("catalogCount = %q, want the catalog size alongside", got)
	}
}

func TestLongDurationAndStamp(t *testing.T) {
	for _, tc := range []struct {
		in   time.Duration
		want string
	}{
		{0, "-"},
		{-time.Hour, "-"},
		{90 * time.Second, "1m"},
		{2*time.Hour + 5*time.Minute, "2h 5m"},
		{38*24*time.Hour + 19*time.Hour + 3*time.Minute, "38d 19h 3m"},
	} {
		if got := longDuration(tc.in); got != tc.want {
			t.Errorf("longDuration(%v) = %q, want %q", tc.in, got, tc.want)
		}
	}

	if got := stamp(time.Time{}); got != "-" {
		t.Errorf("stamp(zero) = %q, want %q", got, "-")
	}
	when := time.Date(2026, 9, 20, 22, 36, 0, 0, time.Local)
	if got := stamp(when); got != "2026-09-20 22:36" {
		t.Errorf("stamp = %q", got)
	}
}

func TestClipMarksWhatItCut(t *testing.T) {
	if got := clip("short", 20); got != "short" {
		t.Errorf("clip(fits) = %q, want it untouched", got)
	}
	got := clip("a much longer line than fits", 10)
	if tview.TaggedStringWidth(got) > 10 || !strings.HasSuffix(got, "\u2026") {
		t.Errorf("clip = %q, want at most 10 cells ending in an ellipsis", got)
	}
	if got := clip("anything", 1); got != "anything" {
		t.Errorf("clip(_, 1) = %q, want the input back rather than a lone ellipsis", got)
	}
}

// --- The card as an overlay ---------------------------------------------

func newLibraryCardApp(t *testing.T) (*App, *fakeMPD) {
	t.Helper()
	f := &fakeMPD{
		stats:  mpdclient.LibraryStats{Tracks: 3, Artists: 2, Albums: 2, Playlists: 1},
		files:  []string{"a/one.mp3", "a/two.mp3", "b/three.mp3"},
		recent: []mpdclient.RecentTrack{{Song: mpdclient.Song{Title: "One"}, LastModified: time.Now()}},
		fallouts: mpdclient.PlaylistFalloutReport{
			Playlists: 1, Entries: 2, Missing: 1,
			Affected: []mpdclient.PlaylistFallout{{Name: "road trip", Total: 2, Missing: []string{"gone.mp3"}}},
		},
	}
	a := newFakeApp(t, f)
	a.musicDir = t.TempDir()
	return a, f
}

func TestLibraryCardOpensAndFillsIn(t *testing.T) {
	a, _ := newLibraryCardApp(t)

	a.globalInputCapture(runeKey('M'))

	if !a.pages.HasPage(libraryCardPageName) {
		t.Fatal("'M' did not open the Library card")
	}
	if a.tv.GetFocus() != a.libraryCard {
		t.Error("the Library card did not take focus")
	}
	// The section flags and the redraw happen in one applyToUI closure,
	// so waiting on the drawn card rather than on the flags is what
	// makes this deterministic from outside that goroutine.
	waitFor(t, func() bool { return strings.Contains(a.libraryCard.GetText(true), "road trip") })

	text := a.libraryCard.GetText(true)
	for _, want := range []string{"Collection", "road trip", "One"} {
		if !strings.Contains(text, want) {
			t.Errorf("card is missing %q once the scans landed:\n%s", want, text)
		}
	}
	if !a.librarySnap.falloutsOK || a.librarySnap.inFlight {
		t.Errorf("snapshot = %+v, want every section in and nothing in flight", a.librarySnap)
	}
}

func TestLibraryCardClosesOnItsOwnKeyAndEsc(t *testing.T) {
	for _, key := range []*tcell.EventKey{runeKey('M'), tcell.NewEventKey(tcell.KeyEscape, 0, tcell.ModNone)} {
		a, _ := newLibraryCardApp(t)
		a.globalInputCapture(runeKey('M'))
		waitFor(t, func() bool { return librarySnapshotSettled(a) })

		a.globalInputCapture(key)

		if a.pages.HasPage(libraryCardPageName) {
			t.Errorf("the card is still open after %v", key.Key())
		}
		if a.mode != modeNormal {
			t.Error("the app is still in overlay mode")
		}
	}
}

// Transport keys stay live while the card is open, like the story card
// and the lyrics viewer: it is something you read while music plays.
func TestLibraryCardKeepsTransportKeysLive(t *testing.T) {
	a, f := newLibraryCardApp(t)
	a.globalInputCapture(runeKey('M'))
	waitFor(t, func() bool { return librarySnapshotSettled(a) })

	a.globalInputCapture(runeKey('n'))

	if !f.did("next") {
		t.Error("'n' did not skip while the Library card was open")
	}
	if !a.pages.HasPage(libraryCardPageName) {
		t.Error("a transport key closed the card")
	}
}

func TestLibraryCardRefreshKeyRescans(t *testing.T) {
	a, f := newLibraryCardApp(t)
	a.globalInputCapture(runeKey('M'))
	waitFor(t, func() bool { return librarySnapshotSettled(a) })
	before := countCalls(f, "RecentTracks(8)")

	a.globalInputCapture(runeKey('r'))
	waitFor(t, func() bool { return countCalls(f, "RecentTracks(8)") > before })

	if a.librarySnap.takenAt.IsZero() {
		t.Error("the rescan left no timestamp")
	}
}

// Reopening the card inside libraryCardFresh reuses the last scan --
// several whole-library round-trips are not worth paying for twice in
// the same minute.
func TestLibraryCardReusesAFreshSnapshot(t *testing.T) {
	a, f := newLibraryCardApp(t)
	a.globalInputCapture(runeKey('M'))
	waitFor(t, func() bool { return librarySnapshotSettled(a) })
	a.closeOverlay()
	scans := countCalls(f, "RecentTracks(8)")

	a.globalInputCapture(runeKey('M'))

	if got := countCalls(f, "RecentTracks(8)"); got != scans {
		t.Errorf("reopening rescanned (%d scans, want %d)", got, scans)
	}

	// ...and a snapshot older than libraryCardFresh is rescanned on
	// the next open, without needing 'r'.
	a.closeOverlay()
	a.librarySnap.takenAt = time.Now().Add(-2 * libraryCardFresh)
	a.globalInputCapture(runeKey('M'))
	waitFor(t, func() bool { return countCalls(f, "RecentTracks(8)") > scans })
}

func TestLibraryCardRefreshWhileCountingSaysSo(t *testing.T) {
	a, _ := newLibraryCardApp(t)
	a.globalInputCapture(runeKey('M'))
	a.librarySnap.inFlight = true

	a.handleLibraryCardRefresh()

	if got := a.hintBar.GetText(true); !strings.Contains(got, "already counting") {
		t.Errorf("hint bar = %q, want the in-flight scan reported", got)
	}
}

func TestLibraryCardReportsAFailedScan(t *testing.T) {
	f := &fakeMPD{err: errTest}
	a := newFakeApp(t, f)
	a.musicDir = t.TempDir()

	a.globalInputCapture(runeKey('M'))
	waitFor(t, func() bool { return strings.Contains(a.libraryCard.GetText(true), errTest.Error()) })

	if got := a.libraryCard.GetText(true); !strings.Contains(got, errTest.Error()) {
		t.Errorf("card = %q, want the failure on it", got)
	}
}

// A scan that lands after the card is closed must not reopen it.
func TestApplyLibrarySnapshotDoesNothingWhenTheCardIsClosed(t *testing.T) {
	a, _ := newLibraryCardApp(t)
	a.globalInputCapture(runeKey('M'))
	waitFor(t, func() bool { return librarySnapshotSettled(a) })
	a.closeOverlay()

	a.applyLibrarySnapshot()

	if a.pages.HasPage(libraryCardPageName) {
		t.Error("a late scan result reopened the card")
	}
}

func TestRenderLibraryCardWithNoCardOpen(t *testing.T) {
	a, _ := newLibraryCardApp(t)

	if got := a.renderLibraryCard(); got != libraryCardMinHeight {
		t.Errorf("renderLibraryCard() with no card = %d, want the floor %d", got, libraryCardMinHeight)
	}
	// handleLibraryCardRefresh before the card has ever opened has no
	// snapshot to refresh and must not panic.
	a.handleLibraryCardRefresh()
}

func TestLibraryCardWithoutTrackMetadata(t *testing.T) {
	a, _ := newLibraryCardApp(t)
	a.globalInputCapture(runeKey('M'))
	waitFor(t, func() bool { return librarySnapshotSettled(a) })

	if got := a.libraryCard.GetText(true); !strings.Contains(got, "track_metadata is off") {
		t.Errorf("card = %q, want the inactive database explained", got)
	}
}

func TestLibraryCardHintsAndHelp(t *testing.T) {
	a, _ := newLibraryCardApp(t)
	a.globalInputCapture(runeKey('M'))

	if got := a.hintBar.GetText(true); !strings.Contains(got, "rescan") {
		t.Errorf("hint bar = %q, want the card's own keys", got)
	}
	if !strings.Contains(helpText, "library card") {
		t.Error("the help overlay does not document 'M'")
	}
}

// countCalls counts how many times the fake recorded one command.
func countCalls(f *fakeMPD, call string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, c := range f.calls {
		if c == call {
			n++
		}
	}
	return n
}

// librarySnapshotSettled reports whether both background scans have
// landed *and* been drawn -- the footer stops saying "counting..." only
// in the same closure that clears inFlight.
func librarySnapshotSettled(a *App) bool {
	return !a.librarySnap.inFlight && !strings.Contains(a.libraryCard.GetText(true), "counting...")
}

func TestLibraryCardShowsTheLocalDatabaseTotals(t *testing.T) {
	f := &fakeMPD{stats: mpdclient.LibraryStats{Tracks: 1}}
	a := newFakeAppWithMetaDB(t, f)
	if err := a.metaDB.Rate("a/one.mp3", 4); err != nil {
		t.Fatalf("Rate: %v", err)
	}

	a.globalInputCapture(runeKey('M'))
	waitFor(t, func() bool { return librarySnapshotSettled(a) })

	got := a.libraryCard.GetText(true)
	if strings.Contains(got, "track_metadata is off") {
		t.Errorf("card calls the database inactive while it is open:\n%s", got)
	}
	if !strings.Contains(got, "(avg 4.0)") {
		t.Errorf("card does not show the rating average:\n%s", got)
	}
}

func TestLibraryCardReportsAFailedDatabaseQuery(t *testing.T) {
	f := &fakeMPD{stats: mpdclient.LibraryStats{Tracks: 1}}
	a := newFakeAppWithMetaDB(t, f)
	if err := a.metaDB.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	a.globalInputCapture(runeKey('M'))
	waitFor(t, func() bool { return a.librarySnap.failure != "" })

	if a.librarySnap.totalsOK {
		t.Error("the snapshot claims totals it could not read")
	}
}

// A second refresh while one is already running must join it rather
// than start a second set of whole-library scans on the same
// connection.
func TestRefreshLibrarySnapshotIgnoresASecondStart(t *testing.T) {
	a, f := newLibraryCardApp(t)
	a.globalInputCapture(runeKey('M'))
	waitFor(t, func() bool { return librarySnapshotSettled(a) })
	scans := countCalls(f, "RecentTracks(8)")

	a.librarySnap.inFlight = true
	a.refreshLibrarySnapshot(true)

	if got := countCalls(f, "RecentTracks(8)"); got != scans {
		t.Errorf("a second refresh started while one was in flight (%d scans, want %d)", got, scans)
	}
}

// A value wide enough to reach the second column still leaves the two
// pairs apart rather than running them together.
func TestWriteLibraryPairKeepsAGapAfterAWideValue(t *testing.T) {
	var b strings.Builder
	writeLibraryPair(&b, "Label", strings.Repeat("9", 40), "Other", "1")

	line := strings.TrimRight(b.String(), "\n")
	if !strings.Contains(line, "9  [::b]Other") {
		t.Errorf("writeLibraryPair = %q, want at least two spaces before the second label", line)
	}
}
