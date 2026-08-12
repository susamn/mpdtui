package ui

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"mpdtui/internal/mpdclient"
)

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

// playlistsSortMode controls the display order of playlistsPanel.pls.
// Cycled with 'o' while the Playlists panel is focused (see
// App.handleCycleSort).
type playlistsSortMode int

const (
	playlistsSortRecent playlistsSortMode = iota // most recently updated first (default)
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

// playlistsHeaderRows is the number of fixed rows (see Table.SetFixed)
// taken up by the column-header row -- mirrors queueHeaderRows/queue.go's
// exact convention: every playlist's table row is offset by this much
// from its index in shown (row = index + playlistsHeaderRows), since row
// 0 is the header.
const playlistsHeaderRows = 1

// playlistNameMaxLen caps the Name column's text (truncateWithEllipsis,
// same helper Queue's Title/Album/Artist columns already use), leaving
// Count guaranteed room. Without a cap, tview.Table sizes each column to
// its widest cell in column order -- Name is column 0, so a long playlist
// name (this app has real ones like "🔴 Do not belong to playlist") would
// claim the panel's entire width before Count is even considered,
// silently truncating Count's own digits with "…" instead (confirmed by
// hand against the real ~215-playlist library before this cap was added:
// "All Songs"'s count rendered as "97…" instead of "5972").
const playlistNameMaxLen = 24

// playlistsHeaderLabels are the column headers for the fixed header row,
// in the same order render() writes data columns. Name absorbs leftover
// width (SetExpansion(1) in render(), the same technique Queue's Artist
// column uses to keep Type/Duration flush right) so Count sits flush
// against the panel's right edge.
var playlistsHeaderLabels = []struct {
	text  string
	align int
}{
	{"Name", tview.AlignLeft},
	{"Count", tview.AlignRight},
}

// setPlaylistsHeader (re)writes the fixed header row. Table.Clear() wipes
// every cell including row 0, so render() calls this again on every
// refresh rather than relying on it being set once at construction time.
// Reuses queueHeaderBg/Fg (queue.go) instead of redefining an identical
// pair, so both panels' headers are guaranteed to look the same, not just
// coincidentally similar.
func setPlaylistsHeader(t *tview.Table) {
	for col, h := range playlistsHeaderLabels {
		t.SetCell(0, col, tview.NewTableCell(h.text).
			SetAlign(h.align).
			SetTextColor(queueHeaderFg).
			SetBackgroundColor(queueHeaderBg).
			SetSelectable(false))
	}
}

// playlistsPanel lists stored (saved) MPD playlists as a table (Name,
// Count columns, mirroring Queue's own table layout and header styling),
// sorted by most recently updated first, optionally filtered by a
// substring set via the search overlay.
type playlistsPanel struct {
	app   *App
	table *tview.Table

	pls    []mpdclient.Playlist // full set, ordered per sortMode
	shown  []mpdclient.Playlist // currently displayed (post-filter), same relative order
	filter string

	sortMode playlistsSortMode

	// trackCounts holds each playlist's track count, keyed by name --
	// populated by App.refreshTrackCounts (an explicit background MPD
	// round-trip, not part of refresh/render's own data), so a name absent
	// from this map just means "not fetched yet", not "zero tracks": its
	// Count cell is left blank rather than showing a misleading "0".
	trackCounts map[string]int
}

func newPlaylistsPanel(app *App) *playlistsPanel {
	p := &playlistsPanel{app: app}

	t := tview.NewTable()
	t.SetBorder(true).SetTitle(" Playlists ")
	t.SetSelectable(true, false)
	t.SetFixed(playlistsHeaderRows, 0)
	t.SetSelectedStyle(tcell.StyleDefault.Background(colorSelectedBg).Foreground(colorSelectedFg))
	t.SetSelectedFunc(func(row, _ int) {
		i := row - playlistsHeaderRows
		if i < 0 || i >= len(p.shown) {
			return
		}
		p.app.loadPlaylist(p.shown[i].Name)
	})
	p.table = t
	setPlaylistsHeader(t)

	return p
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

func (p *playlistsPanel) refresh() {
	pls, err := p.app.client.Playlists()
	if err != nil {
		p.app.showError(err)
		return
	}

	if p.sortMode == playlistsSortName {
		sortPlaylistsByName(pls)
	} else {
		sortPlaylistsByRecency(pls)
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

// render redisplays the (optionally filtered) table and returns how many
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

	p.table.Clear()
	setPlaylistsHeader(p.table)
	for i, pl := range shown {
		row := i + playlistsHeaderRows
		name := playlistDisplayName(truncateWithEllipsis(pl.Name, playlistNameMaxLen))
		p.table.SetCell(row, 0, tview.NewTableCell(name).SetExpansion(1))

		count := ""
		if n, ok := p.trackCounts[pl.Name]; ok {
			count = strconv.Itoa(n)
		}
		p.table.SetCell(row, 1, tview.NewTableCell(count).SetAlign(tview.AlignRight))
	}

	title := fmt.Sprintf(" Playlists (%s) ", p.sortMode.label())
	if p.filter != "" {
		title = fmt.Sprintf(" Playlists (%s): filter %q ", p.sortMode.label(), p.filter)
	}
	p.table.SetTitle(title)
	return len(shown)
}

// setFilter applies f and returns how many playlists matched.
func (p *playlistsPanel) setFilter(f string) int {
	p.filter = f
	return p.render()
}

func (p *playlistsPanel) selectedName() string {
	row, _ := p.table.GetSelection()
	i := row - playlistsHeaderRows
	if i < 0 || i >= len(p.shown) {
		return ""
	}
	return p.shown[i].Name
}
