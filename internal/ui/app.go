// Package ui implements the full, lazygit-style panel TUI for mpdtui.
package ui

import (
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"mpdtui/internal/metadata"
	"mpdtui/internal/mpdclient"
)

const (
	modeNormal = iota
	modeOverlay
)

// flashDuration is how long a transient hint-bar message (an error, a
// "added N tracks" confirmation, etc.) stays up before the hint bar
// reverts to showing keybindings again.
const flashDuration = 3 * time.Second

// App is the full panel-based TUI application.
type App struct {
	tv      *tview.Application
	client  *mpdclient.Client
	watcher *mpdclient.Watcher

	// musicDir is the local filesystem path mirroring MPD's own
	// music_directory (see internal/config.LoadMusicDir), needed by
	// internal/lyrics to find lyrics files -- "" means the lyrics feature
	// is inactive (no config.LoadMusicDir setup), not an error.
	musicDir string

	// metaDB is the local track-metadata database (play count, rating,
	// mark, tags -- see internal/metadata), opened by cmd/mpdtui's main.go
	// only when config.LoadTrackMetadataEnabled() is true. nil means the
	// feature is inactive, not an error -- every consumer of metaDB has to
	// check for nil and no-op (typically via invalidKey) rather than
	// assume it's always present.
	metaDB *metadata.DB

	// cfg is a read-only snapshot of the settings resolved at startup
	// (see ConfigSummary), shown in the Settings overlay's Config tab
	// ('e'). Never mutated after Run constructs App -- internal/ui has
	// no way to edit any of it, by design (see the 'e' request itself:
	// "config data... i cannot modify").
	cfg ConfigSummary

	// runAsync runs a unit of (typically metaDB) work in the background
	// and applies its result on the UI goroutine -- see runAsyncDefault,
	// which build() wires this to. A field rather than calling
	// runAsyncDefault directly so tests can swap in a synchronous stand-
	// in: nothing drains tv's update queue without tv.Run() actually
	// running, so the real (QueueUpdateDraw-based) implementation would
	// otherwise never complete inside a test.
	runAsync func(work func() error, onSuccess func())

	pages *tview.Pages
	root  *tview.Flex

	library   *libraryPanel
	playlists *playlistsPanel
	queue     *queuePanel

	nowPlaying   *tview.TextView
	hintBar      *tview.TextView
	albumArt     *albumArtPanel
	trackInfo    *trackInfoCard
	lyricsViewer *lyricsViewer
	markPicker   *markPicker
	settings     *settingsView
	visualizer   *visualizerPanel

	// currentSong is refreshNowPlaying's own last-fetched CurrentSong,
	// kept around so openLyricsViewer can show it without a redundant
	// fetch of its own -- trackInfoCard gets to skip this same fetch for
	// the same reason, since refreshNowPlaying already re-renders it
	// unconditionally on every tick.
	currentSong mpdclient.Song
	// currentStatus mirrors currentSong -- refreshNowPlaying's own
	// last-fetched Status, so openLyricsViewer can seed the synced-lyrics
	// highlight to the actual current playback position immediately on
	// open, rather than waiting up to one ~500ms tick for it to catch up.
	currentStatus mpdclient.Status

	// playCountedSongID is the queue song id maybeTrackPlayCount has
	// already incremented the play count for, so ticking past the
	// halfway point on every ~500ms refresh doesn't count it again; -1
	// (matching queuePanel.currentID's own "none" convention) means
	// nothing's been counted yet this session.
	playCountedSongID int

	panels   []tview.Primitive
	panelIdx int

	mode               int
	beforeOverlayFocus tview.Primitive
	closeOverlay       func()

	// startedUp is false until refreshAll's initial refreshNowPlaying call
	// completes -- that call is the app *learning* whatever MPD was
	// already playing before mpdtui started, not a real track change, so
	// it must not trigger the auto-jump-to-Queue in refreshNowPlaying
	// (which would override the deliberate default startup focus on
	// Library before the user's done anything).
	startedUp bool

	msgSeq int

	done chan struct{}
}

// Run connects a Watcher and runs the full TUI until the user quits or an
// unrecoverable error occurs. Blocks until the application exits.
// musicDir enables the lyrics feature (see internal/lyrics) when
// non-empty; pass "" to leave it inactive. metaDB enables the local
// play-count/rating/mark/tags database (see internal/metadata) when
// non-nil; pass nil to leave it inactive. Run itself never opens or
// closes metaDB -- that's the caller's (cmd/mpdtui's) responsibility,
// same as the MPD client. cfg is a read-only snapshot shown in the
// Settings overlay's Config tab ('e') -- see ConfigSummary.
func Run(client *mpdclient.Client, musicDir string, metaDB *metadata.DB, cfg ConfigSummary) error {
	a := &App{
		tv:                tview.NewApplication(),
		client:            client,
		musicDir:          musicDir,
		metaDB:            metaDB,
		cfg:               cfg,
		playCountedSongID: -1,
		done:              make(chan struct{}),
	}

	w, err := client.Watch("player", "mixer", "options", "playlist", "stored_playlist", "database")
	if err != nil {
		return fmt.Errorf("watch mpd: %w", err)
	}
	a.watcher = w
	defer func() {
		close(a.done)
		w.Close()
	}()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(sigCh)
	go func() {
		select {
		case <-sigCh:
			a.tv.Stop()
		case <-a.done:
		}
	}()

	a.build()
	a.refreshAll()
	go a.eventLoop()

	return a.tv.Run()
}

// runAsyncDefault is runAsync's real (production) implementation: work
// runs on its own goroutine, off the UI goroutine entirely, so a
// database write never makes a keypress wait on disk I/O; once it's
// done, onSuccess runs safely on the UI goroutine (typical use: repaint
// a Queue-panel cell) via QueueUpdateDraw, or the error is flashed
// instead if work failed.
func (a *App) runAsyncDefault(work func() error, onSuccess func()) {
	go func() {
		err := work()
		a.tv.QueueUpdateDraw(func() {
			if err != nil {
				a.showError(err)
				return
			}
			onSuccess()
		})
	}()
}

func (a *App) build() {
	applyTheme()
	a.runAsync = a.runAsyncDefault

	a.library = newLibraryPanel(a)
	a.playlists = newPlaylistsPanel(a)
	a.queue = newQueuePanel(a)

	wireFocusColors(a.library.tree)
	wireFocusColors(a.playlists.table)
	wireFocusColors(a.queue.table)
	wireFocusColors(a.queue.search)

	a.nowPlaying = tview.NewTextView().SetDynamicColors(true)
	a.nowPlaying.SetBorder(true).SetTitle(" Now Playing ").SetTitleColor(tcell.ColorYellow)
	a.nowPlaying.SetBorderColor(tcell.ColorYellow)

	a.albumArt = newAlbumArtPanel(a)
	a.trackInfo = newTrackInfoCard(a)
	a.lyricsViewer = newLyricsViewer(a)
	a.markPicker = newMarkPicker(a)
	a.settings = newSettingsView(a)
	a.visualizer = newVisualizerPanel(a)

	a.hintBar = tview.NewTextView().SetDynamicColors(true)

	bottomLeft := tview.NewFlex().SetDirection(tview.FlexColumn).
		AddItem(a.albumArt.view, 0, 1, false).
		AddItem(a.playlists.table, 0, 1, false)

	left := tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(a.library.tree, 0, 2, true).
		AddItem(bottomLeft, 0, 1, false)

	searchRow := tview.NewFlex().SetDirection(tview.FlexColumn).
		AddItem(a.queue.search, 0, 3, false).
		AddItem(a.queue.stats, 0, 2, false)

	queueBox := tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(searchRow, 3, 0, false).
		AddItem(a.queue.table, 0, 1, true)

	main := tview.NewFlex().SetDirection(tview.FlexColumn).
		AddItem(left, 0, 1, true).
		AddItem(queueBox, 0, 2, false)

	// nowPlayingRow splits the Now Playing row 50/50: playback status on
	// the left (unchanged), the visualizer container on the right (see
	// visualizer.go's Visualization doc comment for its sizing contract).
	nowPlayingRow := tview.NewFlex().SetDirection(tview.FlexColumn).
		AddItem(a.nowPlaying, 0, 1, false).
		AddItem(a.visualizer.view, 0, 1, false)

	a.root = tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(main, 0, 1, true).
		AddItem(nowPlayingRow, 4, 0, false).
		AddItem(a.hintBar, 1, 0, false)

	a.panels = []tview.Primitive{a.library.tree, a.playlists.table, a.queue.table}

	a.pages = tview.NewPages().AddPage("main", a.root, true, true)

	a.tv.SetInputCapture(a.globalInputCapture)
	a.tv.SetRoot(a.pages, true).SetFocus(a.library.tree)
	a.updateHintBar()

	a.tv.SetAfterDrawFunc(func(tcell.Screen) {
		a.albumArt.draw()
	})
}

func (a *App) refreshAll() {
	a.library.showRoot()
	a.playlists.refresh()
	a.queue.refresh()
	a.queue.refreshStats()
	a.refreshNowPlaying()
	a.startedUp = true
	a.refreshTrackCounts(true) // silent: this is a startup side effect, not a keypress
}

// playlistCountRefreshInterval is how often eventLoop's countTicker
// re-fetches every playlist's track count in the background (see
// refreshTrackCounts) -- MPD has no idle event for "a playlist's track
// count might be stale" (stored_playlist only fires for playlists
// created/renamed/deleted, not edited-in-place by some other client), so
// this is a plain timer rather than something event-driven.
const playlistCountRefreshInterval = 10 * time.Minute

func (a *App) eventLoop() {
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()
	countTicker := time.NewTicker(playlistCountRefreshInterval)
	defer countTicker.Stop()

	for {
		select {
		case name, ok := <-a.watcher.Events():
			if !ok {
				return
			}
			a.tv.QueueUpdateDraw(func() { a.handleSubsystem(name) })
		case _, ok := <-a.watcher.Errors():
			if !ok {
				return
			}
			// The idle connection is dead. This is a personal single-user
			// tool; rather than reconnect-looping forever, surface it and
			// let ticker-driven polling keep the Now Playing bar alive.
			a.tv.QueueUpdateDraw(func() { a.showError(fmt.Errorf("lost MPD event connection")) })
			return
		case <-ticker.C:
			a.tv.QueueUpdateDraw(func() { a.refreshNowPlaying() })
		case <-countTicker.C:
			a.refreshTrackCounts(true) // silent: automatic background refresh
		case <-a.done:
			return
		}
	}
}

// refreshTrackCounts fetches every playlist's track count
// (mpdclient.Client.PlaylistTrackCounts) in the background and applies it
// once done -- one MPD round-trip per playlist, which stays well under a
// second even for a few hundred playlists but is still real enough that
// it must never block the single UI goroutine, whether triggered by
// countTicker's automatic cadence or the 'R' key (handleRefreshPlaylistCounts).
// Mirrors albumArtPanel.fetch's own background-MPD-round-trip-then-
// QueueUpdateDraw pattern; unlike that one, there's no sequence-number
// guard needed here since every call fetches the same thing (a full
// snapshot of current counts) rather than a call being superseded by a
// differently-targeted newer one. silent suppresses the confirmation
// flash: an automatic background refresh shouldn't announce itself, but a
// deliberate keypress should.
func (a *App) refreshTrackCounts(silent bool) {
	go func() {
		counts, err := a.client.PlaylistTrackCounts()
		a.tv.QueueUpdateDraw(func() {
			if err != nil {
				a.showError(err)
				return
			}
			a.playlists.trackCounts = counts
			a.playlists.render()
			if !silent {
				a.showMessage(fmt.Sprintf("refreshed track counts for %d playlists", len(counts)))
			}
		})
	}()
}

// handleRefreshPlaylistCounts is 'R': manually forces refreshTrackCounts,
// gated to the Playlists panel the same way handleSavePlaylist gates 'S'
// -- invalid (flashed, not silently ignored) from any other panel.
func (a *App) handleRefreshPlaylistCounts() {
	if a.tv.GetFocus() != a.playlists.table {
		a.invalidKey("R")
		return
	}
	a.refreshTrackCounts(false)
}

func (a *App) handleSubsystem(name string) {
	switch name {
	case "player", "mixer", "options":
		a.refreshNowPlaying()
	case "playlist":
		a.queue.refresh()
	case "stored_playlist":
		a.playlists.refresh()
		a.queue.refreshStats()
	case "database":
		a.queue.refreshStats()
	}
}

// trackChangedForJump reports whether a play transition from previousID
// to newID (MPD's current status.SongID) is one that refreshNowPlaying
// should react to by jumping focus to the Queue panel and selecting that
// track: true for any actual change to a real playing track -- explicit
// play action or natural auto-advance alike -- but not for the app's own
// startup observation of already-playing state (startedUp), not when
// playback has stopped (newID < 0, nothing to select), and not for the
// same track being re-confirmed on every ~500ms tick (newID == previousID).
func trackChangedForJump(startedUp bool, previousID, newID int) bool {
	return startedUp && newID >= 0 && newID != previousID
}

func (a *App) refreshNowPlaying() {
	st, err := a.client.Status()
	if err != nil {
		a.showError(err)
		return
	}
	song, err := a.client.CurrentSong()
	if err != nil {
		a.showError(err)
		return
	}
	a.renderNowPlaying(st, song)
	a.currentSong = song
	a.currentStatus = st

	// Computed before setCurrent overwrites queue.currentID with the new
	// value.
	trackChanged := trackChangedForJump(a.startedUp, a.queue.currentID, st.SongID)

	a.queue.setCurrent(st.SongID)
	a.albumArt.onTrackChanged(song.File)
	a.trackInfo.render(song, st)
	a.maybeRefreshLyricsViewer(song, trackChanged)
	a.maybeUpdateLyricsHighlight(st)
	a.maybeTrackPlayCount(st, song)
	a.visualizer.tick(st)

	a.maybeJumpToCurrentTrack(trackChanged)
}

// maybeRefreshLyricsViewer re-renders the lyrics viewer for song when
// trackChanged (see trackChangedForJump) and it's the currently open
// overlay -- so a track auto-advancing while the viewer is open shows the
// new track's lyrics without needing to close and reopen it. Deliberately
// NOT called unconditionally the way trackInfo.render is (cheap string
// formatting of data already fetched this tick): lyrics.Read/ReadLRC does
// real filesystem I/O, so re-running it on every ~500ms tick regardless of
// whether the viewer is even visible would be wasted work for a feature
// most ticks won't have it open at all.
func (a *App) maybeRefreshLyricsViewer(song mpdclient.Song, trackChanged bool) {
	if trackChanged && a.mode == modeOverlay && a.tv.GetFocus() == a.lyricsViewer {
		a.lyricsViewer.render(song)
	}
}

// maybeUpdateLyricsHighlight moves the highlighted line in the lyrics
// viewer to match st.Elapsed, when the viewer is open -- unlike
// maybeRefreshLyricsViewer's full reload, this runs on every refresh tick
// (not just trackChanged): updateHighlight is pure in-memory
// recomputation (lyrics.CurrentLineIndex over whatever synced lines
// render already loaded), no filesystem I/O, and no-ops entirely if the
// current track has no synced (.lrc) lyrics loaded or the highlighted
// line hasn't actually moved since the last tick.
func (a *App) maybeUpdateLyricsHighlight(st mpdclient.Status) {
	if a.mode == modeOverlay && a.tv.GetFocus() == a.lyricsViewer {
		a.lyricsViewer.updateHighlight(st.Elapsed)
	}
}

// maybeJumpToCurrentTrack jumps focus to the Queue panel and selects the
// currently playing track when trackChanged (see trackChangedForJump) --
// split out from refreshNowPlaying so it's testable without a live MPD
// client. Skips stealing focus while an overlay is open (e.g. mid-typing
// in the global search popup when a track happens to auto-advance) --
// SetFocus-ing away from the overlay's own widget without also updating
// a.mode/closeOverlay would leave Esc restoring focus to wherever it was
// *before* the overlay opened, not to Queue.
func (a *App) maybeJumpToCurrentTrack(trackChanged bool) {
	if trackChanged && a.mode == modeNormal && a.queue.jumpToCurrent() {
		a.focusPanelPrimitive(a.queue.table)
	}
}

func (a *App) cycleFocus(delta int) {
	a.panelIdx = (a.panelIdx + delta + len(a.panels)) % len(a.panels)
	a.tv.SetFocus(a.panels[a.panelIdx])
	a.updateHintBar()
}

func (a *App) focusPanel(i int) {
	if i < 0 || i >= len(a.panels) {
		return
	}
	a.panelIdx = i
	a.tv.SetFocus(a.panels[i])
	a.updateHintBar()
}

func (a *App) focusPanelPrimitive(p tview.Primitive) {
	for i, pp := range a.panels {
		if pp == p {
			a.panelIdx = i
		}
	}
	a.tv.SetFocus(p)
	a.updateHintBar()
}

func (a *App) showError(err error) {
	a.flash("[red]error: " + err.Error() + "[-]")
}

func (a *App) showMessage(msg string) {
	a.flash("[yellow]" + msg + "[-]")
}

// flash shows text in the hint bar, then reverts to the keybinding hints
// after flashDuration -- unless a newer flash (or a focus change, which
// calls updateHintBar directly) has already superseded it.
func (a *App) flash(text string) {
	a.msgSeq++
	seq := a.msgSeq
	a.hintBar.SetText(text)
	time.AfterFunc(flashDuration, func() {
		a.tv.QueueUpdateDraw(func() {
			if a.msgSeq == seq {
				a.updateHintBar()
			}
		})
	})
}

// hint is a single key:action pair shown in the hint bar.
type hint struct {
	key    string
	action string
}

// hintKeyColor is the tview dynamic-color name used to set a hint's key
// apart from its (differently-styled) action word in the hint bar.
const hintKeyColor = "skyblue"

// formatHints renders hints as "key:action  key:action  ...", each key
// bold and colored (hintKeyColor) so it's visually distinct from its
// action description -- the action text stays unstyled.
func formatHints(hints []hint) string {
	parts := make([]string, len(hints))
	for i, h := range hints {
		parts[i] = fmt.Sprintf("[%s::b]%s[-:-:-]:%s", hintKeyColor, h.key, h.action)
	}
	return strings.Join(parts, "  ")
}

// globalHints work the same regardless of which panel is focused (unlike
// e.g. 'a'/'d'/'o', which are contextual and so live in updateHintBar's
// per-panel lists instead, even though they're dispatched through the
// same globalInputCapture). Each action is deliberately a single word --
// the hint bar is a glance-able reminder, not documentation (that's '?').
var globalHints = []hint{
	{"Space", "toggle"},
	{"s", "stop"},
	{"n/p", "skip"},
	{",/.", "seek"},
	{"-/=", "volume"},
	{"z", "shuffle"},
	{"x", "repeat"},
	{"c", "consume"},
	{"Z", "single"},
	{"D", "empty"},
	{"/", "search"},
	{"f", "find"},
	{"F", "reset"},
	{"i", "info"},
	{"y", "lyrics"},
	{"v", "visualizer"},
	{"L", "locate"},
	{"?", "help"},
	{"Tab/1-3", "panels"},
	{"q", "quit"},
}

func (a *App) updateHintBar() {
	var panelHints []hint
	switch a.tv.GetFocus() {
	case a.library.tree:
		panelHints = []hint{{"Enter", "open"}, {"a", "add"}, {"Bksp", "back"}, {"o", "sort"}}
		if a.library.mode == libSearch {
			panelHints = append(panelHints, hint{"Esc", "clear"})
		}
	case a.playlists.table:
		panelHints = []hint{{"Enter", "load"}, {"a", "append"}, {"d", "delete"}, {"S", "save"}, {"R", "counts"}, {"o", "sort"}}
		if a.playlists.filter != "" {
			panelHints = append(panelHints, hint{"Esc", "clear"})
		}
	case a.queue.table:
		panelHints = []hint{{"Enter", "play"}, {"d", "remove"}, {"J/K", "move"}}
		if a.metaDB != nil {
			panelHints = append(panelHints, hint{"1-5", "rate"}, hint{"m", "mark"})
		}
	}

	text := formatHints(panelHints)
	if text != "" {
		text += "   "
	}
	text += "[::d]Global:[-:-:-]  " + formatHints(globalHints)
	a.hintBar.SetText(text)
}

func (a *App) addAndPlay(song mpdclient.Song) {
	id, err := a.client.QueueAddID(song.File)
	if err != nil {
		a.showError(err)
		return
	}
	if err := a.client.PlayID(id); err != nil {
		a.showError(err)
		return
	}
	a.queue.refresh()
	a.refreshNowPlaying()
}

// queueAddPath adds path to the queue without playing it. path may be a
// single track or a directory -- MPD's own "add" command recurses through
// a directory server-side, so there's no need to fetch and iterate its
// contents here.
func (a *App) queueAddPath(path string) {
	if err := a.client.QueueAdd(path); err != nil {
		a.showError(err)
		return
	}
	a.queue.refresh()
	a.showMessage("added to queue: " + baseName(path))
}

func (a *App) appendPlaylist(name string) {
	if err := a.client.PlaylistAppend(name); err != nil {
		a.showError(err)
		return
	}
	a.queue.refresh()
	a.showMessage("appended playlist " + name)
}

// clearAllSearches is 'F': resets every panel's persistent search/filter
// view back to normal (Library search results -> browse tree, Playlists
// filter -> none), regardless of which panel currently has focus -- unlike
// Esc, which only clears whichever single panel it's pressed in, so a
// search left open in a panel the user has since navigated away from
// would otherwise silently keep filtering that panel indefinitely. The
// Queue's own '/' search has no persistent filtered view to clear (it
// only jumps to a match), so it's untouched here.
func (a *App) clearAllSearches() {
	cleared := false
	if a.library.mode == libSearch {
		a.library.showRoot()
		cleared = true
	}
	if a.playlists.filter != "" {
		a.playlists.setFilter("")
		cleared = true
	}
	if cleared {
		a.showMessage("cleared search/filter")
	} else {
		a.showMessage("nothing to clear")
	}
}

func (a *App) loadPlaylist(name string) {
	if err := a.client.PlaylistLoad(name); err != nil {
		a.showError(err)
		return
	}
	a.queue.refresh()
	a.refreshNowPlaying()
	a.showMessage("loaded playlist " + name)
}
