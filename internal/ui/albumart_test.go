package ui

import (
	"io"
	"os"
	"strings"
	"testing"
)

// captureDraw redirects os.Stdout for the duration of fn (draw's own raw
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

// TestDrawDeletesPreviousPlacementOnResize covers the actual bug seen in
// the wild: a terminal resize changes the Album Art panel's own pixel
// rect mid-session, and draw() retransmits the image at the new
// position/size -- but Kitty's a=T (transmit+display) always creates a
// *new* placement rather than replacing the last one. Without an
// explicit a=d (delete) before the second transmit, the old placement
// stays on screen at its old position, ghosted right alongside the new
// one -- exactly the offset double-image screenshot this was reported
// from. A same-size track change doesn't show this visually (the new
// image happens to land on identical terminal cells, hiding the old
// one), which is why only a resize surfaced it.
func TestDrawDeletesPreviousPlacementOnResize(t *testing.T) {
	t.Setenv("TERM", "xterm-kitty")

	p := newAlbumArtPanel(&App{})
	p.currentURI = "track-a.mp3"
	p.kittyPNG = []byte("fake-png-data")

	p.view.SetRect(0, 0, 20, 10)
	first := captureDraw(t, p.draw)
	if strings.Contains(first, "\033_Ga=d\033\\") {
		t.Errorf("first draw() sent a delete with nothing previously placed: %q", first)
	}
	if !strings.Contains(first, "\033_Ga=T") {
		t.Errorf("first draw() didn't transmit an image: %q", first)
	}

	// Simulate a terminal resize: the panel's inner rect changes, so
	// draw()'s own signature (URI:len:w:h) changes too, without the
	// image data or currentURI changing at all.
	p.view.SetRect(0, 0, 30, 15)
	second := captureDraw(t, p.draw)
	if !strings.Contains(second, "\033_Ga=d\033\\") {
		t.Errorf("resize draw() didn't delete the previous placement before retransmitting: %q", second)
	}
	if !strings.Contains(second, "\033_Ga=T") {
		t.Errorf("resize draw() didn't retransmit the image: %q", second)
	}
	// The delete must come before the retransmit, not after -- sending
	// it after would just delete the placement this call itself made.
	if idx := strings.Index(second, "\033_Ga=d\033\\"); idx > strings.Index(second, "\033_Ga=T") {
		t.Errorf("delete came after retransmit, want before: %q", second)
	}
}

// TestDrawSkipsRetransmitWhenNothingChanged covers draw()'s own
// deduplication: calling it again with the same URI/data/size should
// neither delete nor retransmit anything -- called once per screen
// redraw, this runs far more often than the image actually changes.
func TestDrawSkipsRetransmitWhenNothingChanged(t *testing.T) {
	t.Setenv("TERM", "xterm-kitty")

	p := newAlbumArtPanel(&App{})
	p.currentURI = "track-a.mp3"
	p.kittyPNG = []byte("fake-png-data")
	p.view.SetRect(0, 0, 20, 10)

	captureDraw(t, p.draw)
	again := captureDraw(t, p.draw)
	if again != "" {
		t.Errorf("second draw() with nothing changed wrote %q, want nothing", again)
	}
}

// TestDrawForceRetransmitsAfterResendInterval covers the self-heal for a
// change draw() has no way to detect directly: a pure display-scale
// change (e.g. a Wayland compositor DPI change) can leave the terminal's
// character grid -- all that GetInnerRect()/sig can see -- completely
// unchanged while the actual pixel size of each cell changes underneath
// it. With no resize signal to react to, draw() must eventually
// retransmit anyway, on a timer, or a stale placement (sized for the old
// pixel-per-cell mapping) would stay wrong on screen indefinitely.
func TestDrawForceRetransmitsAfterResendInterval(t *testing.T) {
	t.Setenv("TERM", "xterm-kitty")

	p := newAlbumArtPanel(&App{})
	p.currentURI = "track-a.mp3"
	p.kittyPNG = []byte("fake-png-data")
	p.view.SetRect(0, 0, 20, 10)

	captureDraw(t, p.draw)

	// Still well inside the resend interval: nothing changed, so this
	// must stay a no-op (same as TestDrawSkipsRetransmitWhenNothingChanged).
	soon := captureDraw(t, p.draw)
	if soon != "" {
		t.Errorf("draw() well inside the resend interval wrote %q, want nothing", soon)
	}

	// Simulate the resend interval having elapsed, with nothing else
	// about the image/panel changed -- same sig as before.
	p.lastSentAt = p.lastSentAt.Add(-albumArtResendInterval)
	stale := captureDraw(t, p.draw)
	if !strings.Contains(stale, "\033_Ga=d\033\\") {
		t.Errorf("draw() past the resend interval didn't delete the previous placement: %q", stale)
	}
	if !strings.Contains(stale, "\033_Ga=T") {
		t.Errorf("draw() past the resend interval didn't retransmit: %q", stale)
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
	p := &albumArtPanel{}

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
	p := &albumArtPanel{}
	if _, ok := p.startFetch(""); ok {
		t.Error("empty URI should not start a fetch")
	}
}

// TestAlbumArtStaleFetchRejected covers the out-of-order-completion bug:
// rapidly skipping tracks used to let a slower, older fetch overwrite a
// newer one's result. setKittyPNGIfCurrent must reject a seq that's no
// longer current.
func TestAlbumArtStaleFetchRejected(t *testing.T) {
	p := &albumArtPanel{}

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
