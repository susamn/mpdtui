package ui

import (
	"testing"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

// cellColors reports the fg/bg tview will actually draw for cell, mirroring
// tview's own resolution: SetTextColor/SetBackgroundColor write to the
// legacy Color/BackgroundColor fields if Style was still the zero value at
// call time, or into Style otherwise -- which one depends on whether
// applyTheme() has already mutated the global tview.Styles in this test
// binary, since NewTableCell seeds Style from those globals. Checking only
// one side makes the test's outcome depend on execution order across the
// whole package; this checks whichever side tview itself would use.
func cellColors(cell *tview.TableCell) (fg, bg tcell.Color) {
	if cell.Style == tcell.StyleDefault {
		return cell.Color, cell.BackgroundColor
	}
	fg, bg, _ = cell.Style.Decompose()
	return
}

func TestFormatTagCellKnownFormatUsesItsStyle(t *testing.T) {
	cell := formatTagCell("track.flac")
	if got, want := cell.Text, " FLAC "; got != want {
		t.Errorf("text = %q, want %q", got, want)
	}
	fg, bg := cellColors(cell)
	if want := formatStyles["FLAC"].fg; fg != want {
		t.Errorf("fg = %v, want %v", fg, want)
	}
	if want := formatStyles["FLAC"].bg; bg != want {
		t.Errorf("bg = %v, want %v", bg, want)
	}
	if got := cell.Align; got != tview.AlignRight {
		t.Errorf("align = %v, want AlignRight", got)
	}
}

func TestFormatTagCellUnknownFormatUsesDefaultStyle(t *testing.T) {
	cell := formatTagCell("track.xyz")
	if got, want := cell.Text, " XYZ "; got != want {
		t.Errorf("text = %q, want %q", got, want)
	}
	fg, bg := cellColors(cell)
	if want := defaultFormatStyle.bg; bg != want {
		t.Errorf("bg = %v, want default %v", bg, want)
	}
	if want := defaultFormatStyle.fg; fg != want {
		t.Errorf("fg = %v, want default %v", fg, want)
	}
}

func TestFormatTagCellNoExtensionRendersEmpty(t *testing.T) {
	cell := formatTagCell("no-extension")
	if got := cell.Text; got != "" {
		t.Errorf("text = %q, want empty", got)
	}
}
