package ui

import (
	"fmt"
	"strings"

	"github.com/rivo/tview"

	"mpdtui/internal/trackwiki"
)

// trackWikiPageName is the story modal's page name on App.pages.
const trackWikiPageName = "track-wiki"

// trackWikiWidth/trackWikiHeight size the modal. Wider than the Track
// Info card because this is prose rather than fields -- much below 70
// columns and a paragraph wraps into a column of fragments -- but still
// short of the full screen, so the Queue stays visible behind it and the
// modal reads as something opened over the player rather than a new
// screen.
const (
	trackWikiWidth = 78

	// trackWikiMaxHeight caps the modal; trackWikiMinHeight stops a
	// one-line story from rendering as a sliver. Between the two the
	// modal is sized to what it actually holds -- a fixed height leaves
	// most stories sitting above a field of empty box, which is exactly
	// the complaint the Track Info card had to be fixed for.
	trackWikiMaxHeight = 30
	trackWikiMinHeight = 7
)

// openTrackWiki is 'w': the story behind the track the card would show.
//
// Everything it reads is already on disk (see internal/trackwiki) --
// fetched by music-tui ahead of time, not looked up now -- so this never
// blocks and works with no network at all. A track with no story says so
// in the hint bar rather than opening an empty modal, since "nothing was
// fetched for this one" is the common case and not worth a keypress to
// dismiss.
func (a *App) openTrackWiki() {
	song, ok := a.inspectedSong()
	if !ok {
		a.showMessage("nothing playing")
		return
	}
	if a.musicDir == "" {
		a.showMessage("music_dir is not configured -- no track stories")
		return
	}
	w, ok := trackwiki.Load(a.musicDir, song.File)
	if !ok {
		a.showMessage("no story for " + song.DisplayName())
		return
	}

	text := renderTrackWiki(w)
	view := tview.NewTextView().SetDynamicColors(true).SetWordWrap(true)
	view.SetText(text)
	view.SetBorder(true).SetTitle(" " + trackWikiTitle(w, song.DisplayName()) + " ")
	a.trackWiki = view
	a.showOverlay(trackWikiPageName,
		centered(view, trackWikiWidth, trackWikiModalHeight(text)), view)
}

// trackWikiTitle prefers what the story says it is about over what MPD's
// tags say, so a mismatch is visible rather than papered over: the two
// disagreeing means the directory was misfiled, and seeing the wrong
// name in the border is how the user finds that out.
func trackWikiTitle(w trackwiki.Wiki, fallback string) string {
	switch {
	case w.Track.Artist != "" && w.Track.Title != "":
		return w.Track.Artist + " - " + w.Track.Title
	case w.Track.Title != "":
		return w.Track.Title
	default:
		return fallback
	}
}

// renderTrackWiki lays the whole story out as one scrollable block.
//
// One TextView rather than a Flex of sections: the content is prose of
// wildly varying length, so any fixed division of the modal is wrong for
// most tracks, and a single wrapped column scrolls with j/k for free.
func renderTrackWiki(w trackwiki.Wiki) string {
	var b strings.Builder

	if w.Story.Summary != "" {
		b.WriteString(w.Story.Summary)
		b.WriteString("\n")
	}
	for _, sec := range w.Story.Sections {
		writeWikiProse(&b, sec)
	}
	if len(w.BehindTheScenes) > 0 {
		writeWikiHeading(&b, "Behind the scenes")
		for _, sec := range w.BehindTheScenes {
			writeWikiProse(&b, sec)
		}
	}
	if len(w.Bootlegs) > 0 {
		writeWikiHeading(&b, "Bootlegs")
		for _, bl := range w.Bootlegs {
			b.WriteString("  [::b]" + tview.Escape(bl.Title) + "[-:-:-]\n")
			if line := wikiBootlegLine(bl); line != "" {
				b.WriteString("  [::d]" + tview.Escape(line) + "[-:-:-]\n")
			}
			if bl.Notes != "" {
				b.WriteString("  " + tview.Escape(bl.Notes) + "\n")
			}
			b.WriteString("\n")
		}
	}
	if len(w.Images) > 0 {
		writeWikiHeading(&b, "Images")
		for _, img := range w.Images {
			line := "  " + tview.Escape(img.File)
			if img.Caption != "" {
				line += " [::d]" + tview.Escape(img.Caption) + "[-:-:-]"
			}
			b.WriteString(line + "\n")
		}
		b.WriteString("\n")
	}
	if len(w.Links) > 0 {
		writeWikiHeading(&b, "Links")
		for _, l := range w.Links {
			b.WriteString(fmt.Sprintf("  [%s]%s[-]  [::d]%s[-:-:-]\n",
				hintKeyColor, tview.Escape(l.Label), tview.Escape(l.URL)))
		}
		b.WriteString("\n")
	}
	if footer := wikiFooter(w); footer != "" {
		b.WriteString("[::d]" + tview.Escape(footer) + "[-:-:-]\n")
	}
	return b.String()
}

// trackWikiModalHeight is how tall the modal needs to be for text, up to
// the cap. Measured with tview's own WordWrap at the width the view will
// actually use, so the count matches what gets drawn rather than
// approximating it -- an approximation that runs short clips the last
// line, and one that runs long reintroduces the empty box.
func trackWikiModalHeight(text string) int {
	const border = 2
	lines := 0
	for _, para := range strings.Split(text, "\n") {
		if wrapped := tview.WordWrap(para, trackWikiWidth-border); len(wrapped) > 0 {
			lines += len(wrapped)
		} else {
			lines++ // a blank line still occupies one
		}
	}
	switch h := lines + border; {
	case h > trackWikiMaxHeight:
		return trackWikiMaxHeight
	case h < trackWikiMinHeight:
		return trackWikiMinHeight
	default:
		return h
	}
}

func writeWikiHeading(b *strings.Builder, text string) {
	b.WriteString("\n[::b]" + text + "[-:-:-]\n")
}

func writeWikiProse(b *strings.Builder, p trackwiki.Prose) {
	if p.Body == "" {
		return
	}
	if p.Heading != "" {
		b.WriteString("\n[::b]" + tview.Escape(p.Heading) + "[-:-:-]")
		if p.Source != "" {
			b.WriteString(" [::d]" + tview.Escape(p.Source) + "[-:-:-]")
		}
		b.WriteString("\n")
	}
	b.WriteString(tview.Escape(p.Body) + "\n")
}

// wikiBootlegLine joins whichever of date/venue/city/format a bootleg
// actually carries. Built rather than templated because most entries
// have only some of them, and a template leaves the gaps visible as
// stray commas and empty brackets.
func wikiBootlegLine(bl trackwiki.Bootleg) string {
	var parts []string
	for _, s := range []string{bl.Date, bl.Venue, bl.City} {
		if s != "" {
			parts = append(parts, s)
		}
	}
	line := strings.Join(parts, ", ")
	if bl.Format != "" {
		if line != "" {
			line += " "
		}
		line += "(" + bl.Format + ")"
	}
	return line
}

// wikiFooter credits the services the text came from. Not decoration:
// Wikipedia's CC BY-SA requires attribution wherever its text is shown,
// and the fetch date is what makes a stale story recognisable as stale.
func wikiFooter(w trackwiki.Wiki) string {
	var parts []string
	for _, s := range w.Sources {
		if s.Name == "" {
			continue
		}
		if s.License != "" {
			parts = append(parts, s.Name+" ("+s.License+")")
			continue
		}
		parts = append(parts, s.Name)
	}
	footer := strings.Join(parts, " - ")
	if w.FetchedAt != "" {
		if footer != "" {
			footer += " - "
		}
		// Just the date. The timestamp is RFC 3339 because a machine
		// wrote it; the hour a story was fetched tells a reader nothing,
		// and the full form crowds a one-line footer.
		date, _, _ := strings.Cut(w.FetchedAt, "T")
		footer += "fetched " + date
	}
	return footer
}
