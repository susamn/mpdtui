package ui

import (
	"fmt"
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

// VolumeColor interpolates a tview hex color tag value (no brackets)
// from WhatsApp green (0%) through gold (50%) to red (100%), so a
// displayed volume percentage communicates its level at a glance --
// explicit request ("the volume percentage value with diff color from
// green to all the way to red"). Green/gold reuse the same colors
// already established elsewhere for the same concepts (queueTitleColor,
// queueRatingColor). Returns "" for an unknown volume (v < 0, VolText's
// own "?" case), letting the caller skip coloring it entirely.
func VolumeColor(v int) string {
	if v < 0 {
		return ""
	}
	if v > 100 {
		v = 100
	}
	type rgb struct{ r, g, b int }
	green := rgb{0x25, 0xD3, 0x66}
	gold := rgb{0xFF, 0xD7, 0x00}
	red := rgb{0xDC, 0x35, 0x45}

	from, to, t := green, gold, float64(v)/50
	if v > 50 {
		from, to, t = gold, red, float64(v-50)/50
	}
	r := int(float64(from.r) + t*float64(to.r-from.r))
	g := int(float64(from.g) + t*float64(to.g-from.g))
	b := int(float64(from.b) + t*float64(to.b-from.b))
	return fmt.Sprintf("#%02X%02X%02X", r, g, b)
}

// VolumeText renders v as "N%" (or "?%" if unknown), colored via
// VolumeColor -- falls back to plain, uncolored text for an unknown
// volume, where VolumeColor returns "".
func VolumeText(v int) string {
	text := VolText(v) + "%"
	color := VolumeColor(v)
	if color == "" {
		return text
	}
	return fmt.Sprintf("[%s]%s[-]", color, text)
}

// FlagText renders label + a bold, colored on/off value -- WhatsApp
// green when on, red when off (explicit request: "red being off and
// whatsapp green being on... make the value fonts bold"), for the
// Now Playing bar's repeat/random/single/consume flags.
func FlagText(label string, on bool) string {
	color := "red"
	if on {
		color = "#25D366"
	}
	return fmt.Sprintf("%s [%s::b]%s[-:-:-]", label, color, OnOff(on))
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

// yearFromDate extracts just the year from MPD's "Date" tag, which is
// often already just a year ("1992") but sometimes a full date
// ("1992-05-15") -- takes the first 4 characters either way, so a Year
// column/field never has to show more than that. Shorter values (an
// unusually terse or malformed Date, or "") pass through unchanged rather
// than panicking on the slice.
func yearFromDate(date string) string {
	if len(date) > 4 {
		return date[:4]
	}
	return date
}
