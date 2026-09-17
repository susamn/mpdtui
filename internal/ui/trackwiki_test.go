package ui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gdamore/tcell/v2"

	"mpdtui/internal/mpdclient"
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
	pressGlobal(a, 'w')
	if got := a.trackWiki.GetTitle(); !strings.Contains(got, "Sade - Smooth Operator") {
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
	short := trackWikiModalHeight("one line")
	if short != trackWikiMinHeight {
		t.Errorf("a one-line story sized the modal to %d, want the floor %d", short, trackWikiMinHeight)
	}

	medium := trackWikiModalHeight(strings.Repeat("a line\n", 12))
	if medium <= short || medium >= trackWikiMaxHeight {
		t.Errorf("a 12-line story sized the modal to %d, want between %d and %d",
			medium, short, trackWikiMaxHeight)
	}

	long := trackWikiModalHeight(strings.Repeat("a line\n", 500))
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
	got := trackWikiModalHeight(para)
	if got <= trackWikiMinHeight {
		t.Fatalf("a long single paragraph sized the modal to %d -- measured as if it were one line", got)
	}
	if bigger := trackWikiModalHeight(strings.Repeat("word ", 2000)); bigger != trackWikiMaxHeight {
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
