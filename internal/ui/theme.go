package ui

import (
	"github.com/gdamore/tcell/v2"

	"mpdtui/internal/uitheme"
)

// This file holds the colors that only mean something to this package's
// own panels -- the Queue's rating stars, the lyrics viewer's per-format
// tint, the Now Playing bar. The generic machinery they are built with
// (the active palette, the palette-to-tcell conversion, the contrast and
// luminance math, and the focused/selected colors every tview front end
// needs) lives in internal/uitheme, so that panels extracted out of this
// package can still reach it.

// deriveColors recomputes every color this package owns from the
// active palette. Called by SetThemeFile at startup and again by
// reloadPalette on a theme change (see App.reapplyTheme, and queue.go's
// queueTitleColor and friends, all of which this touches) -- every
// semantic concept (e.g. "this is the accent/success color") is derived
// from one palette field, so it reads as the same color across every
// panel, whatever the active theme actually is.
//
// uitheme's own colors are re-derived by uitheme itself; this only
// covers the panel-specific ones declared across this package.
func deriveColors() {
	p := uitheme.Palette()

	treeSelectedStyle = tcell.StyleDefault.Foreground(uitheme.SelectedFg()).Background(uitheme.SelectedBg())

	locateFlashBg = uitheme.Hex(p.Accent)
	locateFlashFg = uitheme.Contrast(locateFlashBg)

	queueTitleColor = uitheme.Hex(p.Green)
	queueRatingColor = uitheme.Hex(p.Yellow)
	queueStarTopColor = queueRatingColor
	queueStarHighColor = uitheme.RecedeFrom(queueStarTopColor, queueStarMinRatio)
	markTickColors = []tcell.Color{
		uitheme.Hex(p.Red),
		uitheme.Hex(p.Orange),
		uitheme.Hex(p.Yellow),
		uitheme.Hex(p.Magenta),
		uitheme.Hex(p.Blue),
		uitheme.Hex(p.Green),
	}
	formatColors = map[string]tcell.Color{
		"FLAC": uitheme.Hex(p.Green),
		"MP3":  uitheme.Hex(p.Blue),
		"WAV":  uitheme.Hex(p.Cyan),
		"M4A":  uitheme.Hex(p.Magenta),
		"AAC":  uitheme.Hex(p.Magenta),
		"OGG":  uitheme.Hex(p.Cyan),
		"OPUS": uitheme.Hex(p.Cyan),
		"WMA":  uitheme.Hex(p.Orange),
	}
	defaultFormatColor = uitheme.Hex(p.BrightRed)

	nowPlayingBorderColor = uitheme.Hex(p.Yellow)
	nowPlayingBarColor = string(p.Cyan)
	nowPlayingRatingColor = string(p.Yellow)
	nowPlayingTrackColor = string(p.Green)
	nowPlayingArtistColor = string(p.Blue)

	lyricsColor = uitheme.ActiveBorder()
	lyricsLRCColor = nowPlayingTrackColor
	lyricsTxtColor = string(p.Orange)
	lyricsMatchColor = string(p.Yellow)

	stateGlyphPlayColor = string(p.BrightGreen)
	stateGlyphPauseColor = string(p.BrightYellow)
	stateGlyphStopColor = string(p.BrightRed)

	volumeColorLow = uitheme.Hex(p.Green)
	volumeColorMid = uitheme.Hex(p.Yellow)
	volumeColorHigh = uitheme.Hex(p.Red)

	flagOnColor = string(p.Green)
	flagOffColor = string(p.Red)
}

// init derives this package's colors from whatever uitheme already
// loaded, so the package is never left holding zero Colors -- a zero
// tcell.Color renders as pure black, so an unset panel color is not
// "unstyled" but actively wrong. In production SetThemeFile runs again
// with the real theme file before App.Run's build(), so this only
// covers code touching these colors before then (there is none in
// production; it matters for tests that build panels without going
// through Run). uitheme's own init has already run by this point --
// imported packages are initialized first.
func init() {
	deriveColors()
}

// SetThemeFile points the UI at path (config.LoadThemeFile's value) and
// derives every color from it -- called once by App.Run, before build(),
// so the very first render already reflects it.
func SetThemeFile(path string) {
	uitheme.SetFile(path)
	deriveColors()
}

// reloadPalette re-reads the theme file and recomputes every derived
// color. Its one caller is App.reapplyTheme, which additionally has to
// repaint the already-built widgets that captured a color at
// construction time rather than reading it live on each render.
func reloadPalette() {
	uitheme.Reload()
	deriveColors()
}

// ResetPaletteForTest pins this package's colors to theme.Default(),
// bypassing whatever theme file happens to be configured on the machine
// running the tests. Exported for the external ui_test package, whose
// color-dependent assertions need deterministic values.
func ResetPaletteForTest() {
	uitheme.ResetForTest()
	deriveColors()
}
