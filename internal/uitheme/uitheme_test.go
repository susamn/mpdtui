package uitheme

import (
	"math"
	"os"
	"path/filepath"
	"testing"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"mpdtui/internal/theme"
)

// The colour math these cover moved here from internal/ui, where it used
// to sit beside the Queue gutter that is its most demanding caller. The
// gutter's own two-tier assertion stays there; this is the arithmetic
// underneath it.

func TestRecedeFromLeavesUnresolvableColorsAlone(t *testing.T) {
	// A terminal-default color has no RGB to weaken, and guessing what
	// the terminal will paint is not possible -- so it comes back
	// unchanged rather than becoming some invented shade.
	if got := RecedeFrom(tcell.ColorDefault, 1.8); got != tcell.ColorDefault {
		t.Errorf("RecedeFrom(default) = %v, want it left alone", got)
	}
}

func TestRelativeLuminanceMatchesKnownValues(t *testing.T) {
	cases := []struct {
		color tcell.Color
		want  float64
	}{
		{tcell.NewRGBColor(0, 0, 0), 0},
		{tcell.NewRGBColor(255, 255, 255), 1},
		{tcell.NewRGBColor(255, 0, 0), 0.2126},
		{tcell.NewRGBColor(0, 255, 0), 0.7152},
		{tcell.NewRGBColor(0, 0, 255), 0.0722},
	}
	for _, tc := range cases {
		if got := relativeLuminance(tc.color); math.Abs(got-tc.want) > 1e-4 {
			t.Errorf("relativeLuminance(%v) = %g, want %g", tc.color, got, tc.want)
		}
	}
	if got := relativeLuminance(tcell.ColorDefault); got >= 0 {
		t.Errorf("relativeLuminance(default) = %g, want a negative sentinel", got)
	}
}

func TestLuminanceRatioIsSymmetric(t *testing.T) {
	if a, b := luminanceRatio(0.8, 0.1), luminanceRatio(0.1, 0.8); a != b {
		t.Errorf("luminanceRatio is not symmetric: %g vs %g", a, b)
	}
	if got := luminanceRatio(0.5, 0.5); got != 1 {
		t.Errorf("luminanceRatio of equal luminances = %g, want 1", got)
	}
}

// --- Palette lifecycle and the derived colors ---------------------------

// restorePalette snapshots the package's palette and puts it back after
// the test, since it is process-global state shared by every test here.
func restorePalette(t *testing.T) {
	t.Helper()
	old := Palette()
	t.Cleanup(func() { SetPaletteForTest(old) })
}

func TestDerivedColorsComeFromThePalette(t *testing.T) {
	restorePalette(t)

	SetPaletteForTest(theme.Palette{
		Accent:           "#ff0000",
		Selection:        "#0000ff",
		BrightForeground: "#fafafa",
		DarkerBackground: "#101010",
	})

	if got, want := ActiveBorder(), tcell.GetColor("#ff0000"); got != want {
		t.Errorf("ActiveBorder() = %v, want the palette's accent %v", got, want)
	}
	if got, want := SelectedBg(), tcell.GetColor("#0000ff"); got != want {
		t.Errorf("SelectedBg() = %v, want the palette's selection %v", got, want)
	}
	if got, want := TableHeaderBg(), tcell.GetColor("#fafafa"); got != want {
		t.Errorf("TableHeaderBg() = %v, want the palette's bright foreground %v", got, want)
	}
	if got, want := TableHeaderFg(), tcell.GetColor("#101010"); got != want {
		t.Errorf("TableHeaderFg() = %v, want the palette's darker background %v", got, want)
	}
	// InactiveBorder is deliberately the terminal's own, not a palette
	// field -- an unfocused panel should recede into the user's theme.
	if got := InactiveBorder(); got != tcell.ColorDefault {
		t.Errorf("InactiveBorder() = %v, want tcell.ColorDefault", got)
	}
}

// TestSelectedFgStaysLegibleOnEverySelection is the contrast contract:
// whatever the theme picks for the selection background, the text drawn
// on it has to remain readable. A fixed pairing failed exactly this on
// pale-accented themes.
func TestSelectedFgStaysLegibleOnEverySelection(t *testing.T) {
	restorePalette(t)

	for _, selection := range []theme.Color{
		"#000000", "#ffffff", "#f9e2af", "#1e1e2e", "#7f7f7f", "#00ff00", "#0a0a0a",
	} {
		SetPaletteForTest(theme.Palette{
			Selection:        selection,
			BrightForeground: "#ffffff",
			DarkerBackground: "#000000",
		})
		if got := Separation(SelectedBg(), SelectedFg()); got < 3 {
			t.Errorf("selection %s: foreground/background only %.2fx apart, want a readable contrast",
				selection, got)
		}
	}
}

func TestSelectedStyleMatchesTheSelectedColors(t *testing.T) {
	restorePalette(t)
	SetPaletteForTest(theme.Palette{Selection: "#123456", BrightForeground: "#ffffff", DarkerBackground: "#000000"})

	fg, bg, _ := SelectedStyle().Decompose()
	if bg != SelectedBg() {
		t.Errorf("SelectedStyle background = %v, want SelectedBg() %v", bg, SelectedBg())
	}
	if fg != SelectedFg() {
		t.Errorf("SelectedStyle foreground = %v, want SelectedFg() %v", fg, SelectedFg())
	}
}

func TestHex(t *testing.T) {
	if got := Hex(""); got != tcell.ColorDefault {
		t.Errorf("Hex(\"\") = %v, want ColorDefault so an unset field reads as unstyled, not black", got)
	}
	if got, want := Hex("#ff8800"), tcell.GetColor("#ff8800"); got != want {
		t.Errorf("Hex(%q) = %v, want %v", "#ff8800", got, want)
	}
}

func TestContrastPicksTheReadableSide(t *testing.T) {
	restorePalette(t)
	SetPaletteForTest(theme.Palette{BrightForeground: "#ffffff", DarkerBackground: "#000000"})

	if got, want := Contrast(tcell.NewRGBColor(250, 250, 250)), Hex("#000000"); got != want {
		t.Errorf("Contrast(near-white) = %v, want the dark option %v", got, want)
	}
	if got, want := Contrast(tcell.NewRGBColor(5, 5, 5)), Hex("#ffffff"); got != want {
		t.Errorf("Contrast(near-black) = %v, want the bright option %v", got, want)
	}
}

func TestSeparation(t *testing.T) {
	black, white := tcell.NewRGBColor(0, 0, 0), tcell.NewRGBColor(255, 255, 255)

	if got := Separation(black, white); got < 20 {
		t.Errorf("Separation(black, white) = %.2f, want the maximum ~21", got)
	}
	if got := Separation(black, black); got != 1 {
		t.Errorf("Separation of a color with itself = %.2f, want exactly 1", got)
	}
	if a, b := Separation(black, white), Separation(white, black); a != b {
		t.Errorf("Separation is not symmetric: %.4f vs %.4f", a, b)
	}
	// A terminal-default color has no RGB, so there is no way to know
	// how far apart it looks from anything.
	if got := Separation(tcell.ColorDefault, white); got != 0 {
		t.Errorf("Separation(default, white) = %.2f, want 0 (unknowable)", got)
	}
	if got := Separation(white, tcell.ColorDefault); got != 0 {
		t.Errorf("Separation(white, default) = %.2f, want 0 (unknowable)", got)
	}
}

// TestRecedeFromGuaranteesSeparation is the property RecedeFrom exists
// to provide, checked across the awkward palettes that motivated it:
// the weakened color must end up at least minRatio away from the
// original, whatever the background is.
func TestRecedeFromGuaranteesSeparation(t *testing.T) {
	restorePalette(t)

	const minRatio = 1.8
	for _, tc := range []struct{ name, color, background string }{
		{"dark theme", "#f9e2af", "#1e1e2e"},
		{"light theme", "#8a6d00", "#fdf6e3"},
		{"near-white color on light bg", "#cbfff9", "#f0f0f0"},
		{"very dark color on black", "#788216", "#0a0a0a"},
		{"no background configured", "#ffd700", ""},
		{"color equals background", "#808080", "#808080"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			SetPaletteForTest(theme.Palette{Background: theme.Color(tc.background)})
			c := Hex(theme.Color(tc.color))
			receded := RecedeFrom(c, minRatio)

			if receded == c {
				t.Fatalf("RecedeFrom(%s) returned the same color -- the two tiers would look identical", tc.color)
			}
			if got := Separation(c, receded); got < minRatio {
				t.Errorf("RecedeFrom(%s) only %.2fx apart, want at least %.2fx", tc.color, got, minRatio)
			}
		})
	}
}

func TestBlendMovesBetweenTheEndpoints(t *testing.T) {
	black, white := tcell.NewRGBColor(0, 0, 0), tcell.NewRGBColor(255, 255, 255)

	if got := blend(black, white, 0); got != black {
		t.Errorf("blend(t=0) = %v, want the first color %v", got, black)
	}
	mid := blend(black, white, 0.5)
	r, g, b := mid.RGB()
	if r < 120 || r > 135 || g != r || b != r {
		t.Errorf("blend(t=0.5) = rgb(%d,%d,%d), want roughly mid grey", r, g, b)
	}
	// Nothing resolvable to mix means the original comes back rather
	// than some invented shade.
	if got := blend(tcell.ColorDefault, white, 0.5); got != tcell.ColorDefault {
		t.Errorf("blend from default = %v, want it left alone", got)
	}
	if got := blend(black, tcell.ColorDefault, 0.5); got != black {
		t.Errorf("blend toward default = %v, want the original %v", got, black)
	}
}

func TestReloadAndSetFile(t *testing.T) {
	restorePalette(t)

	// A path that does not exist: Found() is false and the colors fall
	// back to theme.Default() rather than going zero (black on black).
	SetFile(filepath.Join(t.TempDir(), "nope.theme"))
	if Found() {
		t.Error("Found() = true for a nonexistent theme file")
	}
	if ActiveBorder() == tcell.Color(0) {
		t.Error("ActiveBorder() is the zero Color after loading a missing theme -- want the built-in default")
	}
	beforeReload := ActiveBorder()

	// Reload re-reads the same path and lands in the same place.
	Reload()
	if got := ActiveBorder(); got != beforeReload {
		t.Errorf("ActiveBorder() changed across Reload with no file change: %v -> %v", beforeReload, got)
	}

	ResetForTest()
	if !Found() {
		t.Error("Found() = false after ResetForTest, want true")
	}
	if got, want := ActiveBorder(), Hex(theme.Default().Accent); got != want {
		t.Errorf("ActiveBorder() after ResetForTest = %v, want theme.Default()'s accent %v", got, want)
	}
}

// TestSetFileReadsARealThemeFile covers the path that actually matters
// at startup: a theme file on disk becomes the live colors.
func TestSetFileReadsARealThemeFile(t *testing.T) {
	restorePalette(t)

	path := filepath.Join(t.TempDir(), "my.theme")
	if err := os.WriteFile(path, []byte(theme.Serialize(theme.Palette{
		Accent:           "#abcdef",
		Selection:        "#123456",
		BrightForeground: "#ffffff",
		DarkerBackground: "#000000",
	})), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	SetFile(path)

	if !Found() {
		t.Fatal("Found() = false for a theme file that exists")
	}
	if got, want := ActiveBorder(), tcell.GetColor("#abcdef"); got != want {
		t.Errorf("ActiveBorder() = %v, want the file's accent %v", got, want)
	}
	if got, want := SelectedBg(), tcell.GetColor("#123456"); got != want {
		t.Errorf("SelectedBg() = %v, want the file's selection %v", got, want)
	}
}

func TestApplyToTviewStylesLeavesBackgroundsToTheTerminal(t *testing.T) {
	restorePalette(t)
	SetPaletteForTest(theme.Palette{Yellow: "#ffff00", Green: "#00ff00", Selection: "#0000ff",
		BrightForeground: "#ffffff", DarkerBackground: "#000000"})

	ApplyToTviewStyles()

	// The whole point: tview's hard-coded black-on-white is replaced by
	// the terminal's own colors, which is what stops text looking flatly
	// grey regardless of the user's scheme.
	for name, got := range map[string]tcell.Color{
		"PrimitiveBackgroundColor":    tview.Styles.PrimitiveBackgroundColor,
		"ContrastBackgroundColor":     tview.Styles.ContrastBackgroundColor,
		"MoreContrastBackgroundColor": tview.Styles.MoreContrastBackgroundColor,
		"PrimaryTextColor":            tview.Styles.PrimaryTextColor,
		"TitleColor":                  tview.Styles.TitleColor,
		"GraphicsColor":               tview.Styles.GraphicsColor,
	} {
		if got != tcell.ColorDefault {
			t.Errorf("tview.Styles.%s = %v, want ColorDefault (the terminal's own)", name, got)
		}
	}
	if got, want := tview.Styles.SecondaryTextColor, Hex("#ffff00"); got != want {
		t.Errorf("SecondaryTextColor = %v, want the palette's yellow %v", got, want)
	}
	if got, want := tview.Styles.InverseTextColor, SelectedFg(); got != want {
		t.Errorf("InverseTextColor = %v, want SelectedFg() %v", got, want)
	}
}

func TestWireFocusAndSetFocused(t *testing.T) {
	restorePalette(t)
	SetPaletteForTest(theme.Palette{Accent: "#ff0000", Selection: "#0000ff",
		BrightForeground: "#ffffff", DarkerBackground: "#000000"})

	// titleSpy records the title colors WireFocus/SetFocused ask for,
	// since tview.Box exposes no getter for them.
	table := tview.NewTable()
	spy := &focusSpy{FocusStyler: table}
	WireFocus(spy)

	// Starts unfocused: the terminal's own border, no accent.
	if got := table.GetBorderColor(); got != InactiveBorder() {
		t.Errorf("border color before focus = %v, want InactiveBorder() %v", got, InactiveBorder())
	}

	// SetFocused paints it as focused without waiting for a real focus
	// event -- which is what a theme reload needs, since WireFocus's
	// closures only fire on an actual focus change.
	SetFocused(spy, true)
	if got := table.GetBorderColor(); got != ActiveBorder() {
		t.Errorf("border color after SetFocused(true) = %v, want ActiveBorder() %v", got, ActiveBorder())
	}
	if got := spy.lastTitle; got != ActiveBorder() {
		t.Errorf("title color after SetFocused(true) = %v, want ActiveBorder() %v", got, ActiveBorder())
	}

	SetFocused(spy, false)
	if got := table.GetBorderColor(); got != InactiveBorder() {
		t.Errorf("border color after SetFocused(false) = %v, want InactiveBorder() %v", got, InactiveBorder())
	}
	if got := spy.lastTitle; got != tcell.ColorDefault {
		t.Errorf("title color after SetFocused(false) = %v, want ColorDefault", got)
	}
}

// focusSpy records title colors, which tview.Box has no getter for.
type focusSpy struct {
	FocusStyler
	lastTitle tcell.Color
}

func (f *focusSpy) SetTitleColor(c tcell.Color) *tview.Box {
	f.lastTitle = c
	return f.FocusStyler.SetTitleColor(c)
}
