package ui

import (
	"time"

	"github.com/gdamore/tcell/v2"
)

// locateFlashBg/Fg are the Queue's selected-row colors for the brief
// flash after 'L' (see App.flashLocatedRow). Assigned by deriveColors
// (theme.go) from the palette's accent -- the same "look here" color the
// focused panel's border uses, and a different palette slot from the
// selection highlight it briefly replaces, so the flash reads as a flash
// rather than as the row simply being selected.
var (
	locateFlashBg tcell.Color
	locateFlashFg tcell.Color
)

// locateFlashPhases is the on/off sequence of the post-'L' flash: two
// brief blinks, then back to the normal selection highlight. Deliberately
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

// flashLocatedRow briefly flashes the Queue's selected row, to point the
// eye at the track 'L' just jumped to -- centering alone moves the row
// under the cursor without anything actually changing on screen where the
// user was looking.
//
// Implemented by swapping the table's *selected-row* style rather than
// recoloring that row's cells: the located row is by definition the
// selected one, the swap is a single call that can't get out of step with
// whatever the row's cells happen to contain, and it can't be clobbered
// by a queue re-render landing mid-flash.
func (a *App) flashLocatedRow() {
	a.locateFlashSeq++
	a.runLocateFlashPhase(a.locateFlashSeq, 0)
}

// runLocateFlashPhase applies phase i and schedules the next one, ending
// on the normal selection style. seq is checked at every step (the same
// guard App.flash uses for the hint bar): pressing 'L' again mid-flash
// starts a new sequence, and the old one must stop rather than restore a
// style the new flash is in the middle of using.
func (a *App) runLocateFlashPhase(seq, i int) {
	if a.locateFlashSeq != seq {
		return
	}
	if i >= len(locateFlashPhases) {
		a.setQueueRowFlash(false)
		return
	}
	phase := locateFlashPhases[i]
	a.setQueueRowFlash(phase.on)
	time.AfterFunc(phase.d, func() {
		a.tv.QueueUpdateDraw(func() {
			a.runLocateFlashPhase(seq, i+1)
		})
	})
}

// setQueueRowFlash switches the Queue's selected-row highlight between
// the flash colors and the normal ones. The "off" style is rebuilt from
// the live colorSelected* vars rather than saved and restored, so a theme
// reload (SIGUSR1) landing mid-flash leaves the row in the *new* theme's
// selection color, not a stale snapshot of the old one.
func (a *App) setQueueRowFlash(on bool) {
	bg, fg := colorSelectedBg, colorSelectedFg
	if on {
		bg, fg = locateFlashBg, locateFlashFg
	}
	a.queue.table.SetSelectedStyle(tcell.StyleDefault.Background(bg).Foreground(fg))
}
