package mini

import (
	"strings"
	"testing"
	"time"
)

func TestFormatDuration(t *testing.T) {
	cases := []struct {
		d    time.Duration
		want string
	}{
		{0, "0:00"},
		{-5 * time.Second, "0:00"},
		{9 * time.Second, "0:09"},
		{65 * time.Second, "1:05"},
		{600 * time.Second, "10:00"},
	}
	for _, tc := range cases {
		if got := formatDuration(tc.d); got != tc.want {
			t.Errorf("formatDuration(%v) = %q, want %q", tc.d, got, tc.want)
		}
	}
}

func TestProgressBar(t *testing.T) {
	if got, want := progressBar(0, 100*time.Second, 10), strings.Repeat("░", 10); got != want {
		t.Errorf("progressBar(0, 100s, 10) = %q, want %q", got, want)
	}
	if got, want := progressBar(50*time.Second, 100*time.Second, 10), strings.Repeat("█", 5)+strings.Repeat("░", 5); got != want {
		t.Errorf("progressBar(50%%, 10) = %q, want %q", got, want)
	}
	if got, want := progressBar(100*time.Second, 100*time.Second, 10), strings.Repeat("█", 10); got != want {
		t.Errorf("progressBar(100%%, 10) = %q, want %q", got, want)
	}
	// duration <= 0 -> ratio 0, not a division by zero panic.
	if got, want := progressBar(5*time.Second, 0, 10), strings.Repeat("░", 10); got != want {
		t.Errorf("progressBar with zero duration = %q, want %q", got, want)
	}
	// width <= 0 defaults to 12.
	if got := progressBar(0, 100*time.Second, 0); len([]rune(got)) != 12 {
		t.Errorf("progressBar with width<=0 defaulted to length %d, want 12", len([]rune(got)))
	}
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

func TestPadOrTruncatePadsShortStrings(t *testing.T) {
	if got, want := padOrTruncate("hi", 5), "hi   "; got != want {
		t.Errorf("padOrTruncate(%q, 5) = %q, want %q", "hi", got, want)
	}
}

func TestPadOrTruncateLeavesExactLengthAlone(t *testing.T) {
	s := strings.Repeat("x", 5)
	if got := padOrTruncate(s, 5); got != s {
		t.Errorf("padOrTruncate at exact width changed: got %q, want %q", got, s)
	}
}

func TestPadOrTruncateTruncatesLongStrings(t *testing.T) {
	s := strings.Repeat("x", 20)
	got := padOrTruncate(s, 10)
	want := strings.Repeat("x", 7) + "..."
	if got != want {
		t.Errorf("padOrTruncate(20 x's, 10) = %q, want %q", got, want)
	}
	if len([]rune(got)) != 10 {
		t.Errorf("truncated length = %d runes, want exactly 10", len([]rune(got)))
	}
}

// TestPadOrTruncateIsRuneSafe guards the same multi-byte-character risk
// internal/ui's truncateWithEllipsis test does: byte-slicing instead of
// rune-slicing would corrupt a trailing non-ASCII character.
func TestPadOrTruncateIsRuneSafe(t *testing.T) {
	s := strings.Repeat("é", 20) // 2 bytes per rune in UTF-8
	got := padOrTruncate(s, 10)
	want := strings.Repeat("é", 7) + "..."
	if got != want {
		t.Errorf("padOrTruncate(20 é's, 10) = %q, want %q", got, want)
	}
}

func TestPlainWidthIgnoresNothingSinceSegmentsHoldNoAnsiYet(t *testing.T) {
	segs := []segment{{text: "abc", fg: ansiTrackColor}, {text: "de"}}
	if got := plainWidth(segs); got != 5 {
		t.Errorf("plainWidth = %d, want 5", got)
	}
}

// TestRenderLineColorsAndPadsToVisibleWidth is the core correctness
// property renderLine exists for: the *rendered* (color-code-included)
// string is longer than width in raw bytes/runes, but its *visible*
// width -- what renderLine pads to -- must still be exactly width, or
// the box border would drift out of alignment on any colored line.
func TestRenderLineColorsAndPadsToVisibleWidth(t *testing.T) {
	segs := []segment{{text: "AB", fg: ansiTrackColor}, {text: "cd"}}
	got := renderLine(segs, 10)
	want := ansiTrackColor + "AB" + ansiReset + "cd" + "      " // 6 trailing spaces: 10 - 4
	if got != want {
		t.Errorf("renderLine = %q, want %q", got, want)
	}
}

func TestRenderLineExactWidthNoTrailingPadding(t *testing.T) {
	segs := []segment{{text: "AB", fg: ansiTrackColor}, {text: "cd"}}
	got := renderLine(segs, 4)
	want := ansiTrackColor + "AB" + ansiReset + "cd"
	if got != want {
		t.Errorf("renderLine at exact width = %q, want %q", got, want)
	}
}

// TestRenderLineFallsBackToPlainWhenTooLong guards the narrow-terminal
// edge case: rather than truncating mid-segment and leaving a colored
// escape code unclosed, renderLine drops color entirely and truncates
// the plain concatenated text instead.
func TestRenderLineFallsBackToPlainWhenTooLong(t *testing.T) {
	segs := []segment{{text: "hello", fg: ansiTrackColor}, {text: " world"}}
	got := renderLine(segs, 8)
	want := padOrTruncate("hello world", 8)
	if got != want {
		t.Errorf("renderLine (over-width) = %q, want %q (plain, no color)", got, want)
	}
	if strings.Contains(got, "\x1b[") {
		t.Errorf("renderLine (over-width) = %q, want no ANSI escape codes", got)
	}
}

// TestBoxWrapsLinesWithConsistentWidth covers box() itself, which now
// just frames already width-prepared lines (padOrTruncate/renderLine's
// job, exercised separately) rather than padding them itself.
func TestBoxWrapsLinesWithConsistentWidth(t *testing.T) {
	lines := box([]string{padOrTruncate("short", 20), padOrTruncate("a longer line here", 20)}, 20)
	if len(lines) != 4 { // top border + 2 content + bottom border
		t.Fatalf("box() returned %d lines, want 4", len(lines))
	}
	width := len([]rune(lines[0]))
	for i, l := range lines {
		if got := len([]rune(l)); got != width {
			t.Errorf("line %d width = %d, want %d (same as the border)", i, got, width)
		}
	}
	if !strings.HasPrefix(lines[0], "┌") || !strings.HasSuffix(lines[0], "┐") {
		t.Errorf("top border = %q, want it framed with ┌...┐", lines[0])
	}
	if !strings.HasPrefix(lines[len(lines)-1], "└") || !strings.HasSuffix(lines[len(lines)-1], "┘") {
		t.Errorf("bottom border = %q, want it framed with └...┘", lines[len(lines)-1])
	}
	for _, l := range lines[1 : len(lines)-1] {
		if !strings.HasPrefix(l, "│ ") || !strings.HasSuffix(l, " │") {
			t.Errorf("content line = %q, want it framed with %q/%q", l, "│ ", " │")
		}
	}
}
