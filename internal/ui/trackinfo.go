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
// primitive's current rect at draw time" trick albumart.Panel.Draw uses
// for the Kitty image, which is what makes this track a terminal resize
// without any extra wiring.
type trackInfoCard struct {
	*tview.Box
	flex     *tview.Flex
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

	// identityRows/markRows/tagRows/bookmarkRows/playlistRows are how many
	// card rows each section currently occupies, kept so contentHeight()
	// can report the card's real size after a section has grown or
	// shrunk. Every one of them is the section's *actual* size, not the
	// maximum it is allowed to reach.
	identityRows int
	markRows     int
	tagRows      int
	bookmarkRows int
	playlistRows int

	// collapsedRows is contentHeight() as of the last render done while
	// collapsed, and is what height() sizes the card to. Recorded rather
	// than recomputed because expanding must not resize the card (see
	// height), so once expanded the live contentHeight is the wrong
	// answer -- it is the thing being scrolled through.
	collapsedRows int

	// inspectSong is the track currently being inspected while the card is
	// open, navigated via j/k. When nil, renderTrackInfo falls back to
	// targetSong (playing track or selection).
	inspectSong *mpdclient.Song

	scrollOffset int

	app *App
}

func newTrackInfoCard(app *App) *trackInfoCard {
	identity := tview.NewTextView().SetDynamicColors(true)
	identity.SetBorderPadding(1, 0, 1, 0)

	flex := tview.NewFlex().SetDirection(tview.FlexRow)
	box := tview.NewBox().SetBorder(true).SetTitle(" Track Info ")

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

	c := &trackInfoCard{Box: box, flex: flex, identity: identity, marks: marks, tags: tags, bookmarks: bookmarks, playlists: playlists, app: app}

	// Fixed row counts, not proportions: every section here has a known
	// maximum number of lines, so stretching them to fill the card just
	// opens gaps between them. Sizing each to its own content also means
	// the card's height is the sum of what it actually shows, which is
	// what height() reports for positioning.
	flex.AddItem(identity, trackInfoIdentityLines, 0, false)
	c.identityRows = trackInfoIdentityLines
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

// height is the card's on-screen height: its border plus the rows its
// sections actually filled the last time it was drawn collapsed.
//
// Sized to real content rather than to the sections' maxima. Those
// maxima are what each section may grow to, not what it typically
// needs: a track with no marks, tags or bookmarks fills two rows in
// each of those sections and the card used to reserve six apiece, so
// most of its lower half was blank.
//
// Still constant while the card is open, though -- expanding with Tab
// does not resize it, the content overflows inside and scrolls (see
// contentHeight and Draw), so the card never jumps around under the
// cursor as sections grow. That is why this reads collapsedRows instead
// of calling contentHeight directly.
//
// Falls back to the old fixed sum before the first render, when there
// are no real row counts to go on yet. Only a Queue panel too short to
// hold the result clamps it (see cardRect).
func (c *trackInfoCard) height() int {
	if c.collapsedRows > 0 {
		return trackInfoCardBorderLines + c.collapsedRows
	}
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
	if !c.expanded {
		c.scrollOffset = 0
	}
	c.app.renderTrackInfo()
	// j/k changes meaning with this flag -- see updateHintBar.
	c.app.updateHintBar()
}

// contentHeight is how many rows the sections currently want, excluding
// the border -- the counterpart to height(), which is what they are
// actually given. Collapsed the two roughly agree; expanded this grows
// past the card and the difference is exactly what Draw scrolls through.
func (c *trackInfoCard) contentHeight() int {
	h := c.identityRows + c.playlistRows
	if c.meta != nil {
		h += trackInfoMetaLines + c.markRows + c.tagRows + c.bookmarkRows
	}
	return h
}

// maxScroll is the furthest scrollOffset that still shows content: the
// overflow past innerH rows, or 0 when everything already fits.
func (c *trackInfoCard) maxScroll(innerH int) int {
	if over := c.contentHeight() - innerH; over > 0 {
		return over
	}
	return 0
}

// clampScroll pins scrollOffset into [0, maxScroll]. Called both from
// Draw (which knows the real inner height) and from handleNav, so the
// field never holds a nonsense value between a keypress and the next
// frame -- j held down at the bottom of the list would otherwise run
// scrollOffset up without bound until Draw pulled it back.
func (c *trackInfoCard) clampScroll(innerH int) {
	if max := c.maxScroll(innerH); c.scrollOffset > max {
		c.scrollOffset = max
	}
	if c.scrollOffset < 0 {
		c.scrollOffset = 0
	}
}

// quadrantRect returns the bottom-right quarter of the rect (x, y, w, h):
// half width, half height (rounded down), with its own top-left corner at
// the source rect's horizontal and vertical midpoint.
func quadrantRect(x, y, w, h int) (int, int, int, int) {
	qw, qh := w/2, h/2
	return x + w - qw, y + h - qh, qw, qh
}

// trackInfoCardWidth is the floating card's fixed width: just enough to
// comfortably fit its own content without clipping, regardless of how
// big the Queue panel happens to be -- explicit correction after an
// earlier version sized the card as a fraction of the Queue panel's own
// quadrant, which made it balloon to dominate most of the screen on a
// normal-sized terminal. A fixed, compact size reads as "a small card
// floating in the corner" the way it originally did, rather than scaling
// up with the window. Its height is the same idea, but depends on which
// sections are present -- see trackInfoCard.height.
const (
	// trackInfoPageName is the card's page name on App.pages. Named
	// rather than repeated as a literal because inspectedSong asks
	// whether that page is up to decide whether the card is still open.
	trackInfoPageName = "track-info"

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
// Horizontally it takes the panel's right half (quadrantRect's x), which
// is what keeps it in the same corner whatever its height. Vertically it
// is anchored 40% down the panel -- pulled up from the old bottom
// quadrant, which no longer had room once metadata and playlists were
// added -- and slides back up towards the panel's top only as far as it
// must to fit.
//
// px/py/pw/ph are the panel's *inner* rect (see positionOverQueue), so
// every clamp here keeps the card off the panel's own border rather than
// flush against it. want is clamped to ph, so on a panel too short for
// the card the card is what gives, never the panel.
func cardRect(px, py, pw, ph, want int) (int, int, int, int) {
	// Only the right half matters here; the quadrant's own y/height are
	// what the 40% anchor below replaced.
	qx, _, qw, _ := quadrantRect(px, py, pw, ph)

	cw := trackInfoCardWidth
	if cw > qw {
		cw = qw
	}
	if cw < 0 {
		cw = 0
	}

	ch := want
	if ch > ph {
		ch = ph
	}
	if ch < 0 {
		ch = 0
	}

	cy := py + ph*40/100
	// Spilling past the panel's bottom: slide the anchor up into the
	// space above rather than overflowing (or being cut off).
	if cy+ch > py+ph {
		cy = py + ph - ch
	}
	if cy < py {
		cy = py
	}
	return qx, cy, cw, ch
}

// positionOverQueue sets the card's own rect to float inside the Queue
// table's current rect. Split out from Draw so the positioning math is
// testable without a real tcell.Screen.
//
// GetInnerRect, not GetRect: the outer rect includes the Queue panel's
// own border rows and columns, so clamping against it let the card's
// last row land exactly on the panel's bottom border and paint over it
// (and, on a narrow panel, its right border too). The inner rect is the
// panel's interior, which is the region the card is actually meant to
// float within. Valid before the first draw as well -- Box.GetInnerRect
// falls back to the border-derived rect until the Queue's own
// SetDrawFunc has run, and both agree here.
func (c *trackInfoCard) positionOverQueue() {
	x, y, w, h := c.app.queue.table.GetInnerRect()
	c.SetRect(cardRect(x, y, w, h, c.height()))
}

// clipScreen restricts SetContent calls to a rectangular boundary,
// preventing child primitives inside a Flex from overflowing their
// container -- which is exactly what the card's content does when
// expanded, since the card itself never grows (see height).
//
// SetContent is the only method that needs intercepting: it is the sole
// route tview takes to the screen when drawing these primitives. Of the
// alternatives, Fill and the deprecated SetCell are used nowhere in
// tview at all, and ShowCursor only by TextArea, which this card has
// none of. If a text-input widget is ever added to the card, ShowCursor
// needs clipping here too.
type clipScreen struct {
	tcell.Screen
	minX, minY, maxX, maxY int
}

func (s *clipScreen) SetContent(x, y int, mainc rune, comb []rune, style tcell.Style) {
	if x < s.minX || x > s.maxX || y < s.minY || y > s.maxY {
		return
	}
	s.Screen.SetContent(x, y, mainc, comb, style)
}

// Draw positions the card over the right side of the Queue table's
// current rect, anchored at the top, then delegates to the embedded Flex to
// actually paint it (identity text and, when active, the metadata table).
// Output is clipped to the card's rect so children never spill into the
// panels below.
func (c *trackInfoCard) Draw(screen tcell.Screen) {
	c.positionOverQueue()
	c.Box.DrawForSubclass(screen, c)

	_, _, w, h := c.GetRect()
	innerX, innerY, innerW, innerH := c.GetInnerRect()
	if w <= 0 || h <= 0 || innerW <= 0 || innerH <= 0 {
		return
	}

	c.clampScroll(innerH)
	c.flex.SetRect(innerX, innerY-c.scrollOffset, innerW, c.contentHeight())

	cs := &clipScreen{
		Screen: screen,
		minX:   innerX,
		minY:   innerY,
		maxX:   innerX + innerW - 1,
		maxY:   innerY + innerH - 1,
	}
	c.flex.Draw(cs)
}

// renderTrackInfo re-renders the 'i' card for whichever track
// App.targetSong resolves to -- the playing one, or the Queue selection
// when nothing is playing, so the card is still useful (and still shows
// local rating/plays/mark) for a track you have merely scrolled to with
// playback stopped, instead of the bare "Nothing playing" it used to be.
// Which track that is comes from inspectedSong, so j/k navigation wins
// over the playing track while the card is open.
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
	song, ok := a.inspectedSong()
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
	// Deferred so both exits record it -- the "nothing playing" card is
	// a real, much shorter layout, not a case to leave the previous
	// track's height standing for.
	defer c.noteCollapsedHeight()

	if song.DisplayName() == "" {
		c.setIdentitySection("[::d]Nothing playing[-:-:-]", 1)
		c.setPlaylistSection("", 0)
		if c.meta != nil {
			c.meta.Clear()
			c.markRows = 0
			c.marks.SetText("")
			c.flex.ResizeItem(c.marks, 0, 0)
			c.tagRows = 0
			c.tags.SetText("")
			c.flex.ResizeItem(c.tags, 0, 0)
			c.bookmarkRows = 0
			c.bookmarks.SetText("")
			c.flex.ResizeItem(c.bookmarks, 0, 0)
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
	c.setIdentitySection(strings.Join(lines, "\n"), len(lines))

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
		markMax = len(track.Marks)
		tagMax = len(track.Tags)
		bookmarkMax = len(track.Bookmarks)
		if membership := c.app.playlistMembership[file]; membership != nil {
			playlistMax = len(membership)
		} else {
			playlistMax = 0
		}
	}

	if c.meta != nil {
		text, rows := marksSection(track.Marks, markMax)
		c.markRows = rows
		c.marks.SetText(text)
		c.flex.ResizeItem(c.marks, rows, 0)

		text, rows = tagsSection(track.Tags, tagMax)
		c.tagRows = rows
		c.tags.SetText(text)
		c.flex.ResizeItem(c.tags, rows, 0)

		text, rows = bookmarksSection(track.Bookmarks, bookmarkMax)
		c.bookmarkRows = rows
		c.bookmarks.SetText(text)
		c.flex.ResizeItem(c.bookmarks, rows, 0)
	}
	text, rows := playlistsSection(c.app.playlistMembership, file, playlistMax)
	c.setPlaylistSection(text, rows)
}

// noteCollapsedHeight remembers the card's content height while it is
// the collapsed one on show, for height() to size the card by. Skipped
// while expanded: contentHeight is then the full, overflowing list,
// which is what Draw scrolls through rather than what the card is.
func (c *trackInfoCard) noteCollapsedHeight() {
	if !c.expanded {
		c.collapsedRows = c.contentHeight()
	}
}

// setIdentitySection applies the identity block's text and resizes it to
// the lines actually written, the section's own top padding row
// included -- the same contract setPlaylistSection has below. Without
// the resize the block holds trackInfoIdentityLines whatever it
// contains, which is a blank row on any setup without a lyrics line.
func (c *trackInfoCard) setIdentitySection(text string, lines int) {
	const padding = 1 // the section's own top padding row
	c.identityRows = padding + lines
	c.identity.SetText(text)
	c.flex.ResizeItem(c.identity, c.identityRows, 0)
}

// setPlaylistSection applies the section's text and its row count in one
// place, so the Flex item's size can never disagree with what was
// actually written into it.
func (c *trackInfoCard) setPlaylistSection(text string, rows int) {
	c.playlistRows = rows
	c.playlists.SetText(text)
	c.flex.ResizeItem(c.playlists, rows, 0)
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
	a.trackInfo.expanded = false
	a.trackInfo.scrollOffset = 0
	// inspectSong stays nil until j/k actually moves. Until then the
	// card follows targetSong live, exactly as it did before navigation
	// existed: a track change while the card is open still updates it,
	// and merely pressing 'i' does not drag the Queue cursor off
	// whatever row the user was browsing.
	a.trackInfo.inspectSong = nil
	activateOverlayBorder(a.trackInfo)
	a.showOverlay(trackInfoPageName, a.trackInfo, a.trackInfo)
	a.renderTrackInfo()
}

// inspectedSong is the track the card shows: whichever j/k last landed
// on, or -- before the first keypress, and any time the card is closed
// -- targetSong's own answer (the playing track, else the Queue cursor).
//
// The open check is what keeps a stale inspectSong from outliving the
// card. renderTrackInfo runs on every refresh tick whether or not the
// card is open, so a leftover pointer would otherwise pin the card to a
// track the user stopped looking at minutes ago. Gating on the page
// rather than clearing the field on close means no close path can
// forget to do it.
func (a *App) inspectedSong() (mpdclient.Song, bool) {
	if a.trackInfo.inspectSong != nil && a.pages.HasPage(trackInfoPageName) {
		return *a.trackInfo.inspectSong, true
	}
	return a.targetSong()
}

// handleNav is j/k (and Up/Down) while the card is open: it scrolls the
// overflowing content when the card is expanded, and walks the Queue --
// re-pointing the card at each track in turn -- when it is collapsed.
func (c *trackInfoCard) handleNav(delta int) {
	if !c.expanded {
		c.app.navigateTrackInfo(delta)
		return
	}
	_, _, _, innerH := c.GetInnerRect()
	c.scrollOffset += delta
	c.clampScroll(innerH)
}

// navigateTrackInfo moves the Queue selection by delta and points the
// card at the newly selected track, leaving it open.
func (a *App) navigateTrackInfo(delta int) {
	n := len(a.queue.songs)
	if n == 0 {
		return
	}
	idx := a.inspectedQueueIndex() + delta
	if idx < 0 {
		idx = 0
	}
	if idx >= n {
		idx = n - 1
	}
	a.queue.table.Select(idx+queueHeaderRows, 0)
	song := a.queue.songs[idx]
	a.trackInfo.inspectSong = &song
	a.trackInfo.scrollOffset = 0
	a.renderTrackInfo()
}

// inspectedQueueIndex is where in the Queue the track the card is
// currently showing sits, so the first j/k steps away from *that* track
// rather than from wherever the Queue cursor happens to be -- which,
// while something is playing, is not necessarily the same row.
//
// Falls back to the Queue cursor when the shown track is not in the
// queue at all (it can be a Library selection), and to the top of the
// queue when even that is out of range.
func (a *App) inspectedQueueIndex() int {
	if song, ok := a.inspectedSong(); ok {
		if i := a.queue.indexOf(song); i >= 0 {
			return i
		}
	}
	row, _ := a.queue.table.GetSelection()
	if idx := row - queueHeaderRows; idx >= 0 && idx < len(a.queue.songs) {
		return idx
	}
	return 0
}
