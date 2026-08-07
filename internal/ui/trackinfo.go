package ui

import (
	"fmt"
	"strings"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"mpdtui/internal/mpdclient"
)

// trackInfoCard shows the currently playing track's Track/Album/Artist/
// Genre/Year. It floats inside the bottom-right quadrant of the Queue
// panel (splitting that panel into a 2x2 grid), anchored to that
// quadrant's own top-left corner and sized to half its width/height rather
// than filling it -- rather than centered on the full screen like the
// other overlays. It embeds *tview.TextView and overrides Draw so its
// position is recomputed from the Queue table's live rect on every frame
// -- the same "read another primitive's current rect at draw time" trick
// albumArtPanel.draw uses for the Kitty image, which is what makes this
// track a terminal resize without any extra wiring.
type trackInfoCard struct {
	*tview.TextView
	app *App
}

func newTrackInfoCard(app *App) *trackInfoCard {
	v := tview.NewTextView().SetDynamicColors(true)
	v.SetBorder(true).SetTitle(" Track Info ")
	v.SetBorderPadding(1, 0, 1, 0)
	return &trackInfoCard{TextView: v, app: app}
}

// quadrantRect returns the bottom-right quarter of the rect (x, y, w, h):
// half width, half height (rounded down), with its own top-left corner at
// the source rect's horizontal and vertical midpoint.
func quadrantRect(x, y, w, h int) (int, int, int, int) {
	qw, qh := w/2, h/2
	return x + w - qw, y + h - qh, qw, qh
}

// cardRect returns where the floating card sits within its target
// quadrant (x, y, w, h): anchored at the quadrant's own top-left corner,
// sized to half its width/height so it visibly floats inside the
// quadrant rather than filling it.
func cardRect(x, y, w, h int) (int, int, int, int) {
	return x, y, w / 2, h / 2
}

// positionOverQueue sets the card's own rect to float inside the
// bottom-right quadrant of the Queue table's current rect. Split out from
// Draw so the positioning math is testable without a real tcell.Screen.
func (c *trackInfoCard) positionOverQueue() {
	x, y, w, h := c.app.queue.table.GetRect()
	c.SetRect(cardRect(quadrantRect(x, y, w, h)))
}

// Draw positions the card over the bottom-right quadrant of the Queue
// table's current rect, then delegates to the embedded TextView to
// actually paint it.
func (c *trackInfoCard) Draw(screen tcell.Screen) {
	c.positionOverQueue()
	c.TextView.Draw(screen)
}

// render fills in the card from the currently playing song, or "Nothing
// playing" if there is none -- the same emptiness check (DisplayName ==
// "") App.renderNowPlaying already uses for the Now Playing bar, so the
// two stay consistent about what counts as "nothing playing".
func (c *trackInfoCard) render(song mpdclient.Song) {
	if song.DisplayName() == "" {
		c.SetText("[::d]Nothing playing[-:-:-]")
		return
	}

	track := song.Title
	if track == "" {
		track = baseName(song.File)
	}
	year := song.Date
	if len(year) > 4 {
		year = year[:4]
	}

	lines := []string{
		fmt.Sprintf("🎵 %s", track),
		fmt.Sprintf("💿 %s", song.Album),
		fmt.Sprintf("🎤 %s", song.Artist),
		fmt.Sprintf("🏷️ %s", song.Genre),
		fmt.Sprintf("📅 %s", year),
	}
	c.SetText(strings.Join(lines, "\n"))
}

// openTrackInfo opens the 'i' track info card. Unlike the centered()
// overlays, it's positioned by its own Draw override, so it's shown
// directly rather than wrapped in centered()'s full-screen grid. It always
// takes focus while open; Esc or pressing 'i' again (both handled globally
// in overlay mode, see globalInputCapture) close it and restore whichever
// panel was focused before 'i' was first pressed, same as every other
// overlay.
func (a *App) openTrackInfo() {
	a.showOverlay("track-info", a.trackInfo, a.trackInfo)
}
