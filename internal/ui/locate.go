package ui

import (
	"time"

	"github.com/gdamore/tcell/v2"
)

// locateFlashBg/Fg are the colors of the brief flash after 'L' -- of the
// Queue's selected row and the Library's revealed node alike (see
// App.flashLocatedTrack). Assigned by deriveColors
// (theme.go) from the palette's accent -- the same "look here" color the
// focused panel's border uses, and a different palette slot from the
// selection highlight it briefly replaces, so the flash reads as a flash
// rather than as the row simply being selected.
var (
	locateFlashBg tcell.Color
	locateFlashFg tcell.Color
)

// locateFlashPhases is the on/off sequence of the post-'L' flash: two
// brief blinks, then back to the normal highlight. Both panels blink
// together, off one sequence -- 'L' points at one track, in two places. Deliberately
// short -- this is a "here it is" cue landing on a row the eye is already
// being pointed at, not an alert; a long or repeating blink would just be
// noise on every locate.
var locateFlashPhases = []struct {
	on bool
	d  time.Duration
}{
	{true, 160 * time.Millisecond},
	{false, 110 * time.Millisecond},
	{true, 160 * time.Millisecond},
}

// flashLocatedTrack briefly flashes the track 'L' just jumped to, in
// both panels that were pointed at it: the Queue's selected row and the
// Library's revealed node. Centering alone moves them under the cursor
// without anything actually changing on screen where the user was
// looking.
//
// In the Queue this swaps the table's *selected-row* style rather than
// recoloring that row's cells: the located row is by definition the
// selected one, the swap is a single call that can't get out of step with
// whatever the row's cells happen to contain, and it can't be clobbered
// by a queue re-render landing mid-flash.
//
// The Library node is captured here, once, rather than re-read on each
// phase: 'L' is the only thing that moves the tree's selection during a
// flash, and it starts a new sequence of its own anyway. A nil node --
// 'L' on a track that isn't in the library at all, so revealInLibrary
// found nothing to select -- simply means only the Queue flashes.
func (a *App) flashLocatedTrack() {
	a.locateFlashSeq++
	a.locateFlashNode = a.library.tree.GetCurrentNode()
	if a.locateFlashNode != nil {
		a.locateFlashNodeStyle = a.locateFlashNode.GetSelectedTextStyle()
	}
	a.runLocateFlashPhase(a.locateFlashSeq, 0)
}

// runLocateFlashPhase applies phase i and schedules the next one, ending
// on the normal styles. seq is checked at every step (the same
// guard App.flash uses for the hint bar): pressing 'L' again mid-flash
// starts a new sequence, and the old one must stop rather than restore a
// style the new flash is in the middle of using.
func (a *App) runLocateFlashPhase(seq, i int) {
	if a.locateFlashSeq != seq {
		return
	}
	if i >= len(locateFlashPhases) {
		a.setLocateFlash(false)
		return
	}
	phase := locateFlashPhases[i]
	a.setLocateFlash(phase.on)
	time.AfterFunc(phase.d, func() {
		a.tv.QueueUpdateDraw(func() {
			a.runLocateFlashPhase(seq, i+1)
		})
	})
}

// setLocateFlash switches both located spots -- the Queue's selected-row
// highlight and the Library's revealed node -- between the flash colors
// and their normal ones.
//
// The two restore differently on purpose. The Queue's "off" style is
// rebuilt from the live colorSelected* vars rather than saved, so a theme
// reload (SIGUSR1) landing mid-flash leaves the row in the *new* theme's
// selection color instead of a stale snapshot. A tree node's selected
// style isn't derived from this package's palette at all -- tview builds
// it per node at construction from tview.Styles -- so there's nothing to
// rebuild it from, and the saved original is restored verbatim.
func (a *App) setLocateFlash(on bool) {
	bg, fg := colorSelectedBg, colorSelectedFg
	if on {
		bg, fg = locateFlashBg, locateFlashFg
	}
	a.queue.table.SetSelectedStyle(tcell.StyleDefault.Background(bg).Foreground(fg))

	if a.locateFlashNode == nil {
		return
	}
	style := a.locateFlashNodeStyle
	if on {
		style = tcell.StyleDefault.Background(bg).Foreground(fg)
	}
	a.locateFlashNode.SetSelectedTextStyle(style)
}
