package ui

import (
	"math"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"mpdtui/internal/theme"
)

// palette is the active color source for every derived color in this
// package (colorActiveBorder, queueTitleColor, nowPlayingBarColor, and
// so on -- see deriveColors). It's fully config-driven: which file it
// comes from is entirely up to internal/config.LoadThemeFile, resolved
// by cmd/mpdtui's main.go and handed to SetThemeFile before App.Run's
// build() -- this package has no built-in notion of Omarchy, matugen,
// or any other specific desktop integration; it just reads themeFilePath
// (see EnsureConfigFiles, which guarantees that path exists as a real
// file the first time mpdtui ever runs, seeded with theme.Default()).
//
// palette is replaced wholesale by reloadPalette on a SIGUSR1 (see
// app.go's signal handling) -- e.g. Omarchy's theme-set flow, or
// matugen's own post_hook, poking this process after regenerating that
// file. mpdtui never polls the file itself.
//
// themeFound is false only if themeFilePath itself turns out to be
// unreadable at load time (permissions, the file got deleted after
// EnsureConfigFiles ran, etc.) -- deriveColors then just runs
// theme.Default()'s own values through the same derivation as any real
// file would get, so there's no separate "legacy" rendering path to
// keep in sync; themeFound only affects what the Settings overlay shows
// ("Theme Status").
var (
	palette       theme.Palette
	themeFound    bool
	themeFilePath string
)

// SetThemeFile points this package at path (config.LoadThemeFile's
// value) and immediately (re)loads and repaints from it -- called once
// by App.Run, before build(), so the very first render already reflects
// it.
func SetThemeFile(path string) {
	themeFilePath = path
	reloadPalette()
}

// Colors chosen to read like lazygit's default theme: an accent color
// on the focused panel's border/title, and everything else left at the
// terminal's own default foreground/background rather than tview's
// hard-coded black-on-white -- that hard-coding is what was making text
// look flatly grey regardless of the user's terminal color scheme.
var (
	colorActiveBorder   tcell.Color
	colorInactiveBorder = tcell.ColorDefault
	colorSelectedBg     tcell.Color
	colorSelectedFg     tcell.Color
)

func init() {
	// "" here means no theme_file resolved yet -- SetThemeFile is
	// called again with the real value before App.Run's build(), so
	// this initial load only matters for code that touches this
	// package's colors before Run does (there is none in production;
	// it exists so this package is never left holding a zero Palette).
	palette, themeFound = theme.LoadFrom(themeFilePath)
	deriveColors()
}

// hexColor converts a theme.Color ("#RRGGBB") to a tcell.Color, via
// tcell's own hex parser (tcell.GetColor already handles "#rrggbb").
// An empty or malformed value falls back to tcell.ColorDefault rather
// than a zero Color (which tcell renders as pure black) -- a theme
// field this package doesn't recognize should look like "unstyled",
// not "black-on-black".
func hexColor(c theme.Color) tcell.Color {
	if c == "" {
		return tcell.ColorDefault
	}
	return tcell.GetColor(string(c))
}

// contrastColor picks a readable foreground for text drawn on top of
// bg, so the selected-row highlight stays legible no matter how bright
// or dark the active theme's accent color is (a fixed "yellow on
// accent" pairing, tried first, went unreadable against several real
// Omarchy themes whose accent is itself a pale yellow). Uses the
// standard relative-luminance approximation (ITU-R BT.601) rather than
// full sRGB gamma correction -- more precision than a two-way pick
// between a dark and a light color actually needs.
func contrastColor(bg tcell.Color) tcell.Color {
	r, g, b := bg.RGB()
	luminance := 0.299*float64(r) + 0.587*float64(g) + 0.114*float64(b)
	if luminance > 140 {
		return hexColor(palette.DarkerBackground)
	}
	return hexColor(palette.BrightForeground)
}

// relativeLuminance is the WCAG relative luminance of c (0 for black, 1
// for white), used to reason about how far apart two colors actually
// look. Unlike contrastColor's cheaper BT.601 approximation, this one
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

// blendColors mixes a toward b by t (0 returns a, 1 returns b), in
// straight sRGB space. Falls back to a when either color has no RGB to
// mix.
func blendColors(a, b tcell.Color, t float64) tcell.Color {
	ar, ag, ab := a.RGB()
	br, bg, bb := b.RGB()
	if ar < 0 || br < 0 {
		return a
	}
	mix := func(x, y int32) int32 { return x + int32(t*float64(y-x)) }
	return tcell.NewRGBColor(mix(ar, br), mix(ag, bg), mix(ab, bb))
}

// recedeFrom returns a visibly weaker version of c: the same hue, mixed
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
func recedeFrom(c tcell.Color, minRatio float64) tcell.Color {
	base := relativeLuminance(c)
	if base < 0 {
		return c // nothing resolvable to weaken
	}

	if out, ok := mixUntilSeparated(c, base, hexColor(palette.Background), minRatio); ok {
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
		last = blendColors(c, target, t)
		if luminanceRatio(base, relativeLuminance(last)) >= minRatio {
			return last, true
		}
	}
	return last, false
}

// deriveColors recomputes every palette-derived color in this package
// from the current palette. Called once at package init and again by
// reloadPalette after a theme change (see this file's own applyTheme/
// App.reapplyTheme and queue.go's queueTitleColor and friends, all of
// which this touches) -- every semantic concept (e.g. "this is the
// accent/success color") is derived from one palette field, so it reads
// as the same color across every panel, whatever the active theme
// actually is (including theme.Default(), when nothing else is
// configured/found -- see palette's own doc comment).
func deriveColors() {
	colorActiveBorder = hexColor(palette.Accent)
	colorSelectedBg = hexColor(palette.Selection)
	colorSelectedFg = contrastColor(colorSelectedBg)
	treeSelectedStyle = tcell.StyleDefault.Foreground(colorSelectedFg).Background(colorSelectedBg)

	locateFlashBg = hexColor(palette.Accent)
	locateFlashFg = contrastColor(locateFlashBg)

	queueTitleColor = hexColor(palette.Green)
	queueHeaderBg = hexColor(palette.BrightForeground)
	queueHeaderFg = hexColor(palette.DarkerBackground)
	queueRatingColor = hexColor(palette.Yellow)
	queueStarTopColor = queueRatingColor
	queueStarHighColor = recedeFrom(queueStarTopColor, queueStarMinRatio)
	markTickColors = []tcell.Color{
		hexColor(palette.Red),
		hexColor(palette.Orange),
		hexColor(palette.Yellow),
		hexColor(palette.Magenta),
		hexColor(palette.Blue),
		hexColor(palette.Green),
	}
	formatColors = map[string]tcell.Color{
		"FLAC": hexColor(palette.Green),
		"MP3":  hexColor(palette.Blue),
		"WAV":  hexColor(palette.Cyan),
		"M4A":  hexColor(palette.Magenta),
		"AAC":  hexColor(palette.Magenta),
		"OGG":  hexColor(palette.Cyan),
		"OPUS": hexColor(palette.Cyan),
		"WMA":  hexColor(palette.Orange),
	}
	defaultFormatColor = hexColor(palette.BrightRed)

	nowPlayingBorderColor = hexColor(palette.Yellow)
	nowPlayingBarColor = string(palette.Cyan)
	nowPlayingRatingColor = string(palette.Yellow)
	nowPlayingTrackColor = string(palette.Green)
	nowPlayingArtistColor = string(palette.Blue)

	lyricsColor = colorActiveBorder
	lyricsLRCColor = nowPlayingTrackColor
	lyricsTxtColor = string(palette.Orange)
	lyricsMatchColor = string(palette.Yellow)

	stateGlyphPlayColor = string(palette.BrightGreen)
	stateGlyphPauseColor = string(palette.BrightYellow)
	stateGlyphStopColor = string(palette.BrightRed)

	volumeColorLow = hexColor(palette.Green)
	volumeColorMid = hexColor(palette.Yellow)
	volumeColorHigh = hexColor(palette.Red)

	flagOnColor = string(palette.Green)
	flagOffColor = string(palette.Red)
}

// reloadPalette re-reads themeFilePath, recomputes every derived color,
// and repaints every already-built widget that captured an old color at
// construction time rather than reading these vars live on each render
// -- see App.reapplyTheme, its one caller.
func reloadPalette() {
	palette, themeFound = theme.LoadFrom(themeFilePath)
	deriveColors()
}

// ResetPaletteForTest resets this package's colors to theme.Default(),
// bypassing whatever theme_file/color file happens to be configured on
// the machine running the tests. Exported only for internal/ui/tests
// (an external test package, so it can't reach the unexported palette
// var directly) -- color-dependent assertions there need deterministic
// values regardless of the test machine's own setup.
func ResetPaletteForTest() {
	palette, themeFound = theme.Default(), true
	deriveColors()
}

// applyTheme overrides tview's global defaults, which otherwise force a
// pure black background and ANSI white text irrespective of the user's
// actual terminal theme. Also called again by App.reapplyTheme after a
// reloadPalette -- tview reads these Styles fields live on each Draw,
// so simply reassigning them (no per-widget touch needed) is enough for
// every primitive using them.
func applyTheme() {
	tview.Styles.PrimitiveBackgroundColor = tcell.ColorDefault
	tview.Styles.ContrastBackgroundColor = tcell.ColorDefault
	tview.Styles.MoreContrastBackgroundColor = tcell.ColorDefault
	tview.Styles.BorderColor = colorInactiveBorder
	tview.Styles.TitleColor = tcell.ColorDefault
	tview.Styles.GraphicsColor = tcell.ColorDefault
	tview.Styles.PrimaryTextColor = tcell.ColorDefault
	tview.Styles.SecondaryTextColor = hexColor(palette.Yellow)
	tview.Styles.TertiaryTextColor = hexColor(palette.Green)
	tview.Styles.InverseTextColor = colorSelectedFg
}

// focusStyler is satisfied by *tview.List and *tview.Table (both embed
// *tview.Box), letting one function wire up focus-driven border/title
// coloring for any focusable panel.
type focusStyler interface {
	SetBorderColor(tcell.Color) *tview.Box
	SetTitleColor(tcell.Color) *tview.Box
	SetFocusFunc(func()) *tview.Box
	SetBlurFunc(func()) *tview.Box
}

func wireFocusColors(p focusStyler) {
	p.SetBorderColor(colorInactiveBorder)
	p.SetTitleColor(tcell.ColorDefault)
	p.SetFocusFunc(func() {
		p.SetBorderColor(colorActiveBorder)
		p.SetTitleColor(colorActiveBorder)
	})
	p.SetBlurFunc(func() {
		p.SetBorderColor(colorInactiveBorder)
		p.SetTitleColor(tcell.ColorDefault)
	})
}

// setFocusColor immediately sets p's border/title to the active or
// inactive color per focused, without waiting for a real focus/blur
// event -- used by App.reapplyTheme to repaint whichever panel already
// has focus right after a theme reload, since wireFocusColors' own
// closures only fire on the next actual focus change.
func setFocusColor(p focusStyler, focused bool) {
	if focused {
		p.SetBorderColor(colorActiveBorder)
		p.SetTitleColor(colorActiveBorder)
		return
	}
	p.SetBorderColor(colorInactiveBorder)
	p.SetTitleColor(tcell.ColorDefault)
}
