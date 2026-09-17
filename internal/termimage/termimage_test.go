package termimage

import (
	"image"
	"image/color"
	"strings"
	"testing"
)

// These three moved here with the code they cover when it was extracted
// from internal/albumart, which used to be its only caller.

func TestSupported(t *testing.T) {
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
			if got := Supported(); got != tc.want {
				t.Errorf("Supported() = %v, want %v (TERM=%q KITTY_WINDOW_ID=%q)", got, tc.want, tc.term, tc.windowID)
			}
		})
	}
}

// TestHalfBlocksDimensions pins the geometry the fallback relies
// on: one character row per two pixel rows, and one character per pixel
// column, so the art fits the width it was asked for.
func TestHalfBlocksDimensions(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 16, 16))
	for y := 0; y < 16; y++ {
		for x := 0; x < 16; x++ {
			img.Set(x, y, color.RGBA{R: 200, G: 100, B: 50, A: 255})
		}
	}

	out := HalfBlocks(img, 10, 4)
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

// TestHalfBlocksUsesSpaceForTransparency covers the alpha case:
// a fully transparent image must render as blank space, not as opaque
// black blocks over the whole panel.
func TestHalfBlocksUsesSpaceForTransparency(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 8, 8)) // zero value: fully transparent

	out := HalfBlocks(img, 4, 2)
	if strings.Contains(out, "▀") {
		t.Errorf("a fully transparent image rendered half-blocks: %q", out)
	}
	if !strings.Contains(out, " ") {
		t.Error("a fully transparent image rendered no spaces")
	}
}
