package mini

import (
	"strconv"
	"strings"
	"time"
)

// formatDuration and progressBar intentionally duplicate the small
// formatting logic in internal/ui rather than importing it: mini's line
// is plain text (no tview color tags) and this mode exists specifically
// to have no dependency on the panel-rendering code (see DEPENDENCY.md).

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
