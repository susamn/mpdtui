package settingsview

import (
	"path/filepath"
	"testing"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"mpdtui/internal/metadata"
)

// These cover the view's own behavior -- tab and sub-tab switching, the
// Database tab's add/delete flows, and which keys it claims. They moved
// here with the code from internal/ui, where they had to drive a whole
// App to reach any of it; a Deps is enough.

func newTestDB(t *testing.T) *metadata.DB {
	t.Helper()
	db, err := metadata.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("metadata.Open: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

// newTestView builds a View over db (nil for the metadata-disabled
// case) with a synchronous RunAsync -- nothing drains a tview
// application's update queue without Run() actually running, so the
// real QueueUpdateDraw-based one would never complete in a test.
func newTestView(t *testing.T, db *metadata.DB) *View {
	t.Helper()
	app := tview.NewApplication()
	var errs []error
	v := New(Deps{
		App:    app,
		MetaDB: db,
		Config: []Row{{Label: "MPD Host", Value: "localhost"}},
		ShowError: func(err error) {
			errs = append(errs, err)
			t.Errorf("unexpected ShowError: %v", err)
		},
		ShowMessage: func(string) {},
		RunAsync: func(work func() error, onSuccess func()) {
			if err := work(); err != nil {
				t.Errorf("unexpected async error: %v", err)
				return
			}
			onSuccess()
		},
	})
	v.Reset()
	app.SetFocus(v.InitialFocus())
	return v
}

func tabKeyEvent() *tcell.EventKey { return tcell.NewEventKey(tcell.KeyTab, 0, tcell.ModNone) }

// openDatabaseTab builds an interactive view already switched to the
// Database tab, which is the starting point for every catalog test.
func openDatabaseTab(t *testing.T) *View {
	t.Helper()
	s := newTestView(t, newTestDB(t))
	s.HandleKey(tabKeyEvent())
	if !s.databaseInteractive {
		t.Fatal("setup: databaseInteractive should be true with metaDB active")
	}
	if s.dbMode != dbModeTable {
		t.Fatalf("setup: dbMode after switching to Database tab = %d, want dbModeTable", s.dbMode)
	}
	if s.deps.App.GetFocus() != s.catalogTable {
		t.Fatalf("setup: focus after switching to Database tab = %T, want the catalog table", s.deps.App.GetFocus())
	}
	return s
}

func TestSettingsHandleKeyTabSwitchesTabs(t *testing.T) {
	s := newTestView(t, newTestDB(t))

	if consumed := s.HandleKey(tabKeyEvent()); !consumed {
		t.Fatal("HandleKey(Tab) should report it consumed the event")
	}
	if s.activeTab != settingsTabDatabase {
		t.Errorf("activeTab after Tab = %d, want settingsTabDatabase", s.activeTab)
	}

	// Only two tabs, so Tab again goes back to Config.
	s.HandleKey(tabKeyEvent())
	if s.activeTab != settingsTabConfig {
		t.Errorf("activeTab after a second Tab = %d, want settingsTabConfig", s.activeTab)
	}
}

func TestSettingsDatabaseTabExplainsWhenMetaDBInactive(t *testing.T) {
	s := newTestView(t, nil)

	if s.databaseInteractive {
		t.Fatal("databaseInteractive should be false without metaDB")
	}

	s.HandleKey(tabKeyEvent())

	// 'a'/'d' must no-op (nothing to act on) rather than panic on nil
	// widget pointers (catalogTable/addInput/confirmView are never built
	// when metaDB is nil).
	aKey := tcell.NewEventKey(tcell.KeyRune, 'a', tcell.ModNone)
	if consumed := s.HandleKey(aKey); consumed {
		t.Error("HandleKey('a') on a non-interactive Database tab should not report it consumed the event")
	}
}

// TestFocusedGating covers what the host's global input capture uses to
// decide whether a key belongs to this view at all: Focused reports
// whether any of the view's own widgets currently holds focus, so a key
// pressed while something else entirely is focused never reaches
// HandleKey.
func TestFocusedGating(t *testing.T) {
	s := newTestView(t, newTestDB(t))

	if !s.Focused() {
		t.Error("Focused() should be true with the Config tab focused")
	}

	// Focus something this view does not own, standing in for whatever
	// the host had focused before the overlay opened.
	elsewhere := tview.NewBox()
	s.deps.App.SetFocus(elsewhere)
	if s.Focused() {
		t.Error("Focused() should be false while a widget outside this view holds focus")
	}

	// And back, through the same entry point the host uses to open it.
	s.deps.App.SetFocus(s.InitialFocus())
	if !s.Focused() {
		t.Error("Focused() should be true again once InitialFocus holds focus")
	}
}

func TestSettingsCatalogTableShowsSeededMarkReasons(t *testing.T) {
	s := openDatabaseTab(t)

	if len(s.currentRows) != 1 || s.currentRows[0].name != "mark for deletion" {
		t.Errorf("currentRows = %+v, want the single seeded mark reason", s.currentRows)
	}
	if got := s.catalogTable.GetCell(1, 1).Text; got != "mark for deletion" {
		t.Errorf("catalogTable row 1 = %q, want %q", got, "mark for deletion")
	}
}

func TestSettingsLeftRightSwitchesCatalog(t *testing.T) {
	s := openDatabaseTab(t)

	rightKey := tcell.NewEventKey(tcell.KeyRight, 0, tcell.ModNone)
	if consumed := s.HandleKey(rightKey); !consumed {
		t.Fatal("HandleKey(Right) should report it consumed the event")
	}
	if s.subTab != dbSubTabTags {
		t.Errorf("subTab after Right = %d, want dbSubTabTags", s.subTab)
	}
	if len(s.currentRows) != 3 { // bengali, hindi, english
		t.Errorf("currentRows after switching to Tags = %+v, want the 3 seeded tags", s.currentRows)
	}

	leftKey := tcell.NewEventKey(tcell.KeyLeft, 0, tcell.ModNone)
	s.HandleKey(leftKey)
	if s.subTab != dbSubTabMarkReasons {
		t.Errorf("subTab after Left = %d, want dbSubTabMarkReasons", s.subTab)
	}
}

func TestSettingsAddMarkReason(t *testing.T) {
	s := openDatabaseTab(t)

	aKey := tcell.NewEventKey(tcell.KeyRune, 'a', tcell.ModNone)
	if consumed := s.HandleKey(aKey); !consumed {
		t.Fatal("HandleKey('a') should report it consumed the event")
	}
	if s.dbMode != dbModeAdd {
		t.Fatalf("dbMode after 'a' = %d, want dbModeAdd", s.dbMode)
	}
	if s.deps.App.GetFocus() != s.addInput {
		t.Fatalf("focus after 'a' = %T, want the add-entry input field", s.deps.App.GetFocus())
	}

	s.addInput.SetText("mark for review")
	s.submitAdd()

	if s.dbMode != dbModeTable {
		t.Errorf("dbMode after submitAdd = %d, want dbModeTable (back to browsing)", s.dbMode)
	}
	if got := s.addInput.GetText(); got != "" {
		t.Errorf("addInput text after submit = %q, want cleared", got)
	}
	reasons, err := s.deps.MetaDB.ListMarkReasons()
	if err != nil {
		t.Fatalf("ListMarkReasons: %v", err)
	}
	if len(reasons) != 2 || reasons[1].Reason != "mark for review" {
		t.Errorf("ListMarkReasons() = %+v, want the new reason appended", reasons)
	}
	if len(s.currentRows) != 2 {
		t.Errorf("currentRows after add = %+v, want the table repainted with 2 rows", s.currentRows)
	}
}

func TestSettingsAddIgnoresBlankInput(t *testing.T) {
	s := openDatabaseTab(t)

	s.startAdd()
	s.addInput.SetText("   ")
	s.submitAdd()

	reasons, err := s.deps.MetaDB.ListMarkReasons()
	if err != nil {
		t.Fatalf("ListMarkReasons: %v", err)
	}
	if len(reasons) != 1 {
		t.Errorf("ListMarkReasons() = %+v, want no row added for blank input", reasons)
	}
}

// TestSettingsAddAcceptsLettersThatAreAlsoShortcutsElsewhere proves 'a'/
// 'd' typed into the add-entry field are literal text, not routed back
// into startAdd/startDelete -- handleKey returns false in dbModeAdd, so
// tview's native InputField handling gets every keystroke.
func TestSettingsAddAcceptsLettersThatAreAlsoShortcutsElsewhere(t *testing.T) {
	s := openDatabaseTab(t)

	s.startAdd()
	s.addInput.SetText("add and delete tracks")
	s.submitAdd()

	reasons, err := s.deps.MetaDB.ListMarkReasons()
	if err != nil {
		t.Fatalf("ListMarkReasons: %v", err)
	}
	if len(reasons) != 2 || reasons[1].Reason != "add and delete tracks" {
		t.Errorf("ListMarkReasons() = %+v, want %q added verbatim", reasons, "add and delete tracks")
	}
}

func TestSettingsAddTag(t *testing.T) {
	s := openDatabaseTab(t)
	s.switchSubTab(dbSubTabTags)

	s.startAdd()
	if got, want := s.addInput.GetLabel(), "Add tag: "; got != want {
		t.Errorf("addInput label on the Tags sub-tab = %q, want %q", got, want)
	}
	s.addInput.SetText("french")
	s.submitAdd()

	tags, err := s.deps.MetaDB.ListTags()
	if err != nil {
		t.Fatalf("ListTags: %v", err)
	}
	if len(tags) != 4 || tags[3].Tagname != "french" {
		t.Errorf("ListTags() = %+v, want the new tag appended", tags)
	}
}

func TestSettingsDeleteRequiresConfirmation(t *testing.T) {
	s := openDatabaseTab(t)
	s.catalogTable.Select(1, 0) // the seeded "mark for deletion" row

	dKey := tcell.NewEventKey(tcell.KeyRune, 'd', tcell.ModNone)
	if consumed := s.HandleKey(dKey); !consumed {
		t.Fatal("HandleKey('d') should report it consumed the event")
	}
	if s.dbMode != dbModeConfirmDelete {
		t.Fatalf("dbMode after 'd' = %d, want dbModeConfirmDelete", s.dbMode)
	}
	if s.pendingDeleteName != "mark for deletion" {
		t.Errorf("pendingDeleteName = %q, want %q", s.pendingDeleteName, "mark for deletion")
	}

	// Not yet deleted -- only confirming with 'y' actually removes it.
	reasons, err := s.deps.MetaDB.ListMarkReasons()
	if err != nil {
		t.Fatalf("ListMarkReasons: %v", err)
	}
	if len(reasons) != 1 {
		t.Errorf("ListMarkReasons() = %+v, want the row still present before confirming", reasons)
	}
}

func TestSettingsDeleteConfirmedWithY(t *testing.T) {
	s := openDatabaseTab(t)
	s.catalogTable.Select(1, 0)
	s.startDelete()

	yKey := tcell.NewEventKey(tcell.KeyRune, 'y', tcell.ModNone)
	if consumed := s.HandleKey(yKey); !consumed {
		t.Fatal("HandleKey('y') during a confirm prompt should report it consumed the event")
	}
	if s.dbMode != dbModeTable {
		t.Errorf("dbMode after confirming = %d, want dbModeTable", s.dbMode)
	}

	reasons, err := s.deps.MetaDB.ListMarkReasons()
	if err != nil {
		t.Fatalf("ListMarkReasons: %v", err)
	}
	if len(reasons) != 0 {
		t.Errorf("ListMarkReasons() after confirmed delete = %+v, want empty", reasons)
	}
}

func TestSettingsDeleteCanceledWithN(t *testing.T) {
	s := openDatabaseTab(t)
	s.catalogTable.Select(1, 0)
	s.startDelete()

	nKey := tcell.NewEventKey(tcell.KeyRune, 'n', tcell.ModNone)
	s.HandleKey(nKey)

	if s.dbMode != dbModeTable {
		t.Errorf("dbMode after canceling = %d, want dbModeTable", s.dbMode)
	}
	reasons, err := s.deps.MetaDB.ListMarkReasons()
	if err != nil {
		t.Fatalf("ListMarkReasons: %v", err)
	}
	if len(reasons) != 1 {
		t.Errorf("ListMarkReasons() after canceled delete = %+v, want the row still present", reasons)
	}
}

// TestSettingsDeleteSwallowsUnrelatedKeysWhileConfirming guards against
// a stray keystroke leaking through to the hidden table (or worse,
// re-triggering another action) while a confirm prompt is up.
func TestSettingsDeleteSwallowsUnrelatedKeysWhileConfirming(t *testing.T) {
	s := openDatabaseTab(t)
	s.catalogTable.Select(1, 0)
	s.startDelete()

	xKey := tcell.NewEventKey(tcell.KeyRune, 'x', tcell.ModNone)
	if consumed := s.HandleKey(xKey); !consumed {
		t.Error("HandleKey(unrelated rune) during a confirm prompt should still report it consumed the event")
	}
	if s.dbMode != dbModeConfirmDelete {
		t.Error("dbMode should remain dbModeConfirmDelete after an unrelated key")
	}
}

// TestSettingsSwitchTabResetsDatabaseSubMode guards against a stale
// in-progress add/confirm lingering if the user Tabs away from Database
// and back -- switchTab always resets dbMode to dbModeTable.
func TestSettingsSwitchTabResetsDatabaseSubMode(t *testing.T) {
	s := openDatabaseTab(t)
	s.startAdd()
	if s.dbMode != dbModeAdd {
		t.Fatal("setup: dbMode should be dbModeAdd")
	}

	s.HandleKey(tabKeyEvent()) // -> Config
	s.HandleKey(tabKeyEvent()) // -> Database again

	if s.dbMode != dbModeTable {
		t.Errorf("dbMode after leaving and returning to Database = %d, want dbModeTable", s.dbMode)
	}
	if s.deps.App.GetFocus() != s.catalogTable {
		t.Errorf("focus after returning to Database = %T, want the catalog table", s.deps.App.GetFocus())
	}
}

// TestPopulateConfigTableRendersRows covers the rendering half of the
// Config tab. What each row says is the host's business (internal/ui's
// configRows, tested there); this pins that the rows arrive in the
// table in order, under a header, offset by it.
func TestPopulateConfigTableRendersRows(t *testing.T) {
	table := tview.NewTable()
	rows := []Row{
		{Label: "MPD Host", Value: "localhost"},
		{Label: "MPD Port", Value: "6600"},
		{Label: "Theme File", Value: "(unknown)"},
	}
	populateConfigTable(table, rows)

	if got, want := table.GetRowCount(), len(rows)+1; got != want {
		t.Fatalf("row count = %d, want %d (one header plus %d rows)", got, want, len(rows))
	}
	if got := table.GetCell(0, 0).Text; got != "Setting" {
		t.Errorf("header column 0 = %q, want %q", got, "Setting")
	}
	if got := table.GetCell(0, 1).Text; got != "Value" {
		t.Errorf("header column 1 = %q, want %q", got, "Value")
	}
	for i, r := range rows {
		row := i + 1
		if got := table.GetCell(row, 0).Text; got != r.Label {
			t.Errorf("row %d label = %q, want %q", row, got, r.Label)
		}
		if got := table.GetCell(row, 1).Text; got != r.Value {
			t.Errorf("row %d value = %q, want %q", row, got, r.Value)
		}
	}

	// The header must not be selectable -- there is nothing to act on
	// in this tab, and a selectable header reads as a data row.
	if table.GetCell(0, 0).NotSelectable != true {
		t.Error("header cell is selectable, want it not to be")
	}

	// Rendering again must replace, not append -- the table is reused.
	populateConfigTable(table, rows[:1])
	if got, want := table.GetRowCount(), 2; got != want {
		t.Errorf("row count after re-render = %d, want %d", got, want)
	}
}

// TestHostFacingAccessors covers the handful of methods the host calls
// to place and repaint the view.
func TestHostFacingAccessors(t *testing.T) {
	s := newTestView(t, newTestDB(t))

	if s.Root() == nil {
		t.Error("Root() is nil")
	}
	if s.InitialFocus() == nil {
		t.Error("InitialFocus() is nil")
	}
	// The Config tab browses, so global keys stay live there.
	if !s.AllowsGlobalKeys() {
		t.Error("AllowsGlobalKeys() is false on the Config tab, want true")
	}

	// ReapplyTheme repaints the catalog table's selected-row style,
	// which is baked in when set rather than read live. tview exposes no
	// getter for it, so this checks it runs against a built table
	// without panicking; the colors themselves are uitheme's contract.
	s.ReapplyTheme()
}

// TestReapplyThemeWithoutADatabase covers the nil guard: with track
// metadata off there is no catalog table to repaint.
func TestReapplyThemeWithoutADatabase(t *testing.T) {
	s := newTestView(t, nil)
	s.ReapplyTheme() // must not panic
}

// TestAllowsGlobalKeysWhileTyping is the other half of the gate: a mark
// reason like "single" contains letters that double as transport
// shortcuts, so they must stay literal while the add field has focus.
func TestAllowsGlobalKeysWhileTyping(t *testing.T) {
	s := openDatabaseTab(t)
	if !s.AllowsGlobalKeys() {
		t.Fatal("global keys should be live while browsing the catalog")
	}

	s.startAdd()
	if s.AllowsGlobalKeys() {
		t.Error("global keys are live while typing a new entry, want them literal")
	}
}

// TestConfirmDeleteReportsAFailedDelete covers the error arm of the
// delete flow.
func TestConfirmDeleteReportsAFailedDelete(t *testing.T) {
	db := newTestDB(t)
	app := tview.NewApplication()
	var reported error
	v := New(Deps{
		App:         app,
		MetaDB:      db,
		Config:      []Row{{Label: "k", Value: "v"}},
		ShowError:   func(err error) { reported = err },
		ShowMessage: func(string) {},
		RunAsync: func(work func() error, onSuccess func()) {
			if err := work(); err != nil {
				reported = err
				return
			}
			onSuccess()
		},
	})
	v.Reset()
	app.SetFocus(v.InitialFocus())
	v.HandleKey(tabKeyEvent())

	v.catalogTable.Select(1, 0)
	v.startDelete()
	db.Close() // make the delete fail
	v.confirmDeleteNow()

	if reported == nil {
		t.Error("a failed delete was swallowed")
	}
}

// TestRefreshCatalogTableReportsAFailedRead covers the read arm behind
// the Database tab: the catalog is re-read on every sub-tab switch and
// after every write, so a failure there has to surface.
func TestRefreshCatalogTableReportsAFailedRead(t *testing.T) {
	db := newTestDB(t)
	app := tview.NewApplication()
	var reported error
	v := New(Deps{
		App:         app,
		MetaDB:      db,
		Config:      []Row{{Label: "k", Value: "v"}},
		ShowError:   func(err error) { reported = err },
		ShowMessage: func(string) {},
		RunAsync:    func(work func() error, onSuccess func()) { work(); onSuccess() },
	})
	v.Reset()
	app.SetFocus(v.InitialFocus())
	v.HandleKey(tabKeyEvent()) // to the Database tab

	db.Close()

	v.refreshCatalogTable()
	if reported == nil {
		t.Error("a failed mark-reason read was swallowed")
	}

	reported = nil
	v.switchSubTab(dbSubTabTags)
	if reported == nil {
		t.Error("a failed tag read was swallowed")
	}
}

// TestStartDeleteWithNothingSelected covers the guard: an empty catalog
// (or the header row) has nothing to confirm deleting.
func TestStartDeleteWithNothingSelected(t *testing.T) {
	s := openDatabaseTab(t)

	// Clear the catalog so there is nothing below the header.
	reasons, err := s.deps.MetaDB.ListMarkReasons()
	if err != nil {
		t.Fatalf("ListMarkReasons: %v", err)
	}
	for _, r := range reasons {
		if err := s.deps.MetaDB.DeleteMarkReason(r.ID); err != nil {
			t.Fatalf("DeleteMarkReason: %v", err)
		}
	}
	s.refreshCatalogTable()

	s.startDelete()

	if s.dbMode == dbModeConfirmDelete {
		t.Error("a confirm prompt opened with nothing selected")
	}
}

// TestFocusedWithoutADatabase covers the non-interactive Database tab:
// it is a plain explanation, so the pages primitive itself holds focus.
func TestFocusedWithoutADatabase(t *testing.T) {
	s := newTestView(t, nil)
	s.HandleKey(tabKeyEvent()) // to the Database tab

	if !s.Focused() {
		t.Error("Focused() is false on a non-interactive Database tab that holds focus")
	}

	s.deps.App.SetFocus(tview.NewBox())
	if s.Focused() {
		t.Error("Focused() is true while something else entirely holds focus")
	}
}
