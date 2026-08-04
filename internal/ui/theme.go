package ui

import (
	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

// Colors chosen to read like lazygit's default theme: a green accent on
// the focused panel's border/title, a blue highlight with yellow text on
// the selected line (high-contrast, easy to spot at a glance), and
// everything else left at the terminal's own default foreground/
// background rather than tview's hard-coded black-on-white -- that
// hard-coding is what was making text look flatly grey regardless of the
// user's terminal color scheme.
const (
	colorActiveBorder   = tcell.ColorGreen
	colorInactiveBorder = tcell.ColorDefault
	colorSelectedBg     = tcell.ColorBlue
	colorSelectedFg     = tcell.ColorYellow
)

// applyTheme overrides tview's global defaults, which otherwise force a
// pure black background and ANSI white text irrespective of the user's
// actual terminal theme.
func applyTheme() {
	tview.Styles.PrimitiveBackgroundColor = tcell.ColorDefault
	tview.Styles.ContrastBackgroundColor = tcell.ColorDefault
	tview.Styles.MoreContrastBackgroundColor = tcell.ColorDefault
	tview.Styles.BorderColor = colorInactiveBorder
	tview.Styles.TitleColor = tcell.ColorDefault
	tview.Styles.GraphicsColor = tcell.ColorDefault
	tview.Styles.PrimaryTextColor = tcell.ColorDefault
	tview.Styles.SecondaryTextColor = tcell.ColorYellow
	tview.Styles.TertiaryTextColor = tcell.ColorGreen
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
