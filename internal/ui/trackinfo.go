package ui

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"mpdtui/internal/metadata"
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
	// marks, tags, and bookmarks list the track's marks, tags, and bookmarks.
	// Lists rather than rows in the metadata table, because a track can
	// carry several of each and comma-joining them into a single table
	// cell ran them off the edge of the card. Present only when meta is
	// -- all three read the same local database.
	marks     *tview.TextView
	tags      *tview.TextView
	bookmarks *tview.TextView

	// playlists lists the stored playlists containing this track. Always
	// present, unlike meta: it needs no local database, only the playlist
	// scan the Playlists panel already runs (see App.playlistMembership).
	playlists *tview.TextView

	// expanded is the card-wide collapsed/expanded state, toggled with
	// Tab while the card is open (see toggleExpanded). One flag for the
	// whole card rather than one per section, deliberately: Tab means
	// "show me everything", and a section that starts summarising later
	// only has to read this flag to join in. The playlist list is
	// currently the only thing it affects.
	expanded bool
	// meta is nil when metaDB is inactive -- decided once at construction
	// (mirrors settingsView.databaseInteractive), never rechecked, since
	// App.metaDB's nil-ness never changes after Run constructs App. render
	// skips the metadata table entirely when nil, rather than showing an
	// empty or explanatory one -- this is a passive info card, not a
	// feature entry point the way Settings' Database tab is, so there's no
	// "here's how to enable it" call to action needed here.
	meta *tview.Table

	// markRows/tagRows/bookmarkRows/playlistRows are how many card rows each list
	// section currently occupies, kept so height() can report the card's
	// real size after a section has grown or shrunk.
	markRows     int
	tagRows      int
	bookmarkRows int
	playlistRows int

	app *App
}

func newTrackInfoCard(app *App) *trackInfoCard {
	identity := tview.NewTextView().SetDynamicColors(true)
	identity.SetBorderPadding(1, 0, 1, 0)

	flex := tview.NewFlex().SetDirection(tview.FlexRow)
	flex.SetBorder(true).SetTitle(" Track Info ")

	marks := tview.NewTextView().SetDynamicColors(true)
	marks.SetBorderPadding(1, 0, 1, 0)

	tags := tview.NewTextView().SetDynamicColors(true)
	tags.SetBorderPadding(1, 0, 1, 0)

	playlists := tview.NewTextView().SetDynamicColors(true)
	// A blank row above the heading: without it the section runs
	// straight into whatever precedes it and the two read as one block.
	playlists.SetBorderPadding(1, 0, 1, 0)

	bookmarks := tview.NewTextView().SetDynamicColors(true)
	bookmarks.SetBorderPadding(1, 0, 1, 0)

	c := &trackInfoCard{Flex: flex, identity: identity, marks: marks, tags: tags, bookmarks: bookmarks, playlists: playlists, app: app}

	// Fixed row counts, not proportions: every section here has a known
	// maximum number of lines, so stretching them to fill the card just
	// opens gaps between them. Sizing each to its own content also means
	// the card's height is the sum of what it actually shows, which is
	// what height() reports for positioning.
	flex.AddItem(identity, trackInfoIdentityLines, 0, false)
	if app.metaDB != nil {
		meta := tview.NewTable()
		meta.SetSelectable(false, false)
		meta.SetBorderPadding(0, 0, 1, 0)
		c.meta = meta
		flex.AddItem(meta, trackInfoMetaLines, 0, false)
		flex.AddItem(marks, trackInfoMarkSectionLines, 0, false)
		c.markRows = trackInfoMarkSectionLines
		flex.AddItem(tags, trackInfoTagSectionLines, 0, false)
		c.tagRows = trackInfoTagSectionLines
		flex.AddItem(bookmarks, trackInfoBookmarkSectionLines, 0, false)
		c.bookmarkRows = trackInfoBookmarkSectionLines
	}
	flex.AddItem(playlists, trackInfoPlaylistSectionLines, 0, false)
	c.playlistRows = trackInfoPlaylistSectionLines
	return c
}

// height is the card's natural height: its border plus whichever
// sections it actually has, at their current sizes. Computed rather than
// fixed because the metadata table is only present when track_metadata
// is active (a card sized for it regardless would sit with a hole in the
// middle for everyone who has not turned that on), and because the
// playlist section grows when the card is expanded.
func (c *trackInfoCard) height() int {
	h := trackInfoCardBorderLines + trackInfoIdentityLines + c.playlistRows
	if c.meta != nil {
		h += trackInfoMetaLines + c.markRows + c.tagRows + c.bookmarkRows
	}
	return h
}

// fixedHeight is the card's height with every section at its collapsed
// size -- what the card occupies before Tab is ever pressed, and the
// baseline expandableRows measures spare quadrant space against.
func (c *trackInfoCard) fixedHeight() int {
	h := trackInfoCardBorderLines + trackInfoIdentityLines + trackInfoPlaylistSectionLines
	if c.meta != nil {
		h += trackInfoMetaLines + trackInfoMarkSectionLines + trackInfoTagSectionLines + trackInfoBookmarkSectionLines
	}
	return h
}

// toggleExpanded is Tab while the card is open: flips every collapsible
// section between its summary and its full contents, then re-renders so
// the card resizes to match.
func (c *trackInfoCard) toggleExpanded() {
	c.expanded = !c.expanded
	c.app.renderTrackInfo()
}

// expandableRows is how many extra rows a section may grow into when
// expanded: whatever the Queue panel has spare beyond the card's
// collapsed size, since an expanded card may grow upwards into the rest
// of the panel (see cardRect).
//
// Expanding is bounded rather than unbounded because the card is clamped
// to the panel -- growing past it would not show more, it would silently
// clip the bottom of the list, which is a worse answer than saying how
// many were left out.
func (c *trackInfoCard) expandableRows() int {
	_, _, _, ph := c.app.queue.table.GetRect()
	spare := ph - c.fixedHeight()
	if spare < 0 {
		return 0
	}
	return spare
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
//
// The card grew taller when the playlist section was added. That does
// not move it: cardRect anchors the card at the quadrant's top-left
// corner, so extra height extends downwards, and the clamp there keeps
// it inside the quadrant -- which ends exactly where the Queue panel
// does. The card's position on screen is unchanged; only its bottom edge
// moved. Its height is no longer a constant either, since it depends on
// which sections are present -- see trackInfoCard.height.
const (
	trackInfoCardWidth = 46

	// Section heights, in rows. identity is its one padding row plus up
	// to 7 content lines; meta is its four fixed rows; the playlist
	// section is its padding row, a heading, trackInfoPlaylistLines
	// names, and the "+N more" line.
	trackInfoCardBorderLines      = 2
	trackInfoIdentityLines        = 8
	trackInfoMetaLines            = 2
	trackInfoMarkSectionLines     = trackInfoMarkLines + 3
	trackInfoTagSectionLines      = trackInfoTagLines + 3
	trackInfoBookmarkSectionLines = trackInfoBookmarkLines + 3
	trackInfoPlaylistSectionLines = trackInfoPlaylistLines + 3

	// trackInfoPlaylistLines is how many playlist names the card lists
	// before summarising the rest as a count. A track usually sits in a
	// handful of playlists; one that sits in thirty should not push the
	// card past the Queue panel it floats inside.
	trackInfoPlaylistLines = 4

	// trackInfoMarkLines and trackInfoTagLines are the same cap for
	// marks and tags, lower because a track carrying more than a couple
	// of remarks or labels is unusual where belonging to several
	// playlists is not.
	trackInfoMarkLines     = 3
	trackInfoTagLines      = 3
	trackInfoBookmarkLines = 3
)

// cardRect returns where the floating card sits over the Queue panel
// (px, py, pw, ph) for a card wanting want rows.
//
// Up to the bottom-right quadrant's own height the card sits at that
// quadrant's top-left corner, which is what keeps its position fixed as
// sections are added: the extra rows extend downwards towards the
// panel's bottom edge.
//
// Past that height -- only reachable by expanding a section with Tab --
// the quadrant has no more room below, so the card keeps its bottom edge
// where it is and grows upwards instead, bounded by the panel's own top.
// Clamping it to the quadrant instead would make expanding almost
// pointless: on an ordinary terminal the collapsed card already nearly
// fills the quadrant, so Tab would buy a row or two. Growing upwards
// keeps the card in the same corner, covering more of the Queue it
// already floats over.
func cardRect(px, py, pw, ph, want int) (int, int, int, int) {
	qx, qy, qw, qh := quadrantRect(px, py, pw, ph)

	cw := trackInfoCardWidth
	if cw > qw {
		cw = qw
	}
	ch := want
	if ch > ph {
		ch = ph
	}
	if ch < 0 {
		ch = 0
	}

	cy := qy
	if ch > qh {
		cy = qy + qh - ch
		if cy < py {
			cy = py
		}
	}
	return qx, cy, cw, ch
}

// positionOverQueue sets the card's own rect to float inside the
// bottom-right quadrant of the Queue table's current rect. Split out from
// Draw so the positioning math is testable without a real tcell.Screen.
func (c *trackInfoCard) positionOverQueue() {
	x, y, w, h := c.app.queue.table.GetRect()
	c.SetRect(cardRect(x, y, w, h, c.height()))
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
	// The live decoder's numbers belong to the loaded track and no
	// other, so they are passed through only when the card is actually
	// showing that track -- which, while paused, it does whenever the
	// cursor happens to be sitting on it.
	st := mpdclient.Status{}
	if a.hasLoadedTrack() && song.File == a.currentSong.File {
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
		c.setPlaylistSection("", trackInfoPlaylistSectionLines)
		if c.meta != nil {
			c.meta.Clear()
			c.markRows = trackInfoMarkSectionLines
			c.marks.SetText("")
			c.ResizeItem(c.marks, trackInfoMarkSectionLines, 0)
			c.tagRows = trackInfoTagSectionLines
			c.tags.SetText("")
			c.ResizeItem(c.tags, trackInfoTagSectionLines, 0)
			c.bookmarkRows = trackInfoBookmarkSectionLines
			c.bookmarks.SetText("")
			c.ResizeItem(c.bookmarks, trackInfoBookmarkSectionLines, 0)
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

	// One database read for both the metadata table and the marks
	// section, rather than each fetching the same row on every tick.
	var meta metadata.Track
	if c.meta != nil {
		meta, _ = c.app.metaDB.Get(song.File)
		c.renderMeta(meta)
	}
	c.renderSections(song.File, meta)
}

// renderSections fills both list sections for file and resizes each to
// the rows it actually needs, so the card grows and shrinks with its
// content rather than reserving its expanded size permanently.
//
// The two sections share one growth budget: expanding is bounded by the
// Queue panel, so a track with thirty playlists must not starve its own
// marks of every spare row (see shareGrowth).
func (c *trackInfoCard) renderSections(file string, track metadata.Track) {
	markMax, tagMax, bookmarkMax, playlistMax := trackInfoMarkLines, trackInfoTagLines, trackInfoBookmarkLines, trackInfoPlaylistLines
	if c.expanded {
		extra := shareGrowth([]int{
			len(track.Marks) - markMax,
			len(track.Tags) - tagMax,
			len(track.Bookmarks) - bookmarkMax,
			len(c.app.playlistMembership[file]) - playlistMax,
		}, c.expandableRows())
		markMax += extra[0]
		tagMax += extra[1]
		bookmarkMax += extra[2]
		playlistMax += extra[3]
	}

	if c.meta != nil {
		text, rows := marksSection(track.Marks, markMax)
		c.markRows = rows
		c.marks.SetText(text)
		c.ResizeItem(c.marks, rows, 0)

		text, rows = tagsSection(track.Tags, tagMax)
		c.tagRows = rows
		c.tags.SetText(text)
		c.ResizeItem(c.tags, rows, 0)

		text, rows = bookmarksSection(track.Bookmarks, bookmarkMax)
		c.bookmarkRows = rows
		c.bookmarks.SetText(text)
		c.ResizeItem(c.bookmarks, rows, 0)
	}
	text, rows := playlistsSection(c.app.playlistMembership, file, playlistMax)
	c.setPlaylistSection(text, rows)
}

// setPlaylistSection applies the section's text and its row count in one
// place, so the Flex item's size can never disagree with what was
// actually written into it.
func (c *trackInfoCard) setPlaylistSection(text string, rows int) {
	c.playlistRows = rows
	c.playlists.SetText(text)
	c.ResizeItem(c.playlists, rows, 0)
}

// shareGrowth splits spare rows between sections asking for extra ones.
// Everyone gets what they ask for when it all fits; otherwise the rows
// go round one at a time to whoever still wants more, so no section is
// starved by a longer neighbour and none is handed rows it cannot use.
func shareGrowth(want []int, spare int) []int {
	got := make([]int, len(want))
	for i, w := range want {
		if w < 0 {
			want[i] = 0
		}
	}
	for spare > 0 {
		progressed := false
		for i := range want {
			if spare == 0 {
				break
			}
			if got[i] < want[i] {
				got[i]++
				spare--
				progressed = true
			}
		}
		if !progressed {
			break
		}
	}
	return got
}

// listSectionText renders one of the card's list sections: a heading,
// then at most max of items, then a "+N more (Tab)" line if any were
// left out. Returns the text and how many card rows it occupies, the
// section's own padding row included.
//
// Shared by the marks and playlists sections so the two cannot drift
// into looking like different kinds of thing, and so a third one is a
// call rather than a copy.
func listSectionText(heading string, items []string, max int) (string, int) {
	const padding = 1 // the section's own top padding row
	if max < 1 {
		max = 1
	}

	shown := items
	var extra int
	if len(shown) > max {
		extra = len(shown) - max
		shown = shown[:max]
	}

	lines := make([]string, 0, len(shown)+2)
	lines = append(lines, heading)
	lines = append(lines, shown...)
	if extra > 0 {
		lines = append(lines, fmt.Sprintf("[::d]  +%d more (Tab)[-:-:-]", extra))
	}
	return strings.Join(lines, "\n"), padding + len(lines)
}

// marksSection renders the "Marks" section from a track's marks, each on
// its own line in its own color.
//
// A list rather than the single metadata-table row this used to be:
// marks became many-to-many, and comma-joining several reasons into one
// table cell ran them straight off the side of the card. Reads exactly
// like the playlists section below it, including responding to Tab.
func marksSection(marks []metadata.MarkReason, max int) (string, int) {
	heading := "[::b]🔖 Marks[-:-:-]"
	if len(marks) == 0 {
		return heading + "\n[::d]  none[-:-:-]", 2
	}
	items := make([]string, len(marks))
	for i, m := range marks {
		items[i] = fmt.Sprintf("  • [%s]%s[-]", markColor(m).String(),
			splitConjuncts(truncateWithEllipsis(m.Reason, trackInfoPlaylistNameMaxLen)))
	}
	return listSectionText(heading, items, max)
}

// tagsSection renders the "Tags" section, the same shape as marks. Tags
// carry no per-entry color the way marks do -- a mark's color is how the
// Queue's narrow Mark column tells one tick from another, and tags have
// no such column to disambiguate.
func tagsSection(tags []metadata.Tag, max int) (string, int) {
	// A width-2 glyph, like the other section headings: 🏷 is width 1 in
	// both tview and the terminal (no drift, but it sits a column short
	// of 🔖 and 📃 above and below it). Not 🏷️ itself, which is width 2
	// but already labels Genre in the identity block above.
	heading := "[::b]📌 Tags[-:-:-]"
	if len(tags) == 0 {
		return heading + "\n[::d]  none[-:-:-]", 2
	}
	items := make([]string, len(tags))
	for i, t := range tags {
		items[i] = "  • " + splitConjuncts(truncateWithEllipsis(t.Tagname, trackInfoPlaylistNameMaxLen))
	}
	return listSectionText(heading, items, max)
}

func bookmarksSection(bookmarks []metadata.Bookmark, max int) (string, int) {
	heading := "[::b]🚩 Bookmarks[-:-:-]"
	if len(bookmarks) == 0 {
		return heading + "\n[::d]  none[-:-:-]", 2
	}
	items := make([]string, len(bookmarks))
	for i, bm := range bookmarks {
		pos := FormatDuration(time.Duration(bm.PositionSeconds * float64(time.Second)))
		items[i] = fmt.Sprintf("  • [%s] %s", pos,
			splitConjuncts(truncateWithEllipsis(bm.Text, trackInfoPlaylistNameMaxLen)))
	}
	return listSectionText(heading, items, max)
}

// playlistsSection renders the "In playlists" section for file from
// membership (App.playlistMembership), listing at most max names and
// summarising any remainder. Returns the text and the number of card
// rows it occupies (its padding row included).
//
// A nil membership map means the background scan has not landed yet,
// which is deliberately not the same answer as "this track is in no
// playlist" -- saying "none" before looking would be wrong for the first
// few seconds of every session, and wrong in a way the user cannot tell
// apart from the truth. Names are split for display the same way every
// other MPD-provided string is (see conjuncts.go): this card floats over
// the Queue, so a name that mis-measures corrupts the rows behind it.
func playlistsSection(membership map[string][]string, file string, max int) (string, int) {
	heading := "[::b]📃 In playlists[-:-:-]"
	if membership == nil {
		return heading + "\n[::d]  loading…[-:-:-]", 2
	}
	names := membership[file]
	if len(names) == 0 {
		return heading + "\n[::d]  none[-:-:-]", 2
	}
	items := make([]string, len(names))
	for i, n := range names {
		items[i] = "  • " + splitConjuncts(truncateWithEllipsis(n, trackInfoPlaylistNameMaxLen))
	}
	return listSectionText(heading, items, max)
}

// trackInfoPlaylistNameMaxLen caps a listed playlist name so a long one
// cannot widen the card or wrap onto a second line and push the rest of
// the list out of view. Sized to the card's own width minus its border,
// padding and the bullet.
const trackInfoPlaylistNameMaxLen = trackInfoCardWidth - 8

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

// renderMeta fills the Rating/Plays/Tags metadata table from an
// already-read Track -- only called when c.meta != nil (metaDB active).
// No header row (unlike Settings' Config/Database tables): every row
// already names its own field in column 0, and this card has much less
// room to spare than a full-screen overlay.
//
// Marks and Tags used to be rows here. Both moved out to their own
// sections once a track could carry several of each: comma-joined into
// one table cell they ran off the side of the card, where a list can be
// capped and expanded like the playlists below it.
func (c *trackInfoCard) renderMeta(track metadata.Track) {
	c.meta.Clear()

	rows := []struct {
		label string
		value *tview.TableCell
	}{
		{"Rating", tview.NewTableCell(ratingStars(track.Rating)).SetTextColor(queueRatingColor)},
		{"Plays", tview.NewTableCell(strconv.Itoa(track.PlayCount))},
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
