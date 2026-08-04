package ui

import (
	"fmt"
	"sort"
	"strings"

	"github.com/rivo/tview"
)

// playlistsPanel lists stored (saved) MPD playlists, optionally filtered
// by a substring set via the search overlay.
type playlistsPanel struct {
	app  *App
	list *tview.List

	names  []string
	filter string
}

func newPlaylistsPanel(app *App) *playlistsPanel {
	list := tview.NewList()
	list.ShowSecondaryText(false)
	list.SetHighlightFullLine(true)
	list.SetSelectedFocusOnly(true)
	list.SetSelectedTextColor(colorSelectedFg)
	list.SetSelectedBackgroundColor(colorSelectedBg)
	list.SetBorder(true)
	list.SetTitle(" Playlists ")
	return &playlistsPanel{app: app, list: list}
}

func (p *playlistsPanel) refresh() {
	pls, err := p.app.client.Playlists()
	if err != nil {
		p.app.showError(err)
		return
	}
	names := make([]string, len(pls))
	for i, pl := range pls {
		names[i] = pl.Name
	}
	sort.Strings(names)
	p.names = names
	p.render()
}

func (p *playlistsPanel) render() {
	shown := p.names
	if p.filter != "" {
		shown = nil
		needle := strings.ToLower(p.filter)
		for _, n := range p.names {
			if strings.Contains(strings.ToLower(n), needle) {
				shown = append(shown, n)
			}
		}
	}

	p.list.Clear()
	for _, n := range shown {
		n := n
		p.list.AddItem(n, "", 0, func() { p.app.loadPlaylist(n) })
	}

	title := " Playlists "
	if p.filter != "" {
		title = fmt.Sprintf(" Playlists: filter %q ", p.filter)
	}
	p.list.SetTitle(title)
}

func (p *playlistsPanel) setFilter(f string) {
	p.filter = f
	p.render()
}

func (p *playlistsPanel) selectedName() string {
	idx := p.list.GetCurrentItem()
	if idx < 0 || p.list.GetItemCount() == 0 {
		return ""
	}
	name, _ := p.list.GetItemText(idx)
	return name
}
