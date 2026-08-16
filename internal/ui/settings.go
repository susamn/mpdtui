package ui

import (
	"fmt"
	"strings"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
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

// The Database tab's own two catalog tables -- a fixed pair today (a
// third table would just extend this, plus a case in refreshCatalogTable/
// addCatalogRow/deleteCatalogRow), not a generic N-table framework, since
// internal/metadata only ever grows catalog tables rarely and by hand.
const (
	dbSubTabMarkReasons = iota
	dbSubTabTags
)

// The Database tab's own three sub-views: browsing the selected
// catalog's table, typing a new entry, or confirming a delete.
const (
	dbModeTable = iota
	dbModeAdd
	dbModeConfirmDelete
)

// catalogRow is one displayed row of whichever catalog table is
// currently selected -- kept alongside the visual tview.Table (indexed
// the same way, row-1 for the header) so a selected row's real id is
// available for delete without re-parsing it back out of a rendered
// cell.
type catalogRow struct {
	id   int64
	name string
}

// settingsView is the 'e' overlay: a two-tab Config (read-only) /
// Database (browse and edit the mark_reason/tags catalog tables) view.
// Config is a fixed snapshot (App.cfg, resolved once at startup) --
// nothing in it changes while the app runs, so it's rendered once at
// construction rather than refreshed on every open. Database is only
// interactive when App.metaDB is active; otherwise it just explains why,
// matching the "off means off" convention every other metaDB-gated
// feature in this app already follows (see e.g. Queue's Plays/Mark/
// Rating columns).
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

	subTabBar    *tview.TextView
	dbPages      *tview.Pages
	catalogTable *tview.Table
	addInput     *tview.InputField
	confirmView  *tview.TextView

	subTab            int
	currentRows       []catalogRow
	pendingDeleteID   int64
	pendingDeleteName string

	activeTab int
	dbMode    int
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

// buildDatabaseTab returns the Database tab's content: a table-selector
// sub-tab bar (Mark Reasons / Tags) over a selectable catalog table with
// add/delete, when metaDB is active -- or a plain explanation (mirroring
// metadataNotEnabled's own message) when it isn't.
func (s *settingsView) buildDatabaseTab() tview.Primitive {
	if s.app.metaDB == nil {
		s.databaseInteractive = false
		view := tview.NewTextView().SetDynamicColors(true)
		view.SetText("[red]track metadata not enabled -- set track_metadata = true in ~/.config/mpdtui/config[-]")
		return view
	}
	s.databaseInteractive = true

	s.catalogTable = tview.NewTable()
	s.catalogTable.SetBorder(true)
	s.catalogTable.SetSelectable(true, false)
	s.catalogTable.SetFixed(1, 0)
	s.catalogTable.SetSelectedStyle(tcell.StyleDefault.Background(colorSelectedBg).Foreground(colorSelectedFg))

	s.addInput = tview.NewInputField()
	s.addInput.SetBorder(true).SetTitle(" New entry (Enter to add, Esc to close Settings) ")
	s.addInput.SetDoneFunc(func(key tcell.Key) {
		if key == tcell.KeyEnter {
			s.submitAdd()
		}
	})

	s.confirmView = tview.NewTextView().SetDynamicColors(true).SetTextAlign(tview.AlignCenter)
	s.confirmView.SetBorder(true).SetTitle(" Confirm delete ")

	s.dbPages = tview.NewPages().
		AddPage("table", s.catalogTable, true, true).
		AddPage("add", centered(s.addInput, 50, 3), true, false).
		AddPage("confirm", centered(s.confirmView, 50, 5), true, false)

	s.subTabBar = tview.NewTextView().SetDynamicColors(true)
	s.switchSubTab(dbSubTabMarkReasons)

	return tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(s.subTabBar, 1, 0, false).
		AddItem(s.dbPages, 0, 1, true)
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

// subTabName is the currently selected catalog's display name, reused
// both for the sub-tab bar and the catalog table's own title/messages.
func (s *settingsView) subTabName() string {
	if s.subTab == dbSubTabMarkReasons {
		return "Mark Reasons"
	}
	return "Tags"
}

// switchSubTab moves to tab (dbSubTabMarkReasons or dbSubTabTags),
// updating the sub-tab bar highlight and repainting the catalog table
// from that table's current contents.
func (s *settingsView) switchSubTab(tab int) {
	s.subTab = tab
	s.renderSubTabBar()
	s.refreshCatalogTable()
}

func (s *settingsView) renderSubTabBar() {
	mr, tags := "Mark Reasons", "Tags"
	if s.subTab == dbSubTabMarkReasons {
		mr = "[green::b]" + mr + "[-:-:-]"
	} else {
		tags = "[green::b]" + tags + "[-:-:-]"
	}
	s.subTabBar.SetText("  " + mr + "    " + tags + "   (Left/Right to switch)")
}

// refreshCatalogTable re-fetches the currently selected catalog
// (mark_reason or tags) and repaints the table -- called on every
// sub-tab switch and after every successful add/delete, so the change
// shows up immediately. A direct synchronous read, not routed through
// App.runAsync: this only ever runs right when the user opens the
// overlay, switches sub-tabs, or submits one add/delete, never on a
// hot/repeated path the way the Queue panel's own metadata refresh does
// -- same reasoning handleOpenMarkPicker's own synchronous
// ListMarkReasons call already relies on.
func (s *settingsView) refreshCatalogTable() {
	if !s.databaseInteractive {
		return
	}
	var rows []catalogRow
	if s.subTab == dbSubTabMarkReasons {
		reasons, err := s.app.metaDB.ListMarkReasons()
		if err != nil {
			s.app.showError(err)
			return
		}
		for _, r := range reasons {
			rows = append(rows, catalogRow{id: r.ID, name: r.Reason})
		}
	} else {
		tags, err := s.app.metaDB.ListTags()
		if err != nil {
			s.app.showError(err)
			return
		}
		for _, t := range tags {
			rows = append(rows, catalogRow{id: t.ID, name: t.Tagname})
		}
	}
	s.currentRows = rows

	s.catalogTable.Clear()
	s.catalogTable.SetCell(0, 0, tview.NewTableCell("ID").
		SetSelectable(false).SetTextColor(queueHeaderFg).SetBackgroundColor(queueHeaderBg))
	s.catalogTable.SetCell(0, 1, tview.NewTableCell("Name").
		SetSelectable(false).SetTextColor(queueHeaderFg).SetBackgroundColor(queueHeaderBg).SetExpansion(1))
	for i, row := range rows {
		r := i + 1
		s.catalogTable.SetCell(r, 0, tview.NewTableCell(fmt.Sprintf("%d", row.id)))
		s.catalogTable.SetCell(r, 1, tview.NewTableCell(row.name).SetExpansion(1))
	}
	s.catalogTable.SetTitle(fmt.Sprintf(" %s (a: add, d: delete) ", s.subTabName()))
}

// startAdd switches the Database tab into its "add" sub-view: a small
// bordered edit box (reusing the centered() popup treatment every other
// text-entry prompt in this app already uses, e.g. openInput), focused
// and ready to type.
func (s *settingsView) startAdd() {
	s.dbMode = dbModeAdd
	label := "Add mark reason: "
	if s.subTab == dbSubTabTags {
		label = "Add tag: "
	}
	s.addInput.SetLabel(label).SetText("")
	s.dbPages.SwitchToPage("add")
	s.app.tv.SetFocus(s.addInput)
}

// submitAdd is addInput's Enter handler: adds the typed text as a new
// row in whichever catalog is currently selected, then returns to the
// table view regardless of outcome (blank input is silently a no-op,
// same as a cancel). The write itself runs through App.runAsync, same
// as every other metaDB write in this app; a duplicate name (both
// mark_reason.reason and tags.tagname are UNIQUE) surfaces as a normal
// error flash via runAsync's own error handling, not a crash.
func (s *settingsView) submitAdd() {
	text := strings.TrimSpace(s.addInput.GetText())
	s.addInput.SetText("")
	s.dbMode = dbModeTable
	s.dbPages.SwitchToPage("table")
	s.app.tv.SetFocus(s.catalogTable)
	if text == "" {
		return
	}
	db := s.app.metaDB
	if s.subTab == dbSubTabMarkReasons {
		s.app.runAsync(func() error {
			_, err := db.AddMarkReason(text)
			return err
		}, func() {
			s.refreshCatalogTable()
			s.app.showMessage("added mark reason: " + text)
		})
		return
	}
	s.app.runAsync(func() error {
		_, err := db.AddTag(text)
		return err
	}, func() {
		s.refreshCatalogTable()
		s.app.showMessage("added tag: " + text)
	})
}

// startDelete switches the Database tab into its "confirm delete"
// sub-view for whichever row is currently selected in the catalog
// table -- a no-op if nothing's selected (an empty catalog, or the
// header row). Confirming is a separate step (see handleKey's
// dbModeConfirmDelete case), matching this app's established
// destructive-action convention (e.g. Playlists' own 'd').
func (s *settingsView) startDelete() {
	row, _ := s.catalogTable.GetSelection()
	idx := row - 1 // header offset
	if idx < 0 || idx >= len(s.currentRows) {
		return
	}
	r := s.currentRows[idx]
	s.pendingDeleteID = r.id
	s.pendingDeleteName = r.name
	s.dbMode = dbModeConfirmDelete
	s.confirmView.SetText(fmt.Sprintf("Delete %q from %s?\n\n[green::b]y[-:-:-]es   [red::b]n[-:-:-]o", r.name, s.subTabName()))
	s.dbPages.SwitchToPage("confirm")
	s.app.tv.SetFocus(s.confirmView)
}

// cancelDelete backs out of the confirm-delete sub-view without
// deleting anything, returning to the table view.
func (s *settingsView) cancelDelete() {
	s.dbMode = dbModeTable
	s.dbPages.SwitchToPage("table")
	s.app.tv.SetFocus(s.catalogTable)
}

// confirmDeleteNow performs the pending delete -- DeleteMarkReason or
// DeleteTag, both of which also clear any track that still references
// the row being removed (see their own doc comments in
// internal/metadata), so this can never leave a dangling reference
// behind. Returns to the table view immediately (optimistic, matching
// this app's other metaDB writes -- see App.runAsync's own doc comment);
// the write itself happens in the background.
func (s *settingsView) confirmDeleteNow() {
	id, name, subTab := s.pendingDeleteID, s.pendingDeleteName, s.subTab
	s.cancelDelete()

	db := s.app.metaDB
	if subTab == dbSubTabMarkReasons {
		s.app.runAsync(func() error {
			return db.DeleteMarkReason(id)
		}, func() {
			s.refreshCatalogTable()
			s.app.showMessage("deleted mark reason: " + name)
		})
		return
	}
	s.app.runAsync(func() error {
		return db.DeleteTag(id)
	}, func() {
		s.refreshCatalogTable()
		s.app.showMessage("deleted tag: " + name)
	})
}

// renderTabBar highlights whichever top-level tab is currently active,
// in colorActiveBorder's own green (the same color a focused panel's
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
// updating the visible page, the tab bar highlight, and focus. Always
// resets the Database tab back to its table sub-view (dbModeTable) --
// whether freshly entering or coming back to it -- so a stale in-
// progress add/confirm from a previous visit never lingers.
func (s *settingsView) switchTab(tab int) {
	s.activeTab = tab
	switch tab {
	case settingsTabConfig:
		s.pages.SwitchToPage("config")
		s.app.tv.SetFocus(s.configView)
	case settingsTabDatabase:
		s.pages.SwitchToPage("database")
		if s.databaseInteractive {
			s.dbMode = dbModeTable
			s.dbPages.SwitchToPage("table")
			s.refreshCatalogTable()
			s.app.tv.SetFocus(s.catalogTable)
		} else {
			s.app.tv.SetFocus(s.pages)
		}
	}
	s.renderTabBar()
}

// handleKey intercepts every key this view manages itself -- Tab/
// Backtab (switch top-level tabs), and, on the Database tab's table
// sub-view, Left/Right (switch catalog), 'a'/'d' (add/delete), or while
// a confirm-delete prompt is up, 'y'/'n' -- called from
// globalInputCapture, which runs before any focused primitive sees the
// event, so this works regardless of which of this view's own widgets
// currently has focus. Reports whether it consumed the event; anything
// it doesn't recognize (typing, Enter, Backspace, the catalog table's
// own native j/k/g/G row navigation) falls through to whatever's
// actually focused.
func (s *settingsView) handleKey(event *tcell.EventKey) bool {
	if event.Key() == tcell.KeyTab || event.Key() == tcell.KeyBacktab {
		s.switchTab(1 - s.activeTab)
		return true
	}
	if s.activeTab != settingsTabDatabase || !s.databaseInteractive {
		return false
	}
	switch s.dbMode {
	case dbModeConfirmDelete:
		if event.Key() == tcell.KeyRune {
			switch event.Rune() {
			case 'y', 'Y':
				s.confirmDeleteNow()
			case 'n', 'N':
				s.cancelDelete()
			}
		}
		// Swallow everything else while a confirm prompt is up, rather
		// than letting a stray key fall through to the hidden table.
		return true
	case dbModeAdd:
		return false // typing/Enter must reach addInput natively
	default: // dbModeTable
		switch event.Key() {
		case tcell.KeyLeft:
			s.switchSubTab(dbSubTabMarkReasons)
			return true
		case tcell.KeyRight:
			s.switchSubTab(dbSubTabTags)
			return true
		case tcell.KeyRune:
			switch event.Rune() {
			case 'a':
				s.startAdd()
				return true
			case 'd':
				s.startDelete()
				return true
			}
		}
	}
	return false
}

// focused reports whether any of this view's own focusable widgets
// currently has application focus -- used by globalInputCapture to
// decide whether to route a key through handleKey instead of the
// default overlay handling (which assumes a single fixed primitive per
// overlay, not true here across two tabs and three Database sub-views).
func (s *settingsView) focused() bool {
	focus := s.app.tv.GetFocus()
	if focus == s.configView {
		return true
	}
	if !s.databaseInteractive {
		return focus == s.pages
	}
	return focus == s.catalogTable || focus == s.addInput || focus == s.confirmView
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
