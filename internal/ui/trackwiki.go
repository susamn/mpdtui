package ui

import (
	"bytes"
	"fmt"
	"image"
	_ "image/jpeg"
	"image/png"
	"os"
	"strings"

	"github.com/rivo/tview"

	"mpdtui/internal/termimage"
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

	// trackWikiImageCols/Rows are the picture's half of the card. Fixed
	// rather than proportional: a terminal cell is roughly twice as tall
	// as it is wide, so 24x12 is about square on screen, and a cover
	// that changes shape with the length of the prose beside it would
	// look like a bug.
	trackWikiImageCols = 24
	trackWikiImageRows = 12

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

	card, view, height := a.newTrackWikiCard(w, song.DisplayName())
	a.trackWiki = view
	a.showOverlay(trackWikiPageName, centered(card, trackWikiWidth, height), view)
}

// newTrackWikiCard lays the card out: the picture down the left, the
// prose scrolling on the right. Returns the card, the view that takes
// focus, and how tall the whole thing should be.
//
// Split out of openTrackWiki so the layout can be exercised without an
// overlay, a page stack, or a focused application -- the card's title,
// its columns and its height are all decided here.
func (a *App) newTrackWikiCard(w trackwiki.Wiki, fallbackTitle string) (*tview.Flex, *tview.TextView, int) {
	text := renderTrackWiki(w)
	view := tview.NewTextView().SetDynamicColors(true).SetWordWrap(true)
	view.SetText(text)

	card := tview.NewFlex().SetDirection(tview.FlexColumn)
	card.SetBorder(true).SetTitle(" " + trackWikiTitle(w, fallbackTitle) + " ")

	img := a.trackWikiImage(w)
	if img != nil {
		card.AddItem(img, trackWikiImageCols, 0, false)
	}
	card.AddItem(view, 0, 1, true)

	return card, view, trackWikiCardHeight(text, img != nil)
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

// trackWikiCardHeight is how tall the card needs to be for text, up to
// the cap. Measured with tview's own WordWrap at the width the view will
// actually use, so the count matches what gets drawn rather than
// approximating it -- an approximation that runs short clips the last
// line, and one that runs long reintroduces the empty box.
func trackWikiCardHeight(text string, withImage bool) int {
	const border = 2
	width := trackWikiWidth - border
	if withImage {
		width -= trackWikiImageCols
	}
	lines := 0
	for _, para := range strings.Split(text, "\n") {
		if wrapped := tview.WordWrap(para, width); len(wrapped) > 0 {
			lines += len(wrapped)
		} else {
			lines++ // a blank line still occupies one
		}
	}
	// A card must not be shorter than the picture in it, or the image is
	// clipped by the border it sits inside.
	if withImage && lines < trackWikiImageRows+1 {
		lines = trackWikiImageRows + 1
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

// trackWikiIDs is the story card's own pair of Kitty image ids. It
// shares the screen with the album art panel, which holds {1, 2} -- and
// two pictures on one pair delete each other (see termimage.IDPair).
var trackWikiIDs = termimage.IDPair{3, 4}

// wikiImage is the card's left-hand picture, and the bookkeeping the
// Kitty path needs.
//
// On a Kitty terminal the picture is not drawn by tview at all: view is
// an empty box holding the space, and the pixels are composited over it
// by the terminal from drawTrackWikiImage. png is nil on every other
// terminal, where the half-blocks are ordinary text inside view and
// none of the rest applies.
type wikiImage struct {
	view    *tview.TextView
	png     []byte
	lastID  int
	sentSig string
}

// trackWikiImage builds the card's picture column, or nil when there is
// nothing to show -- no images listed, the file missing, or the bytes
// not decodable. A card with no picture is just the prose at full
// width, which is the right answer for most tracks.
func (a *App) trackWikiImage(w trackwiki.Wiki) *tview.TextView {
	img, ok := pickWikiImage(w)
	if !ok {
		return nil
	}
	path := w.Path(img)
	if path == "" {
		return nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	decoded, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil
	}

	view := tview.NewTextView().SetDynamicColors(true)
	view.SetBorderPadding(0, 0, 1, 1)

	if !termimage.Supported() {
		// Ordinary terminal: the picture *is* text, so tview draws it
		// like any other content and there is nothing to composite.
		fmt.Fprint(tview.ANSIWriter(view), termimage.HalfBlocks(decoded, trackWikiImageCols-2, trackWikiImageRows))
		a.trackWikiImg = &wikiImage{view: view}
		return view
	}

	var buf bytes.Buffer
	if err := png.Encode(&buf, decoded); err != nil {
		return nil
	}
	a.trackWikiImg = &wikiImage{view: view, png: buf.Bytes()}
	return view
}

// pickWikiImage chooses the one picture the card shows: the cover if
// there is one, else whatever comes first. One rather than a gallery --
// the card is a card, and a strip of thumbnails at 24 columns would be
// unreadable. The rest are listed by name in the prose column.
func pickWikiImage(w trackwiki.Wiki) (trackwiki.Image, bool) {
	if covers := w.ImagesWithRole("cover"); len(covers) > 0 {
		return covers[0], true
	}
	if len(w.Images) > 0 {
		return w.Images[0], true
	}
	return trackwiki.Image{}, false
}

// drawTrackWikiImage composites the card's picture for this frame, on
// terminals that can show one. Called from the App's SetAfterDrawFunc
// alongside the album art panel's own Draw, which is the only place raw
// escape sequences can be written without racing tcell's output.
//
// Retransmits only when the picture's position or size actually changed.
// Unlike the album art panel there is no periodic resend: this card is
// open for seconds at a time, not for the length of a listening session,
// so the display-scale drift that resend exists to self-heal has no time
// to happen.
func (a *App) drawTrackWikiImage() {
	im := a.trackWikiImg
	if im == nil || len(im.png) == 0 {
		return
	}
	// Closed since the last frame: the terminal composites the image
	// over tview's output and knows nothing about pages, so a placement
	// left behind would sit on top of the Queue forever.
	if !a.pages.HasPage(trackWikiPageName) {
		termimage.Delete(im.lastID)
		a.trackWikiImg = nil
		return
	}

	x, y, w, h := im.view.GetInnerRect()
	if w <= 0 || h <= 0 {
		return
	}
	sig := fmt.Sprintf("%d:%d:%d:%d:%d", len(im.png), x, y, w, h)
	if im.sentSig == sig {
		return
	}

	id := trackWikiIDs.Next(im.lastID)
	termimage.Place(im.png, x, y, w, h, id)
	termimage.Delete(im.lastID)
	im.lastID = id
	im.sentSig = sig
}
