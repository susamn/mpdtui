package ui

import (
	"fmt"
	"sort"

	"mpdtui/internal/mpdclient"
)

// recentlyAddedPlaylistName is the stored playlist mpdtui itself generates
// and keeps up to date -- the recentlyAddedCount most recently added
// tracks in the whole library, newest first. Regenerated (see
// regenerateRecentlyAdded) automatically whenever MPD's database actually
// changes (handleSubsystem's "database" case) and on demand via 'R' while
// the Playlists panel is focused (handleRegenerateRecentlyAdded).
const recentlyAddedPlaylistName = "Recently Added"

// recentlyAddedCount is how many tracks recentlyAddedPlaylistName holds.
const recentlyAddedCount = 50

// recentlyAddedURIs returns the file URIs of the n most recently added
// songs, newest first (by mpdclient.Song.Added). Ties -- including every
// song on an MPD server old enough to never report Added at all, which
// all carry the zero time -- break by File path, so the result stays
// deterministic across calls rather than depending on AllSongs' arbitrary
// return order. Split out from regenerateRecentlyAdded so the actual
// selection logic is testable without a live MPD connection.
func recentlyAddedURIs(songs []mpdclient.Song, n int) []string {
	sorted := make([]mpdclient.Song, len(songs))
	copy(sorted, songs)
	sort.SliceStable(sorted, func(i, j int) bool {
		if !sorted[i].Added.Equal(sorted[j].Added) {
			return sorted[i].Added.After(sorted[j].Added)
		}
		return sorted[i].File < sorted[j].File
	})
	if len(sorted) > n {
		sorted = sorted[:n]
	}
	uris := make([]string, len(sorted))
	for i, s := range sorted {
		uris[i] = s.File
	}
	return uris
}

// regenerateRecentlyAdded rebuilds recentlyAddedPlaylistName from
// scratch: fetches the whole library, picks the recentlyAddedCount most
// recently added tracks, and replaces the stored playlist's contents with
// exactly those (see mpdclient.Client.ReplacePlaylist). Runs synchronously
// on the caller's goroutine -- same as Library's own AllSongs()-backed
// searches (showSearch/showAlbumSearch/showArtistSearch) -- rather than
// introducing a separate async path just for this; for this app's stated
// scope (a personal library) that's on the order of tens to a couple
// hundred milliseconds, not a real stall. silent suppresses the
// "regenerated"/"no tracks" hint-bar flash, used when this runs as an
// automatic side effect of a database change rather than a deliberate
// keypress -- an error is always surfaced either way, since silently
// swallowing a real failure would be confusing regardless of how this was
// triggered.
func (a *App) regenerateRecentlyAdded(silent bool) {
	songs, err := a.client.AllSongs()
	if err != nil {
		a.showError(err)
		return
	}

	uris := recentlyAddedURIs(songs, recentlyAddedCount)
	if len(uris) == 0 {
		if !silent {
			a.showMessage("no tracks in the library yet")
		}
		return
	}

	if err := a.client.ReplacePlaylist(recentlyAddedPlaylistName, uris); err != nil {
		a.showError(err)
		return
	}
	a.playlists.refresh()
	if !silent {
		a.showMessage(fmt.Sprintf("regenerated %q (%d tracks)", recentlyAddedPlaylistName, len(uris)))
	}
}

// handleRegenerateRecentlyAdded is 'R': manually forces
// regenerateRecentlyAdded, gated to the Playlists panel the same way
// handleSavePlaylist gates 'S' -- invalid (flashed, not silently ignored)
// from any other panel.
func (a *App) handleRegenerateRecentlyAdded() {
	if a.tv.GetFocus() != a.playlists.list {
		a.invalidKey("R")
		return
	}
	a.regenerateRecentlyAdded(false)
}
