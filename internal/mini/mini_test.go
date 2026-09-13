package mini

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"golang.org/x/term"

	"mpdtui/internal/config"
	"mpdtui/internal/metadata"
	"mpdtui/internal/mpdclient"
	"mpdtui/internal/theme"
)

// dialOrSkip mirrors internal/ui/keys_test.go's own helper of the same
// name -- duplicated, not imported (internal/ui and internal/mini don't
// depend on each other, see DEPENDENCY.md), for the one test here that
// needs a real MPD connection.
func dialOrSkip(t *testing.T) *mpdclient.Client {
	t.Helper()
	c, err := mpdclient.Dial(config.Load())
	if err != nil {
		t.Skipf("no MPD server reachable, skipping: %v", err)
	}
	t.Cleanup(func() { c.Close() })
	return c
}

func openTestMetaDB(t *testing.T) *metadata.DB {
	t.Helper()
	db, err := metadata.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("metadata.Open: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func TestStateGlyph(t *testing.T) {
	cases := []struct {
		state mpdclient.State
		want  string
	}{
		{mpdclient.StatePlay, ">"},
		{mpdclient.StatePause, "||"},
		{mpdclient.StateStop, "[]"},
		{mpdclient.State("unknown"), "?"},
	}
	for _, tc := range cases {
		if got := stateGlyph(tc.state); got != tc.want {
			t.Errorf("stateGlyph(%q) = %q, want %q", tc.state, got, tc.want)
		}
	}
}

// plainText concatenates segs' plain (uncolored) text, for content
// assertions that don't care which parts are colored.
func plainText(segs []segment) string {
	s := ""
	for _, seg := range segs {
		s += seg.text
	}
	return s
}

func TestStatsSegmentsIsAllSkyBlue(t *testing.T) {
	segs := statsSegments(mpdclient.Status{PlaylistLength: 3}, 7)
	want := "3 track(s) in queue  ·  7 playlist(s)"
	if got := plainText(segs); got != want {
		t.Errorf("statsSegments text = %q, want %q", got, want)
	}
	for _, seg := range segs {
		if seg.fg != ansiStatsColor {
			t.Errorf("statsSegments segment %q fg = %q, want %q", seg.text, seg.fg, ansiStatsColor)
		}
	}
}

func TestNowPlayingSegmentsFallsBackWhenNothingPlaying(t *testing.T) {
	segs := nowPlayingSegments(mpdclient.Status{State: mpdclient.StateStop}, mpdclient.Song{})
	want := "[] (nothing playing)"
	if got := plainText(segs); got != want {
		t.Errorf("nowPlayingSegments(empty song) text = %q, want %q", got, want)
	}
}

func TestNowPlayingSegmentsColorsOnlyTheTrackGreen(t *testing.T) {
	segs := nowPlayingSegments(mpdclient.Status{State: mpdclient.StatePlay}, mpdclient.Song{Title: "Track"})
	want := "> Track"
	if got := plainText(segs); got != want {
		t.Errorf("nowPlayingSegments text = %q, want %q", got, want)
	}
	for _, seg := range segs {
		switch seg.text {
		case "Track":
			if seg.fg != ansiTrackColor {
				t.Errorf("track segment fg = %q, want %q", seg.fg, ansiTrackColor)
			}
		default:
			if seg.fg != "" {
				t.Errorf("non-track segment %q fg = %q, want plain (no color)", seg.text, seg.fg)
			}
		}
	}
}

func TestProgressSegmentsUnknownVolume(t *testing.T) {
	segs := progressSegments(mpdclient.Status{Elapsed: 30 * time.Second, Duration: 60 * time.Second, Volume: -1})
	// -1 means "unknown" (see mpdclient.Status), rendered as "?" rather
	// than a nonsensical "-1%".
	want := "[██████░░░░░░] 0:30/1:00  vol ?%"
	if got := plainText(segs); got != want {
		t.Errorf("progressSegments text = %q, want %q", got, want)
	}
	var barFg string
	for _, seg := range segs {
		if strings.Contains(seg.text, "█") || strings.Contains(seg.text, "░") {
			barFg = seg.fg
		}
	}
	if barFg != ansiBarColor {
		t.Errorf("progress bar segment fg = %q, want %q", barFg, ansiBarColor)
	}
}

func TestMetaSegmentsUnratedUnmarked(t *testing.T) {
	segs := metaSegments(metadata.Track{})
	want := "☆☆☆☆☆  played 0x"
	if got := plainText(segs); got != want {
		t.Errorf("metaSegments(zero-value Track) text = %q, want %q", got, want)
	}
}

func TestMetaSegmentsRatedAndMarkedColorsOnlyTheStarsGold(t *testing.T) {
	segs := metaSegments(metadata.Track{Rating: 4, PlayCount: 12, Marks: []metadata.MarkReason{{Reason: "mark for deletion"}}})
	want := "★★★★☆  played 12x  marked: mark for deletion"
	if got := plainText(segs); got != want {
		t.Errorf("metaSegments text = %q, want %q", got, want)
	}
	if segs[0].fg != ansiRatingColor {
		t.Errorf("rating segment fg = %q, want %q", segs[0].fg, ansiRatingColor)
	}
	for _, seg := range segs[1:] {
		if seg.fg != "" {
			t.Errorf("non-rating segment %q fg = %q, want plain (no color)", seg.text, seg.fg)
		}
	}
}

func TestMaybeTrackPlayCountIncrementsAtHalfway(t *testing.T) {
	db := openTestMetaDB(t)
	song := mpdclient.Song{File: "artist/track.mp3"}
	playCountedSongID := -1
	total := 200 * time.Second

	maybeTrackPlayCount(db, mpdclient.Status{SongID: 1, Duration: total, Elapsed: total * 3 / 10}, song, &playCountedSongID) // 30%
	track, err := db.Get(song.File)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if track.PlayCount != 0 {
		t.Fatalf("play count at 30%% = %d, want 0 (not counted yet)", track.PlayCount)
	}

	maybeTrackPlayCount(db, mpdclient.Status{SongID: 1, Duration: total, Elapsed: total / 2}, song, &playCountedSongID) // 50%
	track, err = db.Get(song.File)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if track.PlayCount != 1 {
		t.Errorf("play count at 50%% = %d, want 1", track.PlayCount)
	}

	// Continuing to tick past halfway must not keep incrementing.
	maybeTrackPlayCount(db, mpdclient.Status{SongID: 1, Duration: total, Elapsed: total * 9 / 10}, song, &playCountedSongID)
	track, err = db.Get(song.File)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if track.PlayCount != 1 {
		t.Errorf("play count after continuing past halfway = %d, want still 1", track.PlayCount)
	}
}

func TestMaybeTrackPlayCountCountsAgainForADifferentSongID(t *testing.T) {
	db := openTestMetaDB(t)
	song := mpdclient.Song{File: "artist/track.mp3"}
	playCountedSongID := -1
	total := 100 * time.Second

	maybeTrackPlayCount(db, mpdclient.Status{SongID: 1, Duration: total, Elapsed: total}, song, &playCountedSongID)
	maybeTrackPlayCount(db, mpdclient.Status{SongID: 2, Duration: total, Elapsed: total}, song, &playCountedSongID)

	track, err := db.Get(song.File)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if track.PlayCount != 2 {
		t.Errorf("play count across two distinct song ids = %d, want 2", track.PlayCount)
	}
}

// TestMaybeTrackPlayCountCountsAgainAfterRestartFromBeginning mirrors
// internal/ui's own test of the same name -- see its doc comment.
func TestMaybeTrackPlayCountCountsAgainAfterRestartFromBeginning(t *testing.T) {
	db := openTestMetaDB(t)
	song := mpdclient.Song{File: "artist/track.mp3"}
	playCountedSongID := -1
	total := 200 * time.Second

	maybeTrackPlayCount(db, mpdclient.Status{SongID: 7, Duration: total, Elapsed: total}, song, &playCountedSongID)
	maybeTrackPlayCount(db, mpdclient.Status{SongID: 7, Duration: total, Elapsed: 0}, song, &playCountedSongID)
	maybeTrackPlayCount(db, mpdclient.Status{SongID: 7, Duration: total, Elapsed: total}, song, &playCountedSongID)

	track, err := db.Get(song.File)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if track.PlayCount != 2 {
		t.Errorf("play count after a same-SongID restart-and-replay = %d, want 2", track.PlayCount)
	}
}

func TestMaybeTrackPlayCountNoopWithoutMetaDB(t *testing.T) {
	playCountedSongID := -1
	// Must not panic with a nil *metadata.DB.
	maybeTrackPlayCount(nil, mpdclient.Status{SongID: 1, Duration: time.Second, Elapsed: time.Second}, mpdclient.Song{File: "x"}, &playCountedSongID)
}

// TestRateCurrentTrackSavesRatingForWhateversPlayingNeedsLiveMPD is the
// one test here that needs a real MPD connection (rateCurrentTrack calls
// client.CurrentSong()) -- skipped automatically if none is reachable,
// same convention as internal/ui's own live-gated tests.
func TestRateCurrentTrackSavesRatingForWhateversPlayingNeedsLiveMPD(t *testing.T) {
	c := dialOrSkip(t)
	db := openTestMetaDB(t)

	song, err := c.CurrentSong()
	if err != nil {
		t.Fatalf("CurrentSong: %v", err)
	}
	if song.File == "" {
		t.Skip("nothing currently playing/queued on the reachable MPD server, skipping")
	}

	rateCurrentTrack(c, db, 4)

	track, err := db.Get(song.File)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if track.Rating != 4 {
		t.Errorf("rating after rateCurrentTrack(4) = %d, want 4", track.Rating)
	}
}

func TestRateCurrentTrackNoopWithoutMetaDB(t *testing.T) {
	// Must not panic with a nil client -- metaDB nil short-circuits
	// before ever touching it.
	rateCurrentTrack(nil, nil, 4)
}

// --- Keypress handling, sizing and the render pass ----------------------

// fakeController records what mini mode asked the MPD client to do, so
// the transport keys can be proved wired up without stopping or
// skipping the user's actual music.
type fakeController struct {
	calls     []string
	volume    int
	status    mpdclient.Status
	song      mpdclient.Song
	statErr   error
	songErr   error
	plErr     error
	playlists []mpdclient.Playlist

	// drawn, when set, receives once per render pass -- render asks for
	// the status first, so it is the cheapest observable "a redraw
	// happened" signal.
	drawn chan struct{}
}

func (f *fakeController) Status() (mpdclient.Status, error) {
	if f.drawn != nil {
		select {
		case f.drawn <- struct{}{}:
		default:
		}
	}
	return f.status, f.statErr
}
func (f *fakeController) CurrentSong() (mpdclient.Song, error) {
	return f.song, f.songErr
}
func (f *fakeController) TogglePlayPause() error { f.calls = append(f.calls, "toggle"); return nil }
func (f *fakeController) Stop() error            { f.calls = append(f.calls, "stop"); return nil }
func (f *fakeController) Next() error            { f.calls = append(f.calls, "next"); return nil }
func (f *fakeController) Previous() error        { f.calls = append(f.calls, "previous"); return nil }
func (f *fakeController) Playlists() ([]mpdclient.Playlist, error) {
	return f.playlists, f.plErr
}

func (f *fakeController) ChangeVolume(d int) error {
	f.calls = append(f.calls, fmt.Sprintf("volume%+d", d))
	f.volume += d
	return nil
}

func TestHandleKeyTransportKeys(t *testing.T) {
	cases := []struct {
		key  byte
		want string
	}{
		{' ', "toggle"},
		{'s', "stop"},
		{'n', "next"},
		{'p', "previous"},
		{'-', "volume-5"},
		// '=' is the same physical key as '+' without needing shift,
		// matching '-' also needing no modifier.
		{'=', "volume+5"},
	}
	for _, tc := range cases {
		t.Run(string(tc.key), func(t *testing.T) {
			f := &fakeController{}
			if quit := handleKey(f, nil, tc.key); quit {
				t.Errorf("handleKey(%q) reported quit, want it to keep running", tc.key)
			}
			if len(f.calls) != 1 || f.calls[0] != tc.want {
				t.Errorf("handleKey(%q) did %v, want exactly [%s]", tc.key, f.calls, tc.want)
			}
		})
	}
}

func TestHandleKeyQuits(t *testing.T) {
	for _, key := range []byte{'q', 3} { // 3 = Ctrl-C
		f := &fakeController{}
		if quit := handleKey(f, nil, key); !quit {
			t.Errorf("handleKey(%d) did not report quit", key)
		}
		if len(f.calls) != 0 {
			t.Errorf("handleKey(%d) also talked to the client: %v", key, f.calls)
		}
	}
}

func TestHandleKeyIgnoresUnknownKeys(t *testing.T) {
	for _, key := range []byte{'z', 'A', '\n', 0, '9'} {
		f := &fakeController{}
		if quit := handleKey(f, nil, key); quit {
			t.Errorf("handleKey(%q) reported quit, want it ignored", key)
		}
		if len(f.calls) != 0 {
			t.Errorf("handleKey(%q) did %v, want nothing", key, f.calls)
		}
	}
}

// TestHandleKeyRatingKeysNoOpWithoutMetadata covers the digits when the
// track-metadata feature is off: they must do nothing at all rather
// than reach for a nil database.
func TestHandleKeyRatingKeysNoOpWithoutMetadata(t *testing.T) {
	for _, key := range []byte{'1', '2', '3', '4', '5'} {
		f := &fakeController{song: mpdclient.Song{File: "a.mp3"}}
		if quit := handleKey(f, nil, key); quit {
			t.Errorf("handleKey(%q) reported quit", key)
		}
		// metaDB is nil, so rateCurrentTrack returns before asking the
		// client for anything.
		if len(f.calls) != 0 {
			t.Errorf("handleKey(%q) talked to the client with metadata off: %v", key, f.calls)
		}
	}
}

func TestRateCurrentTrackWritesTheRating(t *testing.T) {
	db := openTestMetaDB(t)
	f := &fakeController{song: mpdclient.Song{File: "a-r-rahman/guru/05.mp3"}}

	if quit := handleKey(f, db, '4'); quit {
		t.Fatal("a rating key reported quit")
	}

	got, err := db.Get("a-r-rahman/guru/05.mp3")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Rating != 4 {
		t.Errorf("rating = %d, want 4", got.Rating)
	}
}

func TestRateCurrentTrackIgnoresNothingPlaying(t *testing.T) {
	db := openTestMetaDB(t)
	// No current song, and a client that errors -- neither may panic or
	// write a row.
	rateCurrentTrack(&fakeController{song: mpdclient.Song{File: ""}}, db, 3)
	rateCurrentTrack(&fakeController{songErr: errors.New("offline")}, db, 3)
}

// TestContentWidthForSizesToContent covers the box sizing: it follows
// the widest line rather than always filling the terminal, so a short
// status line does not leave a lot of empty space to the right.
func TestContentWidthForSizesToContent(t *testing.T) {
	narrow := [][]segment{{{text: "short"}}}
	if got := contentWidthFor(narrow); got != miniContentMinWidth {
		t.Errorf("width for a short line = %d, want the %d minimum", got, miniContentMinWidth)
	}

	wide := [][]segment{{{text: strings.Repeat("x", 200)}}}
	if got := contentWidthFor(wide); got > miniContentMaxWidth {
		t.Errorf("width for a very long line = %d, want it clamped to %d", got, miniContentMaxWidth)
	}

	mid := [][]segment{
		{{text: strings.Repeat("y", 40)}},
		{{text: "short"}},
	}
	if got := contentWidthFor(mid); got != 40 {
		t.Errorf("width = %d, want the widest line's own width 40", got)
	}

	if got := contentWidthFor(nil); got != miniContentMinWidth {
		t.Errorf("width for no lines = %d, want the %d minimum", got, miniContentMinWidth)
	}
}

// capture redirects stdout for the duration of fn, which is the only
// seam on the block printer's own raw terminal writes.
func capture(t *testing.T, fn func()) string {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}
	old := os.Stdout
	os.Stdout = w
	fn()
	os.Stdout = old
	w.Close()
	out, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	return string(out)
}

// TestBlockPrintRedrawsInPlace covers the in-place redraw: the first
// print just writes, and every later one first moves the cursor back up
// over however many lines it last drew, so the box refreshes in place
// instead of scrolling the terminal.
func TestBlockPrintRedrawsInPlace(t *testing.T) {
	var b block

	first := capture(t, func() { b.print([]string{"one", "two", "three"}) })
	if strings.Contains(first, "\x1b[3A") || strings.Contains(first, "\x1b[2A") {
		t.Errorf("first print moved the cursor up with nothing drawn yet: %q", first)
	}
	for _, want := range []string{"one", "two", "three"} {
		if !strings.Contains(first, want) {
			t.Errorf("first print missing %q: %q", want, first)
		}
	}
	if b.lines != 3 {
		t.Errorf("block.lines = %d after printing 3 lines, want 3", b.lines)
	}

	second := capture(t, func() { b.print([]string{"a", "b", "c"}) })
	if !strings.Contains(second, "\x1b[2A") {
		t.Errorf("redraw did not move the cursor back up over the previous 3 lines: %q", second)
	}
	if !strings.Contains(second, "\x1b[K") {
		t.Errorf("redraw did not clear the old line contents: %q", second)
	}

	// Shrinking to fewer lines is tracked, so the next redraw moves up
	// the right amount rather than over-scrolling.
	capture(t, func() { b.print([]string{"only"}) })
	if b.lines != 1 {
		t.Errorf("block.lines = %d after shrinking to 1 line, want 1", b.lines)
	}
}

func TestRenderDrawsTheWholeBox(t *testing.T) {
	f := &fakeController{
		status: mpdclient.Status{State: mpdclient.StatePlay, PlaylistLength: 7, Volume: 50,
			Elapsed: 30 * time.Second, Duration: 120 * time.Second},
		song: mpdclient.Song{Title: "Ay Hairathe", Artist: "Hariharan", File: "a/b.mp3"},
	}
	var b block

	out := capture(t, func() { render(&b, f, nil, 3, nil) })

	for _, want := range []string{"Ay Hairathe", "Hariharan", "7 track(s) in queue", "3 playlist(s)"} {
		if !strings.Contains(out, want) {
			t.Errorf("render output missing %q:\n%s", want, out)
		}
	}
	// Drawn as a bordered box.
	for _, corner := range []string{"┌", "┐", "└", "┘", "│"} {
		if !strings.Contains(out, corner) {
			t.Errorf("render output is missing box character %q:\n%s", corner, out)
		}
	}
}

// TestRenderShowsClientErrorsInline covers the offline case: a failing
// client must produce a single readable line, not a broken box or a
// crash.
func TestRenderShowsClientErrorsInline(t *testing.T) {
	for _, tc := range []struct {
		name string
		f    *fakeController
	}{
		{"status fails", &fakeController{statErr: errors.New("connection refused")}},
		{"current song fails", &fakeController{songErr: errors.New("connection refused")}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var b block
			out := capture(t, func() { render(&b, tc.f, nil, 0, nil) })
			if !strings.Contains(out, "connection refused") {
				t.Errorf("render output does not report the error: %q", out)
			}
			if b.lines != 1 {
				t.Errorf("error render drew %d lines, want a single line", b.lines)
			}
		})
	}
}

// TestRenderIncludesMetadataRowWhenEnabled covers the extra row mini
// mode grows when the track-metadata feature is on.
func TestRenderIncludesMetadataRowWhenEnabled(t *testing.T) {
	db := openTestMetaDB(t)
	if err := db.Rate("a/b.mp3", 3); err != nil {
		t.Fatalf("Rate: %v", err)
	}

	f := &fakeController{
		status: mpdclient.Status{State: mpdclient.StatePlay, SongID: 1, PlaylistLength: 1},
		song:   mpdclient.Song{Title: "T", File: "a/b.mp3"},
	}
	var b block
	counted := -1

	out := capture(t, func() { render(&b, f, db, 0, &counted) })

	if !strings.Contains(out, "★") {
		t.Errorf("render with metadata enabled has no rating row:\n%s", out)
	}
	if b.lines <= 3 {
		t.Errorf("render drew %d lines, want an extra row for metadata", b.lines)
	}
}

func TestProgressBarEdgeCases(t *testing.T) {
	// A non-positive width falls back to a sensible default rather than
	// producing an empty string that would collapse the line.
	for _, w := range []int{0, -1} {
		if got := progressBar(10*time.Second, 20*time.Second, w); got == "" {
			t.Errorf("progressBar(width=%d) = empty, want the fallback width", w)
		}
	}
	// Nothing playing (zero duration) is all-empty rather than a
	// divide-by-zero or a full bar.
	if got := progressBar(0, 0, 10); got != strings.Repeat("░", 10) {
		t.Errorf("progressBar with no duration = %q, want an empty bar", got)
	}
	// Elapsed past the end clamps to full rather than overflowing.
	if got := progressBar(99*time.Second, 10*time.Second, 10); got != strings.Repeat("█", 10) {
		t.Errorf("progressBar past the end = %q, want a full bar", got)
	}
	// Negative elapsed clamps to empty.
	if got := progressBar(-5*time.Second, 10*time.Second, 10); got != strings.Repeat("░", 10) {
		t.Errorf("progressBar with negative elapsed = %q, want an empty bar", got)
	}
}

func TestPadOrTruncate(t *testing.T) {
	cases := []struct {
		in    string
		width int
		want  string
	}{
		{"abc", 5, "abc  "},
		{"abc", 3, "abc"},
		{"abcdefgh", 5, "ab..."},
		// At or below three columns there is no room for an ellipsis,
		// so it hard-cuts rather than rendering "..." and nothing else.
		{"abcdefgh", 3, "abc"},
		{"abcdefgh", 1, "a"},
		{"", 3, "   "},
		{"abc", 0, ""},
		{"abc", -1, ""},
	}
	for _, tc := range cases {
		if got := padOrTruncate(tc.in, tc.width); got != tc.want {
			t.Errorf("padOrTruncate(%q, %d) = %q, want %q", tc.in, tc.width, got, tc.want)
		}
	}
}

// TestRenderLineFallsBackToPlainTextWhenTooNarrow covers the squeeze
// case: rather than truncate mid-segment and cut through a color code,
// the whole line falls back to plain truncated text so the right-hand
// border still lines up.
func TestRenderLineFallsBackToPlainTextWhenTooNarrow(t *testing.T) {
	segs := []segment{
		{text: "aaaaaaaaaa", fg: "\x1b[38;2;255;0;0m"},
		{text: "bbbbbbbbbb", fg: "\x1b[38;2;0;255;0m"},
	}

	squeezed := renderLine(segs, 8)
	if strings.Contains(squeezed, "\x1b[38;2;") {
		t.Errorf("squeezed line kept color codes, want plain text: %q", squeezed)
	}
	if !strings.HasSuffix(squeezed, "...") {
		t.Errorf("squeezed line = %q, want it truncated with an ellipsis", squeezed)
	}

	// With room to spare, colors are kept and the line is padded to
	// exactly the requested visible width.
	roomy := renderLine(segs, 30)
	if !strings.Contains(roomy, "\x1b[38;2;") {
		t.Errorf("roomy line lost its colors: %q", roomy)
	}
	if got := plainWidth([]segment{{text: stripANSI(roomy)}}); got != 30 {
		t.Errorf("roomy line is %d visible columns, want exactly 30", got)
	}

	if got := renderLine(segs, 0); got != "" {
		t.Errorf("renderLine(width=0) = %q, want empty", got)
	}
}

// stripANSI removes SGR escape sequences so a rendered line's visible
// width can be measured.
func stripANSI(s string) string {
	for {
		i := strings.Index(s, "\x1b[")
		if i < 0 {
			return s
		}
		j := strings.IndexByte(s[i:], 'm')
		if j < 0 {
			return s
		}
		s = s[:i] + s[i+j+1:]
	}
}

func TestAnsiFG(t *testing.T) {
	if got, want := ansiFG("#ff8800"), "\x1b[38;2;255;136;0m"; got != want {
		t.Errorf("ansiFG(%q) = %q, want %q", "#ff8800", got, want)
	}
	// A malformed or empty value produces no color code at all, so the
	// segment simply renders unstyled rather than emitting garbage.
	for _, bad := range []string{"", "not-a-color", "#xyz", "ff8800"} {
		if got := ansiFG(theme.Color(bad)); got != "" {
			t.Errorf("ansiFG(%q) = %q, want no color code", bad, got)
		}
	}
}

// TestFetchPlaylistCountSurvivesAnOfflineServer pins the "show
// something rather than fail" behavior: the count is cosmetic, so an
// unreachable server reports zero instead of taking the display down.
func TestFetchPlaylistCountSurvivesAnOfflineServer(t *testing.T) {
	c, err := mpdclient.Dial(config.Config{Host: "127.0.0.1", Port: "1"})
	if err != nil {
		// Dial failed outright, which is also fine -- the point is that
		// nothing panics on an unreachable server.
		return
	}
	t.Cleanup(func() { c.Close() })
	if got := fetchPlaylistCount(c); got != 0 {
		t.Errorf("fetchPlaylistCount against an unreachable server = %d, want 0", got)
	}
}

// TestReadKeysForwardsBytesAndClosesOnEOF covers the stdin reader: every
// byte reaches the channel in order, and the channel closes when stdin
// does, which is what lets Run's own loop terminate.
func TestReadKeysForwardsBytesAndClosesOnEOF(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}
	old := os.Stdin
	os.Stdin = r
	t.Cleanup(func() { os.Stdin = old })

	go func() {
		w.Write([]byte("snq"))
		w.Close()
	}()

	ch := make(chan byte, 8)
	go readKeys(ch)

	var got []byte
	for b := range ch { // ranges until readKeys closes it
		got = append(got, b)
	}
	if string(got) != "snq" {
		t.Errorf("readKeys forwarded %q, want %q", got, "snq")
	}
}

// TestRunRequiresATerminal covers Run's own first guard. Mini mode puts
// the terminal into raw mode and redraws in place, neither of which
// means anything for a pipe, so it refuses rather than emitting escape
// sequences into whatever is capturing its output.
func TestRunRequiresATerminal(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}
	t.Cleanup(func() { r.Close(); w.Close() })

	old := os.Stdin
	os.Stdin = r
	t.Cleanup(func() { os.Stdin = old })

	err = Run(nil, nil, "")
	if err == nil {
		t.Fatal("Run with a pipe on stdin returned no error, want it to refuse")
	}
	if !strings.Contains(err.Error(), "interactive terminal") {
		t.Errorf("Run error = %q, want it to say an interactive terminal is required", err)
	}
}

// TestFetchPlaylistCountNeedsLiveMPD checks the count against a real
// server, the only place the query itself can be exercised.
func TestFetchPlaylistCountNeedsLiveMPD(t *testing.T) {
	c := dialOrSkip(t)

	got := fetchPlaylistCount(c)
	pls, err := c.Playlists()
	if err != nil {
		t.Fatalf("Playlists: %v", err)
	}
	if got != len(pls) {
		t.Errorf("fetchPlaylistCount() = %d, want %d", got, len(pls))
	}
}

// --- The loop -----------------------------------------------------------
//
// Run itself is terminal setup -- raw mode, signal handlers, an MPD
// watch -- none of which happens without a real tty. loop is where the
// behavior lives, and these drive it through its channels.

// runLoop starts loop in the background and returns a function that
// waits for it to finish.
func runLoop(t *testing.T, d loopDeps) func() {
	t.Helper()
	done := make(chan error, 1)
	go func() { done <- loop(d) }()
	return func() {
		t.Helper()
		select {
		case err := <-done:
			if err != nil {
				t.Errorf("loop returned %v, want nil", err)
			}
		case <-time.After(2 * time.Second):
			t.Fatal("loop did not return")
		}
	}
}

func playingController() *fakeController {
	return &fakeController{
		status:    mpdclient.Status{State: mpdclient.StatePlay, PlaylistLength: 2, Volume: 40},
		song:      mpdclient.Song{Title: "Ay Hairathe", Artist: "Hariharan", File: "a.mp3"},
		playlists: []mpdclient.Playlist{{Name: "Road Trip"}},
	}
}

func TestLoopQuitsOnQ(t *testing.T) {
	keys := make(chan byte, 1)
	f := playingController()

	out := capture(t, func() {
		wait := runLoop(t, loopDeps{client: f, keys: keys})
		keys <- 'q'
		wait()
	})

	if !strings.Contains(out, "Ay Hairathe") {
		t.Errorf("loop drew %q, want the current track", out)
	}
}

// TestLoopQuitsWhenStdinCloses covers the closed-keys channel: readKeys
// closes it on EOF, and the loop must exit rather than spin on a closed
// channel.
func TestLoopQuitsWhenStdinCloses(t *testing.T) {
	keys := make(chan byte)
	f := playingController()

	capture(t, func() {
		wait := runLoop(t, loopDeps{client: f, keys: keys})
		close(keys)
		wait()
	})
}

func TestLoopQuitsOnSignal(t *testing.T) {
	sig := make(chan os.Signal, 1)
	f := playingController()

	capture(t, func() {
		wait := runLoop(t, loopDeps{client: f, signals: sig})
		sig <- syscall.SIGTERM
		wait()
	})
}

// TestLoopActsOnAKeyAndKeepsGoing covers a key that is handled but is
// not a quit: the command runs and the display redraws.
func TestLoopActsOnAKeyAndKeepsGoing(t *testing.T) {
	keys := make(chan byte, 2)
	f := playingController()

	capture(t, func() {
		wait := runLoop(t, loopDeps{client: f, keys: keys})
		keys <- 'n' // next track
		keys <- 'q'
		wait()
	})

	if len(f.calls) != 1 || f.calls[0] != "next" {
		t.Errorf("did %v, want [next]", f.calls)
	}
}

// TestLoopRefetchesPlaylistCountOnStoredPlaylistEvent covers the one
// event that needs more than a redraw: the playlist count is not in the
// status, so it has to be re-queried.
func TestLoopRefetchesPlaylistCountOnStoredPlaylistEvent(t *testing.T) {
	keys := make(chan byte, 1)
	events := make(chan string, 1)
	f := playingController()

	capture(t, func() {
		wait := runLoop(t, loopDeps{client: f, keys: keys, events: events})
		f.playlists = []mpdclient.Playlist{{Name: "A"}, {Name: "B"}, {Name: "C"}}
		events <- "stored_playlist"
		// A second event with no refetch, to prove only the one kind does.
		events <- "player"
		keys <- 'q'
		wait()
	})
}

// TestLoopStopsWatchingAfterAWatchError covers the degraded path: when
// the idle connection drops, the loop drops both watch channels and
// keeps running on its ticker, rather than spinning on closed channels.
func TestLoopStopsWatchingAfterAWatchError(t *testing.T) {
	keys := make(chan byte, 1)
	events := make(chan string)
	watchErrs := make(chan error, 1)
	f := playingController()

	capture(t, func() {
		wait := runLoop(t, loopDeps{client: f, keys: keys, events: events, watchErrs: watchErrs})
		watchErrs <- errors.New("idle connection dropped")
		keys <- 'q'
		wait()
	})
}

// TestLoopHandlesClosedWatchChannels covers a watcher that closes
// rather than errors: each channel is dropped as it closes, and the
// loop carries on.
func TestLoopHandlesClosedWatchChannels(t *testing.T) {
	keys := make(chan byte, 1)
	events := make(chan string)
	watchErrs := make(chan error)
	f := playingController()

	capture(t, func() {
		wait := runLoop(t, loopDeps{client: f, keys: keys, events: events, watchErrs: watchErrs})
		close(events)
		close(watchErrs)
		keys <- 'q'
		wait()
	})
}

func TestLoopRedrawsOnItsTicker(t *testing.T) {
	keys := make(chan byte, 1)
	tick := make(chan time.Time, 1)
	f := playingController()
	f.drawn = make(chan struct{}, 8)

	out := capture(t, func() {
		wait := runLoop(t, loopDeps{client: f, keys: keys, tick: tick})
		<-f.drawn // the initial draw
		tick <- time.Now()
		<-f.drawn // the tick's draw -- waited for, so 'q' cannot race it
		keys <- 'q'
		wait()
	})

	if n := strings.Count(out, "Ay Hairathe"); n < 2 {
		t.Errorf("drew the track %d times, want at least 2 (initial plus the tick)", n)
	}
}

// TestLoopReloadsTheThemeOnSIGUSR1 covers mpdtui's own theme-reload
// signal, which an Omarchy theme-set hook sends to every running
// instance -- mini mode re-colors along with the panel UI.
func TestLoopReloadsTheThemeOnSIGUSR1(t *testing.T) {
	keys := make(chan byte, 1)
	themeSig := make(chan os.Signal, 1)
	f := playingController()

	capture(t, func() {
		wait := runLoop(t, loopDeps{client: f, keys: keys, themeSig: themeSig})
		themeSig <- syscall.SIGUSR1
		keys <- 'q'
		wait()
	})

	// The colors are resolved from the theme file; with none configured
	// that is internal/theme's default, and the vars must be populated
	// rather than left empty.
	if ansiTrackColor == "" {
		t.Error("the track color is unset after a theme reload")
	}
}

// TestFetchPlaylistCountReportsZeroOnFailure covers the "show something
// rather than fail" choice: the count is cosmetic, so a failed query
// reports zero instead of taking the whole display down.
func TestFetchPlaylistCountReportsZeroOnFailure(t *testing.T) {
	f := &fakeController{plErr: errors.New("offline")}
	if got := fetchPlaylistCount(f); got != 0 {
		t.Errorf("fetchPlaylistCount with a failing query = %d, want 0", got)
	}

	f = &fakeController{playlists: []mpdclient.Playlist{{Name: "A"}, {Name: "B"}}}
	if got := fetchPlaylistCount(f); got != 2 {
		t.Errorf("fetchPlaylistCount = %d, want 2", got)
	}
}

// TestRunOnARealTerminal covers Run's own body -- the terminal setup
// the loop tests deliberately skip: raw mode, the signal handlers, the
// key reader, the MPD watch, and restoring the terminal on the way out.
//
// Needs both a pty (for the tty checks) and a reachable MPD (Run dials
// a watch), so it skips without either.
func TestRunOnARealTerminal(t *testing.T) {
	client := dialOrSkip(t)
	ptmx := withPTYStdio(t)

	done := make(chan error, 1)
	go func() { done <- Run(client, nil, "") }()

	waitForRawMode(t, 5*time.Second)
	// Quit through the same path a user would: a byte on stdin.
	if _, err := ptmx.Write([]byte("q")); err != nil {
		t.Fatalf("writing to the pty: %v", err)
	}

	select {
	case err := <-done:
		if err != nil {
			t.Errorf("Run returned %v, want nil", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not return after 'q'")
	}
}

// TestRunRestoresTheTerminal covers the deferred term.Restore: mini
// mode puts the terminal into raw mode, and leaving it that way would
// wreck the shell it was run from.
func TestRunRestoresTheTerminal(t *testing.T) {
	client := dialOrSkip(t)
	ptmx := withPTYStdio(t)

	before, err := term.GetState(int(os.Stdin.Fd()))
	if err != nil {
		t.Skipf("cannot read the terminal state: %v", err)
	}

	done := make(chan error, 1)
	go func() { done <- Run(client, nil, "") }()
	waitForRawMode(t, 5*time.Second)
	ptmx.Write([]byte("q"))
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not return")
	}

	after, err := term.GetState(int(os.Stdin.Fd()))
	if err != nil {
		t.Fatalf("reading the terminal state back: %v", err)
	}
	if fmt.Sprintf("%+v", before) != fmt.Sprintf("%+v", after) {
		t.Error("Run left the terminal in a different state than it found it")
	}
}

// TestRunQuitsOnCtrlC covers the other way out, and the interrupt
// handler Run installs alongside it.
func TestRunQuitsOnCtrlC(t *testing.T) {
	client := dialOrSkip(t)
	ptmx := withPTYStdio(t)

	done := make(chan error, 1)
	go func() { done <- Run(client, nil, "") }()

	// Ctrl-C only reaches the key reader once raw mode has cleared
	// ISIG; before that the line discipline treats it as a signal.
	waitForRawMode(t, 5*time.Second)
	ptmx.Write([]byte{3}) // Ctrl-C, which handleKey treats as quit

	select {
	case err := <-done:
		if err != nil {
			t.Errorf("Run returned %v, want nil", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not return after Ctrl-C")
	}
}
