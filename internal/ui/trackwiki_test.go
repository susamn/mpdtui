package ui

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"mpdtui/internal/mpdclient"
	"mpdtui/internal/termimage"
	"mpdtui/internal/trackwiki"
)

const testWikiJSON = `{
  "schema": 1,
  "track": {"title": "Smooth Operator", "artist": "Sade"},
  "fetched_at": "2026-09-16T20:55:00Z",
  "story": {
    "summary": "A song about a charming con man.",
    "sections": [{"heading": "Writing", "body": "Written before the band signed.", "source": "Wikipedia"}]
  },
  "behind_the_scenes": [{"heading": "The single edit", "body": "The 7 inch drops the intro."}],
  "bootlegs": [{"title": "Hammersmith Odeon", "date": "1984-11-09", "city": "London", "format": "12 inch Vinyl", "notes": "FM broadcast."}],
  "images": [{"file": "cover.jpg", "role": "cover", "caption": "1984 sleeve"}],
  "links": [{"label": "Wikipedia", "url": "https://en.wikipedia.org/wiki/Smooth_Operator"}],
  "sources": [{"name": "Wikipedia", "license": "CC BY-SA 4.0"}]
}`

// seedWiki writes a story where wiki-push would have put it and returns
// the musicDir. The layout is the point: mpdtui has to look inside the
// wiki/ level the push inserts, not beside the audio.
func seedWiki(t *testing.T, file, body string) string {
	t.Helper()
	music := t.TempDir()
	dir := trackwiki.Dir(music, file)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "wiki.json"), []byte(body), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	return music
}

func wikiTestApp(t *testing.T, music string) (*App, *fakeMPD) {
	t.Helper()
	f := &fakeMPD{}
	a := newFakeApp(t, f)
	a.musicDir = music
	seedQueue(a, f, mpdclient.Song{ID: 1, Pos: 0, Title: "Smooth Operator",
		Artist: "Sade", File: "sade/best/01-smooth.m4a"})
	a.tv.SetFocus(a.queue.table)
	a.queue.table.Select(queueHeaderRows, 0)
	return a, f
}

func pressGlobal(a *App, r rune) *tcell.EventKey {
	ev := tcell.NewEventKey(tcell.KeyRune, r, tcell.ModNone)
	return a.globalInputCapture(ev)
}

// TestWOpensTheStoryForTheSelectedTrack is the feature end to end: a
// story pushed into the music dir, 'w' on the Queue, the modal up with
// its content in it.
func TestWOpensTheStoryForTheSelectedTrack(t *testing.T) {
	music := seedWiki(t, "sade/best/01-smooth.m4a", testWikiJSON)
	a, _ := wikiTestApp(t, music)

	if got := pressGlobal(a, 'w'); got != nil {
		t.Fatal("'w' was not claimed")
	}
	if !a.pages.HasPage(trackWikiPageName) {
		t.Fatal("no story modal opened")
	}
	if a.trackWiki == nil {
		t.Fatal("trackWiki was never set")
	}

	body := a.trackWiki.GetText(true)
	for _, want := range []string{
		"charming con man",         // summary
		"Writing",                  // story section heading
		"Behind the scenes",        // section heading
		"The 7 inch drops",         // behind-the-scenes body
		"Bootlegs",                 //
		"Hammersmith Odeon",        //
		"1984-11-09, London",       // joined bootleg line
		"12 inch Vinyl",            // format in brackets
		"cover.jpg",                // image listed
		"1984 sleeve",              // its caption
		"Wikipedia (CC BY-SA 4.0)", // attribution -- a licence requirement
		"fetched 2026-09-16",       //
	} {
		if !strings.Contains(body, want) {
			t.Errorf("modal text is missing %q", want)
		}
	}
}

// TestStoryModalTitlePrefersTheFile covers a misfiled directory: the
// border shows what the story says it is about, so a mismatch with the
// track is visible rather than hidden.
func TestStoryModalTitlePrefersTheFile(t *testing.T) {
	music := seedWiki(t, "sade/best/01-smooth.m4a", testWikiJSON)
	a, _ := wikiTestApp(t, music)
	w, ok := trackwiki.Load(music, "sade/best/01-smooth.m4a")
	if !ok {
		t.Fatal("setup: no story")
	}
	card, _, _ := a.newTrackWikiCard(w, "whatever MPD says")
	if got := card.GetTitle(); !strings.Contains(got, "Sade - Smooth Operator") {
		t.Errorf("title = %q, want the story's own artist and title", got)
	}
}

// TestWOnATrackWithNoStorySaysSo: the common case by far. It must not
// open an empty modal the user then has to dismiss.
func TestWOnATrackWithNoStorySaysSo(t *testing.T) {
	a, _ := wikiTestApp(t, t.TempDir())

	if got := pressGlobal(a, 'w'); got != nil {
		t.Fatal("'w' was not claimed")
	}
	if a.pages.HasPage(trackWikiPageName) {
		t.Error("a modal opened for a track with no story")
	}
	if got := a.hintBar.GetText(true); !strings.Contains(got, "no story") {
		t.Errorf("hint bar = %q, want it to say there is no story", got)
	}
}

func TestWWithoutMusicDirSaysSo(t *testing.T) {
	a, _ := wikiTestApp(t, "")
	pressGlobal(a, 'w')
	if a.pages.HasPage(trackWikiPageName) {
		t.Error("a modal opened with no music_dir configured")
	}
	if got := a.hintBar.GetText(true); !strings.Contains(got, "music_dir") {
		t.Errorf("hint bar = %q, want it to name music_dir", got)
	}
}

// TestStoryModalClosesOnWAndEsc covers both close routes, since 'w'
// doubling as the close key is what the noTextInputOverlays entry buys.
func TestStoryModalClosesOnWAndEsc(t *testing.T) {
	music := seedWiki(t, "sade/best/01-smooth.m4a", testWikiJSON)
	for _, tc := range []struct {
		name  string
		close func(*App)
	}{
		{"w", func(a *App) { pressGlobal(a, 'w') }},
		{"Esc", func(a *App) { a.globalInputCapture(tcell.NewEventKey(tcell.KeyEscape, 0, tcell.ModNone)) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a, _ := wikiTestApp(t, music)
			pressGlobal(a, 'w')
			if !a.pages.HasPage(trackWikiPageName) {
				t.Fatal("setup: modal did not open")
			}
			tc.close(a)
			if a.pages.HasPage(trackWikiPageName) {
				t.Errorf("%s did not close the modal", tc.name)
			}
		})
	}
}

// TestTransportStaysLiveWhileTheStoryIsOpen: this is something you read
// while the music plays, like the lyrics viewer, so Space must still
// reach the player rather than the text view.
func TestTransportStaysLiveWhileTheStoryIsOpen(t *testing.T) {
	music := seedWiki(t, "sade/best/01-smooth.m4a", testWikiJSON)
	a, f := wikiTestApp(t, music)
	pressGlobal(a, 'w')

	before := len(f.calls)
	if got := pressGlobal(a, ' '); got != nil {
		t.Error("Space was not claimed while the story was open")
	}
	if len(f.calls) == before || f.calls[len(f.calls)-1] != "toggle" {
		t.Errorf("Space did not reach the player; calls = %v", f.calls[before:])
	}
	if !a.pages.HasPage(trackWikiPageName) {
		t.Error("the transport key closed the modal")
	}
}

// TestStoryModalLeavesScrollKeysToTheTextView: j/k must fall through to
// tview's own TextView scrolling rather than being eaten by the router
// (which uses them to walk the Queue behind other overlays).
func TestStoryModalLeavesScrollKeysToTheTextView(t *testing.T) {
	music := seedWiki(t, "sade/best/01-smooth.m4a", testWikiJSON)
	a, _ := wikiTestApp(t, music)
	pressGlobal(a, 'w')

	for _, r := range []rune{'j', 'k'} {
		ev := tcell.NewEventKey(tcell.KeyRune, r, tcell.ModNone)
		if got := a.globalInputCapture(ev); got != ev {
			t.Errorf("%q was claimed by the router, want it passed to the text view", r)
		}
	}
}

// TestRenderSkipsSectionsWithNothingInThem: most stories carry only some
// of the fields, and an empty heading is worse than no heading.
func TestRenderSkipsSectionsWithNothingInThem(t *testing.T) {
	out := renderTrackWiki(trackwiki.Wiki{
		Story: trackwiki.Story{Summary: "Just a summary."},
	})
	for _, absent := range []string{"Behind the scenes", "Bootlegs", "Images", "Links"} {
		if strings.Contains(out, absent) {
			t.Errorf("rendered an empty %q section", absent)
		}
	}
	if !strings.Contains(out, "Just a summary.") {
		t.Error("the summary itself is missing")
	}
}

func TestBootlegLineJoinsOnlyWhatIsThere(t *testing.T) {
	for _, tc := range []struct {
		name string
		in   trackwiki.Bootleg
		want string
	}{
		{"everything", trackwiki.Bootleg{Date: "1984", Venue: "Odeon", City: "London", Format: "Vinyl"},
			"1984, Odeon, London (Vinyl)"},
		{"date only", trackwiki.Bootleg{Date: "1984"}, "1984"},
		{"format only", trackwiki.Bootleg{Format: "FM"}, "(FM)"},
		{"nothing", trackwiki.Bootleg{}, ""},
	} {
		if got := wikiBootlegLine(tc.in); got != tc.want {
			t.Errorf("%s: = %q, want %q", tc.name, got, tc.want)
		}
	}
}

// TestModalHeightFollowsItsContent is the Track Info card's lesson
// applied here before it could become a complaint: a short story must
// not open a tall box with a field of empty under it.
func TestModalHeightFollowsItsContent(t *testing.T) {
	short := trackWikiCardHeight("one line", false)
	if short != trackWikiMinHeight {
		t.Errorf("a one-line story sized the modal to %d, want the floor %d", short, trackWikiMinHeight)
	}

	medium := trackWikiCardHeight(strings.Repeat("a line\n", 12), false)
	if medium <= short || medium >= trackWikiMaxHeight {
		t.Errorf("a 12-line story sized the modal to %d, want between %d and %d",
			medium, short, trackWikiMaxHeight)
	}

	long := trackWikiCardHeight(strings.Repeat("a line\n", 500), false)
	if long != trackWikiMaxHeight {
		t.Errorf("a 500-line story sized the modal to %d, want the cap %d", long, trackWikiMaxHeight)
	}
}

// TestModalHeightCountsWrappedLines: a single long paragraph occupies
// many rows once wrapped, and measuring the unwrapped string would size
// the modal to one line and hide the rest.
func TestModalHeightCountsWrappedLines(t *testing.T) {
	// One paragraph, no newlines at all: measured unwrapped it is a
	// single line, and the modal would open three rows tall with the
	// rest of the story hidden below the fold.
	para := strings.Repeat("word ", 200)
	got := trackWikiCardHeight(para, false)
	if got <= trackWikiMinHeight {
		t.Fatalf("a long single paragraph sized the modal to %d -- measured as if it were one line", got)
	}
	if bigger := trackWikiCardHeight(strings.Repeat("word ", 2000), false); bigger != trackWikiMaxHeight {
		t.Errorf("a paragraph ten times longer sized the modal to %d, want the cap %d",
			bigger, trackWikiMaxHeight)
	}
}

func TestFooterShowsTheDateNotTheTimestamp(t *testing.T) {
	got := wikiFooter(trackwiki.Wiki{
		FetchedAt: "2026-09-17T04:02:26Z",
		Sources:   []trackwiki.Source{{Name: "Wikipedia", License: "CC BY-SA 4.0"}},
	})
	if !strings.Contains(got, "fetched 2026-09-17") {
		t.Errorf("footer = %q, want the plain date", got)
	}
	if strings.Contains(got, "04:02:26") {
		t.Errorf("footer = %q, want the time dropped", got)
	}
	if !strings.Contains(got, "Wikipedia (CC BY-SA 4.0)") {
		t.Errorf("footer = %q, want the licence attributed", got)
	}
}

// writePNG puts a real decodable image next to a story, the way the
// fetch + push pipeline would.
func writePNG(t *testing.T, music, file, name string) {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 40, 40))
	for y := 0; y < 40; y++ {
		for x := 0; x < 40; x++ {
			img.Set(x, y, color.RGBA{R: 180, G: 90, B: 40, A: 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("encode: %v", err)
	}
	if err := os.WriteFile(filepath.Join(trackwiki.Dir(music, file), name), buf.Bytes(), 0o644); err != nil {
		t.Fatalf("write image: %v", err)
	}
}

func loadStory(t *testing.T, music, file string) trackwiki.Wiki {
	t.Helper()
	w, ok := trackwiki.Load(music, file)
	if !ok {
		t.Fatal("setup: no story loaded")
	}
	return w
}

// TestCardPutsThePictureLeftOfTheProse is the layout itself: two
// columns, picture first at a fixed width, prose taking the rest.
func TestCardPutsThePictureLeftOfTheProse(t *testing.T) {
	const file = "sade/best/01-smooth.m4a"
	music := seedWiki(t, file, testWikiJSON)
	writePNG(t, music, file, "cover.jpg") // name per the manifest; content is PNG, which image.Decode handles
	a, _ := wikiTestApp(t, music)

	card, view, _ := a.newTrackWikiCard(loadStory(t, music, file), "x")
	if a.trackWikiImg == nil {
		t.Fatal("no picture column was built for a story with a readable image")
	}
	if got := card.GetItemCount(); got != 2 {
		t.Fatalf("card has %d columns, want 2 (picture, prose)", got)
	}
	if card.GetItem(0) != tview.Primitive(a.trackWikiImg.view) {
		t.Error("the first column is not the picture")
	}
	if card.GetItem(1) != tview.Primitive(view) {
		t.Error("the second column is not the prose")
	}
}

// TestCardWithoutAPictureIsProseAtFullWidth: most tracks have no image,
// and an empty column reserved for one would waste a quarter of a small
// card.
func TestCardWithoutAPictureIsProseAtFullWidth(t *testing.T) {
	const file = "sade/best/01-smooth.m4a"
	// The manifest names cover.jpg, but nothing was ever written there.
	music := seedWiki(t, file, testWikiJSON)
	a, _ := wikiTestApp(t, music)

	card, _, _ := a.newTrackWikiCard(loadStory(t, music, file), "x")
	if a.trackWikiImg != nil {
		t.Error("a picture column was built for an image file that does not exist")
	}
	if got := card.GetItemCount(); got != 1 {
		t.Errorf("card has %d columns, want 1 (prose only)", got)
	}
}

// TestCardIsAtLeastAsTallAsItsPicture: the image occupies a fixed number
// of rows, so a two-line story must still leave room for it rather than
// clipping it against the border.
func TestCardIsAtLeastAsTallAsItsPicture(t *testing.T) {
	withImage := trackWikiCardHeight("one line", true)
	if withImage < trackWikiImageRows+2 {
		t.Errorf("a card with a picture is %d rows, too short for a %d-row image plus its border",
			withImage, trackWikiImageRows)
	}
	if withoutImage := trackWikiCardHeight("one line", false); withoutImage >= withImage {
		t.Errorf("a card with no picture (%d) is not shorter than one with (%d)", withoutImage, withImage)
	}
}

// TestCardHeightAccountsForTheNarrowerProseColumn: the picture takes
// columns away from the text, so the same story wraps to more lines and
// needs a taller card.
func TestCardHeightAccountsForTheNarrowerProseColumn(t *testing.T) {
	long := strings.Repeat("word ", 120)
	wide := trackWikiCardHeight(long, false)
	narrow := trackWikiCardHeight(long, true)
	if narrow <= wide {
		t.Errorf("with the picture %d rows, without it %d -- the narrower column must wrap to more lines",
			narrow, wide)
	}
}

// TestPickWikiImagePrefersTheCover covers the choice of which single
// picture the card shows.
func TestPickWikiImagePrefersTheCover(t *testing.T) {
	got, ok := pickWikiImage(trackwiki.Wiki{Images: []trackwiki.Image{
		{File: "scene-01.jpg", Role: "scene"},
		{File: "cover.jpg", Role: "cover"},
	}})
	if !ok || got.File != "cover.jpg" {
		t.Errorf("picked %+v, want the cover", got)
	}

	got, ok = pickWikiImage(trackwiki.Wiki{Images: []trackwiki.Image{{File: "scene-01.jpg", Role: "scene"}}})
	if !ok || got.File != "scene-01.jpg" {
		t.Errorf("with no cover, picked %+v, want the first image", got)
	}

	if _, ok := pickWikiImage(trackwiki.Wiki{}); ok {
		t.Error("picked an image from a story that has none")
	}
}

// TestClosingTheCardRemovesThePlacement: on a Kitty terminal the picture
// is composited by the terminal over tview's output, so a placement left
// behind after the card closes sits on top of the Queue forever.
func TestClosingTheCardRemovesThePlacement(t *testing.T) {
	a := newTestApp()
	// lastID 0 so the cleanup's termimage.Delete is a no-op: it would
	// otherwise write Kitty escape bytes straight into the test log.
	// That the delete names the right id is termimage's own test.
	a.trackWikiImg = &wikiImage{view: tview.NewTextView(), png: []byte("not really a png")}

	// The page is not open, which is what the hook checks.
	a.drawTrackWikiImage()

	if a.trackWikiImg != nil {
		t.Error("the picture outlived the card that held it")
	}
}

// TestCardDrawsThePictureAsTextWithoutKitty is the layout as it actually
// reaches an ordinary terminal: half-blocks down the left, prose to
// their right, on the same rows. Column indices and item counts can all
// be right while the screen shows something else.
func TestCardDrawsThePictureAsTextWithoutKitty(t *testing.T) {
	t.Setenv("TERM", "xterm-256color")
	t.Setenv("KITTY_WINDOW_ID", "")

	const file = "sade/best/01-smooth.m4a"
	music := seedWiki(t, file, testWikiJSON)
	writePNG(t, music, file, "cover.jpg")
	a, _ := wikiTestApp(t, music)

	card, _, height := a.newTrackWikiCard(loadStory(t, music, file), "x")
	card.SetRect(0, 0, trackWikiWidth, height)

	screen := tcell.NewSimulationScreen("UTF-8")
	if err := screen.Init(); err != nil {
		t.Fatalf("init screen: %v", err)
	}
	defer screen.Fini()
	screen.SetSize(trackWikiWidth, height)
	card.Draw(screen)
	screen.Show()

	cells, w, _ := screen.GetContents()
	var blockRows, proseRows int
	for row := 1; row < height-1; row++ { // inside the border
		var left, right strings.Builder
		for col := 1; col < trackWikiWidth-1; col++ {
			r := cells[row*w+col].Runes
			if len(r) == 0 {
				continue
			}
			if col < trackWikiImageCols {
				left.WriteRune(r[0])
			} else {
				right.WriteRune(r[0])
			}
		}
		if strings.Contains(left.String(), "▀") {
			blockRows++
		}
		if strings.TrimSpace(right.String()) != "" {
			proseRows++
		}
	}
	if blockRows == 0 {
		t.Error("no half-blocks in the picture column -- the image did not render as text")
	}
	if proseRows == 0 {
		t.Error("no prose in the right column")
	}
	if blockRows < 4 || proseRows < 4 {
		t.Errorf("picture rows %d, prose rows %d -- want both columns filled side by side", blockRows, proseRows)
	}
}

// TestCardTransmitsThePictureOnKitty covers the other path, where the
// terminal composites the image and tview draws nothing: the only
// evidence is the escape sequence, so that is what gets asserted.
func TestCardTransmitsThePictureOnKitty(t *testing.T) {
	t.Setenv("KITTY_WINDOW_ID", "1")

	const file = "sade/best/01-smooth.m4a"
	music := seedWiki(t, file, testWikiJSON)
	writePNG(t, music, file, "cover.jpg")
	a, _ := wikiTestApp(t, music)

	var buf bytes.Buffer
	restore := termimage.SetOutputForTest(&buf)
	defer restore()

	if got := pressGlobal(a, 'w'); got != nil {
		t.Fatal("'w' was not claimed")
	}
	if a.trackWikiImg == nil || len(a.trackWikiImg.png) == 0 {
		t.Fatal("no PNG was prepared for the Kitty path")
	}
	// The view has no rect until something lays it out.
	a.trackWikiImg.view.SetRect(2, 3, trackWikiImageCols, trackWikiImageRows)
	a.drawTrackWikiImage()

	if got := buf.String(); !strings.Contains(got, "\033_Ga=T,") {
		t.Errorf("no transmit escape was written; got %q", got)
	}

	// Closing it must take the placement down, or the picture sits over
	// the Queue for the rest of the session.
	buf.Reset()
	pressGlobal(a, 'w')
	a.drawTrackWikiImage()
	if got := buf.String(); !strings.Contains(got, "\033_Ga=d,d=i,") {
		t.Errorf("closing the card did not delete the placement; got %q", got)
	}
	if a.trackWikiImg != nil {
		t.Error("the picture outlived the card")
	}
}

// TestCardNeverTouchesTheAlbumArtsImageIDs is the regression test for
// the card flickering in and out while the album art disappeared. Both
// pictures are on screen together and a Kitty image id is terminal-wide,
// so the two sharing a pair meant every retransmit deleted the other's
// placement.
func TestCardNeverTouchesTheAlbumArtsImageIDs(t *testing.T) {
	t.Setenv("KITTY_WINDOW_ID", "1")

	const file = "sade/best/01-smooth.m4a"
	music := seedWiki(t, file, testWikiJSON)
	writePNG(t, music, file, "cover.jpg")
	a, _ := wikiTestApp(t, music)

	var buf bytes.Buffer
	restore := termimage.SetOutputForTest(&buf)
	defer restore()

	pressGlobal(a, 'w')
	if a.trackWikiImg == nil {
		t.Fatal("setup: no picture was prepared")
	}
	// Several frames, including a move, so every transmit and delete the
	// card can produce is in the buffer.
	for i, rect := range [][4]int{{2, 3, 24, 12}, {5, 6, 24, 12}, {5, 6, 20, 10}} {
		a.trackWikiImg.view.SetRect(rect[0], rect[1], rect[2], rect[3])
		a.drawTrackWikiImage()
		if buf.Len() == 0 && i == 0 {
			t.Fatal("the first frame transmitted nothing")
		}
	}
	pressGlobal(a, 'w') // close
	a.drawTrackWikiImage()

	got := buf.String()
	for _, forbidden := range []string{"i=1,", "i=1\033", "i=2,", "i=2\033"} {
		if strings.Contains(got, forbidden) {
			t.Errorf("the card used %q -- an album art id", strings.TrimSuffix(forbidden, "\033"))
		}
	}
	if !strings.Contains(got, "i=3") && !strings.Contains(got, "i=4") {
		t.Errorf("the card used neither of its own ids; output was %q", got)
	}
}
