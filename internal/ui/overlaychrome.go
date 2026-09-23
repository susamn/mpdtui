package ui

import (
	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"mpdtui/internal/uitheme"
)

// borderTitler is the small subset of tview.Box styling every bordered
// overlay card needs. Unlike the main panels, most overlay cards do not
// receive focus on the exact primitive that owns the visible border
// (Settings and Bookmarks focus a child), so WireFocus cannot color the
// card itself. They are only visible while open, so painting them active
// on open is enough and keeps the change local.
type borderTitler interface {
	SetBorderColor(tcell.Color) *tview.Box
	SetTitleColor(tcell.Color) *tview.Box
}

func activateOverlayBorder(p borderTitler) {
	p.SetBorderColor(uitheme.ActiveBorder())
	p.SetTitleColor(uitheme.ActiveBorder())
}
