// Package ui implements the full, lazygit-style panel TUI for mpdtui.
package ui

import (
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

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

	pages *tview.Pages
	root  *tview.Flex

	library   *libraryPanel
	playlists *playlistsPanel
	queue     *queuePanel

	nowPlaying *tview.TextView
	hintBar    *tview.TextView
	albumArt   *albumArtPanel
	trackInfo  *trackInfoCard
	visualizer *visualizerPanel

	panels   []tview.Primitive
	panelIdx int

	mode               int
	beforeOverlayFocus tview.Primitive
	closeOverlay       func()

	msgSeq int

	done chan struct{}
}

// Run connects a Watcher and runs the full TUI until the user quits or an
// unrecoverable error occurs. Blocks until the application exits.
func Run(client *mpdclient.Client) error {
	a := &App{
		tv:     tview.NewApplication(),
		client: client,
		done:   make(chan struct{}),
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

func (a *App) build() {
	applyTheme()

	a.library = newLibraryPanel(a)
	a.playlists = newPlaylistsPanel(a)
	a.queue = newQueuePanel(a)

	wireFocusColors(a.library.tree)
	wireFocusColors(a.playlists.list)
	wireFocusColors(a.queue.table)
	wireFocusColors(a.queue.search)

	a.nowPlaying = tview.NewTextView().SetDynamicColors(true)
	a.nowPlaying.SetBorder(true).SetTitle(" Now Playing ").SetTitleColor(tcell.ColorYellow)
	a.nowPlaying.SetBorderColor(tcell.ColorYellow)

	a.albumArt = newAlbumArtPanel(a)
	a.trackInfo = newTrackInfoCard(a)
	a.visualizer = newVisualizerPanel(a)

	a.hintBar = tview.NewTextView().SetDynamicColors(true)

	bottomLeft := tview.NewFlex().SetDirection(tview.FlexColumn).
		AddItem(a.albumArt.view, 0, 1, false).
		AddItem(a.playlists.list, 0, 1, false)

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

	a.panels = []tview.Primitive{a.library.tree, a.playlists.list, a.queue.table}

	a.pages = tview.NewPages().AddPage("main", a.root, true, true)

	a.tv.SetInputCapture(a.globalInputCapture)
	a.tv.SetRoot(a.pages, true).SetFocus(a.library.tree)
	a.updateHintBar()

	a.tv.SetAfterDrawFunc(func(tcell.Screen) {
		a.albumArt.draw()
		a.playlists.realign()
	})
}

func (a *App) refreshAll() {
	a.library.showRoot()
	a.playlists.refresh()
	a.queue.refresh()
	a.queue.refreshStats()
	a.refreshNowPlaying()
}

func (a *App) eventLoop() {
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

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
		case <-a.done:
			return
		}
	}
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
	a.queue.setCurrent(st.SongID)
	a.albumArt.onTrackChanged(song.File)
	a.trackInfo.render(song)
	a.visualizer.tick(st)
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

func (a *App) updateHintBar() {
	var panelHints string
	switch a.tv.GetFocus() {
	case a.library.tree:
		panelHints = "Enter:expand/play  a:add  Bksp:back"
		if a.library.mode == libSearch {
			panelHints += "  Esc:clear search"
		}
	case a.playlists.list:
		panelHints = "Enter:load+play  a:append  d:delete  S:save queue"
		if a.playlists.filter != "" {
			panelHints += "  Esc:clear filter"
		}
	case a.queue.table:
		panelHints = "Enter:play  d:remove  J/K:move"
	}
	global := "Space:play/pause  s:stop  n/p:next/prev  ,/.:seek  -/=:vol  z:shuffle  x:repeat  D:clear queue  /:search  f:find  i:info  v:visualizer  ?:help  Tab/1-3:panels  q:quit"
	a.hintBar.SetText("[::b]" + panelHints + "[-:-:-]  |  " + global)
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

func (a *App) loadPlaylist(name string) {
	if err := a.client.PlaylistLoad(name); err != nil {
		a.showError(err)
		return
	}
	a.queue.refresh()
	a.refreshNowPlaying()
	a.showMessage("loaded playlist " + name)
}
