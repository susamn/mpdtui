package ui

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/rivo/tview"

	"mpdtui/internal/libraryscan"
	"mpdtui/internal/metadata"
	"mpdtui/internal/mpdclient"
)

// libraryCardPageName is the Library card's page name on App.pages.
const libraryCardPageName = "library-card"

const (
	// libraryCardWidth fits the card's widest natural line -- a
	// two-column label/value row plus a recently-added entry's
	// "Artist - Title" -- without stretching short rows across the
	// screen.
	libraryCardWidth = 76

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
	view := tview.NewTextView().SetDynamicColors(true).SetWordWrap(false)
	view.SetBorder(true).SetTitle(" Library ")
	a.libraryCard = view

	// Page first, scan second. applyLibrarySnapshot only redraws a card
	// that is actually on the page stack, so starting the scans before
	// the page exists would drop any section that answered instantly.
	height := a.renderLibraryCard()
	a.libraryCardFrame = centeredGrid(view, libraryCardWidth, height)
	a.showOverlay(libraryCardPageName, a.libraryCardFrame, view)
	a.refreshLibrarySnapshot(false)
}

// renderLibraryCard redraws the card from the current snapshot and
// returns the height it wants. Called both when the card opens and
// whenever a background section lands, which is why it tolerates the
// card being closed.
func (a *App) renderLibraryCard() int {
	if a.libraryCard == nil {
		return libraryCardMinHeight
	}
	text := renderLibrarySnapshot(a.librarySnap, a.musicDir, a.metaDB != nil)
	a.libraryCard.SetText(text)
	return libraryCardHeight(text, a.libraryCardCap())
}

// libraryCardCap is the tallest this card may be on the terminal as it
// currently is: two rows short of the screen, so the card still reads
// as something opened over the player rather than as a new screen, and
// so its footer is not the line that falls off the bottom.
//
// Read off the root layout's live rect rather than from a stored size,
// the same way the Track Info card reads the Queue's -- it is the only
// number that is right after a resize. A zero rect (nothing drawn yet,
// as in a test) leaves the static cap in charge.
func (a *App) libraryCardCap() int {
	_, _, _, height := a.root.GetRect()
	if height <= 0 {
		return libraryCardMaxHeight
	}
	if height-2 < libraryCardMaxHeight {
		return max(height-2, libraryCardMinHeight)
	}
	return libraryCardMaxHeight
}

// applyLibrarySnapshot re-renders the open card after a background
// section arrives, resizing the overlay to match -- the card grows as
// the slow sections fill in, and a grid laid out at one height does not
// change on its own.
//
// Resizes the existing frame rather than replacing the page. The two
// scans finish independently, and taking the page down to put a
// taller one up means the other one can arrive while there is no page
// there, see a closed card, and drop its own section's redraw.
func (a *App) applyLibrarySnapshot() {
	if a.libraryCard == nil || a.libraryCardFrame == nil || !a.pages.HasPage(libraryCardPageName) {
		return
	}
	a.libraryCardFrame.SetRows(0, a.renderLibraryCard(), 0)
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

	go func() {
		stats, statsErr := client.LibraryStats()
		var totals metadata.Totals
		var totalsErr error
		if metaDB != nil {
			totals, totalsErr = metaDB.Totals()
		}
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
func renderLibrarySnapshot(s *librarySnapshot, musicDir string, metaActive bool) string {
	var b strings.Builder

	writeLibrarySection(&b, "Collection")
	if s.statsOK {
		writeLibraryPair(&b, "Tracks", comma(s.stats.Tracks), "Albums", comma(s.stats.Albums))
		writeLibraryPair(&b, "Artists", comma(s.stats.Artists), "Playlists", comma(s.stats.Playlists))
		writeLibraryPair(&b, "Playtime", longDuration(s.stats.Playtime), "DB updated", stamp(s.stats.Updated))
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
		writeLibraryPair(&b, "Synced .lrc", comma(sc.Synced), "Plain .txt", comma(sc.Plain))
		writeLibraryPair(&b, "Both formats", comma(sc.Both), "Orphan files", comma(sc.Orphans))
		writeLibraryPair(&b, "Stories", comma(sc.Stories), "Story images", comma(sc.Images))
		if sc.Unreadable > 0 {
			writeLibraryRow(&b, "Unreadable", comma(sc.Unreadable)+" story file(s) this version will not open")
		}
	}

	writeLibrarySection(&b, "Local metadata")
	switch {
	case !metaActive:
		writeLibraryNote(&b, "track_metadata is off -- no ratings, plays, marks, tags or bookmarks")
	case !s.totalsOK:
		writeLibraryRow(&b, "Rated", pending(s))
	default:
		t := s.totals
		writeLibraryPair(&b, "Rated", comma(t.Rated)+average(t.Stars, t.Rated), "Played", comma(t.Played))
		writeLibraryPair(&b, "Plays", comma(t.Plays), "Bookmarks", bookmarkCount(t))
		writeLibraryPair(&b, "Marked", catalogCount(t.Marked, t.MarkReasons, "reason"),
			"Tagged", catalogCount(t.Tagged, t.Tags, "tag"))
		writeLibraryRow(&b, "Tracks known", comma(t.Tracks)+" have anything recorded against them")
	}

	writeLibrarySection(&b, "Playlist fallouts")
	writeLibraryFallouts(&b, s)

	writeLibrarySection(&b, "Recently added")
	writeLibraryRecent(&b, s)

	b.WriteString("\n" + libraryCardFooter(s))
	return b.String()
}

// writeLibraryFallouts reports the playlist entries the library cannot
// resolve: the totals, then the worst offenders by name.
func writeLibraryFallouts(b *strings.Builder, s *librarySnapshot) {
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
	for _, f := range shown {
		b.WriteString(fmt.Sprintf("  %-*s [%s]%s[-] of %s\n",
			libraryCardLabel+18, clip(tview.Escape(f.Name), libraryCardLabel+17),
			flagOffColor, comma(len(f.Missing)), comma(f.Total)))
	}
	if rest := len(r.Affected) - len(shown); rest > 0 {
		writeLibraryNote(b, fmt.Sprintf("and %s more playlist(s)", comma(rest)))
	}
}

// writeLibraryRecent lists the newest tracks by file modification time
// (see mpdclient.RecentTracks for why that is what "added" means here).
func writeLibraryRecent(b *strings.Builder, s *librarySnapshot) {
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
			clip(tview.Escape(t.DisplayName()), libraryCardWidth-18)))
	}
}

// libraryCardFooter says how old the numbers are and which key
// refreshes them -- the card is a snapshot, and a snapshot with no
// timestamp is indistinguishable from a live reading.
func libraryCardFooter(s *librarySnapshot) string {
	var parts []string
	if s.inFlight {
		parts = append(parts, "counting...")
	} else if !s.takenAt.IsZero() {
		parts = append(parts, "as of "+s.takenAt.Local().Format("15:04:05"))
	}
	parts = append(parts, fmt.Sprintf("[%s]r[-] refresh  [%s]Esc[-] close", hintKeyColor, hintKeyColor))
	line := "[::d]" + strings.Join(parts[:len(parts)-1], "  ")
	if len(parts) > 1 {
		line += "  "
	}
	return line + "[-:-:-]" + parts[len(parts)-1] + "\n"
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

// writeLibraryPair puts two label/value pairs on one line. The card has
// far more numbers than lines to spare, and the pairs on each line are
// chosen to be the ones worth reading against each other.
func writeLibraryPair(b *strings.Builder, l1, v1, l2, v2 string) {
	const col = 36
	left := fmt.Sprintf("[::b]%-*s[-:-:-]%s", libraryCardLabel, l1, v1)
	pad := col - libraryCardLabel - tview.TaggedStringWidth(v1)
	if pad < 2 {
		pad = 2
	}
	fmt.Fprintf(b, "  %s%s[::b]%-*s[-:-:-]%s\n", left, strings.Repeat(" ", pad), libraryCardLabel, l2, v2)
}

// libraryCardHeight sizes the card to its content, up to cap, measured
// against the width the text will actually be drawn at -- the story
// card's rule, and for the same reason: a fixed height leaves a short
// card sitting above a field of empty box.
func libraryCardHeight(text string, cap int) int {
	const border = 2
	lines := 0
	for _, para := range strings.Split(strings.TrimRight(text, "\n"), "\n") {
		// WordWrap returns one empty line for an empty paragraph rather
		// than nothing, so a blank line between sections counts like
		// any other row.
		lines += len(tview.WordWrap(para, libraryCardWidth-border))
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
