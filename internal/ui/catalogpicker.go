package ui

import (
	"fmt"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"mpdtui/internal/metadata"
	"mpdtui/internal/mpdclient"
)

// A track has two many-to-many relations to a hand-edited catalog:
// marks ('m') and tags ('t'). They differ in almost nothing a picker
// cares about -- where the catalog comes from, which field of a Track
// holds the current set, and which two database calls toggle and clear
// it -- so one picker serves both rather than two near-identical
// copies drifting apart.

// catalogItem is one row of a catalog, reduced to what a picker needs.
type catalogItem struct {
	ID    int64
	Label string
}

// catalog is the handful of things that differ between the marks picker
// and the tags picker.
//
// The cache is deliberately not part of this: rather than each catalog
// describing how to patch a cached Track after a write, the picker
// re-reads the track once the write lands. A local SQLite read on a
// background goroutine is cheap, and it removes the only way the cache
// and the database could disagree about what just happened.
type catalog interface {
	// noun names one entry ("mark"), plural names the set ("marks").
	// Used in the popup's title and its confirmation messages.
	noun() string
	plural() string
	// entries is the whole catalog, in the order it should be listed.
	entries(db *metadata.DB) ([]catalogItem, error)
	// on is which entries the given track currently carries.
	on(track metadata.Track) []int64
	// toggle flips one entry on the track and reports whether it ended
	// up set.
	toggle(db *metadata.DB, file string, id int64) (bool, error)
	// clear removes every entry from the track.
	clear(db *metadata.DB, file string) error
}

// catalogPicker toggles catalog entries on the track App.targetSong
// resolves to. Built once (like trackInfoCard/lyricsViewer) and
// repopulated fresh from the catalog every time it's opened, in case
// entries were added since (the catalogs are edited in Settings, or by
// hand -- see internal/metadata's own doc comment).
//
// A track can carry several entries at once, so this is a checklist
// rather than a one-of-N choice: Enter toggles the highlighted entry and
// the popup stays open, because the natural thing after adding one is to
// add another. Each row shows whether it is currently set, so the popup
// doubles as the answer to "what does this track have?".
type catalogPicker struct {
	*tview.List
	app   *App
	kind  catalog
	items []catalogItem

	// song is the track this popup was opened for, captured once by
	// render rather than re-resolved in apply. Transport controls stay
	// live while an overlay is up (see globalInputCapture's modeOverlay
	// branch) and a track can auto-advance on its own, so re-resolving
	// the target on Enter could act on a track other than the one the
	// popup's own title said it was for.
	song mpdclient.Song
}

func newCatalogPicker(app *App, kind catalog) *catalogPicker {
	p := &catalogPicker{app: app, kind: kind}
	l := tview.NewList()
	l.ShowSecondaryText(false)
	l.SetHighlightFullLine(true)
	l.SetSelectedTextColor(colorSelectedFg)
	l.SetSelectedBackgroundColor(colorSelectedBg)
	l.SetBorder(true)
	// j/k/g/G: List has no native vim bindings (unlike Table/TreeView --
	// see the same note on internal/ui/globalsearch.go's list). Reuses
	// moveHintHighlight, the exact same wrap-around arithmetic the
	// global-search hint list already uses, rather than a second copy of
	// it.
	l.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		if event.Key() == tcell.KeyRune {
			switch event.Rune() {
			case 'j':
				l.SetCurrentItem(moveHintHighlight(l.GetCurrentItem(), l.GetItemCount(), 1))
				return nil
			case 'k':
				l.SetCurrentItem(moveHintHighlight(l.GetCurrentItem(), l.GetItemCount(), -1))
				return nil
			case 'g':
				if l.GetItemCount() > 0 {
					l.SetCurrentItem(0)
				}
				return nil
			case 'G':
				if n := l.GetItemCount(); n > 0 {
					l.SetCurrentItem(n - 1)
				}
				return nil
			}
		}
		return event
	})
	// Enter: List's own native SetSelectedFunc, no custom handling needed
	// (unlike global search's hint list, which is driven from an
	// InputField rather than the list itself holding focus).
	l.SetSelectedFunc(func(index int, _, _ string, _ rune) {
		p.apply(index)
	})
	p.List = l
	return p
}

// render repopulates the list from items, plus a synthetic leading
// "(clear all ...)" entry -- still worth having when entries are a set,
// since clearing several one by one is tedious. song is the track the
// popup acts on for as long as it stays open (see the field's own
// comment), and names it in the title so there's no doubt which track is
// about to be changed.
func (p *catalogPicker) render(song mpdclient.Song, items []catalogItem) {
	p.song = song
	p.SetTitle(fmt.Sprintf(" %s %q (Enter to toggle, Esc to close) ",
		titleCase(p.kind.noun()), song.DisplayName()))
	p.items = items
	p.refresh()
}

// refresh redraws the list against the track's current entries, keeping
// the cursor where it was. Called after every toggle so the row the user
// just changed visibly reflects it without closing the popup.
func (p *catalogPicker) refresh() {
	current := p.GetCurrentItem()
	set := map[int64]bool{}
	for _, id := range p.kind.on(p.app.queue.metaCache[p.song.File]) {
		set[id] = true
	}

	p.Clear()
	p.AddItem("(clear all "+p.kind.plural()+")", "", 0, nil)
	for _, it := range p.items {
		p.AddItem(catalogPickerLabel(it.Label, set[it.ID]), "", 0, nil)
	}
	if current < p.GetItemCount() {
		p.SetCurrentItem(current)
	}
}

// catalogPickerLabel prefixes an entry with whether it is currently set,
// so the checklist reads at a glance.
//
// Deliberately a tick and a blank, not "[x]"/"[ ]". tview parses "[...]"
// in list text as a style tag, and the first version of this shipped
// exactly that bug: "[x]" looks like a valid tag so it was swallowed
// whole, while "[ ]" contains a space and so survived -- leaving set
// entries with no marker and unset ones with a visible box, precisely
// backwards. Same trap queue.go's formatColors comment describes for
// "[MP3]" in table cells.
//
// The blank is two spaces so both states occupy the same width and the
// labels stay aligned down the list, and the tick is queueMarkTick, so
// "set" looks the same here as it does in the Queue's own columns.
func catalogPickerLabel(label string, on bool) string {
	if on {
		return queueMarkTick + " " + label
	}
	return "  " + label
}

// apply is the list's own SetSelectedFunc (Enter): index 0 is the
// synthetic "clear all" entry, index i>0 is p.items[i-1].
//
// Unlike the other metadata popups this does not close on Enter --
// entries are a set, and closing after each one would make applying two
// a two-popup job. Like handleRateSelectedTrack, the database write and
// the Queue panel's repaint both happen in the background (see
// App.runAsync) so the keypress never waits on disk.
func (p *catalogPicker) apply(index int) {
	song := p.song
	if song.File == "" {
		p.app.closeOverlay()
		return
	}
	db := p.app.metaDB

	if index == 0 {
		p.app.runAsync(func() error {
			return p.kind.clear(db, song.File)
		}, func() {
			p.reload(song.File)
			p.app.showMessage(fmt.Sprintf("cleared %s: %s", p.kind.plural(), song.DisplayName()))
		})
		return
	}
	if index-1 >= len(p.items) {
		return
	}
	item := p.items[index-1]

	// The database decides whether this is an add or a remove, and
	// reports which -- rather than the UI predicting it from the cache
	// and risking a message that contradicts what was written.
	var added bool
	p.app.runAsync(func() error {
		var err error
		added, err = p.kind.toggle(db, song.File, item.ID)
		return err
	}, func() {
		p.reload(song.File)
		verb := "set"
		if !added {
			verb = "cleared"
		}
		p.app.showMessage(fmt.Sprintf("%s %s (%s): %s", verb, p.kind.noun(), item.Label, song.DisplayName()))
	})
}

// reload re-reads the track and pushes it through the Queue's own cache,
// so the Mark column, the Track Info card and this popup all redraw from
// one freshly-read row rather than three guesses at what the write did.
func (p *catalogPicker) reload(file string) {
	track, err := p.app.metaDB.Get(file)
	if err != nil {
		p.app.showError(err)
		return
	}
	p.app.queue.applyTrackMeta(file, track)
	p.refresh()
}

// titleCase uppercases the first letter, for the popup's title.
func titleCase(s string) string {
	if s == "" {
		return s
	}
	return string(s[0]-32) + s[1:]
}

// markCatalog and tagCatalog are the two relations a catalogPicker can
// edit. Each is a handful of one-liners over internal/metadata.

type markCatalog struct{}

func (markCatalog) noun() string   { return "mark" }
func (markCatalog) plural() string { return "marks" }

func (markCatalog) entries(db *metadata.DB) ([]catalogItem, error) {
	reasons, err := db.ListMarkReasons()
	if err != nil {
		return nil, err
	}
	items := make([]catalogItem, len(reasons))
	for i, r := range reasons {
		items[i] = catalogItem{ID: r.ID, Label: r.Reason}
	}
	return items, nil
}

func (markCatalog) on(track metadata.Track) []int64 {
	ids := make([]int64, len(track.Marks))
	for i, m := range track.Marks {
		ids[i] = m.ID
	}
	return ids
}

func (markCatalog) toggle(db *metadata.DB, file string, id int64) (bool, error) {
	return db.ToggleMark(file, id)
}

func (markCatalog) clear(db *metadata.DB, file string) error {
	return db.SetMarks(file, nil)
}

type tagCatalog struct{}

func (tagCatalog) noun() string   { return "tag" }
func (tagCatalog) plural() string { return "tags" }

func (tagCatalog) entries(db *metadata.DB) ([]catalogItem, error) {
	tags, err := db.ListTags()
	if err != nil {
		return nil, err
	}
	items := make([]catalogItem, len(tags))
	for i, t := range tags {
		items[i] = catalogItem{ID: t.ID, Label: t.Tagname}
	}
	return items, nil
}

func (tagCatalog) on(track metadata.Track) []int64 {
	ids := make([]int64, len(track.Tags))
	for i, t := range track.Tags {
		ids[i] = t.ID
	}
	return ids
}

func (tagCatalog) toggle(db *metadata.DB, file string, id int64) (bool, error) {
	return db.ToggleTag(file, id)
}

func (tagCatalog) clear(db *metadata.DB, file string) error {
	return db.SetTags(file, nil)
}
