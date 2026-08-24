package ui

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/gdamore/tcell/v2"

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

// stateGlyphPlayColor/PauseColor/StopColor are theme-derived
// (deriveColors, from the active theme's BrightGreen/BrightYellow/
// BrightRed), see theme.go -- StateGlyphColor just selects between
// them.
var (
	stateGlyphPlayColor  string
	stateGlyphPauseColor string
	stateGlyphStopColor  string
)

// StateGlyphColor returns the tview color tag value for s's own
// StateGlyph -- a vibrant green for Play, yellow for Pause, red for
// Stop (and any other state), per explicit direction ("a bit more
// vibrant... pause: bright yellow, play: bright green, stop: bright
// red"): the glyph character itself is unchanged, only its color.
func StateGlyphColor(s mpdclient.State) string {
	switch s {
	case mpdclient.StatePlay:
		return stateGlyphPlayColor
	case mpdclient.StatePause:
		return stateGlyphPauseColor
	default:
		return stateGlyphStopColor
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

// volumeColorLow/Mid/High are the three stops VolumeColor interpolates
// between (0%/50%/100%) -- theme-derived (deriveColors, from the active
// theme's Green/Yellow/Red), see theme.go.
var (
	volumeColorLow  tcell.Color
	volumeColorMid  tcell.Color
	volumeColorHigh tcell.Color
)

// VolumeColor interpolates a tview hex color tag value (no brackets)
// from volumeColorLow (0%) through volumeColorMid (50%) to
// volumeColorHigh (100%), so a displayed volume percentage communicates
// its level at a glance -- explicit request ("the volume percentage
// value with diff color from green to all the way to red"). Returns ""
// for an unknown volume (v < 0, VolText's own "?" case), letting the
// caller skip coloring it entirely.
func VolumeColor(v int) string {
	if v < 0 {
		return ""
	}
	if v > 100 {
		v = 100
	}
	from, to, t := volumeColorLow, volumeColorMid, float64(v)/50
	if v > 50 {
		from, to, t = volumeColorMid, volumeColorHigh, float64(v-50)/50
	}
	fr, fg, fb := from.RGB()
	tr, tg, tb := to.RGB()
	r := int(float64(fr) + t*float64(tr-fr))
	g := int(float64(fg) + t*float64(tg-fg))
	b := int(float64(fb) + t*float64(tb-fb))
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

// flagOnColor/flagOffColor are theme-derived (deriveColors, from the
// active theme's Green/Red), see theme.go.
var (
	flagOnColor  string
	flagOffColor string
)

// FlagText renders label + a bold, colored on/off value -- theme green
// when on, theme red when off (explicit request: "red being off and
// whatsapp green being on... make the value fonts bold"), for the
// Now Playing bar's repeat/random/single/consume flags.
func FlagText(label string, on bool) string {
	color := flagOffColor
	if on {
		color = flagOnColor
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

// FormatAudioQuality renders MPD's live audio quality as e.g. "128kbps
// 44.1kHz/16-bit/2ch" -- audioFormat is Status.AudioFormat, MPD's raw
// "samplerate:bits:channels" status field (bits is "f" for floating
// point). Either half can be missing on its own (MPD only reports
// bitrate/audio at all once a decoder is active, and a variable-bitrate
// stream's bitrate can still take a moment to settle) -- each renders
// only what it actually has rather than an all-or-nothing blank, so
// "128kbps" alone or "44.1kHz/16-bit/2ch" alone are both valid results,
// not just the combination.
func FormatAudioQuality(bitrate int, audioFormat string) string {
	var segs []string
	if bitrate > 0 {
		segs = append(segs, strconv.Itoa(bitrate)+"kbps")
	}
	if triplet := formatAudioFormatTriplet(audioFormat); triplet != "" {
		segs = append(segs, triplet)
	}
	return strings.Join(segs, " ")
}

// formatAudioFormatTriplet renders "samplerate:bits:channels" as
// "44.1kHz/16-bit/2ch", tolerating a partially-known triplet (each
// segment renders only if MPD actually reported it) but not a malformed
// one (any shape other than exactly 3 colon-separated fields renders as
// "", since there's nothing sensible to show for it).
func formatAudioFormatTriplet(audioFormat string) string {
	parts := strings.SplitN(audioFormat, ":", 3)
	if len(parts) != 3 {
		return ""
	}
	var segs []string
	if hz, err := strconv.Atoi(parts[0]); err == nil && hz > 0 {
		segs = append(segs, formatSampleRate(hz))
	}
	if parts[1] != "" {
		bits := parts[1] + "-bit"
		if parts[1] == "f" {
			bits = "float"
		}
		segs = append(segs, bits)
	}
	if parts[2] != "" {
		segs = append(segs, parts[2]+"ch")
	}
	return strings.Join(segs, "/")
}

// formatSampleRate renders hz in kHz, trimming a trailing ".0" for exact
// multiples of 1000 (48000 -> "48kHz") but keeping one decimal place
// otherwise (44100 -> "44.1kHz") -- strconv.FormatFloat's -1 precision
// already picks the shortest exact representation, so this needs no
// explicit rounding.
func formatSampleRate(hz int) string {
	return strconv.FormatFloat(float64(hz)/1000, 'f', -1, 64) + "kHz"
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
