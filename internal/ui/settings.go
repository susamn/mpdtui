package ui

import (
	"fmt"
	"strings"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"mpdtui/internal/metadata"
)

// ConfigSummary is a read-only snapshot of the settings mpdtui resolved
// at startup (MPD connection, music_dir, track_metadata), shown in the
// Settings overlay's Config tab ('e'). internal/ui doesn't depend on
// internal/config (see DEPENDENCY.md) -- cmd/mpdtui/main.go, which
// already resolves all of these via that package, builds this struct and
// passes it into Run, the same "plain already-resolved values, not the
// config system itself" pattern musicDir/metaDB already use. Deliberately
// excludes the MPD password's actual value (MPDPasswordSet is just
// whether one is configured) -- a settings view has no business
// displaying a credential.
type ConfigSummary struct {
	MPDHost              string
	MPDPort              string
	MPDPasswordSet       bool
	MusicDir             string
	TrackMetadataEnabled bool
	ConfigFilePath       string
	DBFilePath           string
}

const (
	settingsTabConfig = iota
	settingsTabDatabase
)

// settingsView is the 'e' overlay: a two-tab Config (read-only) /
// Database (add mark_reason/tags catalog rows) view. Config is a fixed
// snapshot (App.cfg, resolved once at startup) -- nothing in it changes
// while the app runs, so it's rendered once at construction rather than
// refreshed on every open. Database is only interactive when App.metaDB
// is active; otherwise it just explains why, matching the "off means
// off" convention every other metaDB-gated feature in this app already
// follows (see e.g. Queue's Plays/Mark/Rating columns).
type settingsView struct {
	*tview.Flex
	app    *App
	pages  *tview.Pages
	tabBar *tview.TextView

	configView *tview.TextView

	// databaseInteractive is false when metaDB is nil -- fixed for the
	// whole session (metaDB's nil-ness never changes after startup), so
	// this is decided once in newSettingsView rather than re-checked.
	databaseInteractive bool
	markReasonsList     *tview.TextView
	newReasonInput      *tview.InputField
	tagsList            *tview.TextView
	newTagInput         *tview.InputField

	activeTab int
}

func newSettingsView(app *App) *settingsView {
	s := &settingsView{app: app}

	s.configView = tview.NewTextView().SetDynamicColors(true).SetScrollable(true)
	s.configView.SetText(formatConfigSummary(app.cfg))

	s.pages = tview.NewPages().
		AddPage("config", s.configView, true, true).
		AddPage("database", s.buildDatabaseTab(), true, false)

	s.tabBar = tview.NewTextView().SetDynamicColors(true)

	s.Flex = tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(s.tabBar, 1, 0, false).
		AddItem(s.pages, 0, 1, true)
	s.Flex.SetBorder(true).SetTitle(" Settings (Tab to switch tabs, Esc to close) ")

	s.renderTabBar()
	return s
}

// buildDatabaseTab returns the Database tab's content: the interactive
// add-mark-reason/add-tag form when metaDB is active, or a plain
// explanation (mirroring metadataNotEnabled's own message) when it isn't.
func (s *settingsView) buildDatabaseTab() tview.Primitive {
	if s.app.metaDB == nil {
		s.databaseInteractive = false
		view := tview.NewTextView().SetDynamicColors(true)
		view.SetText("[red]track metadata not enabled -- set track_metadata = true in ~/.config/mpdtui/config[-]")
		return view
	}
	s.databaseInteractive = true

	s.markReasonsList = tview.NewTextView()
	s.newReasonInput = tview.NewInputField().SetLabel("Add mark reason: ")
	s.newReasonInput.SetDoneFunc(func(key tcell.Key) {
		if key == tcell.KeyEnter {
			s.submitNewMarkReason()
		}
	})

	s.tagsList = tview.NewTextView()
	s.newTagInput = tview.NewInputField().SetLabel("Add tag: ")
	s.newTagInput.SetDoneFunc(func(key tcell.Key) {
		if key == tcell.KeyEnter {
			s.submitNewTag()
		}
	})

	s.refreshCatalogs()

	return tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(sectionLabel("Mark Reasons"), 1, 0, false).
		AddItem(s.markReasonsList, 6, 0, false).
		AddItem(s.newReasonInput, 1, 0, false).
		AddItem(sectionLabel("Tags"), 1, 0, false).
		AddItem(s.tagsList, 6, 0, false).
		AddItem(s.newTagInput, 1, 0, false)
}

func sectionLabel(text string) *tview.TextView {
	return tview.NewTextView().SetDynamicColors(true).SetText("[::b]" + text + "[-:-:-]")
}

// formatConfigSummary renders cfg as the Config tab's fixed content.
func formatConfigSummary(cfg ConfigSummary) string {
	password := "not set"
	if cfg.MPDPasswordSet {
		password = "set"
	}
	musicDir := cfg.MusicDir
	if musicDir == "" {
		musicDir = "(not configured -- lyrics feature inactive)"
	}
	trackMetadata := "no"
	if cfg.TrackMetadataEnabled {
		trackMetadata = "yes"
	}
	return fmt.Sprintf(
		"[::b]MPD[-:-:-]\n  Host:     %s\n  Port:     %s\n  Password: %s\n\n"+
			"[::b]Music directory[-:-:-]\n  %s\n\n"+
			"[::b]Track metadata[-:-:-]\n  Enabled:       %s\n  Config file:   %s\n  Database file: %s\n",
		cfg.MPDHost, cfg.MPDPort, password,
		musicDir,
		trackMetadata, orPlaceholder(cfg.ConfigFilePath), orPlaceholder(cfg.DBFilePath),
	)
}

func orPlaceholder(s string) string {
	if s == "" {
		return "(unknown)"
	}
	return s
}

func formatMarkReasons(reasons []metadata.MarkReason) string {
	var b strings.Builder
	for _, r := range reasons {
		fmt.Fprintf(&b, "%d: %s\n", r.ID, r.Reason)
	}
	return b.String()
}

func formatTags(tags []metadata.Tag) string {
	var b strings.Builder
	for _, t := range tags {
		fmt.Fprintf(&b, "%d: %s\n", t.ID, t.Tagname)
	}
	return b.String()
}

// refreshCatalogs re-fetches mark_reason/tags and repaints their display
// lists -- called once when the Database tab is first built and again
// after every successful add, so the just-added entry shows up
// immediately. A direct synchronous read, not routed through
// App.runAsync: this only ever runs when the user opens the overlay or
// submits one add, never on a hot/repeated path the way the Queue
// panel's own metadata refresh does -- same reasoning
// handleOpenMarkPicker's own synchronous ListMarkReasons call already
// relies on.
func (s *settingsView) refreshCatalogs() {
	if !s.databaseInteractive {
		return
	}
	reasons, err := s.app.metaDB.ListMarkReasons()
	if err != nil {
		s.app.showError(err)
		return
	}
	s.markReasonsList.SetText(formatMarkReasons(reasons))

	tags, err := s.app.metaDB.ListTags()
	if err != nil {
		s.app.showError(err)
		return
	}
	s.tagsList.SetText(formatTags(tags))
}

// submitNewMarkReason adds newReasonInput's current text as a new
// mark_reason catalog row -- the in-app counterpart to editing the
// database by hand. The write runs through App.runAsync so it never
// blocks the UI goroutine; a duplicate reason (the column is UNIQUE)
// surfaces as a normal error flash via runAsync's own error handling,
// not a crash.
func (s *settingsView) submitNewMarkReason() {
	text := strings.TrimSpace(s.newReasonInput.GetText())
	if text == "" {
		return
	}
	db := s.app.metaDB
	s.app.runAsync(func() error {
		_, err := db.AddMarkReason(text)
		return err
	}, func() {
		s.newReasonInput.SetText("")
		s.refreshCatalogs()
		s.app.showMessage("added mark reason: " + text)
	})
}

// submitNewTag mirrors submitNewMarkReason for the tags catalog.
func (s *settingsView) submitNewTag() {
	text := strings.TrimSpace(s.newTagInput.GetText())
	if text == "" {
		return
	}
	db := s.app.metaDB
	s.app.runAsync(func() error {
		_, err := db.AddTag(text)
		return err
	}, func() {
		s.newTagInput.SetText("")
		s.refreshCatalogs()
		s.app.showMessage("added tag: " + text)
	})
}

// renderTabBar highlights whichever tab is currently active, in
// colorActiveBorder's own green (the same color a focused panel's
// border/title uses) -- "green" by W3C name here since tview's dynamic-
// color tags accept names directly, no need for the hex form.
func (s *settingsView) renderTabBar() {
	configLabel, dbLabel := "Config", "Database"
	if s.activeTab == settingsTabConfig {
		configLabel = "[green::b]" + configLabel + "[-:-:-]"
	} else {
		dbLabel = "[green::b]" + dbLabel + "[-:-:-]"
	}
	s.tabBar.SetText("  " + configLabel + "    " + dbLabel)
}

// switchTab moves to tab (settingsTabConfig or settingsTabDatabase),
// updating the visible page, the tab bar highlight, and focus -- Config
// focuses its own (read-only, scrollable) text view; Database focuses
// the mark-reason input if interactive, or the page itself if metaDB
// isn't active (nothing to focus there).
func (s *settingsView) switchTab(tab int) {
	s.activeTab = tab
	switch tab {
	case settingsTabConfig:
		s.pages.SwitchToPage("config")
		s.app.tv.SetFocus(s.configView)
	case settingsTabDatabase:
		s.pages.SwitchToPage("database")
		if s.databaseInteractive {
			s.app.tv.SetFocus(s.newReasonInput)
		} else {
			s.app.tv.SetFocus(s.pages)
		}
	}
	s.renderTabBar()
}

// handleKey intercepts Tab/Backtab (switch tabs) and, while the Database
// tab is active, Down/Up (move between the two add-new fields) --
// called from globalInputCapture, which runs before any focused
// primitive sees the event, so this works regardless of which of this
// view's own widgets currently has focus. Reports whether it consumed
// the event; everything else (typing, Enter, Backspace) falls through
// to whatever's actually focused.
func (s *settingsView) handleKey(event *tcell.EventKey) bool {
	switch event.Key() {
	case tcell.KeyTab, tcell.KeyBacktab:
		s.switchTab(1 - s.activeTab)
		return true
	case tcell.KeyDown:
		if s.activeTab == settingsTabDatabase && s.databaseInteractive {
			s.app.tv.SetFocus(s.newTagInput)
			return true
		}
	case tcell.KeyUp:
		if s.activeTab == settingsTabDatabase && s.databaseInteractive {
			s.app.tv.SetFocus(s.newReasonInput)
			return true
		}
	}
	return false
}

// focused reports whether any of this view's own focusable widgets
// currently has application focus -- used by globalInputCapture to
// decide whether to route a key through handleKey instead of the
// default overlay handling (which assumes a single fixed primitive per
// overlay, not true here across two tabs).
func (s *settingsView) focused() bool {
	focus := s.app.tv.GetFocus()
	if focus == s.configView {
		return true
	}
	if s.databaseInteractive {
		return focus == s.newReasonInput || focus == s.newTagInput
	}
	return focus == s.pages
}

// openSettings is 'e': opens the Settings overlay, always starting on
// the Config tab. Resets the tab state directly (page + bar highlight)
// rather than via switchTab, which also calls SetFocus -- doing that
// before showOverlay captures a.beforeOverlayFocus would clobber it with
// the settings view's own configView instead of whatever was actually
// focused before 'e' was pressed, breaking focus restoration on close.
func (a *App) openSettings() {
	a.settings.activeTab = settingsTabConfig
	a.settings.pages.SwitchToPage("config")
	a.settings.renderTabBar()
	a.showOverlay("settings", centered(a.settings.Flex, 76, 22), a.settings.configView)
}
