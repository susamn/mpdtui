// Package settingsview is the Settings overlay ('e'): a two-tab
// Config (read-only) / Database (browse and edit the mark_reason and
// tags catalogs) view.
//
// It lives outside internal/ui because it needs nothing from the App
// beyond three values and three callbacks, all named in Deps. Colors
// come from internal/uitheme, which is what lets a panel live outside
// internal/ui at all -- they change under it on a theme reload, so they
// cannot be constructor arguments.
package settingsview

import (
	"fmt"
	"strings"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"mpdtui/internal/metadata"
	"mpdtui/internal/uitheme"
)

// Row is one line of the read-only Config tab. The view renders label/
// value pairs and knows nothing about where they came from: the caller
// owns the settings type and how to describe it (see internal/ui's
// configRows), so adding a setting never touches this package.
type Row struct {
	Label, Value string
}

// Deps is everything this view needs from its host. Spelled out rather
// than taking the App, so what the Settings overlay actually touches is
// visible here instead of having to be inferred from the whole struct.
type Deps struct {
	// App is the tview application, used only to move focus between
	// this view's own widgets (the two tabs, and the Database tab's
	// table / add-input / confirm sub-views).
	App *tview.Application

	// MetaDB backs the Database tab. nil means the track-metadata
	// feature is off, and the tab says so instead of being interactive
	// -- the same "off means off" convention every other metaDB-gated
	// feature follows.
	MetaDB *metadata.DB

	// Config is the Config tab's content, rendered once at construction
	// because none of it changes while the app runs.
	Config []Row

	// ShowError and ShowMessage report a failed and a completed action
	// respectively, through whatever the host uses for transient
	// feedback (in mpdtui, the hint bar).
	ShowError   func(error)
	ShowMessage func(string)

	// RunAsync runs a metaDB write off the UI goroutine and applies
	// onSuccess back on it, reporting any error itself. Every write
	// this view makes goes through it.
	RunAsync func(work func() error, onSuccess func())
}

// populateConfigTable renders rows into the Config tab's table, under a
// header styled like every other table's in the app.
func populateConfigTable(table *tview.Table, rows []Row) {
	table.Clear()
	table.SetCell(0, 0, tview.NewTableCell("Setting").
		SetSelectable(false).SetTextColor(uitheme.TableHeaderFg()).SetBackgroundColor(uitheme.TableHeaderBg()))
	table.SetCell(0, 1, tview.NewTableCell("Value").
		SetSelectable(false).SetTextColor(uitheme.TableHeaderFg()).SetBackgroundColor(uitheme.TableHeaderBg()).SetExpansion(1))

	for i, r := range rows {
		row := i + 1
		table.SetCell(row, 0, tview.NewTableCell(r.Label))
		table.SetCell(row, 1, tview.NewTableCell(r.Value).SetExpansion(1))
	}
}

// centered gives a primitive the same small-popup treatment every other
// text-entry prompt in this app uses. Deliberately duplicated from
// internal/ui's identical helper rather than imported: that would be an
// edge from this package back up to its own host (see DEPENDENCY.md),
// and this is five lines of tview.Grid arithmetic that has never
// changed.
func centered(p tview.Primitive, width, height int) tview.Primitive {
	return tview.NewGrid().
		SetColumns(0, width, 0).
		SetRows(0, height, 0).
		AddItem(p, 1, 1, 1, 1, 0, 0, true)
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

// View is the 'e' overlay: a two-tab Config (read-only) /
// Database (browse and edit the mark_reason/tags catalog tables) view.
// Config is a fixed snapshot (Deps.Config, resolved once at startup) --
// nothing in it changes while the app runs, so it's rendered once at
// construction rather than refreshed on every open. Database is only
// interactive when Deps.MetaDB is set; otherwise it just explains why,
// matching the "off means off" convention every other metaDB-gated
// feature in this app already follows (see e.g. Queue's Plays/Mark/
// Rating columns).
type View struct {
	*tview.Flex
	deps   Deps
	pages  *tview.Pages
	tabBar *tview.TextView

	configView *tview.Table

	// databaseInteractive is false when metaDB is nil -- fixed for the
	// whole session (metaDB's nil-ness never changes after startup), so
	// this is decided once in New rather than re-checked.
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

func New(deps Deps) *View {
	s := &View{deps: deps}

	s.configView = tview.NewTable()
	s.configView.SetBorder(true).SetTitle(" Config (read-only) ")
	s.configView.SetSelectable(false, false)
	populateConfigTable(s.configView, deps.Config)

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
func (s *View) buildDatabaseTab() tview.Primitive {
	if s.deps.MetaDB == nil {
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
	s.catalogTable.SetSelectedStyle(uitheme.SelectedStyle())

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

// subTabName is the currently selected catalog's display name, reused
// both for the sub-tab bar and the catalog table's own title/messages.
func (s *View) subTabName() string {
	if s.subTab == dbSubTabMarkReasons {
		return "Mark Reasons"
	}
	return "Tags"
}

// switchSubTab moves to tab (dbSubTabMarkReasons or dbSubTabTags),
// updating the sub-tab bar highlight and repainting the catalog table
// from that table's current contents.
func (s *View) switchSubTab(tab int) {
	s.subTab = tab
	s.renderSubTabBar()
	s.refreshCatalogTable()
}

func (s *View) renderSubTabBar() {
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
// Deps.RunAsync: this only ever runs right when the user opens the
// overlay, switches sub-tabs, or submits one add/delete, never on a
// hot/repeated path the way the Queue panel's own metadata refresh does
// -- the same reasoning internal/ui's own mark picker relies on for
// its synchronous ListMarkReasons call.
func (s *View) refreshCatalogTable() {
	if !s.databaseInteractive {
		return
	}
	var rows []catalogRow
	if s.subTab == dbSubTabMarkReasons {
		reasons, err := s.deps.MetaDB.ListMarkReasons()
		if err != nil {
			s.deps.ShowError(err)
			return
		}
		for _, r := range reasons {
			rows = append(rows, catalogRow{id: r.ID, name: r.Reason})
		}
	} else {
		tags, err := s.deps.MetaDB.ListTags()
		if err != nil {
			s.deps.ShowError(err)
			return
		}
		for _, t := range tags {
			rows = append(rows, catalogRow{id: t.ID, name: t.Tagname})
		}
	}
	s.currentRows = rows

	s.catalogTable.Clear()
	s.catalogTable.SetCell(0, 0, tview.NewTableCell("ID").
		SetSelectable(false).SetTextColor(uitheme.TableHeaderFg()).SetBackgroundColor(uitheme.TableHeaderBg()))
	s.catalogTable.SetCell(0, 1, tview.NewTableCell("Name").
		SetSelectable(false).SetTextColor(uitheme.TableHeaderFg()).SetBackgroundColor(uitheme.TableHeaderBg()).SetExpansion(1))
	for i, row := range rows {
		r := i + 1
		s.catalogTable.SetCell(r, 0, tview.NewTableCell(fmt.Sprintf("%d", row.id)))
		s.catalogTable.SetCell(r, 1, tview.NewTableCell(row.name).SetExpansion(1))
	}
	s.catalogTable.SetTitle(fmt.Sprintf(" %s (a: add, d: delete) ", s.subTabName()))
}

// startAdd switches the Database tab into its "add" sub-view: a small
// bordered edit box (reusing the centered() popup treatment every other
// text-entry prompt in this app already uses -- see centered), focused
// and ready to type.
func (s *View) startAdd() {
	s.dbMode = dbModeAdd
	label := "Add mark reason: "
	if s.subTab == dbSubTabTags {
		label = "Add tag: "
	}
	s.addInput.SetLabel(label).SetText("")
	s.dbPages.SwitchToPage("add")
	s.deps.App.SetFocus(s.addInput)
}

// submitAdd is addInput's Enter handler: adds the typed text as a new
// row in whichever catalog is currently selected, then returns to the
// table view regardless of outcome (blank input is silently a no-op,
// same as a cancel). The write itself runs through Deps.RunAsync, same
// as every other metaDB write in this app; a duplicate name (both
// mark_reason.reason and tags.tagname are UNIQUE) surfaces as a normal
// error flash via runAsync's own error handling, not a crash.
func (s *View) submitAdd() {
	text := strings.TrimSpace(s.addInput.GetText())
	s.addInput.SetText("")
	s.dbMode = dbModeTable
	s.dbPages.SwitchToPage("table")
	s.deps.App.SetFocus(s.catalogTable)
	if text == "" {
		return
	}
	db := s.deps.MetaDB
	if s.subTab == dbSubTabMarkReasons {
		s.deps.RunAsync(func() error {
			_, err := db.AddMarkReason(text)
			return err
		}, func() {
			s.refreshCatalogTable()
			s.deps.ShowMessage("added mark reason: " + text)
		})
		return
	}
	s.deps.RunAsync(func() error {
		_, err := db.AddTag(text)
		return err
	}, func() {
		s.refreshCatalogTable()
		s.deps.ShowMessage("added tag: " + text)
	})
}

// startDelete switches the Database tab into its "confirm delete"
// sub-view for whichever row is currently selected in the catalog
// table -- a no-op if nothing's selected (an empty catalog, or the
// header row). Confirming is a separate step (see HandleKey's
// dbModeConfirmDelete case), matching this app's established
// destructive-action convention (e.g. Playlists' own 'd').
func (s *View) startDelete() {
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
	s.deps.App.SetFocus(s.confirmView)
}

// cancelDelete backs out of the confirm-delete sub-view without
// deleting anything, returning to the table view.
func (s *View) cancelDelete() {
	s.dbMode = dbModeTable
	s.dbPages.SwitchToPage("table")
	s.deps.App.SetFocus(s.catalogTable)
}

// confirmDeleteNow performs the pending delete -- DeleteMarkReason or
// DeleteTag, both of which also clear any track that still references
// the row being removed (see their own doc comments in
// internal/metadata), so this can never leave a dangling reference
// behind. Returns to the table view immediately (optimistic, matching
// this app's other metaDB writes -- see Deps.RunAsync's own doc comment);
// the write itself happens in the background.
func (s *View) confirmDeleteNow() {
	id, name, subTab := s.pendingDeleteID, s.pendingDeleteName, s.subTab
	s.cancelDelete()

	db := s.deps.MetaDB
	if subTab == dbSubTabMarkReasons {
		s.deps.RunAsync(func() error {
			return db.DeleteMarkReason(id)
		}, func() {
			s.refreshCatalogTable()
			s.deps.ShowMessage("deleted mark reason: " + name)
		})
		return
	}
	s.deps.RunAsync(func() error {
		return db.DeleteTag(id)
	}, func() {
		s.refreshCatalogTable()
		s.deps.ShowMessage("deleted tag: " + name)
	})
}

// renderTabBar highlights whichever top-level tab is currently active,
// in uitheme.ActiveBorder()'s own green (the same color a focused panel's
// border/title uses) -- "green" by W3C name here since tview's dynamic-
// color tags accept names directly, no need for the hex form.
func (s *View) renderTabBar() {
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
func (s *View) switchTab(tab int) {
	s.activeTab = tab
	switch tab {
	case settingsTabConfig:
		s.pages.SwitchToPage("config")
		s.deps.App.SetFocus(s.configView)
	case settingsTabDatabase:
		s.pages.SwitchToPage("database")
		if s.databaseInteractive {
			s.dbMode = dbModeTable
			s.dbPages.SwitchToPage("table")
			s.refreshCatalogTable()
			s.deps.App.SetFocus(s.catalogTable)
		} else {
			s.deps.App.SetFocus(s.pages)
		}
	}
	s.renderTabBar()
}

// HandleKey intercepts every key this view manages itself -- Tab/
// Backtab (switch top-level tabs), and, on the Database tab's table
// sub-view, Left/Right (switch catalog), 'a'/'d' (add/delete), or while
// a confirm-delete prompt is up, 'y'/'n' -- called from
// the host's global input capture, which runs before any focused
// primitive sees the
// event, so this works regardless of which of this view's own widgets
// currently has focus. Reports whether it consumed the event; anything
// it doesn't recognize (typing, Enter, Backspace, the catalog table's
// own native j/k/g/G row navigation) falls through to whatever's
// actually focused.
func (s *View) HandleKey(event *tcell.EventKey) bool {
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

// AllowsGlobalKeys reports whether 'q'-quit and the transport cluster
// should stay live while this view has focus. True for the Config table
// and the Database tab's catalog table -- both just navigable displays,
// no different from trackInfoCard/lyricsViewer/markPicker, which already
// get this treatment in globalInputCapture. False only while addInput is
// actually accepting typed text (dbModeAdd): a mark reason or tag like
// "single" contains letters ('s') that double as transport shortcuts, so
// those must stay literal there. dbModeConfirmDelete doesn't need a case
// here -- HandleKey already swallows every key but y/n itself.
func (s *View) AllowsGlobalKeys() bool {
	return !(s.activeTab == settingsTabDatabase && s.dbMode == dbModeAdd)
}

// focused reports whether any of this view's own focusable widgets
// currently has application focus -- used by globalInputCapture to
// decide whether to route a key through HandleKey instead of the
// default overlay handling (which assumes a single fixed primitive per
// overlay, not true here across two tabs and three Database sub-views).
func (s *View) Focused() bool {
	focus := s.deps.App.GetFocus()
	if focus == s.configView {
		return true
	}
	if !s.databaseInteractive {
		return focus == s.pages
	}
	return focus == s.catalogTable || focus == s.addInput || focus == s.confirmView
}

// Root is the view's top-level primitive, for the host to place in an
// overlay.
func (s *View) Root() tview.Primitive { return s.Flex }

// InitialFocus is the widget the host should focus when opening the
// view -- always the Config tab, which Reset has just switched to.
func (s *View) InitialFocus() tview.Primitive { return s.configView }

// Reset returns the view to the Config tab, for the host to call just
// before showing it. Sets the page and tab bar directly rather than
// going through switchTab, which would also move focus -- doing that
// before the host has captured the pre-overlay focus would clobber it
// with this view's own configView, breaking focus restoration on close.
func (s *View) Reset() {
	s.activeTab = settingsTabConfig
	s.pages.SwitchToPage("config")
	s.renderTabBar()
}

// ReapplyTheme repaints what a theme reload cannot reach on its own:
// the catalog table bakes its selected-row style in when set, rather
// than reading it live on each draw.
func (s *View) ReapplyTheme() {
	if s.catalogTable == nil {
		return
	}
	s.catalogTable.SetSelectedStyle(uitheme.SelectedStyle())
}
