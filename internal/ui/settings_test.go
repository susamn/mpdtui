package ui

import (
	"strings"
	"testing"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

// configTableText concatenates every cell's text in a populateConfigTable
// table (including the header row), space-joined per row and newline-
// joined across rows, so tests can assert on it with simple substring
// checks the same way the old flat-string formatConfigSummary let them.
func configTableText(table *tview.Table) string {
	var b strings.Builder
	for row := 0; row < table.GetRowCount(); row++ {
		for col := 0; col < table.GetColumnCount(); col++ {
			cell := table.GetCell(row, col)
			if cell == nil {
				continue
			}
			b.WriteString(cell.Text)
			b.WriteByte(' ')
		}
		b.WriteByte('\n')
	}
	return b.String()
}

func TestPopulateConfigTableShowsPlaceholdersForEmptyValues(t *testing.T) {
	table := tview.NewTable()
	populateConfigTable(table, ConfigSummary{MPDHost: "localhost", MPDPort: "6600"})
	got := configTableText(table)

	if !strings.Contains(got, "(not configured -- lyrics feature inactive)") {
		t.Errorf("populateConfigTable(empty MusicDir) = %q, want it to explain the lyrics feature is inactive", got)
	}
	if !strings.Contains(got, "MPD Password not set") {
		t.Errorf("populateConfigTable(no password) = %q, want %q", got, "MPD Password not set")
	}
	if !strings.Contains(got, "Track Metadata no") {
		t.Errorf("populateConfigTable(TrackMetadataEnabled=false) = %q, want it to say no", got)
	}
}

func TestPopulateConfigTableNeverShowsThePasswordItself(t *testing.T) {
	// MPDPasswordSet only ever carries a bool -- there's no field a
	// caller could even pass the real password through by mistake, but
	// this guards the actual rendered output too.
	table := tview.NewTable()
	populateConfigTable(table, ConfigSummary{MPDPasswordSet: true})
	got := configTableText(table)
	if !strings.Contains(got, "MPD Password set") {
		t.Errorf("populateConfigTable(password set) = %q, want %q", got, "MPD Password set")
	}
}

func TestPopulateConfigTableShowsResolvedValues(t *testing.T) {
	table := tview.NewTable()
	populateConfigTable(table, ConfigSummary{
		MPDHost: "192.168.1.5", MPDPort: "6601", MusicDir: "/music",
		TrackMetadataEnabled: true, ConfigFilePath: "/cfg", DBFilePath: "/db",
	})
	got := configTableText(table)
	for _, want := range []string{"192.168.1.5", "6601", "/music", "Track Metadata yes", "/cfg", "/db"} {
		if !strings.Contains(got, want) {
			t.Errorf("populateConfigTable(...) = %q, missing %q", got, want)
		}
	}
}

func TestOpenSettingsStartsOnConfigTabAndFocusesIt(t *testing.T) {
	a := newTestApp()
	a.openSettings()

	if a.mode != modeOverlay {
		t.Fatal("mode after openSettings should be modeOverlay")
	}
	if a.tv.GetFocus() != a.settings.configView {
		t.Errorf("focus after openSettings = %T, want the Config tab's text view", a.tv.GetFocus())
	}
	if a.settings.activeTab != settingsTabConfig {
		t.Errorf("activeTab after openSettings = %d, want settingsTabConfig", a.settings.activeTab)
	}
}

func tabKeyEvent() *tcell.EventKey { return tcell.NewEventKey(tcell.KeyTab, 0, tcell.ModNone) }

func TestSettingsHandleKeyTabSwitchesTabs(t *testing.T) {
	a := newTestApp()
	a.openSettings()

	if consumed := a.settings.handleKey(tabKeyEvent()); !consumed {
		t.Fatal("handleKey(Tab) should report it consumed the event")
	}
	if a.settings.activeTab != settingsTabDatabase {
		t.Errorf("activeTab after Tab = %d, want settingsTabDatabase", a.settings.activeTab)
	}

	// Only two tabs, so Tab again goes back to Config.
	a.settings.handleKey(tabKeyEvent())
	if a.settings.activeTab != settingsTabConfig {
		t.Errorf("activeTab after a second Tab = %d, want settingsTabConfig", a.settings.activeTab)
	}
}

func TestSettingsDatabaseTabExplainsWhenMetaDBInactive(t *testing.T) {
	a := newTestApp() // nil metaDB
	a.openSettings()

	if a.settings.databaseInteractive {
		t.Fatal("databaseInteractive should be false without metaDB")
	}

	a.settings.handleKey(tabKeyEvent())

	// 'a'/'d' must no-op (nothing to act on) rather than panic on nil
	// widget pointers (catalogTable/addInput/confirmView are never built
	// when metaDB is nil).
	aKey := tcell.NewEventKey(tcell.KeyRune, 'a', tcell.ModNone)
	if consumed := a.settings.handleKey(aKey); consumed {
		t.Error("handleKey('a') on a non-interactive Database tab should not report it consumed the event")
	}
}

func TestSettingsFocusedGating(t *testing.T) {
	a := newTestApp()
	if a.settings.focused() {
		t.Error("settings.focused() should be false before the overlay is even open")
	}

	a.openSettings()
	if !a.settings.focused() {
		t.Error("settings.focused() should be true right after openSettings (Config tab's text view has focus)")
	}
}

func openTestAppSettingsDatabaseTab(t *testing.T) *App {
	t.Helper()
	a := newTestAppWithMetaDB(t)
	a.openSettings()
	a.settings.handleKey(tabKeyEvent())
	if !a.settings.databaseInteractive {
		t.Fatal("setup: databaseInteractive should be true with metaDB active")
	}
	if a.settings.dbMode != dbModeTable {
		t.Fatalf("setup: dbMode after switching to Database tab = %d, want dbModeTable", a.settings.dbMode)
	}
	if a.tv.GetFocus() != a.settings.catalogTable {
		t.Fatalf("setup: focus after switching to Database tab = %T, want the catalog table", a.tv.GetFocus())
	}
	return a
}

func TestSettingsCatalogTableShowsSeededMarkReasons(t *testing.T) {
	a := openTestAppSettingsDatabaseTab(t)

	if len(a.settings.currentRows) != 1 || a.settings.currentRows[0].name != "mark for deletion" {
		t.Errorf("currentRows = %+v, want the single seeded mark reason", a.settings.currentRows)
	}
	if got := a.settings.catalogTable.GetCell(1, 1).Text; got != "mark for deletion" {
		t.Errorf("catalogTable row 1 = %q, want %q", got, "mark for deletion")
	}
}

func TestSettingsLeftRightSwitchesCatalog(t *testing.T) {
	a := openTestAppSettingsDatabaseTab(t)

	rightKey := tcell.NewEventKey(tcell.KeyRight, 0, tcell.ModNone)
	if consumed := a.settings.handleKey(rightKey); !consumed {
		t.Fatal("handleKey(Right) should report it consumed the event")
	}
	if a.settings.subTab != dbSubTabTags {
		t.Errorf("subTab after Right = %d, want dbSubTabTags", a.settings.subTab)
	}
	if len(a.settings.currentRows) != 3 { // bengali, hindi, english
		t.Errorf("currentRows after switching to Tags = %+v, want the 3 seeded tags", a.settings.currentRows)
	}

	leftKey := tcell.NewEventKey(tcell.KeyLeft, 0, tcell.ModNone)
	a.settings.handleKey(leftKey)
	if a.settings.subTab != dbSubTabMarkReasons {
		t.Errorf("subTab after Left = %d, want dbSubTabMarkReasons", a.settings.subTab)
	}
}

func TestSettingsAddMarkReason(t *testing.T) {
	a := openTestAppSettingsDatabaseTab(t)

	aKey := tcell.NewEventKey(tcell.KeyRune, 'a', tcell.ModNone)
	if consumed := a.settings.handleKey(aKey); !consumed {
		t.Fatal("handleKey('a') should report it consumed the event")
	}
	if a.settings.dbMode != dbModeAdd {
		t.Fatalf("dbMode after 'a' = %d, want dbModeAdd", a.settings.dbMode)
	}
	if a.tv.GetFocus() != a.settings.addInput {
		t.Fatalf("focus after 'a' = %T, want the add-entry input field", a.tv.GetFocus())
	}

	a.settings.addInput.SetText("mark for review")
	a.settings.submitAdd()

	if a.settings.dbMode != dbModeTable {
		t.Errorf("dbMode after submitAdd = %d, want dbModeTable (back to browsing)", a.settings.dbMode)
	}
	if got := a.settings.addInput.GetText(); got != "" {
		t.Errorf("addInput text after submit = %q, want cleared", got)
	}
	reasons, err := a.metaDB.ListMarkReasons()
	if err != nil {
		t.Fatalf("ListMarkReasons: %v", err)
	}
	if len(reasons) != 2 || reasons[1].Reason != "mark for review" {
		t.Errorf("ListMarkReasons() = %+v, want the new reason appended", reasons)
	}
	if len(a.settings.currentRows) != 2 {
		t.Errorf("currentRows after add = %+v, want the table repainted with 2 rows", a.settings.currentRows)
	}
}

func TestSettingsAddIgnoresBlankInput(t *testing.T) {
	a := openTestAppSettingsDatabaseTab(t)

	a.settings.startAdd()
	a.settings.addInput.SetText("   ")
	a.settings.submitAdd()

	reasons, err := a.metaDB.ListMarkReasons()
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
	a := openTestAppSettingsDatabaseTab(t)

	a.settings.startAdd()
	a.settings.addInput.SetText("add and delete tracks")
	a.settings.submitAdd()

	reasons, err := a.metaDB.ListMarkReasons()
	if err != nil {
		t.Fatalf("ListMarkReasons: %v", err)
	}
	if len(reasons) != 2 || reasons[1].Reason != "add and delete tracks" {
		t.Errorf("ListMarkReasons() = %+v, want %q added verbatim", reasons, "add and delete tracks")
	}
}

func TestSettingsAddTag(t *testing.T) {
	a := openTestAppSettingsDatabaseTab(t)
	a.settings.switchSubTab(dbSubTabTags)

	a.settings.startAdd()
	if got, want := a.settings.addInput.GetLabel(), "Add tag: "; got != want {
		t.Errorf("addInput label on the Tags sub-tab = %q, want %q", got, want)
	}
	a.settings.addInput.SetText("french")
	a.settings.submitAdd()

	tags, err := a.metaDB.ListTags()
	if err != nil {
		t.Fatalf("ListTags: %v", err)
	}
	if len(tags) != 4 || tags[3].Tagname != "french" {
		t.Errorf("ListTags() = %+v, want the new tag appended", tags)
	}
}

func TestSettingsDeleteRequiresConfirmation(t *testing.T) {
	a := openTestAppSettingsDatabaseTab(t)
	a.settings.catalogTable.Select(1, 0) // the seeded "mark for deletion" row

	dKey := tcell.NewEventKey(tcell.KeyRune, 'd', tcell.ModNone)
	if consumed := a.settings.handleKey(dKey); !consumed {
		t.Fatal("handleKey('d') should report it consumed the event")
	}
	if a.settings.dbMode != dbModeConfirmDelete {
		t.Fatalf("dbMode after 'd' = %d, want dbModeConfirmDelete", a.settings.dbMode)
	}
	if a.settings.pendingDeleteName != "mark for deletion" {
		t.Errorf("pendingDeleteName = %q, want %q", a.settings.pendingDeleteName, "mark for deletion")
	}

	// Not yet deleted -- only confirming with 'y' actually removes it.
	reasons, err := a.metaDB.ListMarkReasons()
	if err != nil {
		t.Fatalf("ListMarkReasons: %v", err)
	}
	if len(reasons) != 1 {
		t.Errorf("ListMarkReasons() = %+v, want the row still present before confirming", reasons)
	}
}

func TestSettingsDeleteConfirmedWithY(t *testing.T) {
	a := openTestAppSettingsDatabaseTab(t)
	a.settings.catalogTable.Select(1, 0)
	a.settings.startDelete()

	yKey := tcell.NewEventKey(tcell.KeyRune, 'y', tcell.ModNone)
	if consumed := a.settings.handleKey(yKey); !consumed {
		t.Fatal("handleKey('y') during a confirm prompt should report it consumed the event")
	}
	if a.settings.dbMode != dbModeTable {
		t.Errorf("dbMode after confirming = %d, want dbModeTable", a.settings.dbMode)
	}

	reasons, err := a.metaDB.ListMarkReasons()
	if err != nil {
		t.Fatalf("ListMarkReasons: %v", err)
	}
	if len(reasons) != 0 {
		t.Errorf("ListMarkReasons() after confirmed delete = %+v, want empty", reasons)
	}
}

func TestSettingsDeleteCanceledWithN(t *testing.T) {
	a := openTestAppSettingsDatabaseTab(t)
	a.settings.catalogTable.Select(1, 0)
	a.settings.startDelete()

	nKey := tcell.NewEventKey(tcell.KeyRune, 'n', tcell.ModNone)
	a.settings.handleKey(nKey)

	if a.settings.dbMode != dbModeTable {
		t.Errorf("dbMode after canceling = %d, want dbModeTable", a.settings.dbMode)
	}
	reasons, err := a.metaDB.ListMarkReasons()
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
	a := openTestAppSettingsDatabaseTab(t)
	a.settings.catalogTable.Select(1, 0)
	a.settings.startDelete()

	xKey := tcell.NewEventKey(tcell.KeyRune, 'x', tcell.ModNone)
	if consumed := a.settings.handleKey(xKey); !consumed {
		t.Error("handleKey(unrelated rune) during a confirm prompt should still report it consumed the event")
	}
	if a.settings.dbMode != dbModeConfirmDelete {
		t.Error("dbMode should remain dbModeConfirmDelete after an unrelated key")
	}
}

func TestEKeyOpensSettingsGlobally(t *testing.T) {
	a := newTestApp()
	a.tv.SetFocus(a.library.tree)

	eKey := tcell.NewEventKey(tcell.KeyRune, 'e', tcell.ModNone)
	if result := a.globalInputCapture(eKey); result != nil {
		t.Errorf("'e' should be consumed (opens Settings), got %v", result)
	}
	if a.mode != modeOverlay {
		t.Error("mode after 'e' should be modeOverlay")
	}
}

func TestEscWhileSettingsOpenRestoresOriginalFocus(t *testing.T) {
	a := newTestApp()
	a.tv.SetFocus(a.queue.table)
	a.openSettings()

	esc := tcell.NewEventKey(tcell.KeyEscape, 0, tcell.ModNone)
	if result := a.globalInputCapture(esc); result != nil {
		t.Errorf("Escape while Settings is open should be consumed, got %v", result)
	}
	if a.mode != modeNormal {
		t.Error("mode after Escape should be modeNormal")
	}
	if a.tv.GetFocus() != a.queue.table {
		t.Errorf("focus after Escape = %T, want the originally-focused Queue table", a.tv.GetFocus())
	}
}

// TestSettingsSwitchTabResetsDatabaseSubMode guards against a stale
// in-progress add/confirm lingering if the user Tabs away from Database
// and back -- switchTab always resets dbMode to dbModeTable.
func TestSettingsSwitchTabResetsDatabaseSubMode(t *testing.T) {
	a := openTestAppSettingsDatabaseTab(t)
	a.settings.startAdd()
	if a.settings.dbMode != dbModeAdd {
		t.Fatal("setup: dbMode should be dbModeAdd")
	}

	a.settings.handleKey(tabKeyEvent()) // -> Config
	a.settings.handleKey(tabKeyEvent()) // -> Database again

	if a.settings.dbMode != dbModeTable {
		t.Errorf("dbMode after leaving and returning to Database = %d, want dbModeTable", a.settings.dbMode)
	}
	if a.tv.GetFocus() != a.settings.catalogTable {
		t.Errorf("focus after returning to Database = %T, want the catalog table", a.tv.GetFocus())
	}
}

// TestQKeyWhileSettingsConfigTabOpenIsConsumed proves 'q' still quits
// while Settings is open and focused on the (read-only) Config table --
// the bug report this guards against was that being inside Settings at
// all blocked every global key, even on widgets with nothing to type.
func TestQKeyWhileSettingsConfigTabOpenIsConsumed(t *testing.T) {
	a := newTestApp()
	a.openSettings()

	qKey := tcell.NewEventKey(tcell.KeyRune, 'q', tcell.ModNone)
	if result := a.globalInputCapture(qKey); result != nil {
		t.Errorf("'q' while Settings' Config tab is open should be consumed (quit), got %v", result)
	}
}

// TestQKeyWhileSettingsCatalogTableOpenIsConsumed is the Database tab's
// counterpart -- the catalog table is just as read-only/browsable as the
// Config table, so 'q' should quit there too.
func TestQKeyWhileSettingsCatalogTableOpenIsConsumed(t *testing.T) {
	a := openTestAppSettingsDatabaseTab(t)

	qKey := tcell.NewEventKey(tcell.KeyRune, 'q', tcell.ModNone)
	if result := a.globalInputCapture(qKey); result != nil {
		t.Errorf("'q' while the Database catalog table is focused should be consumed (quit), got %v", result)
	}
}

// TestQKeyWhileAddingSettingsEntryIsNotConsumed proves the fix stays
// scoped: while addInput actually has focus and is accepting typed text
// (dbModeAdd), 'q' must stay literal -- a mark reason or tag could
// legitimately contain the letter 'q'.
func TestQKeyWhileAddingSettingsEntryIsNotConsumed(t *testing.T) {
	a := openTestAppSettingsDatabaseTab(t)
	a.settings.startAdd()

	qKey := tcell.NewEventKey(tcell.KeyRune, 'q', tcell.ModNone)
	if result := a.globalInputCapture(qKey); result == nil {
		t.Error("'q' while typing a new Settings entry should not quit -- it must reach addInput as literal text")
	}
	if a.tv.GetFocus() != a.settings.addInput {
		t.Error("focus should still be addInput after 'q' while adding")
	}
}

// TestTransportKeysNotConsumedWhileAddingSettingsEntry is the transport-
// key counterpart to TestQKeyWhileAddingSettingsEntryIsNotConsumed: a
// mark reason or tag like "single" or "stereo" contains 's', which
// doubles as the stop shortcut, so it must stay literal while addInput
// has focus. Offline-safe -- no a.client call happens, since
// allowsGlobalKeys() is false here, so handleTransportKey is never
// reached.
func TestTransportKeysNotConsumedWhileAddingSettingsEntry(t *testing.T) {
	a := openTestAppSettingsDatabaseTab(t)
	a.settings.startAdd()

	space := tcell.NewEventKey(tcell.KeyRune, ' ', tcell.ModNone)
	if result := a.globalInputCapture(space); result == nil {
		t.Error("Space while typing a new Settings entry should not be consumed by the transport-key passthrough")
	}
	sKey := tcell.NewEventKey(tcell.KeyRune, 's', tcell.ModNone)
	if result := a.globalInputCapture(sKey); result == nil {
		t.Error("'s' while typing a new Settings entry should not be consumed by the transport-key passthrough")
	}
}

// TestTransportKeysStayLiveWhileSettingsOpenNeedsLiveMPD needs a real
// client, since handleTransportKey's whole point is calling one -- see
// TestTransportKeysStayLiveWhileLyricsViewerOpenNeedsLiveMPD's own doc
// comment for why a small, reversible live side effect is an accepted
// cost here.
func TestTransportKeysStayLiveWhileSettingsOpenNeedsLiveMPD(t *testing.T) {
	c := dialOrSkip(t)
	a := &App{tv: tview.NewApplication(), client: c}
	a.build()
	a.openSettings()

	space := tcell.NewEventKey(tcell.KeyRune, ' ', tcell.ModNone)
	if result := a.globalInputCapture(space); result != nil {
		t.Errorf("Space while Settings' Config tab is open should be consumed (routed to togglePlayPause), got %v", result)
	}
	a.globalInputCapture(space) // toggle back, restoring whatever state playback was already in
}
