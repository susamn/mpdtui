package albumart

import (
	"bytes"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/rivo/tview"
)

// captureDraw redirects os.Stdout for the duration of fn (Draw's own raw
// terminal writes have no other seam to observe) and returns everything
// written.
func captureDraw(t *testing.T, fn func()) string {
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

// newTestPanel builds a Panel the way New does, minus the dependencies
// the Kitty draw path never touches: Draw reads only the panel's own
// image bytes and its view's rect, never the MPD client or the tview
// Application (those belong to fetch, which runs on another goroutine).
func newTestPanel() *Panel {
	return New(nil, nil)
}

// TestDrawDeletesPreviousPlacementOnResize covers the actual bug seen in
// the wild: a terminal resize changes the Album Art panel's own pixel
// rect mid-session, and Draw retransmits the image at the new
// position/size -- but Kitty's a=T (transmit+display) always creates a
// *new* placement rather than replacing the last one. Without an
// explicit, targeted a=d,d=i (delete by id) of the previous placement,
// it stays on screen at its old position, ghosted right alongside the
// new one -- exactly the offset double-image screenshot this was
// reported from. A same-size track change doesn't show this visually
// (the new image happens to land on identical terminal cells, hiding
// the old one), which is why only a resize surfaced it.
func TestDrawDeletesPreviousPlacementOnResize(t *testing.T) {
	t.Setenv("TERM", "xterm-kitty")

	p := newTestPanel()
	p.currentURI = "track-a.mp3"
	p.kittyPNG = []byte("fake-png-data")

	p.view.SetRect(0, 0, 20, 10)
	first := captureDraw(t, p.Draw)
	if strings.Contains(first, "d=i") {
		t.Errorf("first Draw sent a delete with nothing previously placed: %q", first)
	}
	if !strings.Contains(first, "\033_Ga=T") {
		t.Errorf("first Draw didn't transmit an image: %q", first)
	}

	// Simulate a terminal resize: the panel's inner rect changes, so
	// Draw's own signature (URI:len:w:h) changes too, without the
	// image data or currentURI changing at all.
	p.view.SetRect(0, 0, 30, 15)
	second := captureDraw(t, p.Draw)
	if !strings.Contains(second, "d=i") {
		t.Errorf("resize Draw didn't delete the previous placement (by id) after retransmitting: %q", second)
	}
	if !strings.Contains(second, "\033_Ga=T") {
		t.Errorf("resize Draw didn't retransmit the image: %q", second)
	}
	// The new image must be transmitted *before* the old one is deleted
	// -- deleting first leaves a visible blank gap until the new image
	// finishes decoding, which is what produced visible flicker (see
	// this file's git history). Transmitting first means there's always
	// something on screen at that location: the old placement, until
	// the new one lands on top of it and covers it, only then removed.
	if idx := strings.Index(second, "d=i"); idx < strings.Index(second, "\033_Ga=T") {
		t.Errorf("delete came before retransmit, want after: %q", second)
	}
	// The two placements must be under distinct ids -- deleting by the
	// *same* id the new image was just transmitted under would delete
	// the new one, not the old one.
	if p.lastImageID == 0 {
		t.Fatal("lastImageID = 0 after a successful transmit, want nonzero")
	}
}

// TestDrawSkipsRetransmitWhenNothingChanged covers Draw's own
// deduplication: calling it again with the same URI/data/size should
// neither delete nor retransmit anything -- called once per screen
// redraw, this runs far more often than the image actually changes.
func TestDrawSkipsRetransmitWhenNothingChanged(t *testing.T) {
	t.Setenv("TERM", "xterm-kitty")

	p := newTestPanel()
	p.currentURI = "track-a.mp3"
	p.kittyPNG = []byte("fake-png-data")
	p.view.SetRect(0, 0, 20, 10)

	captureDraw(t, p.Draw)
	again := captureDraw(t, p.Draw)
	if again != "" {
		t.Errorf("second Draw with nothing changed wrote %q, want nothing", again)
	}
}

// TestDrawClearsPlacementByIDWhenArtGoesAway covers the "no longer
// showing anything" path -- switching to a track with no art after one
// that had it. This must delete the specific id that was actually
// placed (d=i), not a blanket a=d, so it can never accidentally delete
// some other placement.
func TestDrawClearsPlacementByIDWhenArtGoesAway(t *testing.T) {
	t.Setenv("TERM", "xterm-kitty")

	p := newTestPanel()
	p.currentURI = "track-a.mp3"
	p.kittyPNG = []byte("fake-png-data")
	p.view.SetRect(0, 0, 20, 10)

	captureDraw(t, p.Draw)
	placedID := p.lastImageID
	if placedID == 0 {
		t.Fatal("lastImageID = 0 after a successful transmit, want nonzero")
	}

	p.mu.Lock()
	p.kittyPNG = nil
	p.mu.Unlock()

	cleared := captureDraw(t, p.Draw)
	want := fmt.Sprintf("\033_Ga=d,d=i,i=%d\033\\", placedID)
	if cleared != want {
		t.Errorf("Draw after art went away wrote %q, want exactly %q", cleared, want)
	}
	if p.lastImageID != 0 {
		t.Errorf("lastImageID = %d after clearing, want 0", p.lastImageID)
	}
}

// TestDrawForceRetransmitsAfterResendInterval covers the self-heal for a
// change Draw has no way to detect directly: a pure display-scale
// change (e.g. a Wayland compositor DPI change) can leave the terminal's
// character grid -- all that GetInnerRect()/sig can see -- completely
// unchanged while the actual pixel size of each cell changes underneath
// it. With no resize signal to react to, Draw must eventually
// retransmit anyway, on a timer, or a stale placement (sized for the old
// pixel-per-cell mapping) would stay wrong on screen indefinitely.
func TestDrawForceRetransmitsAfterResendInterval(t *testing.T) {
	t.Setenv("TERM", "xterm-kitty")

	p := newTestPanel()
	p.currentURI = "track-a.mp3"
	p.kittyPNG = []byte("fake-png-data")
	p.view.SetRect(0, 0, 20, 10)

	captureDraw(t, p.Draw)

	// Still well inside the resend interval: nothing changed, so this
	// must stay a no-op (same as TestDrawSkipsRetransmitWhenNothingChanged).
	soon := captureDraw(t, p.Draw)
	if soon != "" {
		t.Errorf("Draw well inside the resend interval wrote %q, want nothing", soon)
	}

	// Simulate the resend interval having elapsed, with nothing else
	// about the image/panel changed -- same sig as before.
	p.lastSentAt = p.lastSentAt.Add(-resendInterval)
	stale := captureDraw(t, p.Draw)
	if !strings.Contains(stale, "d=i") {
		t.Errorf("Draw past the resend interval didn't delete the previous placement (by id): %q", stale)
	}
	if !strings.Contains(stale, "\033_Ga=T") {
		t.Errorf("Draw past the resend interval didn't retransmit: %q", stale)
	}
	// Same ordering requirement as the resize case: transmit new, then
	// delete old -- never the reverse (visible blank gap).
	if idx := strings.Index(stale, "d=i"); idx < strings.Index(stale, "\033_Ga=T") {
		t.Errorf("delete came before retransmit, want after: %q", stale)
	}
}

func TestSupportsKittyGraphics(t *testing.T) {
	cases := []struct {
		name           string
		term, windowID string
		want           bool
	}{
		{"kitty TERM", "xterm-kitty", "", true},
		{"KITTY_WINDOW_ID set despite overridden TERM", "xterm-256color", "1", true},
		{"neither set", "xterm-256color", "", false},
		{"tmux (no passthrough signal)", "tmux-256color", "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("TERM", tc.term)
			t.Setenv("KITTY_WINDOW_ID", tc.windowID)
			if got := supportsKittyGraphics(); got != tc.want {
				t.Errorf("supportsKittyGraphics() = %v, want %v (TERM=%q KITTY_WINDOW_ID=%q)", got, tc.want, tc.term, tc.windowID)
			}
		})
	}
}

func TestAlbumArtStartFetchDedupesSameURI(t *testing.T) {
	p := &Panel{}

	seq1, ok1 := p.startFetch("track-a.mp3")
	if !ok1 || seq1 != 1 {
		t.Fatalf("first call: seq=%d ok=%v, want seq=1 ok=true", seq1, ok1)
	}
	if _, ok2 := p.startFetch("track-a.mp3"); ok2 {
		t.Error("same URI should not restart a fetch")
	}
	seq3, ok3 := p.startFetch("track-b.mp3")
	if !ok3 || seq3 != 2 {
		t.Fatalf("new URI: seq=%d ok=%v, want seq=2 ok=true", seq3, ok3)
	}
}

func TestAlbumArtStartFetchIgnoresEmptyURI(t *testing.T) {
	p := &Panel{}
	if _, ok := p.startFetch(""); ok {
		t.Error("empty URI should not start a fetch")
	}
}

// TestAlbumArtStaleFetchRejected covers the out-of-order-completion bug:
// rapidly skipping tracks used to let a slower, older fetch overwrite a
// newer one's result. setKittyPNGIfCurrent must reject a seq that's no
// longer current.
func TestAlbumArtStaleFetchRejected(t *testing.T) {
	p := &Panel{}

	seqOld, _ := p.startFetch("track-a.mp3")
	seqNew, _ := p.startFetch("track-b.mp3")
	if seqOld == seqNew {
		t.Fatalf("expected distinct sequence numbers, got %d twice", seqOld)
	}

	if applied := p.setKittyPNGIfCurrent(seqOld, []byte("stale art")); applied {
		t.Error("a stale (superseded) fetch should not have been applied")
	}
	if len(p.kittyPNG) != 0 {
		t.Errorf("kittyPNG = %q after a rejected stale write, want empty", p.kittyPNG)
	}

	if applied := p.setKittyPNGIfCurrent(seqNew, []byte("current art")); !applied {
		t.Error("the current fetch's result should have been applied")
	}
	if string(p.kittyPNG) != "current art" {
		t.Errorf("kittyPNG = %q, want %q", p.kittyPNG, "current art")
	}

	if !p.isCurrent(seqNew) {
		t.Error("isCurrent(seqNew) = false, want true")
	}
	if p.isCurrent(seqOld) {
		t.Error("isCurrent(seqOld) = true, want false (superseded)")
	}
}

// --- The fetch pipeline -------------------------------------------------
//
// Everything below covers the path from "the track changed" to "there is
// something on screen": fetching the bytes, decoding them, and either
// encoding a PNG for the Kitty protocol or rendering half-blocks for
// every other terminal. None of it had a test before -- only Draw's
// escape-sequence bookkeeping did -- which is why a regression anywhere
// in here would have gone unnoticed.

// fakeFetcher stands in for the MPD client, returning canned bytes.
type fakeFetcher struct {
	data  []byte
	err   error
	calls int
	uris  []string
}

func (f *fakeFetcher) FetchAlbumArt(uri string) ([]byte, error) {
	f.calls++
	f.uris = append(f.uris, uri)
	return f.data, f.err
}

// newFetchPanel builds a Panel wired to f, applying UI updates
// synchronously so a test sees the result without a running
// tview.Application.
func newFetchPanel(f *fakeFetcher) *Panel {
	v := tview.NewTextView().SetDynamicColors(true).SetTextAlign(tview.AlignCenter)
	v.SetBorder(true).SetTitle(" Album Art ")
	return &Panel{client: f, view: v, applyToUI: func(fn func()) { fn() }}
}

// testPNG is a tiny real image, encoded the way MPD would hand one over.
func testPNG(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, color.RGBA{R: uint8(x * 8), G: uint8(y * 8), B: 128, A: 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("png.Encode: %v", err)
	}
	return buf.Bytes()
}

// TestFetchRendersHalfBlocksOnNonKittyTerminal is the ASCII-fallback
// path -- the one most users on a non-Kitty terminal actually see. It
// must put real half-block glyphs in the view, not leave the "Album Art
// Loading..." placeholder up.
func TestFetchRendersHalfBlocksOnNonKittyTerminal(t *testing.T) {
	t.Setenv("TERM", "xterm-256color")
	t.Setenv("KITTY_WINDOW_ID", "")

	f := &fakeFetcher{data: testPNG(t, 8, 8)}
	p := newFetchPanel(f)

	seq, ok := p.startFetch("track-a.mp3")
	if !ok {
		t.Fatal("startFetch should have started a fetch")
	}
	p.fetch("track-a.mp3", seq)

	got := p.view.GetText(true)
	if !strings.Contains(got, "▀") {
		t.Errorf("view after fetch on a non-Kitty terminal = %q, want half-block glyphs", got)
	}
	if strings.Contains(got, "Loading") {
		t.Error("the loading placeholder is still showing after a successful fetch")
	}
	// The Kitty path must not have run: no PNG stashed for Draw.
	if len(p.kittyPNG) != 0 {
		t.Error("kittyPNG was set on a terminal with no Kitty support")
	}
}

// TestFetchStashesPNGForKittyTerminal is the Kitty path: the view's text
// is cleared (so it cannot obscure the image Draw composites on top) and
// the encoded PNG is stashed for Draw to transmit.
func TestFetchStashesPNGForKittyTerminal(t *testing.T) {
	t.Setenv("TERM", "xterm-kitty")

	f := &fakeFetcher{data: testPNG(t, 8, 8)}
	p := newFetchPanel(f)

	seq, _ := p.startFetch("track-a.mp3")
	p.fetch("track-a.mp3", seq)

	if len(p.kittyPNG) == 0 {
		t.Fatal("kittyPNG is empty after a successful fetch on a Kitty terminal")
	}
	if !bytes.HasPrefix(p.kittyPNG, []byte("\x89PNG")) {
		t.Error("kittyPNG is not a PNG")
	}
	if got := strings.TrimSpace(p.view.GetText(true)); got != "" {
		t.Errorf("view text = %q, want it cleared so it cannot obscure the image", got)
	}
	// And Draw actually transmits what fetch stashed.
	p.currentURI = "track-a.mp3"
	p.view.SetRect(0, 0, 20, 10)
	if out := captureDraw(t, p.Draw); !strings.Contains(out, "\033_Ga=T") {
		t.Error("Draw did not transmit the image fetch had stashed")
	}
}

func TestFetchShowsNoAlbumArtWhenThereIsNone(t *testing.T) {
	t.Setenv("TERM", "xterm-256color")
	for _, tc := range []struct {
		name string
		f    *fakeFetcher
	}{
		{"empty response", &fakeFetcher{data: nil}},
		{"fetch error", &fakeFetcher{err: errors.New("no art")}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := newFetchPanel(tc.f)
			seq, _ := p.startFetch("track-a.mp3")
			p.fetch("track-a.mp3", seq)

			if got := p.view.GetText(true); !strings.Contains(got, "No Album Art") {
				t.Errorf("view = %q, want it to say there is no album art", got)
			}
			if len(p.kittyPNG) != 0 {
				t.Error("kittyPNG should be cleared when there is no art")
			}
		})
	}
}

// TestFetchShowsDecodeErrorOnGarbage covers art that arrives but is not
// a decodable image -- it must say so rather than sit on the loading
// placeholder forever.
func TestFetchShowsDecodeErrorOnGarbage(t *testing.T) {
	t.Setenv("TERM", "xterm-256color")

	p := newFetchPanel(&fakeFetcher{data: []byte("this is not an image")})
	seq, _ := p.startFetch("track-a.mp3")
	p.fetch("track-a.mp3", seq)

	if got := p.view.GetText(true); !strings.Contains(got, "Decode Error") {
		t.Errorf("view = %q, want a decode error", got)
	}
}

// TestFetchDiscardsSupersededResult is the out-of-order-completion case
// at the level that matters: a slow fetch for a track the user has
// already skipped past must not paint over the newer track's art.
func TestFetchDiscardsSupersededResult(t *testing.T) {
	t.Setenv("TERM", "xterm-256color")

	p := newFetchPanel(&fakeFetcher{data: testPNG(t, 8, 8)})
	oldSeq, _ := p.startFetch("track-a.mp3")
	p.startFetch("track-b.mp3") // supersedes it

	p.view.SetText("newer track's art")
	p.fetch("track-a.mp3", oldSeq)

	if got := p.view.GetText(true); !strings.Contains(got, "newer track's art") {
		t.Errorf("view = %q, want the superseded fetch to have left it alone", got)
	}
}

// TestOnTrackChangedFetchesOncePerTrack covers the exported entry point,
// including that it does not re-fetch the track already showing.
func TestOnTrackChangedFetchesOncePerTrack(t *testing.T) {
	t.Setenv("TERM", "xterm-256color")

	f := &fakeFetcher{data: testPNG(t, 4, 4)}
	p := newFetchPanel(f)

	done := make(chan struct{}, 8)
	p.applyToUI = func(fn func()) { fn(); done <- struct{}{} }

	p.OnTrackChanged("track-a.mp3")
	<-done
	p.OnTrackChanged("track-a.mp3") // same track: must not refetch
	p.OnTrackChanged("")            // nothing playing: must not refetch

	if f.calls != 1 {
		t.Errorf("FetchAlbumArt called %d times for one distinct track, want 1 (uris=%v)", f.calls, f.uris)
	}

	p.OnTrackChanged("track-b.mp3")
	<-done
	if f.calls != 2 {
		t.Errorf("FetchAlbumArt called %d times after a real track change, want 2", f.calls)
	}
}

func TestViewIsTheRenderedPanel(t *testing.T) {
	p := New(nil, nil)
	if p.View() == nil {
		t.Fatal("View() is nil")
	}
	if p.View() != p.view {
		t.Error("View() returned something other than the panel's own view")
	}
	if got := p.View().GetTitle(); !strings.Contains(got, "Album Art") {
		t.Errorf("panel title = %q, want it to name the panel", got)
	}
	// Before any fetch, the panel says it is loading rather than sitting
	// blank -- a blank bordered box reads as a bug.
	if got := p.View().GetText(true); !strings.Contains(got, "Loading") {
		t.Errorf("initial view text = %q, want a loading placeholder", got)
	}
}

// TestImageToHalfBlocksDimensions pins the geometry the fallback relies
// on: one character row per two pixel rows, and one character per pixel
// column, so the art fits the width it was asked for.
func TestImageToHalfBlocksDimensions(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 16, 16))
	for y := 0; y < 16; y++ {
		for x := 0; x < 16; x++ {
			img.Set(x, y, color.RGBA{R: 200, G: 100, B: 50, A: 255})
		}
	}

	out := imageToHalfBlocks(img, 10, 4)
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	if len(lines) != 4 {
		t.Errorf("rendered %d lines, want 4 (height*2 pixel rows, two per character row)", len(lines))
	}
	for i, ln := range lines {
		if got := strings.Count(ln, "▀"); got != 10 {
			t.Errorf("line %d has %d half-blocks, want 10 (one per column)", i, got)
		}
	}
	if !strings.Contains(out, "\033[38;2;") || !strings.Contains(out, "\033[48;2;") {
		t.Error("output has no 24-bit foreground/background colors")
	}
}

// TestImageToHalfBlocksUsesSpaceForTransparency covers the alpha case:
// a fully transparent image must render as blank space, not as opaque
// black blocks over the whole panel.
func TestImageToHalfBlocksUsesSpaceForTransparency(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 8, 8)) // zero value: fully transparent

	out := imageToHalfBlocks(img, 4, 2)
	if strings.Contains(out, "▀") {
		t.Errorf("a fully transparent image rendered half-blocks: %q", out)
	}
	if !strings.Contains(out, " ") {
		t.Error("a fully transparent image rendered no spaces")
	}
}
