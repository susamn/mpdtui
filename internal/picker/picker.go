// Package picker implements one-shot fzf-style fuzzy finders for the
// -p (playlist) and -t (track) CLI flags. Each takes over the terminal
// just long enough to pick one item, then exits -- neither the full
// panel UI nor the mini player is involved.
package picker

import (
	"fmt"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"mpdtui/internal/mpdclient"
	"mpdtui/internal/theme"
)

// conn is what the pickers ask of the MPD server. An interface rather
// than *mpdclient.Client so each picker's "what happens after you press
// Enter" can be tested without a live server -- and so proving that the
// track picker queues and plays the chosen track does not require
// actually interrupting whatever is playing.
type conn interface {
	Playlists() ([]mpdclient.Playlist, error)
	PlaylistLoad(name string) error
	AllSongs() ([]mpdclient.Song, error)
	QueueAddID(uri string) (int, error)
	PlayID(id int) error
}

var _ conn = (*mpdclient.Client)(nil)

// Colors mirror internal/ui/theme.go's own derivation from the live
// theme (internal/theme.LoadFrom) -- picker still has no dependency on
// ui itself (see DEPENDENCY.md), only on the shared internal/theme leaf
// package both import independently. Resolved once per run (initColors,
// called by RunPlaylistPicker/RunTrackPicker with
// internal/config.LoadThemeFile's value), not re-read on a theme-change
// signal like ui's own palette: a picker run is a single short-lived
// pick-one-and-exit invocation, gone well before a theme switch could
// plausibly land mid-run.
var colorAccent, colorSelectedBg, colorSelectedFg tcell.Color

// initColors resolves colorAccent/colorSelectedBg/colorSelectedFg from
// themeFile (internal/config.LoadThemeFile's value -- always a real
// path once internal/config.EnsureConfigFiles has run; LoadFrom's own
// Default() fallback covers the rare case it's unreadable anyway).
func initColors(themeFile string) {
	p, _ := theme.LoadFrom(themeFile)
	colorAccent = hexColor(p.Accent)
	colorSelectedBg = hexColor(p.Selection)
	colorSelectedFg = contrastColor(colorSelectedBg)
}

// hexColor converts a theme.Color ("#RRGGBB") to a tcell.Color, via
// tcell's own hex parser.
func hexColor(c theme.Color) tcell.Color {
	if c == "" {
		return tcell.ColorDefault
	}
	return tcell.GetColor(string(c))
}

// contrastColor picks a readable foreground for text drawn on top of
// bg, so the selected-row highlight stays legible regardless of how
// bright or dark the active theme's accent color is.
func contrastColor(bg tcell.Color) tcell.Color {
	r, g, b := bg.RGB()
	luminance := 0.299*float64(r) + 0.587*float64(g) + 0.114*float64(b)
	if luminance > 140 {
		return tcell.ColorBlack
	}
	return tcell.ColorWhite
}

// applyTheme overrides tview's global defaults, which otherwise force a
// pure black background and ANSI white text irrespective of the user's
// actual terminal theme.
func applyTheme() {
	tview.Styles.PrimitiveBackgroundColor = tcell.ColorDefault
	tview.Styles.ContrastBackgroundColor = tcell.ColorDefault
	tview.Styles.MoreContrastBackgroundColor = tcell.ColorDefault
	tview.Styles.BorderColor = tcell.ColorDefault
	tview.Styles.TitleColor = tcell.ColorDefault
	tview.Styles.GraphicsColor = tcell.ColorDefault
	tview.Styles.PrimaryTextColor = tcell.ColorDefault
}

// RunPlaylistPicker fuzzy-searches stored playlists. Selecting one
// (Enter) clears the queue and plays it. Cancelling (Esc/Ctrl-C) does
// nothing. themeFile is config.LoadThemeFile()'s value -- see
// initColors.
func RunPlaylistPicker(client conn, themeFile string) error {
	initColors(themeFile)

	playlists, err := client.Playlists()
	if err != nil {
		return fmt.Errorf("list playlists: %w", err)
	}
	if len(playlists) == 0 {
		fmt.Println("mpdtui: no playlists found")
		return nil
	}

	labels := make([]string, len(playlists))
	for i, p := range playlists {
		labels[i] = p.Name
	}

	idx, err := pick("Playlists", labels)
	if err != nil {
		return err
	}
	if idx < 0 {
		return nil
	}
	return client.PlaylistLoad(playlists[idx].Name)
}

// RunTrackPicker fuzzy-searches every track in the library. Selecting
// one (Enter) appends it to the queue and plays it. Cancelling
// (Esc/Ctrl-C) does nothing. themeFile is config.LoadThemeFile()'s
// value -- see initColors.
func RunTrackPicker(client conn, themeFile string) error {
	initColors(themeFile)

	songs, err := client.AllSongs()
	if err != nil {
		return fmt.Errorf("list tracks: %w", err)
	}
	if len(songs) == 0 {
		fmt.Println("mpdtui: no tracks found")
		return nil
	}

	labels := make([]string, len(songs))
	for i, s := range songs {
		labels[i] = s.DisplayName()
	}

	idx, err := pick("Tracks", labels)
	if err != nil {
		return err
	}
	if idx < 0 {
		return nil
	}

	id, err := client.QueueAddID(songs[idx].File)
	if err != nil {
		return fmt.Errorf("add track: %w", err)
	}
	return client.PlayID(id)
}

// pick shows an fzf-style fuzzy finder over labels and returns the
// index into labels the user picked, or -1 if they cancelled.
//
// A variable rather than a plain function so the two Run* pickers above
// -- which have their own logic either side of it -- can be tested
// without one of them taking over the terminal.
var pick = func(title string, labels []string) (int, error) {
	return newFinder(title, labels).run()
}

// finder is the fuzzy finder's state: the labels it was given, the
// filtered ordering currently on screen, and what the user settled on.
// Split out from the run loop so the filtering and key handling can be
// exercised directly, without a terminal or a tview event loop.
type finder struct {
	app   *tview.Application
	list  *tview.List
	input *tview.InputField

	labels []string
	// order maps a row in list back to its index in labels, since the
	// list only holds whatever the current query matched.
	order []int

	// selected is the chosen index into labels, -1 for "cancelled",
	// which is also its value until the user confirms.
	selected int
	stopped  bool
}

func newFinder(title string, labels []string) *finder {
	applyTheme()

	f := &finder{
		app:      tview.NewApplication(),
		list:     tview.NewList(),
		input:    tview.NewInputField().SetLabel("> "),
		labels:   labels,
		selected: -1,
	}

	f.list.ShowSecondaryText(false)
	f.list.SetHighlightFullLine(true)
	f.list.SetSelectedTextColor(colorSelectedFg)
	f.list.SetSelectedBackgroundColor(colorSelectedBg)
	f.list.SetBorderColor(colorAccent)
	f.list.SetBorder(true).SetTitle(fmt.Sprintf(" %s ", title))

	f.input.SetBorderColor(colorAccent)
	f.input.SetBorder(true)

	f.rebuild("")
	f.input.SetChangedFunc(f.rebuild)
	f.input.SetInputCapture(f.handleKey)
	return f
}

// rebuild re-filters the list for query, keeping the selection on the
// best match (row 0) so Enter always acts on something sensible.
func (f *finder) rebuild(query string) {
	f.order = FilterSortIndex(query, f.labels)
	f.list.Clear()
	for _, idx := range f.order {
		f.list.AddItem(f.labels[idx], "", 0, nil)
	}
	if f.list.GetItemCount() > 0 {
		f.list.SetCurrentItem(0)
	}
}

// confirm settles on whatever row is highlighted. A query matching
// nothing leaves selected at -1, so confirming an empty list cancels
// rather than picking an arbitrary item.
func (f *finder) confirm() {
	if i := f.list.GetCurrentItem(); i >= 0 && i < len(f.order) {
		f.selected = f.order[i]
	}
	f.stop()
}

func (f *finder) cancel() {
	f.selected = -1
	f.stop()
}

func (f *finder) stop() {
	f.stopped = true
	f.app.Stop()
}

// handleKey is the input field's capture: navigation and confirm/cancel
// are claimed, and everything else falls through so it types normally.
func (f *finder) handleKey(event *tcell.EventKey) *tcell.EventKey {
	switch event.Key() {
	case tcell.KeyDown, tcell.KeyCtrlN:
		moveSelection(f.list, 1)
		return nil
	case tcell.KeyUp, tcell.KeyCtrlP:
		moveSelection(f.list, -1)
		return nil
	case tcell.KeyEnter:
		f.confirm()
		return nil
	case tcell.KeyEscape, tcell.KeyCtrlC:
		f.cancel()
		return nil
	}
	return event
}

func (f *finder) run() (int, error) {
	layout := tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(f.input, 3, 0, true).
		AddItem(f.list, 0, 1, false)

	f.app.SetRoot(layout, true).SetFocus(f.input)
	if err := f.app.Run(); err != nil {
		return -1, err
	}
	return f.selected, nil
}

func moveSelection(list *tview.List, delta int) {
	n := list.GetItemCount()
	if n == 0 {
		return
	}
	cur := (list.GetCurrentItem() + delta + n) % n
	list.SetCurrentItem(cur)
}
