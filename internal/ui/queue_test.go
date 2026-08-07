package ui

import (
	"strings"
	"testing"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

// cellFg reports the foreground color tview will actually draw for cell,
// mirroring tview's own resolution: SetTextColor writes to the legacy
// Color field if Style was still the zero value at call time, or into
// Style otherwise -- which one depends on whether applyTheme() has
// already mutated the global tview.Styles in this test binary, since
// NewTableCell seeds Style from those globals. Checking only one side
// makes the test's outcome depend on execution order across the whole
// package; this checks whichever side tview itself would use.
func cellFg(cell *tview.TableCell) tcell.Color {
	if cell.Style == tcell.StyleDefault {
		return cell.Color
	}
	fg, _, _ := cell.Style.Decompose()
	return fg
}

func TestFormatTagCellKnownFormatUsesItsColor(t *testing.T) {
	cell := formatTagCell("track.flac")
	if got, want := cell.Text, "FLAC"+formatGap; got != want {
		t.Errorf("text = %q, want %q", got, want)
	}
	if got, want := cellFg(cell), formatColors["FLAC"]; got != want {
		t.Errorf("fg = %v, want %v", got, want)
	}
	if got := cell.Align; got != tview.AlignRight {
		t.Errorf("align = %v, want AlignRight", got)
	}
}

func TestFormatTagCellUnknownFormatUsesDefaultColor(t *testing.T) {
	cell := formatTagCell("track.xyz")
	if got, want := cell.Text, "XYZ"+formatGap; got != want {
		t.Errorf("text = %q, want %q", got, want)
	}
	if got, want := cellFg(cell), defaultFormatColor; got != want {
		t.Errorf("fg = %v, want default %v", got, want)
	}
}

func TestFormatTagCellNoExtensionRendersEmpty(t *testing.T) {
	cell := formatTagCell("no-extension")
	if got := cell.Text; got != "" {
		t.Errorf("text = %q, want empty", got)
	}
}

// TestFormatTagCellTextHasNoBracket guards against a real regression:
// tview.Table has no way to disable dynamic-color tag parsing (unlike
// TextView's SetDynamicColors), so any "[...]" in cell text is always
// parsed as a style/region tag and silently vanishes from the rendered
// output instead of showing as literal brackets.
func TestFormatTagCellTextHasNoBracket(t *testing.T) {
	for _, file := range []string{"track.flac", "track.mp3", "track.unknownformat"} {
		if cell := formatTagCell(file); strings.ContainsAny(cell.Text, "[]") {
			t.Errorf("formatTagCell(%q).Text = %q contains a bracket -- tview will silently swallow it as a tag", file, cell.Text)
		}
	}
}
