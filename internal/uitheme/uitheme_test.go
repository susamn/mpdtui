package uitheme

import (
	"math"
	"testing"

	"github.com/gdamore/tcell/v2"
)

// The colour math these cover moved here from internal/ui, where it used
// to sit beside the Queue gutter that is its most demanding caller. The
// gutter's own two-tier assertion stays there; this is the arithmetic
// underneath it.

func TestRecedeFromLeavesUnresolvableColorsAlone(t *testing.T) {
	// A terminal-default color has no RGB to weaken, and guessing what
	// the terminal will paint is not possible -- so it comes back
	// unchanged rather than becoming some invented shade.
	if got := RecedeFrom(tcell.ColorDefault, 1.8); got != tcell.ColorDefault {
		t.Errorf("RecedeFrom(default) = %v, want it left alone", got)
	}
}

func TestRelativeLuminanceMatchesKnownValues(t *testing.T) {
	cases := []struct {
		color tcell.Color
		want  float64
	}{
		{tcell.NewRGBColor(0, 0, 0), 0},
		{tcell.NewRGBColor(255, 255, 255), 1},
		{tcell.NewRGBColor(255, 0, 0), 0.2126},
		{tcell.NewRGBColor(0, 255, 0), 0.7152},
		{tcell.NewRGBColor(0, 0, 255), 0.0722},
	}
	for _, tc := range cases {
		if got := relativeLuminance(tc.color); math.Abs(got-tc.want) > 1e-4 {
			t.Errorf("relativeLuminance(%v) = %g, want %g", tc.color, got, tc.want)
		}
	}
	if got := relativeLuminance(tcell.ColorDefault); got >= 0 {
		t.Errorf("relativeLuminance(default) = %g, want a negative sentinel", got)
	}
}

func TestLuminanceRatioIsSymmetric(t *testing.T) {
	if a, b := luminanceRatio(0.8, 0.1), luminanceRatio(0.1, 0.8); a != b {
		t.Errorf("luminanceRatio is not symmetric: %g vs %g", a, b)
	}
	if got := luminanceRatio(0.5, 0.5); got != 1 {
		t.Errorf("luminanceRatio of equal luminances = %g, want 1", got)
	}
}
