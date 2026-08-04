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
)

// Colors mirror internal/ui/theme.go's scheme (kept independent per
// DEPENDENCY.md rather than imported -- picker has no dependency on ui).
const (
	colorAccent     = tcell.ColorGreen
	colorSelectedBg = tcell.ColorBlue
	colorSelectedFg = tcell.ColorYellow
)

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
// nothing.
func RunPlaylistPicker(client *mpdclient.Client) error {
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

	idx, err := pickString("Playlists", labels)
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
// (Esc/Ctrl-C) does nothing.
func RunTrackPicker(client *mpdclient.Client) error {
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

	idx, err := pickString("Tracks", labels)
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

// pickString shows an fzf-style fuzzy finder over labels and returns the
// index into labels the user picked, or -1 if they cancelled.
func pickString(title string, labels []string) (int, error) {
	applyTheme()
	app := tview.NewApplication()

	list := tview.NewList()
	list.ShowSecondaryText(false)
	list.SetHighlightFullLine(true)
	list.SetSelectedTextColor(colorSelectedFg)
	list.SetSelectedBackgroundColor(colorSelectedBg)
	list.SetBorderColor(colorAccent)
	list.SetBorder(true).SetTitle(fmt.Sprintf(" %s ", title))

	input := tview.NewInputField().SetLabel("> ")
	input.SetBorderColor(colorAccent)
	input.SetBorder(true)

	var order []int
	rebuild := func(query string) {
		order = FilterSortIndex(query, labels)
		list.Clear()
		for _, idx := range order {
			list.AddItem(labels[idx], "", 0, nil)
		}
		if list.GetItemCount() > 0 {
			list.SetCurrentItem(0)
		}
	}
	rebuild("")
	input.SetChangedFunc(rebuild)

	selected := -1
	confirm := func() {
		if i := list.GetCurrentItem(); i >= 0 && i < len(order) {
			selected = order[i]
		}
		app.Stop()
	}
	cancel := func() {
		selected = -1
		app.Stop()
	}

	input.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		switch event.Key() {
		case tcell.KeyDown, tcell.KeyCtrlN:
			moveSelection(list, 1)
			return nil
		case tcell.KeyUp, tcell.KeyCtrlP:
			moveSelection(list, -1)
			return nil
		case tcell.KeyEnter:
			confirm()
			return nil
		case tcell.KeyEscape, tcell.KeyCtrlC:
			cancel()
			return nil
		}
		return event
	})

	layout := tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(input, 3, 0, true).
		AddItem(list, 0, 1, false)

	app.SetRoot(layout, true).SetFocus(input)
	if err := app.Run(); err != nil {
		return -1, err
	}
	return selected, nil
}

func moveSelection(list *tview.List, delta int) {
	n := list.GetItemCount()
	if n == 0 {
		return
	}
	cur := (list.GetCurrentItem() + delta + n) % n
	list.SetCurrentItem(cur)
}
