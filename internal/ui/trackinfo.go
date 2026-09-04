package ui

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"mpdtui/internal/mpdclient"
)

// trackInfoCard shows the currently playing track's Track/Album/Artist/
// Genre/Year/lyrics-availability/audio-quality, plus (when track_metadata
// is active) a small Rating/Plays/Mark/Tags metadata table. It floats
// inside the bottom-right quadrant of the Queue panel (splitting that
// panel into a 2x2 grid), anchored to that quadrant's own top-left corner
// -- rather than centered on the full screen like the other overlays. The
// root primitive is a *tview.Flex (identity text on top, metadata table
// below) rather than a single TextView, so the metadata section can be a
// real table -- Draw overrides so its position is recomputed from the
// Queue table's live rect on every frame, the same "read another
// primitive's current rect at draw time" trick albumArtPanel.draw uses
// for the Kitty image, which is what makes this track a terminal resize
// without any extra wiring.
type trackInfoCard struct {
	*tview.Flex
	identity *tview.TextView
	// meta is nil when metaDB is inactive -- decided once at construction
	// (mirrors settingsView.databaseInteractive), never rechecked, since
	// App.metaDB's nil-ness never changes after Run constructs App. render
	// skips the metadata table entirely when nil, rather than showing an
	// empty or explanatory one -- this is a passive info card, not a
	// feature entry point the way Settings' Database tab is, so there's no
	// "here's how to enable it" call to action needed here.
	meta *tview.Table
	app  *App
}

func newTrackInfoCard(app *App) *trackInfoCard {
	identity := tview.NewTextView().SetDynamicColors(true)
	identity.SetBorderPadding(1, 0, 1, 0)

	flex := tview.NewFlex().SetDirection(tview.FlexRow)
	flex.SetBorder(true).SetTitle(" Track Info ")

	c := &trackInfoCard{Flex: flex, identity: identity, app: app}
	if app.metaDB != nil {
		meta := tview.NewTable()
		meta.SetSelectable(false, false)
		meta.SetBorderPadding(0, 0, 1, 0)
		c.meta = meta
		// Proportions roughly follow each section's own line count (7
		// identity lines vs. 4 metadata rows) so neither is starved of
		// space relative to how much it actually needs.
		flex.AddItem(identity, 0, 7, false).AddItem(meta, 0, 4, false)
	} else {
		flex.AddItem(identity, 0, 1, false)
	}
	return c
}

// quadrantRect returns the bottom-right quarter of the rect (x, y, w, h):
// half width, half height (rounded down), with its own top-left corner at
// the source rect's horizontal and vertical midpoint.
func quadrantRect(x, y, w, h int) (int, int, int, int) {
	qw, qh := w/2, h/2
	return x + w - qw, y + h - qh, qw, qh
}

// trackInfoCardWidth/trackInfoCardHeight are the floating card's fixed
// footprint: just enough to comfortably fit its own content (up to 8
// identity lines including the top padding row, 4 metadata rows, 2
// border rows = 14, plus a little slack) without clipping, regardless of
// how big the Queue panel happens to be -- explicit correction after an
// earlier version sized the card as a fraction of the Queue panel's own
// quadrant, which made it balloon to dominate most of the screen on a
// normal-sized terminal. A fixed, compact size reads as "a small card
// floating in the corner" the way it originally did, rather than scaling
// up with the window.
const (
	trackInfoCardWidth  = 46
	trackInfoCardHeight = 15
)

// cardRect returns where the floating card sits within its target
// quadrant (x, y, w, h): anchored at the quadrant's own top-left corner,
// sized to trackInfoCardWidth x trackInfoCardHeight -- clamped down to
// the quadrant's own size if that's smaller (a small enough terminal),
// so the card never overflows past the quadrant it's meant to float
// inside of.
func cardRect(x, y, w, h int) (int, int, int, int) {
	cw, ch := trackInfoCardWidth, trackInfoCardHeight
	if cw > w {
		cw = w
	}
	if ch > h {
		ch = h
	}
	return x, y, cw, ch
}

// positionOverQueue sets the card's own rect to float inside the
// bottom-right quadrant of the Queue table's current rect. Split out from
// Draw so the positioning math is testable without a real tcell.Screen.
func (c *trackInfoCard) positionOverQueue() {
	x, y, w, h := c.app.queue.table.GetRect()
	c.SetRect(cardRect(quadrantRect(x, y, w, h)))
}

// Draw positions the card over the bottom-right quadrant of the Queue
// table's current rect, then delegates to the embedded Flex to actually
// paint it (identity text and, when active, the metadata table).
func (c *trackInfoCard) Draw(screen tcell.Screen) {
	c.positionOverQueue()
	c.Flex.Draw(screen)
}

// renderTrackInfo re-renders the 'i' card for whichever track
// App.targetSong resolves to -- the playing one, or the Queue selection
// when nothing is playing, so the card is still useful (and still shows
// local rating/plays/mark) for a track you have merely scrolled to with
// playback stopped, instead of the bare "Nothing playing" it used to be.
//
// The live status is only passed through when it actually describes that
// track: bitrate and sample format are properties of the running decoder,
// not of the track's tags, so attributing the playing track's numbers to
// a merely-selected one would be a lie. A zero Status renders the quality
// line as empty (see FormatAudioQuality), which is the honest answer.
//
// Called on every ~500ms refresh tick regardless of whether the card is
// open, same as before -- resolving the target is an in-memory table
// lookup, no MPD round-trip of its own.
func (a *App) renderTrackInfo() {
	song, ok := a.targetSong()
	if !ok {
		a.trackInfo.render(mpdclient.Song{}, mpdclient.Status{})
		return
	}
	st := mpdclient.Status{}
	if a.hasLivePlayback() {
		st = a.currentStatus
	}
	a.trackInfo.render(song, st)
}

// render fills in the card from song/status, or "Nothing playing" if
// there is no song at all -- the same emptiness check (DisplayName == "")
// App.renderNowPlaying already uses for the Now Playing bar, so the two
// stay consistent about what counts as "nothing playing". st supplies the
// live audio-quality line (Song carries no bitrate/format of its own --
// that's a property of the active decoder, not the track's tags), and is
// passed zeroed when song isn't the one actually playing (see
// App.renderTrackInfo).
func (c *trackInfoCard) render(song mpdclient.Song, st mpdclient.Status) {
	if song.DisplayName() == "" {
		c.identity.SetText("[::d]Nothing playing[-:-:-]")
		if c.meta != nil {
			c.meta.Clear()
		}
		return
	}

	track := song.Title
	if track == "" {
		track = baseName(song.File)
	}
	year := yearFromDate(song.Date)

	lines := []string{
		fmt.Sprintf("🎵 %s", track),
		fmt.Sprintf("💿 %s", song.Album),
		fmt.Sprintf("🎤 %s", song.Artist),
		fmt.Sprintf("🏷️ %s", song.Genre),
		fmt.Sprintf("📅 %s", year),
	}
	if c.app.musicDir != "" {
		lines = append(lines, fmt.Sprintf("📝 %s", lyricsFormatBadges(c.app.musicDir, song.File)))
	}
	lines = append(lines, fmt.Sprintf("🎚️ %s", FormatAudioQuality(st.Bitrate, st.AudioFormat)))
	c.identity.SetText(strings.Join(lines, "\n"))

	if c.meta != nil {
		c.renderMeta(song.File)
	}
}

// lyricsFormatBadges renders colored text labels for whichever lyrics
// formats exist for file under musicDir (see internal/lyrics) -- "LRC"
// (green) and/or "TXT" (orange), space-separated when both are present,
// "" if neither -- explicit request to show which format(s), not just a
// single present/absent tick the way the Queue table's Lyr column used
// to (and, for a single glance-able badge in a narrow column, still
// does -- see lyricsCellText, queue.go). Reuses lyricsAvailableFormats
// (lyrics.go) so this card and the Lyr column can never disagree about
// what's actually available.
func lyricsFormatBadges(musicDir, file string) string {
	var labels []string
	for _, f := range lyricsAvailableFormats(musicDir, file) {
		switch f {
		case lyricsFormatLRC:
			labels = append(labels, fmt.Sprintf("[%s::b]LRC[-:-:-]", lyricsLRCColor))
		case lyricsFormatTxt:
			labels = append(labels, fmt.Sprintf("[%s::b]TXT[-:-:-]", lyricsTxtColor))
		}
	}
	return strings.Join(labels, " ")
}

// renderMeta fills the Rating/Plays/Mark/Tags metadata table for file --
// only called when c.meta != nil (metaDB active). No header row (unlike
// Settings' Config/Database tables): every row already names its own
// field in column 0, and this card has much less room to spare than a
// full-screen overlay. Errors from metaDB.Get are swallowed, same as
// renderNowPlaying's identical call -- this runs unconditionally on every
// ~500ms refresh tick regardless of whether the card is actually open, so
// a transient read error flashing every tick would be far more disruptive
// than just showing a zero-opinion row until it clears.
func (c *trackInfoCard) renderMeta(file string) {
	c.meta.Clear()
	track, _ := c.app.metaDB.Get(file)

	mark := tview.NewTableCell("-")
	if track.Mark != nil {
		mark = tview.NewTableCell(track.Mark.Reason).SetTextColor(markColor(track.Mark))
	}

	tags := "-"
	if len(track.Tags) > 0 {
		names := make([]string, len(track.Tags))
		for i, tg := range track.Tags {
			names[i] = tg.Tagname
		}
		tags = strings.Join(names, ", ")
	}

	rows := []struct {
		label string
		value *tview.TableCell
	}{
		{"Rating", tview.NewTableCell(ratingStars(track.Rating)).SetTextColor(queueRatingColor)},
		{"Plays", tview.NewTableCell(strconv.Itoa(track.PlayCount))},
		{"Mark", mark},
		{"Tags", tview.NewTableCell(tags)},
	}
	for r, row := range rows {
		c.meta.SetCell(r, 0, tview.NewTableCell(row.label))
		c.meta.SetCell(r, 1, row.value.SetExpansion(1))
	}
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
