package ui

import (
	"fmt"
	"sort"
	"strings"

	"github.com/rivo/tview"

	"mpdtui/internal/mpdclient"
)

// playlistRecentBadgeCount is how many of the most recently updated
// playlists (by MPD's Last-Modified timestamp) get the recency badge.
const playlistRecentBadgeCount = 5

// playlistRecentIcon marks a playlist among the playlistRecentBadgeCount
// most recently modified, right-aligned against the panel's current
// width -- recalculated on every redraw (see realign, called from
// App.build's SetAfterDrawFunc) so it stays pinned to the right edge
// across terminal resizes, the same technique the album art panel uses
// for its own redraw-time positioning.
const playlistRecentIcon = "🆕"

// playlistsPanel lists stored (saved) MPD playlists, sorted by most
// recently updated first, optionally filtered by a substring set via the
// search overlay. The playlistRecentBadgeCount most recently updated get
// a right-aligned 🆕 badge.
type playlistsPanel struct {
	app  *App
	list *tview.List

	pls    []mpdclient.Playlist // full set, sorted by Last-Modified descending
	shown  []mpdclient.Playlist // currently displayed (post-filter), same relative order
	filter string

	badged map[string]bool // playlist names among the most recently updated

	lastWidth int  // inner width last used by realign, to skip redundant work
	dirty     bool // set by render(), forces realign to reapply regardless of lastWidth
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

// sortPlaylistsByRecency sorts pls in place, most recently modified
// first. Equal (including zero, for servers/entries with no reported
// Last-Modified) timestamps break ties alphabetically by name, so
// ordering stays deterministic across refreshes instead of depending on
// whatever order MPD happened to return.
func sortPlaylistsByRecency(pls []mpdclient.Playlist) {
	sort.Slice(pls, func(i, j int) bool {
		if !pls[i].LastModified.Equal(pls[j].LastModified) {
			return pls[i].LastModified.After(pls[j].LastModified)
		}
		return pls[i].Name < pls[j].Name
	})
}

// recentPlaylistBadges returns the names of the first n entries of pls as
// a set, for O(1) badge lookup during render/realign. Assumes pls is
// already sorted most-recent-first (sortPlaylistsByRecency); n larger
// than len(pls) is fine, it just badges everything.
func recentPlaylistBadges(pls []mpdclient.Playlist, n int) map[string]bool {
	badged := make(map[string]bool, n)
	for i := 0; i < len(pls) && i < n; i++ {
		badged[pls[i].Name] = true
	}
	return badged
}

func (p *playlistsPanel) refresh() {
	pls, err := p.app.client.Playlists()
	if err != nil {
		p.app.showError(err)
		return
	}
	sortPlaylistsByRecency(pls)
	p.pls = pls
	p.badged = recentPlaylistBadges(pls, playlistRecentBadgeCount)
	p.render()
}

// render redisplays the (optionally filtered) list and returns how many
// entries are currently shown.
func (p *playlistsPanel) render() int {
	shown := p.pls
	if p.filter != "" {
		shown = nil
		needle := strings.ToLower(p.filter)
		for _, pl := range p.pls {
			if strings.Contains(strings.ToLower(pl.Name), needle) {
				shown = append(shown, pl)
			}
		}
	}
	p.shown = shown

	p.list.Clear()
	for _, pl := range shown {
		name := pl.Name
		p.list.AddItem(name, "", 0, func() { p.app.loadPlaylist(name) })
	}
	p.dirty = true
	p.realign()

	title := " Playlists "
	if p.filter != "" {
		title = fmt.Sprintf(" Playlists: filter %q ", p.filter)
	}
	p.list.SetTitle(title)
	return len(shown)
}

// realign right-pads each badged item's text so its 🆕 icon lands at the
// panel's current right edge. Recomputes only when the inner width has
// changed or render() has rebuilt the list since the last call (dirty) --
// called every redraw from App.build's SetAfterDrawFunc, mirroring how
// the album art panel repositions itself on redraw.
func (p *playlistsPanel) realign() {
	_, _, width, _ := p.list.GetInnerRect()
	if width <= 0 {
		return
	}
	if width == p.lastWidth && !p.dirty {
		return
	}
	p.lastWidth = width
	p.dirty = false

	iconWidth := tview.TaggedStringWidth(playlistRecentIcon)
	for i, pl := range p.shown {
		if !p.badged[pl.Name] {
			continue
		}
		gap := width - tview.TaggedStringWidth(pl.Name) - iconWidth
		if gap < 1 {
			gap = 1
		}
		p.list.SetItemText(i, pl.Name+strings.Repeat(" ", gap)+playlistRecentIcon, "")
	}
}

// setFilter applies f and returns how many playlists matched.
func (p *playlistsPanel) setFilter(f string) int {
	p.filter = f
	return p.render()
}

func (p *playlistsPanel) selectedName() string {
	idx := p.list.GetCurrentItem()
	if idx < 0 || idx >= len(p.shown) {
		return ""
	}
	return p.shown[idx].Name
}
