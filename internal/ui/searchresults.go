package ui

import (
	"github.com/rivo/tview"
)

// Search-result cell icons -- one glyph per entity, so a track / artist /
// album / playlist reads the same wherever the global search shows it.
const (
	iconTrackCell    = "🎵"
	iconArtistCell   = "🎤"
	iconAlbumCell    = "💿"
	iconLyricsCell   = "📝"
	iconPlaylistCell = "🎧"
)

// albumArtistUnknown fills the album-results table's Album Artist column
// when MPD has no AlbumArtist tag for that album.
const albumArtistUnknown = "Not available"

// searchColumn is one column of a per-kind results table: an icon
// prefixed to every cell, the column's hard max text width (longer
// content is truncated with an ellipsis), and whether it absorbs slack
// width. markup marks a column whose cell text arrives already formatted
// with tview style tags (the lyrics excerpt) -- it's passed through
// untouched, not escaped and not re-truncated.
type searchColumn struct {
	icon     string
	maxWidth int
	expand   bool
	markup   bool
}

// searchLayouts is the single place that decides how each search kind's
// results are laid out. To change a kind's columns, widths, or icons,
// edit here -- renderSearchResults and openGlobalSearch's rowFor read
// straight off this.
var searchLayouts = map[globalSearchKind][]searchColumn{
	globalSearchTrack: {
		{icon: iconTrackCell, maxWidth: 40, expand: true},
		{icon: iconArtistCell, maxWidth: 26},
	},
	globalSearchLyrics: {
		{icon: iconTrackCell, maxWidth: 26},
		{icon: iconArtistCell, maxWidth: 18},
		{icon: iconLyricsCell, maxWidth: 60, expand: true, markup: true},
	},
	globalSearchAlbum: {
		{icon: iconAlbumCell, maxWidth: 40, expand: true},
		{icon: iconArtistCell, maxWidth: 26},
	},
	globalSearchArtist: {
		{icon: iconArtistCell, maxWidth: 56, expand: true},
	},
	globalSearchPlaylist: {
		{icon: iconPlaylistCell, maxWidth: 56, expand: true},
	},
}

// searchResultColumns returns kind's column layout, or a lone plain
// column as a safety net for a kind not in searchLayouts.
func searchResultColumns(kind globalSearchKind) []searchColumn {
	if cols, ok := searchLayouts[kind]; ok {
		return cols
	}
	return []searchColumn{{maxWidth: 60, expand: true}}
}

// renderSearchResults fills table with one row per entry in rows -- each
// row a slice of raw cell strings column-aligned with the kind's layout
// (a short row leaves trailing columns blank). There is no header row:
// the per-cell icons carry the column meaning. highlight is the row to
// select, or a negative value for none.
func renderSearchResults(table *tview.Table, kind globalSearchKind, rows [][]string, highlight int) {
	cols := searchResultColumns(kind)
	table.Clear()
	for r, row := range rows {
		for c, col := range cols {
			raw := ""
			if c < len(row) {
				raw = row[c]
			}
			cell := tview.NewTableCell(searchCellText(col, raw)).
				SetMaxWidth(col.maxWidth + 4).
				SetSelectable(true)
			if col.expand {
				cell.SetExpansion(1)
			}
			table.SetCell(r, c, cell)
		}
	}
	if highlight >= 0 && highlight < len(rows) {
		table.Select(highlight, 0)
	}
}

// searchCellText renders one cell: the column icon then the value,
// truncated with an ellipsis at the column's max width and escaped for
// tview's tag parser -- unless the column is markup (an already-shaped
// lyrics excerpt), which is emitted verbatim.
func searchCellText(col searchColumn, value string) string {
	if !col.markup {
		value = tview.Escape(truncateWithEllipsis(value, col.maxWidth))
	}
	switch {
	case col.icon == "":
		return value
	case value == "":
		return col.icon
	default:
		return col.icon + " " + value
	}
}
