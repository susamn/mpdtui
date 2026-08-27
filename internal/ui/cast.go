package ui

import (
	"context"
	"fmt"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"mpdtui/internal/cast"
)

// castController is the entire surface internal/ui uses for casting --
// *cast.Manager satisfies it. Everything about discovery, the Cast
// protocol, output management and session persistence stays in
// internal/cast; this package only drives a picker and shows an
// indicator.
type castController interface {
	Discover(ctx context.Context) []cast.Target
	StartCast(ctx context.Context, t cast.Target, meta cast.MediaMeta) (*cast.Session, error)
	StopCast(ctx context.Context) error
	Session() *cast.Session
	Reattach(ctx context.Context) (*cast.Session, error)
}

// castPicker is the 'P' overlay: pick a discovered device to cast MPD's
// audio to, or stop an active cast. Built once (like markPicker) and
// repopulated each time it opens. Discovery runs in the background, so
// the list first shows "scanning..." and repaints when results land.
type castPicker struct {
	*tview.List
	app     *App
	targets []cast.Target
}

func newCastPicker(app *App) *castPicker {
	m := &castPicker{app: app}
	l := tview.NewList()
	l.ShowSecondaryText(true)
	l.SetHighlightFullLine(true)
	l.SetSelectedTextColor(colorSelectedFg)
	l.SetSelectedBackgroundColor(colorSelectedBg)
	l.SetBorder(true).SetTitle(" Cast (Enter to select, Esc to cancel) ")
	// j/k/g/G: List has no native vim motions -- same helper markPicker
	// and the global-search hint list reuse.
	l.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		if event.Key() == tcell.KeyRune {
			switch event.Rune() {
			case 'j':
				l.SetCurrentItem(moveHintHighlight(l.GetCurrentItem(), l.GetItemCount(), 1))
				return nil
			case 'k':
				l.SetCurrentItem(moveHintHighlight(l.GetCurrentItem(), l.GetItemCount(), -1))
				return nil
			case 'g':
				if l.GetItemCount() > 0 {
					l.SetCurrentItem(0)
				}
				return nil
			case 'G':
				if n := l.GetItemCount(); n > 0 {
					l.SetCurrentItem(n - 1)
				}
				return nil
			}
		}
		return event
	})
	m.List = l
	return m
}

// render repopulates the list: an optional leading "Stop casting" row
// when a session is active, then the discovered targets, then a status
// row ("scanning..." or "no devices found") when there's nothing else.
func (m *castPicker) render(targets []cast.Target, scanning bool) {
	m.targets = targets
	m.Clear()

	sess := m.app.cast.Session()
	hasStop := sess != nil
	if hasStop {
		m.AddItem("■ Stop casting to "+sess.Target.Name, "", 0, nil)
	}
	for _, t := range targets {
		m.AddItem(t.Name, castDetail(t), 0, nil)
	}
	switch {
	case scanning:
		m.AddItem("scanning for devices…", "", 0, nil)
	case len(targets) == 0:
		m.AddItem("no devices found — check that mDNS multicast works here", "", 0, nil)
	}
	m.SetCurrentItem(0)

	m.SetSelectedFunc(func(index int, _, _ string, _ rune) {
		m.apply(index, hasStop)
	})
}

func castDetail(t cast.Target) string {
	kind := map[cast.Kind]string{
		cast.KindChromecast:    "Google Cast",
		cast.KindHomeAssistant: "Home Assistant",
		cast.KindDLNA:          "DLNA",
	}[t.Kind]
	if t.Model != "" {
		return kind + " · " + t.Model
	}
	return kind
}

// apply acts on the selected row. The network work runs through
// App.runAsync (a background goroutine in production), so the keypress
// never blocks on discovery or a device round-trip; the overlay closes
// immediately with a flash, and the Now Playing indicator repaints from
// cached status once the call lands.
func (m *castPicker) apply(index int, hasStop bool) {
	if hasStop && index == 0 {
		m.app.closeOverlay()
		m.app.showMessage("stopping cast…")
		m.app.runAsync(func() error {
			return m.app.cast.StopCast(context.Background())
		}, func() {
			m.app.showMessage("cast stopped")
			m.app.renderNowPlaying(m.app.currentStatus, m.app.currentSong)
		})
		return
	}

	ti := index
	if hasStop {
		ti--
	}
	if ti < 0 || ti >= len(m.targets) {
		return // the "scanning" / "no devices" placeholder row
	}
	target := m.targets[ti]
	song := m.app.currentSong
	m.app.closeOverlay()
	m.app.showMessage("casting to " + target.Name + "…")
	m.app.runAsync(func() error {
		_, err := m.app.cast.StartCast(context.Background(), target, cast.MediaMeta{
			Title:  song.Title,
			Artist: song.Artist,
		})
		return err
	}, func() {
		m.app.showMessage("casting to " + target.Name)
		m.app.renderNowPlaying(m.app.currentStatus, m.app.currentSong)
	})
}

// openCastPicker is 'P': opens the overlay and kicks off a background
// discovery that repaints the list when it finishes.
func (a *App) openCastPicker() {
	if a.cast == nil {
		a.flash("[red]casting unavailable[-]")
		return
	}
	p := a.castPicker
	p.render(nil, true)
	a.showOverlay("cast", centered(p, 64, 14), p)

	go func() {
		targets := a.cast.Discover(context.Background())
		a.tv.QueueUpdateDraw(func() {
			// Repaint only if the cast overlay is still the one on top --
			// the user may have Esc'd or opened something else during the
			// (multi-second) discovery.
			if name, _ := a.pages.GetFrontPage(); name == "cast" {
				p.render(targets, false)
			}
		})
	}()
}

// castStatusLine is appended to the Now Playing panel's second line; ""
// unless a cast is active.
func (a *App) castStatusLine() string {
	if a.cast == nil {
		return ""
	}
	if s := a.cast.Session(); s != nil {
		return fmt.Sprintf("   [%s]▶ cast %s[-]", nowPlayingArtistColor, s.Target.Name)
	}
	return ""
}

// reattachCast checks, in the background at startup, whether a cast
// started by an earlier mpdtui process is still running on its device and
// adopts it if so -- so the indicator and "Stop casting" work across
// restarts.
func (a *App) reattachCast() {
	if a.cast == nil {
		return
	}
	go func() {
		if s, _ := a.cast.Reattach(context.Background()); s != nil {
			a.tv.QueueUpdateDraw(a.refreshNowPlaying)
		}
	}()
}
