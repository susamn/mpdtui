package ui

import (
	"fmt"
	"sort"

	"github.com/rivo/tview"

	"mpdtui/internal/mpdclient"
)

type libraryLevel int

const (
	libArtists libraryLevel = iota
	libAlbums
	libTracks
	libSearch
)

// libraryPanel browses the MPD library Artist -> Album -> Track, or shows
// free-text search results in place of that hierarchy.
type libraryPanel struct {
	app  *App
	list *tview.List

	level  libraryLevel
	artist string
	album  string
	query  string
	songs  []mpdclient.Song // index-aligned with list items at libTracks/libSearch
}

func newLibraryPanel(app *App) *libraryPanel {
	list := tview.NewList()
	list.ShowSecondaryText(false)
	list.SetHighlightFullLine(true)
	list.SetBorder(true)
	list.SetTitle(" Library ")
	return &libraryPanel{app: app, list: list}
}

func (p *libraryPanel) showArtists() {
	artists, err := p.app.client.Artists()
	if err != nil {
		p.app.showError(err)
		return
	}
	sort.Strings(artists)

	p.level = libArtists
	p.songs = nil
	p.list.Clear()
	for _, artist := range artists {
		artist := artist
		p.list.AddItem(artist, "", 0, func() { p.showAlbums(artist) })
	}
	p.list.SetTitle(" Library ")
}

func (p *libraryPanel) showAlbums(artist string) {
	albums, err := p.app.client.Albums(artist)
	if err != nil {
		p.app.showError(err)
		return
	}
	sort.Strings(albums)

	p.level = libAlbums
	p.artist = artist
	p.songs = nil
	p.list.Clear()
	for _, album := range albums {
		album := album
		p.list.AddItem(album, "", 0, func() { p.showTracks(artist, album) })
	}
	p.list.SetTitle(fmt.Sprintf(" Library: %s ", artist))
}

func (p *libraryPanel) showTracks(artist, album string) {
	songs, err := p.app.client.Tracks(artist, album)
	if err != nil {
		p.app.showError(err)
		return
	}

	p.level = libTracks
	p.artist, p.album = artist, album
	p.songs = songs
	p.list.Clear()
	for _, s := range songs {
		s := s
		p.list.AddItem(trackLabel(s), "", 0, func() { p.app.addAndPlay(s) })
	}
	p.list.SetTitle(fmt.Sprintf(" Library: %s - %s ", artist, album))
}

func (p *libraryPanel) showSearch(query string) {
	songs, err := p.app.client.Search(query)
	if err != nil {
		p.app.showError(err)
		return
	}

	p.level = libSearch
	p.query = query
	p.songs = songs
	p.list.Clear()
	for _, s := range songs {
		s := s
		p.list.AddItem(trackLabel(s), "", 0, func() { p.app.addAndPlay(s) })
	}
	p.list.SetTitle(fmt.Sprintf(" Library: search %q (%d) ", query, len(songs)))
	if len(songs) == 0 {
		p.app.showMessage("no results for " + query)
	}
}

// back moves up one level in the browsing hierarchy. No-op at the top.
func (p *libraryPanel) back() {
	switch p.level {
	case libAlbums:
		p.showArtists()
	case libTracks:
		p.showAlbums(p.artist)
	case libSearch:
		p.showArtists()
	}
}

// selectedForAdd resolves the current selection to the song(s) it
// represents, for the 'a' add-to-queue action: a whole artist's
// discography, a whole album, or a single track.
func (p *libraryPanel) selectedForAdd() ([]mpdclient.Song, error) {
	idx := p.list.GetCurrentItem()
	if idx < 0 || p.list.GetItemCount() == 0 {
		return nil, nil
	}
	switch p.level {
	case libArtists:
		artist, _ := p.list.GetItemText(idx)
		return p.app.client.ArtistTracks(artist)
	case libAlbums:
		album, _ := p.list.GetItemText(idx)
		return p.app.client.Tracks(p.artist, album)
	case libTracks, libSearch:
		if idx < len(p.songs) {
			return []mpdclient.Song{p.songs[idx]}, nil
		}
	}
	return nil, nil
}

func trackLabel(s mpdclient.Song) string {
	return fmt.Sprintf("%s  [%s]", s.DisplayName(), FormatDuration(s.Duration))
}
