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

// playlistIcon prefixes every playlist's display name, in both this panel
// and the Library tree's own playlist entries (see entryLabel), the same
// way folderClosedIcon/folderOpenIcon mark directories -- so a playlist is
// visually identifiable as one wherever it's shown, not just inferred
// from position/context.
const playlistIcon = "🎵"

// playlistDisplayName is what's actually shown for a playlist (icon
// prefix + name), as opposed to its bare Name -- selectedName/loadPlaylist
// etc. always work with the bare name, this is display-only.
func playlistDisplayName(name string) string {
	return playlistIcon + " " + name
}

// recentlyAddedColor tints recentlyAddedPlaylistName's row in this panel
// (see playlistListText) so it reads as generated/managed by mpdtui, not
// something the user saved themselves. Only a best-effort visual cue:
// most terminals render color emoji from their own fixed font palette,
// so an ANSI foreground color rarely actually recolors playlistIcon
// itself -- but it does reliably recolor the name text next to it, which
// is enough to set the row apart at a glance.
const recentlyAddedColor = "teal"

// playlistListText is what's actually shown for a playlist in this
// panel's List: playlistDisplayName, additionally tinted with
// recentlyAddedColor for name == recentlyAddedPlaylistName. Distinct from
// playlistDisplayName itself since Library's tree entries for playlists
// (entryLabel) use that directly and never carry this tinting -- the
// auto-generated/managed distinction is specific to this panel.
func playlistListText(name string) string {
	text := playlistDisplayName(name)
	if name == recentlyAddedPlaylistName {
		return fmt.Sprintf("[%s]%s[-]", recentlyAddedColor, text)
	}
	return text
}

// playlistsSortMode controls the display order of playlistsPanel.pls.
// Independent of the 🆕 badge, which always reflects actual recency
// (recentPlaylistBadges from a recency-sorted copy) regardless of which
// mode is currently displayed -- so sorting alphabetically doesn't hide
// which ones are actually recent, it just changes where they show up in
// the list. Cycled with 'o' while the Playlists panel is focused (see
// App.handleCycleSort).
type playlistsSortMode int

const (
	playlistsSortRecent playlistsSortMode = iota // most recently updated first (default, matches badge criterion)
	playlistsSortName                            // alphabetical, case-insensitive
)

func (m playlistsSortMode) label() string {
	if m == playlistsSortName {
		return "name"
	}
	return "recent"
}

func (m playlistsSortMode) next() playlistsSortMode {
	return (m + 1) % 2
}

// playlistsPanel lists stored (saved) MPD playlists, sorted by most
// recently updated first, optionally filtered by a substring set via the
// search overlay. The playlistRecentBadgeCount most recently updated get
// a right-aligned 🆕 badge.
type playlistsPanel struct {
	app  *App
	list *tview.List

	pls    []mpdclient.Playlist // full set, ordered per sortMode
	shown  []mpdclient.Playlist // currently displayed (post-filter), same relative order
	filter string

	sortMode playlistsSortMode
	badged   map[string]bool // playlist names among the most recently updated, independent of sortMode

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

// sortPlaylistsByName sorts pls in place, alphabetically and
// case-insensitively -- the same comparison Library's buildNodes uses,
// for the same reason (this library mixes naming conventions, and a
// case-sensitive sort would scatter otherwise-adjacent names apart).
func sortPlaylistsByName(pls []mpdclient.Playlist) {
	sort.Slice(pls, func(i, j int) bool {
		return strings.ToLower(pls[i].Name) < strings.ToLower(pls[j].Name)
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

	// Badges always reflect actual recency, independent of sortMode --
	// computed from a separate sorted copy so displaying alphabetically
	// doesn't change which playlists count as "recently updated".
	byRecency := make([]mpdclient.Playlist, len(pls))
	copy(byRecency, pls)
	sortPlaylistsByRecency(byRecency)
	p.badged = recentPlaylistBadges(byRecency, playlistRecentBadgeCount)

	if p.sortMode == playlistsSortName {
		sortPlaylistsByName(pls)
	} else {
		pls = byRecency
	}
	p.pls = pls
	p.render()
}

// cycleSortMode advances to the next sort mode and re-sorts the
// already-fetched playlists in place -- no MPD round-trip needed, this is
// purely a different view of the same data.
func (p *playlistsPanel) cycleSortMode() {
	p.sortMode = p.sortMode.next()
	if p.sortMode == playlistsSortName {
		sortPlaylistsByName(p.pls)
	} else {
		sortPlaylistsByRecency(p.pls)
	}
	p.render()
}

// render redisplays the (optionally filtered) list and returns how many
// entries are currently shown.
func (p *playlistsPanel) render() int {
	shown := p.pls
	if p.filter != "" {
		shown = nil
		for _, pl := range p.pls {
			if containsFold(pl.Name, p.filter) {
				shown = append(shown, pl)
			}
		}
	}
	p.shown = shown

	p.list.Clear()
	for _, pl := range shown {
		name := pl.Name
		p.list.AddItem(playlistListText(name), "", 0, func() { p.app.loadPlaylist(name) })
	}
	p.dirty = true
	p.realign()

	title := fmt.Sprintf(" Playlists (%s) ", p.sortMode.label())
	if p.filter != "" {
		title = fmt.Sprintf(" Playlists (%s): filter %q ", p.sortMode.label(), p.filter)
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

	badgeWidth := tview.TaggedStringWidth(playlistRecentIcon)
	for i, pl := range p.shown {
		if !p.badged[pl.Name] {
			continue
		}
		text := playlistListText(pl.Name)
		gap := width - tview.TaggedStringWidth(text) - badgeWidth
		if gap < 1 {
			gap = 1
		}
		p.list.SetItemText(i, text+strings.Repeat(" ", gap)+playlistRecentIcon, "")
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
