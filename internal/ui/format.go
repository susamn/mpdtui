package ui

import (
	"strconv"
	"strings"
	"time"

	"mpdtui/internal/mpdclient"
)

// FormatDuration renders d as "m:ss". Exported so it's testable from
// internal/ui/tests without a terminal.
func FormatDuration(d time.Duration) string {
	if d <= 0 {
		return "0:00"
	}
	total := int(d.Seconds())
	return strconv.Itoa(total/60) + ":" + pad2(total%60)
}

func pad2(n int) string {
	if n < 10 {
		return "0" + strconv.Itoa(n)
	}
	return strconv.Itoa(n)
}

// ProgressBar renders a width-wide block-character bar for elapsed/duration.
func ProgressBar(elapsed, duration time.Duration, width int) string {
	if width <= 0 {
		width = 20
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

// StateGlyph renders a single-character transport indicator for s.
func StateGlyph(s mpdclient.State) string {
	switch s {
	case mpdclient.StatePlay:
		return "▶"
	case mpdclient.StatePause:
		return "⏸"
	default:
		return "■"
	}
}

// OnOff renders b as "on"/"off".
func OnOff(b bool) string {
	if b {
		return "on"
	}
	return "off"
}

// VolText renders an MPD volume value, where -1 means unknown.
func VolText(v int) string {
	if v < 0 {
		return "?"
	}
	return strconv.Itoa(v)
}

// TrackFormat derives a track's container/codec label (MP3, FLAC, M4A,
// WMA, ...) from its file extension, ignoring any dots in the directory
// portion of the path. Returns "" if the file has no usable extension.
func TrackFormat(file string) string {
	if i := strings.LastIndexByte(file, '/'); i >= 0 {
		file = file[i+1:]
	}
	i := strings.LastIndexByte(file, '.')
	if i <= 0 || i == len(file)-1 {
		return ""
	}
	return strings.ToUpper(file[i+1:])
}
