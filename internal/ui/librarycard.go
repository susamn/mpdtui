package ui

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"mpdtui/internal/libraryscan"
	"mpdtui/internal/lyricsindex"
	"mpdtui/internal/metadata"
	"mpdtui/internal/mpdclient"
)

// libraryCardPageName is the Library card's page name on App.pages.
const libraryCardPageName = "library-card"

const (
	// libraryCardWidth fits the card's widest natural line -- a
	// two-column label/value row plus a recently-added entry's
	// "Artist - Title" -- without stretching short rows across the
	// screen. The card never gets wider than this however much room
	// the Queue panel has; libraryCardMinWidth is how narrow it may be
	// squeezed before it simply overflows the panel instead.
	libraryCardWidth    = 76
	libraryCardMinWidth = 44

	// libraryCardPairWidth is the narrowest card that still puts two
	// label/value pairs on one line. Below it they stack: a second
	// column that does not fit runs the two pairs together into one
	// unreadable string, which is worse than the extra rows.
	libraryCardPairWidth = 72

	// libraryCardMarginX/Y are how much of the Queue panel stays
	// visible around the card. Without them the card's border lands
	// against the panel's own and the two read as one thick seam.
	// Narrower vertically because rows are the scarcer resource: two
	// of them are a section of the card, two columns are nothing.
	libraryCardMarginX = 2
	libraryCardMarginY = 1

	// libraryCardMaxHeight caps the card; libraryCardMinHeight keeps it
	// from rendering as a sliver while the first numbers are still
	// being counted. Between the two it is sized to its content, the
	// same rule the story card follows.
	libraryCardMaxHeight = 34
	libraryCardMinHeight = 10

	// libraryCardRecent is how many recently-added tracks the card
	// lists. Enough to answer "what did I just add", short enough that
	// the section does not push everything above it off the card.
	libraryCardRecent = 8

	// libraryCardFallouts is how many affected playlists are named
	// individually before the rest are summarised as a count. A
	// collection imported from elsewhere can have dozens; the card's
	// job is to say that it happened and which are worst, not to be
	// the repair tool.
	libraryCardFallouts = 6

	// libraryCardLabel is the label column's width, so every value in
	// a section lines up whatever its label.
	libraryCardLabel = 16

	// libraryCardFresh is how recent a snapshot has to be for
	// re-opening the card to reuse it instead of rescanning. The scan
	// is several whole-library round-trips (see librarySnapshot), so
	// reopening the card twice in a row should not pay for it twice --
	// but a minute is short enough that the numbers are never
	// meaningfully behind what the user just did.
	libraryCardFresh = time.Minute
)

// librarySnapshot is everything the Library card shows, and how much of
// it has arrived.
//
// Each section carries its own ok flag rather than the whole snapshot
// having one: the fast half (MPD's own totals, the local database)
// answers in milliseconds while the slow half is several whole-library
// scans, and a card that showed nothing until the slow half landed
// would look broken for the second it takes.
type librarySnapshot struct {
	stats   mpdclient.LibraryStats
	statsOK bool

	totals   metadata.Totals
	totalsOK bool

	// index is the lyrics search index's own account of itself (see
	// internal/lyricsindex.ReadInfo) -- whether one has been built at
	// all, how many tracks are in it and when. Read here because it is
	// the one thing 'f l' depends on that nothing in the app otherwise
	// reports: an unbuilt or stale index just returns no matches,
	// which is indistinguishable from a term that is genuinely absent.
	index   lyricsindex.Info
	indexOK bool

	scan   libraryscan.Counts
	scanOK bool

	fallouts   mpdclient.PlaylistFalloutReport
	falloutsOK bool

	recent   []mpdclient.RecentTrack
	recentOK bool

	// inFlight is true while a refresh is running, so a second 'M' (or
	// 'r') joins the one already going rather than starting a second
	// set of scans against the same connection.
	inFlight bool

	// takenAt is when the last refresh finished, zero before the first
	// one does. Shown on the card, and what libraryCardFresh is
	// measured against.
	takenAt time.Time

	// failure is the last error any section hit, kept on the card
	// rather than flashed: the hint bar reverts after three seconds,
	// and "why is this section empty" needs to stay readable next to
	// the empty section.
	failure string
}

// openLibraryCard is 'M': a standing summary of the whole collection --
// what MPD has, what is on disk beside it, what the local database has
// recorded, and where the stored playlists point at tracks that are not
// there.
//
// Opens immediately on whatever the last scan found (or on placeholders
// the first time) and fills in behind itself, rather than blocking the
// keypress on several whole-library round-trips.
func (a *App) openLibraryCard() {
	if a.librarySnap == nil {
		a.librarySnap = &librarySnapshot{}
	}
	// Word wrap on: the numbers are laid out to fit the card (see
	// writeLibraryPair), but the sentences between them -- "music_dir
	// is not configured", "track_metadata is off" -- are prose, and on
	// a narrow card prose has to wrap rather than be cut off mid-word.
	// libraryCardHeight measures with tview's own WordWrap for the
	// same reason, so the height already accounts for it.
	view := tview.NewTextView().SetDynamicColors(true).SetWordWrap(true)
	view.SetBorder(true)
	a.libraryCard = view
	a.renderLibraryCard()

	// Page first, scan second. applyLibrarySnapshot only redraws a card
	// that is actually on the page stack, so starting the scans before
	// the page exists would drop any section that answered instantly.
	a.showOverlay(libraryCardPageName, newQueueCenteredFrame(a, view, a.libraryCardSize), view)
	a.refreshLibrarySnapshot(false)
}

// queueCenteredFrame centers one primitive on the Queue panel, every
// frame.
//
// The page it sits on is given the whole screen, so centering on that
// puts the card's left border exactly on the Library panel's right one
// at common terminal widths -- two borders in adjacent columns, which
// reads as one thick seam rather than as a card floating over the
// player. Centering on the Queue instead puts it over the panel it is
// about.
//
// Reading the Queue table's rect at draw time rather than once at open
// time is the same trick trackInfoCard and albumart.Panel use, and is
// what makes the card follow a terminal resize with no extra wiring.
type queueCenteredFrame struct {
	*tview.Box
	app   *App
	child tview.Primitive
	// size reports the card's wanted width and height for this frame.
	// A function rather than two numbers because the card re-sizes
	// itself as its background sections land.
	size func() (int, int)
}

func newQueueCenteredFrame(a *App, child tview.Primitive, size func() (int, int)) *queueCenteredFrame {
	return &queueCenteredFrame{Box: tview.NewBox(), app: a, child: child, size: size}
}

func (f *queueCenteredFrame) Draw(screen tcell.Screen) {
	x, y, w, h := f.app.queue.table.GetInnerRect()
	if w <= 0 || h <= 0 {
		// Nothing drawn yet, so there is no Queue rect to centre on.
		// The page's own (full-screen) rect is a worse position but a
		// far better one than not drawing the card at all.
		x, y, w, h = f.GetRect()
	}
	cw, ch := f.size()
	cw = min(cw, w)
	ch = min(ch, h)
	f.child.SetRect(x+(w-cw)/2, y+(h-ch)/2, cw, ch)
	f.child.Draw(screen)
}

// HasFocus and InputHandler forward to the child, which is what
// showOverlay actually focuses -- without them tview would treat the
// frame as an unfocusable box sitting between the application and the
// card.
func (f *queueCenteredFrame) HasFocus() bool { return f.child.HasFocus() }

func (f *queueCenteredFrame) InputHandler() func(*tcell.EventKey, func(tview.Primitive)) {
	return f.child.InputHandler()
}

// libraryCardSize is the card's wanted width and height right now,
// re-rendering first if the Queue panel has changed width since the
// last render -- the text is laid out for a specific width (see
// writeLibraryPair), so a resize changes the content, not just the box
// around it.
func (a *App) libraryCardSize() (int, int) {
	if w := a.libraryCardWidth(); w != a.libraryCardW {
		a.renderLibraryCard()
	}
	return a.libraryCardW, a.libraryCardH
}

// libraryCardWidth is how wide the card may be inside the Queue panel:
// its preferred width, or as much of the panel as leaves a margin of
// it visible either side. A panel too narrow even for
// libraryCardMinWidth gets that anyway and the card overflows it,
// which is still more readable than a column of one-word lines.
func (a *App) libraryCardWidth() int {
	_, _, w, _ := a.queue.table.GetInnerRect()
	if w <= 0 {
		return libraryCardWidth
	}
	return max(min(libraryCardWidth, w-2*libraryCardMarginX), libraryCardMinWidth)
}

// libraryCardCap is the tallest the card may be: the Queue panel, less
// the same margin, and never more than the static cap. A zero rect
// (nothing drawn yet, as in a test) leaves the static cap in charge.
func (a *App) libraryCardCap() int {
	_, _, _, h := a.queue.table.GetInnerRect()
	if h <= 0 {
		return libraryCardMaxHeight
	}
	return max(min(libraryCardMaxHeight, h-2*libraryCardMarginY), libraryCardMinHeight)
}

// renderLibraryCard redraws the card from the current snapshot and
// returns the height it wants. Called both when the card opens and
// whenever a background section lands, which is why it tolerates the
// card being closed.
func (a *App) renderLibraryCard() {
	if a.libraryCard == nil {
		return
	}
	width := a.libraryCardWidth()
	text := renderLibrarySnapshot(a.librarySnap, a.musicDir, a.metaDB != nil, width)
	a.libraryCard.SetText(text)
	a.libraryCard.SetTitle(libraryCardTitle(a.librarySnap))
	a.libraryCardW = width
	a.libraryCardH = libraryCardHeight(text, width, a.libraryCardCap())
}

// applyLibrarySnapshot re-renders the open card after a background
// section arrives. The card grows as the slow sections fill in; the
// frame reads the new height on the next draw, so nothing here has to
// touch the page stack -- which matters because the two scans finish
// independently, and taking the page down to put a taller one up lets
// the other one arrive to find no page and drop its own redraw.
func (a *App) applyLibrarySnapshot() {
	if a.libraryCard == nil || !a.pages.HasPage(libraryCardPageName) {
		return
	}
	a.renderLibraryCard()
}

// refreshLibrarySnapshot starts a background rescan. force is 'r'
// inside the card; without it a snapshot younger than libraryCardFresh
// is left alone.
//
// Two goroutines rather than one, split by cost: MPD's stats command
// and the local database answer immediately, while the file list, the
// sidecar scan, the recently-added listing and the playlist fallout
// scan are whole-library work. Splitting them is what lets the top of
// the card be correct while the bottom still says it is counting.
func (a *App) refreshLibrarySnapshot(force bool) {
	snap := a.librarySnap
	if snap.inFlight {
		return
	}
	if !force && !snap.takenAt.IsZero() && time.Since(snap.takenAt) < libraryCardFresh {
		return
	}
	snap.inFlight = true
	snap.failure = ""

	client := a.client
	musicDir := a.musicDir
	metaDB := a.metaDB
	indexPath := a.cfg.LyricsIndexPath

	go func() {
		stats, statsErr := client.LibraryStats()
		var totals metadata.Totals
		var totalsErr error
		if metaDB != nil {
			totals, totalsErr = metaDB.Totals()
		}
		// A missing index file is not an error (ReadInfo reports it as
		// Exists false), so only a corrupt or unreadable one lands
		// here -- and that is worth saying, because the symptom
		// otherwise is 'f l' quietly matching nothing.
		index, indexErr := lyricsindex.ReadInfo(indexPath)

		a.applyToUI(func() {
			if statsErr == nil {
				snap.stats, snap.statsOK = stats, true
			} else {
				snap.failure = statsErr.Error()
			}
			if metaDB != nil {
				if totalsErr == nil {
					snap.totals, snap.totalsOK = totals, true
				} else {
					snap.failure = totalsErr.Error()
				}
			}
			if indexErr == nil {
				snap.index, snap.indexOK = index, true
			} else {
				snap.failure = indexErr.Error()
			}
			a.applyLibrarySnapshot()
		})
	}()

	go func() {
		files, filesErr := client.LibraryFiles()
		var counts libraryscan.Counts
		if filesErr == nil && musicDir != "" {
			counts = libraryscan.Scan(musicDir, files)
		}
		recent, recentErr := client.RecentTracks(libraryCardRecent)
		fallouts, falloutErr := client.PlaylistFallouts()

		a.applyToUI(func() {
			snap.inFlight = false
			snap.takenAt = time.Now()
			switch {
			case filesErr != nil:
				snap.failure = filesErr.Error()
			case musicDir != "":
				snap.scan, snap.scanOK = counts, true
			}
			if recentErr == nil {
				snap.recent, snap.recentOK = recent, true
			} else {
				snap.failure = recentErr.Error()
			}
			if falloutErr == nil {
				snap.fallouts, snap.falloutsOK = fallouts, true
			} else {
				snap.failure = falloutErr.Error()
			}
			a.applyLibrarySnapshot()
		})
	}()
}

// handleLibraryCardRefresh is 'r' inside the card.
func (a *App) handleLibraryCardRefresh() {
	if a.librarySnap == nil {
		return
	}
	if a.librarySnap.inFlight {
		a.showMessage("already counting")
		return
	}
	a.refreshLibrarySnapshot(true)
	a.applyLibrarySnapshot()
}

// renderLibrarySnapshot lays the whole card out as one scrollable
// block, the way the story card does: the sections are of wildly
// different lengths (nought to six playlists, nought to eight tracks),
// so any fixed division of the card is wrong most of the time, and one
// wrapped column scrolls with j/k for free.
func renderLibrarySnapshot(s *librarySnapshot, musicDir string, metaActive bool, width int) string {
	var b strings.Builder

	writeLibrarySection(&b, "Collection")
	if s.statsOK {
		writeLibraryPair(&b, width, "Tracks", comma(s.stats.Tracks), "Albums", comma(s.stats.Albums))
		writeLibraryPair(&b, width, "Artists", comma(s.stats.Artists), "Playlists", comma(s.stats.Playlists))
		writeLibraryPair(&b, width, "Playtime", longDuration(s.stats.Playtime), "DB updated", stamp(s.stats.Updated))
	} else {
		writeLibraryRow(&b, "Tracks", pending(s))
	}

	writeLibrarySection(&b, "Lyrics and stories")
	switch {
	case musicDir == "":
		writeLibraryNote(&b, "music_dir is not configured -- nothing to scan")
	case !s.scanOK:
		writeLibraryRow(&b, "With lyrics", pending(s))
	default:
		sc := s.scan
		writeLibraryRow(&b, "With lyrics", fmt.Sprintf("%s of %s  %s",
			comma(sc.WithAny), comma(sc.Tracks), percent(sc.WithAny, sc.Tracks)))
		writeLibraryPair(&b, width, "Synced .lrc", comma(sc.Synced), "Plain .txt", comma(sc.Plain))
		writeLibraryPair(&b, width, "Both formats", comma(sc.Both), "Orphan files", comma(sc.Orphans))
		writeLibraryPair(&b, width, "Stories", comma(sc.Stories), "Story images", comma(sc.Images))
		if sc.Unreadable > 0 {
			writeLibraryRow(&b, "Unreadable", comma(sc.Unreadable)+" story file(s) this version will not open")
		}
	}
	writeLibraryIndex(&b, s, musicDir, width)

	writeLibrarySection(&b, "Local metadata")
	switch {
	case !metaActive:
		writeLibraryNote(&b, "track_metadata is off -- no ratings, plays, marks, tags or bookmarks")
	case !s.totalsOK:
		writeLibraryRow(&b, "Rated", pending(s))
	default:
		t := s.totals
		writeLibraryPair(&b, width, "Rated", comma(t.Rated)+average(t.Stars, t.Rated), "Played", comma(t.Played))
		writeLibraryPair(&b, width, "Plays", comma(t.Plays), "Bookmarks", bookmarkCount(t))
		writeLibraryPair(&b, width, "Marked", catalogCount(t.Marked, t.MarkReasons, "reason"),
			"Tagged", catalogCount(t.Tagged, t.Tags, "tag"))
		writeLibraryRow(&b, "Tracks known", comma(t.Tracks)+" have anything recorded against them")
	}

	writeLibrarySection(&b, "Playlist fallouts")
	writeLibraryFallouts(&b, s, width)

	writeLibrarySection(&b, "Recently added")
	writeLibraryRecent(&b, s, width)

	return b.String()
}

// writeLibraryIndex reports the lyrics search index: whether one has
// been built, how many tracks are in it and when it was built.
//
// Part of this section rather than a section of its own because it is
// the same subject from the other side -- the sidecar counts are what
// is on disk, this is how much of it 'f l' can actually search. An
// index built against a different music_dir is called out rather than
// reported as a count, because its rows are for tracks that are not
// the ones in front of you.
func writeLibraryIndex(b *strings.Builder, s *librarySnapshot, musicDir string, width int) {
	switch {
	case !s.indexOK:
		writeLibraryRow(b, "Lyrics index", pending(s))
	case !s.index.Exists:
		writeLibraryRow(b, "Lyrics index", fmt.Sprintf("[::d]not built -- press[-:-:-] [%s]I[-]", hintKeyColor))
	default:
		when := "date unknown"
		if !s.index.IndexedAt.IsZero() {
			when = s.index.IndexedAt.Local().Format("2006-01-02 15:04")
		}
		writeLibraryPair(b, width, "Lyrics index", comma(s.index.Count)+" tracks", "Indexed", when)
		if musicDir != "" && s.index.MusicDir != "" && s.index.MusicDir != musicDir {
			writeLibraryRow(b, "", fmt.Sprintf("[%s]built for %s[-]",
				flagOffColor, clip(tview.Escape(s.index.MusicDir), width-libraryCardLabel-14)))
		}
	}
}

// writeLibraryFallouts reports the playlist entries the library cannot
// resolve: the totals, then the worst offenders by name.
func writeLibraryFallouts(b *strings.Builder, s *librarySnapshot, width int) {
	if !s.falloutsOK {
		writeLibraryRow(b, "Unresolved", pending(s))
		return
	}
	r := s.fallouts
	if r.Missing == 0 {
		writeLibraryNote(b, fmt.Sprintf("every one of %s entries across %s playlists resolves",
			comma(r.Entries), comma(r.Playlists)))
		return
	}
	writeLibraryRow(b, "Unresolved", fmt.Sprintf("[%s]%s[-] of %s entries, in %s of %s playlists",
		flagOffColor, comma(r.Missing), comma(r.Entries), comma(len(r.Affected)), comma(r.Playlists)))
	shown := r.Affected
	if len(shown) > libraryCardFallouts {
		shown = shown[:libraryCardFallouts]
	}
	// The name column is whatever is left after "N of M" -- a playlist
	// name is the one field here with no natural width.
	nameCol := max(width-22, 12)
	for _, f := range shown {
		name := clip(tview.Escape(f.Name), nameCol)
		b.WriteString(fmt.Sprintf("  %s%s [%s]%s[-] of %s\n",
			name, strings.Repeat(" ", max(nameCol-tview.TaggedStringWidth(name), 0)),
			flagOffColor, comma(len(f.Missing)), comma(f.Total)))
	}
	if rest := len(r.Affected) - len(shown); rest > 0 {
		writeLibraryNote(b, fmt.Sprintf("and %s more playlist(s)", comma(rest)))
	}
}

// writeLibraryRecent lists the newest tracks by file modification time
// (see mpdclient.RecentTracks for why that is what "added" means here).
func writeLibraryRecent(b *strings.Builder, s *librarySnapshot, width int) {
	if !s.recentOK {
		writeLibraryNote(b, pending(s))
		return
	}
	if len(s.recent) == 0 {
		writeLibraryNote(b, "MPD reported no modification times")
		return
	}
	for _, t := range s.recent {
		b.WriteString(fmt.Sprintf("  [::d]%s[-:-:-]  %s\n",
			t.LastModified.Local().Format("2006-01-02"),
			clip(tview.Escape(t.DisplayName()), width-18)))
	}
}

// libraryCardTitle says how old the numbers are, in the border rather
// than in a footer row: the card is a snapshot, and a snapshot with no
// timestamp is indistinguishable from a live reading.
//
// In the border because the two rows a footer costs are two rows of
// content on a 45-line terminal, and because the keys a footer would
// also have listed are already on the hint bar while the card is open
// (see App.updateHintBar).
func libraryCardTitle(s *librarySnapshot) string {
	switch {
	case s.inFlight:
		return " Library -- counting... "
	case s.takenAt.IsZero():
		return " Library "
	default:
		return " Library -- as of " + s.takenAt.Local().Format("15:04:05") + " "
	}
}

// pending is what a section shows before its scan lands, or instead of
// its numbers when that scan failed.
func pending(s *librarySnapshot) string {
	if s.failure != "" {
		return "[" + flagOffColor + "]" + tview.Escape(s.failure) + "[-]"
	}
	return "[::d]counting...[-:-:-]"
}

func writeLibrarySection(b *strings.Builder, title string) {
	if b.Len() > 0 {
		b.WriteString("\n")
	}
	b.WriteString("[::b]" + title + "[-:-:-]\n")
}

// writeLibraryNote is an unlabelled line inside a section -- a whole
// answer that is a sentence rather than a number.
func writeLibraryNote(b *strings.Builder, text string) {
	b.WriteString("  [::d]" + text + "[-:-:-]\n")
}

func writeLibraryRow(b *strings.Builder, label, value string) {
	fmt.Fprintf(b, "  [::b]%-*s[-:-:-]%s\n", libraryCardLabel, label, value)
}

// writeLibraryPair puts two label/value pairs on one line, or stacks
// them on a card too narrow for that. The card has far more numbers
// than lines to spare, and the pairs on each line are chosen to be the
// ones worth reading against each other.
func writeLibraryPair(b *strings.Builder, width int, l1, v1, l2, v2 string) {
	if width < libraryCardPairWidth {
		writeLibraryRow(b, l1, v1)
		writeLibraryRow(b, l2, v2)
		return
	}
	const col = 36
	left := fmt.Sprintf("[::b]%-*s[-:-:-]%s", libraryCardLabel, l1, v1)
	pad := col - libraryCardLabel - tview.TaggedStringWidth(v1)
	if pad < 2 {
		pad = 2
	}
	fmt.Fprintf(b, "  %s%s[::b]%-*s[-:-:-]%s\n", left, strings.Repeat(" ", pad), libraryCardLabel, l2, v2)
}

// libraryCardHeight sizes the card to its content, up to cap, measured
// against width, which is what the text will actually be drawn at -- the story
// card's rule, and for the same reason: a fixed height leaves a short
// card sitting above a field of empty box.
func libraryCardHeight(text string, width, cap int) int {
	const border = 2
	lines := 0
	for _, para := range strings.Split(strings.TrimRight(text, "\n"), "\n") {
		// WordWrap returns one empty line for an empty paragraph rather
		// than nothing, so a blank line between sections counts like
		// any other row.
		lines += len(tview.WordWrap(para, width-border))
	}
	switch h := lines + border; {
	case h > cap:
		return cap
	case h < libraryCardMinHeight:
		return libraryCardMinHeight
	default:
		return h
	}
}

// comma groups n in threes. Six- and seven-figure totals are the normal
// case on this card and are unreadable without it.
func comma(n int) string {
	s := strconv.Itoa(n)
	neg := strings.HasPrefix(s, "-")
	s = strings.TrimPrefix(s, "-")
	var b strings.Builder
	for i, r := range s {
		if i > 0 && (len(s)-i)%3 == 0 {
			b.WriteByte(',')
		}
		b.WriteRune(r)
	}
	if neg {
		return "-" + b.String()
	}
	return b.String()
}

// percent renders part of whole as "(62%)", or nothing at all when
// there is no whole -- "(0%)" of nothing reads as a finding rather than
// as the absence of one.
func percent(part, whole int) string {
	if whole <= 0 {
		return ""
	}
	return fmt.Sprintf("[::d](%d%%)[-:-:-]", part*100/whole)
}

// average renders the mean rating as "(avg 4.1)", or nothing when
// nothing is rated.
func average(sum, count int) string {
	if count <= 0 {
		return ""
	}
	return fmt.Sprintf(" [::d](avg %.1f)[-:-:-]", float64(sum)/float64(count))
}

// bookmarkCount says how many bookmarks there are and how few tracks
// they are on, since the two differ whenever a track carries several.
func bookmarkCount(t metadata.Totals) string {
	if t.Bookmarks == 0 {
		return "0"
	}
	return fmt.Sprintf("%s [::d]on %s track(s)[-:-:-]", comma(t.Bookmarks), comma(t.BookmarkedTracks))
}

// catalogCount pairs "how many tracks carry one of these" with "how big
// the catalog is", which is what makes a zero readable: no catalog
// entries yet, or a catalog nothing has been filed under.
func catalogCount(used, catalog int, noun string) string {
	return fmt.Sprintf("%s [::d]/ %s %s(s)[-:-:-]", comma(used), comma(catalog), noun)
}

// longDuration renders a whole-library playtime as "23d 4h 12m".
// FormatDuration's "m:ss" is right for a track and useless for a
// fortnight of music.
func longDuration(d time.Duration) string {
	if d <= 0 {
		return "-"
	}
	days := int(d.Hours()) / 24
	hours := int(d.Hours()) % 24
	mins := int(d.Minutes()) % 60
	switch {
	case days > 0:
		return fmt.Sprintf("%dd %dh %dm", days, hours, mins)
	case hours > 0:
		return fmt.Sprintf("%dh %dm", hours, mins)
	default:
		return fmt.Sprintf("%dm", mins)
	}
}

// stamp renders a timestamp, or "-" for the zero time MPD reports
// nothing as.
func stamp(t time.Time) string {
	if t.IsZero() {
		return "-"
	}
	return t.Local().Format("2006-01-02 15:04")
}

// clip shortens s to width, marking that it was cut. Measured with
// tview's own tag-aware width so a color tag does not count towards it.
func clip(s string, width int) string {
	if width <= 1 || tview.TaggedStringWidth(s) <= width {
		return s
	}
	runes := []rune(s)
	if len(runes) > width-1 {
		runes = runes[:width-1]
	}
	return string(runes) + "…"
}
