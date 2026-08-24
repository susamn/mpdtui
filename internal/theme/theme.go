// Package theme resolves the color palette mpdtui renders with from a
// file on disk, so switching a desktop theme re-colors mpdtui too
// instead of leaving it stuck on a hardcoded scheme. This package
// itself has no opinion on *which* file -- that's entirely
// config-driven (see internal/config.LoadThemeFile/EnsureConfigFiles,
// wired up by cmd/mpdtui's main.go): LoadFrom just reads whatever path
// it's given, in a flat "key = \"value\"" shape modeled on Omarchy's
// own colors.toml (see parse and this repo's README for the full field
// list). Default is what EnsureConfigFiles writes out as mpdtui's own
// default color file the first time it runs, so a generic Linux system
// with no desktop theme integration at all still gets a real,
// inspectable, editable file rather than a value baked into the binary.
// It has no dependency on tcell/tview (see internal/ui, internal/picker,
// internal/mini's own independent-leaf-package convention) -- colors
// are plain "#RRGGBB" strings; each caller converts to whatever color
// type its own rendering needs.
package theme

import (
	"bufio"
	"bytes"
	"fmt"
	"os"
	"strings"
)

// Color is a "#RRGGBB" hex color string.
type Color string

// Palette is the full set of semantic colors mpdtui derives its own
// widget colors from. Field names and roles mirror Omarchy's own
// colors.toml (see Load), so a Palette parsed from that file needs no
// translation -- only the reverse: each caller picks which field plays
// which role in its own UI (e.g. Accent for a focused border, Red/
// Green/... for the handful of categorical colors like format badges).
type Palette struct {
	// Mode is "dark" or "light", as Omarchy itself classifies the
	// theme -- informational only; nothing in this package or its
	// callers currently branches on it.
	Mode string

	Accent    Color
	Selection Color
	Muted     Color

	Background        Color
	DarkBackground    Color
	DarkerBackground  Color
	LighterBackground Color

	Foreground       Color
	DarkForeground   Color
	LightForeground  Color
	BrightForeground Color

	Red, Yellow, Orange, Green, Cyan, Blue, Magenta, Brown Color

	BrightRed, BrightYellow, BrightGreen, BrightCyan, BrightBlue, BrightMagenta Color
}

// Default is mpdtui's own built-in palette -- the same fixed colors it
// used before Omarchy integration existed. Load falls back to this
// wholesale when no theme file is readable, and merges it under
// whatever a theme file does supply, so a theme missing a field (an
// older/hand-edited colors.toml, say) degrades to a sensible color
// rather than an empty one.
func Default() Palette {
	return Palette{
		Mode:      "dark",
		Accent:    "#00FF00",
		Selection: "#0000FF",
		Muted:     "#808080",

		Background:        "#000000",
		DarkBackground:    "#000000",
		DarkerBackground:  "#000000",
		LighterBackground: "#FFFFFF",

		Foreground:       "#FFFFFF",
		DarkForeground:   "#808080",
		LightForeground:  "#D3D3D3",
		BrightForeground: "#FFFFFF",

		Red:     "#DC3545",
		Yellow:  "#FFD700",
		Orange:  "#FFA500",
		Green:   "#25D366",
		Cyan:    "#00E5E5",
		Blue:    "#87CEEB",
		Magenta: "#DDA0DD",
		Brown:   "#935E6D",

		BrightRed:     "#FF0000",
		BrightYellow:  "#FFFF00",
		BrightGreen:   "#00FF00",
		BrightCyan:    "#00E5E5",
		BrightBlue:    "#87CEEB",
		BrightMagenta: "#DDA0DD",
	}
}

// LoadFrom reads path as a flat `key = "value"` file (see parse) and
// returns the resulting Palette, merged over Default() field-by-field
// so a partial file (one that only sets a few fields -- e.g. a matugen
// template someone wrote by hand, covering just accent/background/
// foreground) degrades to Default()'s values for whatever it's missing,
// rather than a zero Palette. The second return value is false when
// path is empty or unreadable, meaning every field in the returned
// Palette is Default()'s own -- practically only reachable if the file
// EnsureConfigFiles is supposed to have created got deleted or lost its
// read permission out from under a running mpdtui.
func LoadFrom(path string) (Palette, bool) {
	p := Default()

	if path == "" {
		return p, false
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return p, false
	}

	fields := parse(data)
	set := func(dst *Color, key string) {
		if v, ok := fields[key]; ok && v != "" {
			*dst = Color(v)
		}
	}
	if v, ok := fields["mode"]; ok && v != "" {
		p.Mode = v
	}
	set(&p.Accent, "accent")
	set(&p.Selection, "selection")
	set(&p.Muted, "muted")

	set(&p.Background, "background")
	set(&p.DarkBackground, "dark_background")
	set(&p.DarkerBackground, "darker_background")
	set(&p.LighterBackground, "lighter_background")

	set(&p.Foreground, "foreground")
	set(&p.DarkForeground, "dark_foreground")
	set(&p.LightForeground, "light_foreground")
	set(&p.BrightForeground, "bright_foreground")

	set(&p.Red, "red")
	set(&p.Yellow, "yellow")
	set(&p.Orange, "orange")
	set(&p.Green, "green")
	set(&p.Cyan, "cyan")
	set(&p.Blue, "blue")
	set(&p.Magenta, "magenta")
	set(&p.Brown, "brown")

	set(&p.BrightRed, "bright_red")
	set(&p.BrightYellow, "bright_yellow")
	set(&p.BrightGreen, "bright_green")
	set(&p.BrightCyan, "bright_cyan")
	set(&p.BrightBlue, "bright_blue")
	set(&p.BrightMagenta, "bright_magenta")

	return p, true
}

// Serialize renders p back into the same flat `key = "value"` shape
// LoadFrom/parse reads, field-commented for a human reading the file --
// used by internal/config.EnsureConfigFiles to write out mpdtui's own
// default color file (always Default()) the first time it runs, so
// there's a real, inspectable, editable file on disk to point theme_file
// at, rather than a value only ever visible in this package's source.
func Serialize(p Palette) string {
	var b strings.Builder
	writeField := func(comment, key string, value Color) {
		if comment != "" {
			fmt.Fprintf(&b, "# %s\n", comment)
		}
		fmt.Fprintf(&b, "%s = %q\n\n", key, value)
	}

	fmt.Fprintf(&b, "mode = %q\n\n", p.Mode)

	writeField("Accent color -- focused panel borders, the selected-row highlight.", "accent", p.Accent)
	writeField("Selected-row background.", "selection", p.Selection)
	writeField("Header rows, secondary borders.", "muted", p.Muted)

	writeField("Base background.", "background", p.Background)
	writeField("A darker background shade.", "dark_background", p.DarkBackground)
	writeField("The darkest background shade.", "darker_background", p.DarkerBackground)
	writeField("A lighter background shade.", "lighter_background", p.LighterBackground)

	writeField("Base foreground/text color.", "foreground", p.Foreground)
	writeField("A dimmer foreground shade.", "dark_foreground", p.DarkForeground)
	writeField("A lighter foreground shade.", "light_foreground", p.LightForeground)
	writeField("The brightest foreground shade.", "bright_foreground", p.BrightForeground)

	writeField("The eight categorical colors -- format badges, mark ticks,\n# ratings, volume/flag indicators, and so on.", "red", p.Red)
	writeField("", "yellow", p.Yellow)
	writeField("", "orange", p.Orange)
	writeField("", "green", p.Green)
	writeField("", "cyan", p.Cyan)
	writeField("", "blue", p.Blue)
	writeField("", "magenta", p.Magenta)
	writeField("", "brown", p.Brown)

	writeField("Brighter variants of the same eight, used where a color\n# needs to read as more vivid (e.g. the play/pause/stop glyph).", "bright_red", p.BrightRed)
	writeField("", "bright_yellow", p.BrightYellow)
	writeField("", "bright_green", p.BrightGreen)
	writeField("", "bright_cyan", p.BrightCyan)
	writeField("", "bright_blue", p.BrightBlue)
	fmt.Fprintf(&b, "bright_magenta = %q\n", p.BrightMagenta)

	return b.String()
}

// parse reads a color file as flat `key = "value"` pairs -- hand-rolled
// rather than pulling in a TOML library, since that's the entire shape
// this package ever actually consumes (see Serialize: no tables,
// arrays, or nesting). Comment lines (leading #)
// and anything that isn't a simple `key = "value"` pair are ignored
// rather than erroring, so an unrelated line (a future field this
// package doesn't know about yet, e.g.) doesn't break parsing of every
// other line.
func parse(data []byte) map[string]string {
	fields := make(map[string]string)
	scanner := bufio.NewScanner(bytes.NewReader(data))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)
		value = strings.Trim(value, `"`)
		if key == "" || value == "" {
			continue
		}
		fields[key] = value
	}
	return fields
}
