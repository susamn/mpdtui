package mini

import (
	"fmt"

	"mpdtui/internal/theme"
)

// ansiReset, ansiTrackColor, ansiRatingColor, ansiStatsColor, and
// ansiBarColor are raw 24-bit-color escape sequences, matching
// internal/ui's own color choices for the same concepts (Green/Yellow/
// Blue/Cyan respectively -- see internal/ui/theme.go's deriveColors).
// Theme-derived (deriveANSIColors) rather than literals, so mini mode
// re-colors along with everything else on a theme change; raw escapes
// rather than tcell.Style because this package renders via bare
// terminal control sequences, not tcell/tview (see format.go's own
// top-of-file doc comment).
const ansiReset = "\x1b[0m"

var (
	ansiTrackColor  string
	ansiRatingColor string
	ansiStatsColor  string
	ansiBarColor    string
)

func init() {
	// "" here means no theme_file resolved yet -- Run itself calls
	// reloadTheme again with the real value (internal/config.
	// LoadThemeFile's) once it's known, before the first redraw.
	reloadTheme("")
}

// deriveANSIColors recomputes every ansi* var from p.
func deriveANSIColors(p theme.Palette) {
	ansiTrackColor = ansiFG(p.Green)
	ansiRatingColor = ansiFG(p.Yellow)
	ansiStatsColor = ansiFG(p.Blue)
	ansiBarColor = ansiFG(p.Cyan)
}

// reloadTheme re-reads themeFile (internal/config.LoadThemeFile's
// value -- always a real path once internal/config.EnsureConfigFiles
// has run; LoadFrom's own Default() fallback covers the rare case it's
// unreadable anyway) and recomputes the ansi* vars above.
func reloadTheme(themeFile string) {
	p, _ := theme.LoadFrom(themeFile)
	deriveANSIColors(p)
}

// ansiFG converts a theme.Color ("#RRGGBB") to a raw 24-bit ANSI
// foreground escape sequence. A malformed or empty value falls back to
// "" (no color code), same as an unset segment.fg -- see format.go's
// segment.render.
func ansiFG(c theme.Color) string {
	var r, g, b int
	if _, err := fmt.Sscanf(string(c), "#%02x%02x%02x", &r, &g, &b); err != nil {
		return ""
	}
	return fmt.Sprintf("\x1b[38;2;%d;%d;%dm", r, g, b)
}
