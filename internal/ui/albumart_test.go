package ui

import "testing"

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
