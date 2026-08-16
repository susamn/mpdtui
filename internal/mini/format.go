package mini

import (
	"strconv"
	"strings"
	"time"
)

// formatDuration, progressBar, and ratingStars intentionally duplicate
// the small formatting logic in internal/ui rather than importing it:
// mini's output is plain text (no tview color tags) and this mode
// exists specifically to have no dependency on the panel-rendering code
// (see DEPENDENCY.md).

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
	return strings.Repeat("=", filled) + ">" + strings.Repeat(" ", width-filled)
}

// ratingStars renders a 0-5 rating as filled/empty star glyphs.
func ratingStars(rating int) string {
	return strings.Repeat("★", rating) + strings.Repeat("☆", 5-rating)
}

// padOrTruncate returns s truncated with a trailing "..." if it's
// longer than width runes, or right-padded with spaces to exactly width
// runes if shorter. Every line inside box's border must be exactly the
// same rune width for the right-hand border to line up, regardless of
// each line's own natural length (a long track title, an empty stats
// line, etc).
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

// box wraps lines in a border, each padded/truncated to exactly
// contentWidth runes (see padOrTruncate) first so every row -- and the
// top/bottom border -- ends up the same total width regardless of what
// each line actually contains.
func box(lines []string, contentWidth int) []string {
	top := "┌" + strings.Repeat("─", contentWidth+2) + "┐"
	bottom := "└" + strings.Repeat("─", contentWidth+2) + "┘"
	out := make([]string, 0, len(lines)+2)
	out = append(out, top)
	for _, l := range lines {
		out = append(out, "│ "+padOrTruncate(l, contentWidth)+" │")
	}
	out = append(out, bottom)
	return out
}
