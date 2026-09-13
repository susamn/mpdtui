// Package uitheme turns the raw palette in internal/theme into the
// tcell colors a tview front end actually draws with, and owns the
// active palette for the whole UI.
//
// It exists as its own package so that a panel extracted out of
// internal/ui -- internal/albumart and internal/visualizer already, and
// whatever follows -- can still reach the colors it needs. Those colors
// were package-level vars inside internal/ui, invisible to anything
// outside it, which is what made every such extraction a choice between
// duplicating the derivation or passing four tcell.Colors through every
// constructor.
//
// Colors are read through accessors rather than exported vars. That
// keeps callers from reassigning them, and it removes a real hazard the
// var form had: a package-level initializer elsewhere that captured a
// color would run before this package's own init and bake in the zero
// Color (see internal/ui's treeSelectedStyle, which had exactly that
// bug). A function call always reads the current value, including after
// a theme reload.
//
// internal/mini deliberately does not use this package: it renders via
// bare ANSI escapes and must not depend on tcell (see DEPENDENCY.md).
// It derives its own escape sequences straight from internal/theme.
package uitheme

import (
	"math"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"mpdtui/internal/theme"
)

// palette is the active color source for every derived color in this
// package (activeBorder, queueTitleColor, nowPlayingBarColor, and
// so on -- see deriveColors). It's fully config-driven: which file it
// comes from is entirely up to internal/config.LoadThemeFile, resolved
// by cmd/mpdtui's main.go and handed to SetFile before App.Run's
// build() -- this package has no built-in notion of Omarchy, matugen,
// or any other specific desktop integration; it just reads filePath
// (see EnsureConfigFiles, which guarantees that path exists as a real
// file the first time mpdtui ever runs, seeded with theme.Default()).
//
// palette is replaced wholesale by Reload on a SIGUSR1 (see
// app.go's signal handling) -- e.g. Omarchy's theme-set flow, or
// matugen's own post_hook, poking this process after regenerating that
// file. mpdtui never polls the file itself.
//
// found is false only if filePath itself turns out to be
// unreadable at load time (permissions, the file got deleted after
// EnsureConfigFiles ran, etc.) -- deriveColors then just runs
// theme.Default()'s own values through the same derivation as any real
// file would get, so there's no separate "legacy" rendering path to
// keep in sync; found only affects what the Settings overlay shows
// ("Theme Status").
var (
	palette  theme.Palette
	found    bool
	filePath string
)

// SetFile points this package at path (config.LoadThemeFile's
// value) and immediately (re)loads and repaints from it -- called once
// by App.Run, before build(), so the very first render already reflects
// it.
func SetFile(path string) {
	filePath = path
	Reload()
}

// Colors chosen to read like lazygit's default theme: an accent color
// on the focused panel's border/title, and everything else left at the
// terminal's own default foreground/background rather than tview's
// hard-coded black-on-white -- that hard-coding is what was making text
// look flatly grey regardless of the user's terminal color scheme.
var (
	activeBorder   tcell.Color
	inactiveBorder = tcell.ColorDefault
	selectedBg     tcell.Color
	selectedFg     tcell.Color
	tableHeaderBg  tcell.Color
	tableHeaderFg  tcell.Color
)

func init() {
	// "" here means no theme_file resolved yet -- SetFile is
	// called again with the real value before App.Run's build(), so
	// this initial load only matters for code that touches this
	// package's colors before Run does (there is none in production;
	// it exists so this package is never left holding a zero Palette).
	palette, found = theme.LoadFrom(filePath)
	deriveColors()
}

// Hex converts a theme.Color ("#RRGGBB") to a tcell.Color, via
// tcell's own hex parser (tcell.GetColor already handles "#rrggbb").
// An empty or malformed value falls back to tcell.ColorDefault rather
// than a zero Color (which tcell renders as pure black) -- a theme
// field this package doesn't recognize should look like "unstyled",
// not "black-on-black".
func Hex(c theme.Color) tcell.Color {
	if c == "" {
		return tcell.ColorDefault
	}
	return tcell.GetColor(string(c))
}

// Contrast picks a readable foreground for text drawn on top of
// bg, so the selected-row highlight stays legible no matter how bright
// or dark the active theme's accent color is (a fixed "yellow on
// accent" pairing, tried first, went unreadable against several real
// Omarchy themes whose accent is itself a pale yellow). Uses the
// standard relative-luminance approximation (ITU-R BT.601) rather than
// full sRGB gamma correction -- more precision than a two-way pick
// between a dark and a light color actually needs.
func Contrast(bg tcell.Color) tcell.Color {
	r, g, b := bg.RGB()
	luminance := 0.299*float64(r) + 0.587*float64(g) + 0.114*float64(b)
	if luminance > 140 {
		return Hex(palette.DarkerBackground)
	}
	return Hex(palette.BrightForeground)
}

// relativeLuminance is the WCAG relative luminance of c (0 for black, 1
// for white), used to reason about how far apart two colors actually
// look. Unlike Contrast's cheaper BT.601 approximation, this one
// is gamma-corrected: it's compared against a ratio threshold rather
// than a single light/dark cutoff, so the error from skipping gamma
// would actually change the outcome. Returns -1 for a color with no
// resolvable RGB (notably tcell.ColorDefault, i.e. "the terminal's
// own"), which callers have to handle -- there is no way to know what
// the terminal will actually paint.
func relativeLuminance(c tcell.Color) float64 {
	r, g, b := c.RGB()
	if r < 0 || g < 0 || b < 0 {
		return -1
	}
	channel := func(v int32) float64 {
		f := float64(v) / 255
		if f <= 0.03928 {
			return f / 12.92
		}
		return math.Pow((f+0.055)/1.055, 2.4)
	}
	return 0.2126*channel(r) + 0.7152*channel(g) + 0.0722*channel(b)
}

// luminanceRatio is the WCAG contrast ratio between two relative
// luminances -- 1.0 for identical, higher the further apart.
func luminanceRatio(a, b float64) float64 {
	if a < b {
		a, b = b, a
	}
	return (a + 0.05) / (b + 0.05)
}

// blend mixes a toward b by t (0 returns a, 1 returns b), in
// straight sRGB space. Falls back to a when either color has no RGB to
// mix.
func blend(a, b tcell.Color, t float64) tcell.Color {
	ar, ag, ab := a.RGB()
	br, bg, bb := b.RGB()
	if ar < 0 || br < 0 {
		return a
	}
	mix := func(x, y int32) int32 { return x + int32(t*float64(y-x)) }
	return tcell.NewRGBColor(mix(ar, br), mix(ag, bg), mix(ab, bb))
}

// Separation reports how far apart a and b look, as a WCAG contrast
// ratio: 1.0 for two colors that read identically, higher the further
// apart. This is the measure RecedeFrom guarantees a minimum of, so it
// is also the measure to assert on when checking that two derived tiers
// are actually distinguishable. Returns 0 if either color has no
// resolvable RGB (notably tcell.ColorDefault), since there is no way to
// know what the terminal will paint.
func Separation(a, b tcell.Color) float64 {
	la, lb := relativeLuminance(a), relativeLuminance(b)
	if la < 0 || lb < 0 {
		return 0
	}
	return luminanceRatio(la, lb)
}

// RecedeFrom returns a visibly weaker version of c: the same hue, mixed
// toward the theme's own background until it is at least minRatio apart
// from c in relative luminance.
//
// This exists because deriving two visually distinct colors from two
// separate palette fields does not work. The obvious pairing -- the
// theme's "yellow" and "bright_yellow" -- turns out to be a coin flip:
// across the Omarchy themes installed on the machine this was written
// on, 8 of 15 had the two within 1.2x luminance of each other and 3 had
// them byte-identical, so a display relying on that difference showed
// one color most of the time. Anything that has to read as two tiers
// therefore derives both from a single palette field, with the second
// computed to a guaranteed separation rather than hoped for.
//
// Mixing toward the background (rather than simply darkening) is what
// makes this work on light themes too: the weaker tier always recedes
// toward whatever the panel is actually drawn on. Where the background
// is the terminal's own and so has no known RGB, it falls back to
// mixing toward black or white, whichever c is further from.
func RecedeFrom(c tcell.Color, minRatio float64) tcell.Color {
	base := relativeLuminance(c)
	if base < 0 {
		return c // nothing resolvable to weaken
	}

	if out, ok := mixUntilSeparated(c, base, Hex(palette.Background), minRatio); ok {
		return out
	}

	// The background is too close to c in luminance to separate against
	// -- mixing toward it barely moves the color, so the two tiers would
	// still look the same. Fall back to the far end of the scale, which
	// always has room. This trades the "weaker tier recedes into the
	// panel" reading for a tier that is at least distinguishable, and
	// only happens on a theme whose star color is already nearly
	// invisible against its own background.
	extreme := tcell.NewRGBColor(0, 0, 0)
	if base < 0.5 {
		extreme = tcell.NewRGBColor(255, 255, 255)
	}
	out, _ := mixUntilSeparated(c, base, extreme, minRatio)
	return out
}

// mixUntilSeparated mixes c toward target in increasing steps until it
// is at least minRatio away from base in luminance, reporting whether it
// got there. Stepping rather than using one fixed fraction is what makes
// this theme-independent: how far a given fraction moves the luminance
// depends on how far apart c and target already are, which varies per
// palette.
func mixUntilSeparated(c tcell.Color, base float64, target tcell.Color, minRatio float64) (tcell.Color, bool) {
	if relativeLuminance(target) < 0 {
		return c, false
	}
	last := c
	for t := 0.3; t <= 0.9; t += 0.05 {
		last = blend(c, target, t)
		if luminanceRatio(base, relativeLuminance(last)) >= minRatio {
			return last, true
		}
	}
	return last, false
}

// Reload re-reads filePath, recomputes every derived color,
// and repaints every already-built widget that captured an old color at
// construction time rather than reading these vars live on each render
// -- see App.reapplyTheme, its one caller.
func Reload() {
	palette, found = theme.LoadFrom(filePath)
	deriveColors()
}

// SetPaletteForTest replaces the active palette with p and re-derives
// from it, for tests that need to assert how a specific palette renders
// (e.g. one whose two yellows are identical) rather than whatever theme
// the machine running the tests happens to have. Callers with their own
// palette-derived colors must re-derive those afterwards, the same as
// after Reload.
func SetPaletteForTest(p theme.Palette) {
	palette, found = p, true
	deriveColors()
}

// ResetForTest resets this package's colors to theme.Default(),
// bypassing whatever theme_file/color file happens to be configured on
// the machine running the tests. Exported only for internal/ui/tests
// (an external test package, so it can't reach the unexported palette
// var directly) -- color-dependent assertions there need deterministic
// values regardless of the test machine's own setup.
func ResetForTest() {
	palette, found = theme.Default(), true
	deriveColors()
}

// ApplyToTviewStyles overrides tview's global defaults, which otherwise force a
// pure black background and ANSI white text irrespective of the user's
// actual terminal theme. Also called again by App.reapplyTheme after a
// Reload -- tview reads these Styles fields live on each Draw,
// so simply reassigning them (no per-widget touch needed) is enough for
// every primitive using them.
func ApplyToTviewStyles() {
	tview.Styles.PrimitiveBackgroundColor = tcell.ColorDefault
	tview.Styles.ContrastBackgroundColor = tcell.ColorDefault
	tview.Styles.MoreContrastBackgroundColor = tcell.ColorDefault
	tview.Styles.BorderColor = inactiveBorder
	tview.Styles.TitleColor = tcell.ColorDefault
	tview.Styles.GraphicsColor = tcell.ColorDefault
	tview.Styles.PrimaryTextColor = tcell.ColorDefault
	tview.Styles.SecondaryTextColor = Hex(palette.Yellow)
	tview.Styles.TertiaryTextColor = Hex(palette.Green)
	tview.Styles.InverseTextColor = selectedFg
}

// FocusStyler is satisfied by *tview.List and *tview.Table (both embed
// *tview.Box), letting one function wire up focus-driven border/title
// coloring for any focusable panel.
type FocusStyler interface {
	SetBorderColor(tcell.Color) *tview.Box
	SetTitleColor(tcell.Color) *tview.Box
	SetFocusFunc(func()) *tview.Box
	SetBlurFunc(func()) *tview.Box
}

func WireFocus(p FocusStyler) {
	p.SetBorderColor(inactiveBorder)
	p.SetTitleColor(tcell.ColorDefault)
	p.SetFocusFunc(func() {
		p.SetBorderColor(activeBorder)
		p.SetTitleColor(activeBorder)
	})
	p.SetBlurFunc(func() {
		p.SetBorderColor(inactiveBorder)
		p.SetTitleColor(tcell.ColorDefault)
	})
}

// SetFocused immediately sets p's border/title to the active or
// inactive color per focused, without waiting for a real focus/blur
// event -- used by App.reapplyTheme to repaint whichever panel already
// has focus right after a theme reload, since WireFocus' own
// closures only fire on the next actual focus change.
func SetFocused(p FocusStyler, focused bool) {
	if focused {
		p.SetBorderColor(activeBorder)
		p.SetTitleColor(activeBorder)
		return
	}
	p.SetBorderColor(inactiveBorder)
	p.SetTitleColor(tcell.ColorDefault)
}

// Palette is the active palette every derived color comes from.
func Palette() theme.Palette { return palette }

// Found reports whether the configured theme file was actually
// readable. False means these colors came from theme.Default() instead,
// which renders identically -- it only affects what the Settings
// overlay reports as "Theme Status".
func Found() bool { return found }

// ActiveBorder is the accent color a focused panel's border and title
// take; InactiveBorder is what every unfocused one falls back to.
func ActiveBorder() tcell.Color { return activeBorder }

// InactiveBorder is the terminal's own border color -- see ActiveBorder.
func InactiveBorder() tcell.Color { return inactiveBorder }

// SelectedBg and SelectedFg are the highlight colors for a selected row
// in any list or table. SelectedFg is derived from SelectedBg via
// Contrast, so the pair stays legible on any theme.
func SelectedBg() tcell.Color { return selectedBg }

// SelectedFg is the readable foreground for SelectedBg -- see SelectedBg.
func SelectedFg() tcell.Color { return selectedFg }

// SelectedStyle is the SelectedBg/SelectedFg pair as a tcell.Style,
// which is the form tview's SetSelectedStyle wants. Provided because
// every table in the app built that same style by hand.
func SelectedStyle() tcell.Style {
	return tcell.StyleDefault.Background(selectedBg).Foreground(selectedFg)
}

// TableHeaderBg and TableHeaderFg give a table's header row an inverted
// look -- a filled bar in the theme's brightest foreground with the
// text knocked out of it -- so the header reads as a label rather than
// as a row of data. Shared by every table in the app (the Queue, the
// Playlists panel, both Settings tables); it is a property of "this is
// a table header", not of any one panel.
func TableHeaderBg() tcell.Color { return tableHeaderBg }

// TableHeaderFg is the knocked-out text color -- see TableHeaderBg.
func TableHeaderFg() tcell.Color { return tableHeaderFg }

// deriveColors recomputes this package's own colors from palette.
// Callers with their own palette-derived colors (internal/ui has many,
// one per panel concept) re-derive theirs after calling Reload.
func deriveColors() {
	activeBorder = Hex(palette.Accent)
	selectedBg = Hex(palette.Selection)
	selectedFg = Contrast(selectedBg)
	tableHeaderBg = Hex(palette.BrightForeground)
	tableHeaderFg = Hex(palette.DarkerBackground)
}
