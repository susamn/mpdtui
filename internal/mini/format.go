package mini

import (
	"strconv"
	"strings"
	"time"
)

// formatDuration, progressBar, and ratingStars intentionally duplicate
// the small formatting logic in internal/ui rather than importing it:
// this mode exists specifically to have no dependency on the
// panel-rendering code (see DEPENDENCY.md) -- it renders via bare ANSI
// escape sequences of its own (see the ansi* constants below) rather
// than tcell/tview.

func formatDuration(d time.Duration) string {
	if d <= 0 {
		return "0:00"
	}
	total := int(d.Seconds())
	s := total % 60
	sec := strconv.Itoa(s)
	if s < 10 {
		sec = "0" + sec
	}
	return strconv.Itoa(total/60) + ":" + sec
}

// progressBar renders as solid/empty Unicode blocks (█/░), not the
// "="/">"-style ASCII bar this used to be -- explicitly asked for
// ("use a diff progressbar").
func progressBar(elapsed, duration time.Duration, width int) string {
	if width <= 0 {
		width = 12
	}
	ratio := 0.0
	if duration > 0 {
		ratio = elapsed.Seconds() / duration.Seconds()
	}
	if ratio < 0 {
		ratio = 0
	}
	if ratio > 1 {
		ratio = 1
	}
	filled := int(ratio * float64(width))
	if filled > width {
		filled = width
	}
	return strings.Repeat("█", filled) + strings.Repeat("░", width-filled)
}

// ratingStars renders a 0-5 rating as filled/empty star glyphs.
func ratingStars(rating int) string {
	return strings.Repeat("★", rating) + strings.Repeat("☆", 5-rating)
}

// ansi* are raw 24-bit-color escape sequences, matching internal/ui's
// own color choices for the same concepts as closely as makes sense
// here: ansiTrackGreen is queueTitleColor's exact RGB (WhatsApp green),
// ansiRatingGold is queueRatingColor's, and ansiStatsSkyBlue is
// tcell.ColorSkyblue's. ansiBarCyan has no full-UI equivalent to match
// (the Queue table has no progress bar) -- picked to read as clearly
// distinct from the other three. Raw escapes rather than tcell.Style
// because this package renders via bare terminal control sequences, not
// tcell/tview (see this file's own top-of-file doc comment).
const (
	ansiReset        = "\x1b[0m"
	ansiTrackGreen   = "\x1b[38;2;37;211;102m"
	ansiRatingGold   = "\x1b[38;2;255;215;0m"
	ansiStatsSkyBlue = "\x1b[38;2;135;206;235m"
	ansiBarCyan      = "\x1b[38;2;0;229;229m"
)

// segment is one colored (or plain, if fg == "") run of text within a
// line -- a line mixing color and plain text (e.g. the progress line's
// plain "[", colored bar, plain "] 1:23/4:56  vol 80%") is built as a
// []segment rather than one pre-colored string, since padding/
// truncation (renderLine) has to reason about each piece's own visible
// width separately from any ANSI codes wrapped around it.
type segment struct {
	text string
	fg   string
}

func (s segment) render() string {
	if s.fg == "" {
		return s.text
	}
	return s.fg + s.text + ansiReset
}

// plainWidth is the combined visible (rune) width of segs, ignoring any
// color -- ANSI escape codes are zero-width on screen but do add to a
// Go string's rune count, so every width calculation in this file
// always operates on segments' plain text, never a string that might
// already have escape codes embedded in it.
func plainWidth(segs []segment) int {
	n := 0
	for _, s := range segs {
		n += len([]rune(s.text))
	}
	return n
}

// renderLine concatenates segs' rendered (colored where set) text, then
// pads with plain spaces to exactly width visible runes -- every line
// inside box's border must end up the same visible width for the
// right-hand border to line up, regardless of what each line contains
// or how many of its segments are colored.
//
// If the combined plain text is longer than width (a narrow terminal, a
// long track title), this falls back to plain, truncated text
// (padOrTruncate) rather than truncating mid-segment and threading
// color codes through the cut -- a rare edge case where correctness
// (right border still lines up) matters more than keeping every color
// in a squeezed line.
func renderLine(segs []segment, width int) string {
	if width <= 0 {
		return ""
	}
	total := plainWidth(segs)
	if total > width {
		var plain strings.Builder
		for _, s := range segs {
			plain.WriteString(s.text)
		}
		return padOrTruncate(plain.String(), width)
	}
	var b strings.Builder
	for _, s := range segs {
		b.WriteString(s.render())
	}
	b.WriteString(strings.Repeat(" ", width-total))
	return b.String()
}

// padOrTruncate returns s truncated with a trailing "..." if it's
// longer than width runes, or right-padded with spaces to exactly width
// runes if shorter. Plain-text-only counterpart to renderLine, used
// directly for uncolored lines and as renderLine's own narrow-terminal
// fallback.
func padOrTruncate(s string, width int) string {
	if width <= 0 {
		return ""
	}
	runes := []rune(s)
	if len(runes) > width {
		if width <= 3 {
			return string(runes[:width])
		}
		return string(runes[:width-3]) + "..."
	}
	return s + strings.Repeat(" ", width-len(runes))
}

// box wraps already width-prepared lines (see renderLine -- each must
// already be exactly contentWidth *visible* runes, colored or not) in a
// border.
func box(lines []string, contentWidth int) []string {
	top := "┌" + strings.Repeat("─", contentWidth+2) + "┐"
	bottom := "└" + strings.Repeat("─", contentWidth+2) + "┘"
	out := make([]string, 0, len(lines)+2)
	out = append(out, top)
	for _, l := range lines {
		out = append(out, "│ "+l+" │")
	}
	out = append(out, bottom)
	return out
}
