package ui

import (
	"testing"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"mpdtui/internal/settingsview"
)

// What stays here is the App's wiring to the Settings overlay -- that
// 'e' opens it, that Escape restores the previous focus, and which
// global keys stay live while it is up. The view's own behavior (tabs,
// the catalog add/delete flows) is tested in internal/settingsview,
// against a Deps rather than a whole App.

func tabKeyEvent() *tcell.EventKey { return tcell.NewEventKey(tcell.KeyTab, 0, tcell.ModNone) }

// openTestAppSettingsDatabaseTab opens Settings and switches to the
// Database tab, which is where the interesting key-gating cases live
// (the catalog table browses, the add field types).
func openTestAppSettingsDatabaseTab(t *testing.T) *App {
	t.Helper()
	a := newTestAppWithMetaDB(t)
	a.openSettings()
	a.settings.HandleKey(tabKeyEvent())
	if !a.settings.Focused() {
		t.Fatal("setup: the settings view should hold focus on the Database tab")
	}
	if !a.settings.AllowsGlobalKeys() {
		t.Fatal("setup: the catalog table browses, so global keys should stay live")
	}
	return a
}

// startAddingEntry drives the view into its add-entry sub-view through
// the same 'a' keypress a user would press, rather than reaching for an
// unexported method across the package boundary.
func startAddingEntry(t *testing.T, a *App) {
	t.Helper()
	aKey := tcell.NewEventKey(tcell.KeyRune, 'a', tcell.ModNone)
	if consumed := a.settings.HandleKey(aKey); !consumed {
		t.Fatal("setup: 'a' on the catalog table should open the add-entry field")
	}
	if a.settings.AllowsGlobalKeys() {
		t.Fatal("setup: global keys must be off while the add-entry field is accepting text")
	}
}

func TestOpenSettingsStartsOnConfigTabAndFocusesIt(t *testing.T) {
	a := newTestApp()
	a.openSettings()

	if a.mode != modeOverlay {
		t.Fatal("mode after openSettings should be modeOverlay")
	}
	if a.tv.GetFocus() != a.settings.InitialFocus() {
		t.Errorf("focus after openSettings = %T, want the Config tab's table", a.tv.GetFocus())
	}
	if !a.settings.Focused() {
		t.Error("the settings view should report itself focused after openSettings")
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
	startAddingEntry(t, a)

	qKey := tcell.NewEventKey(tcell.KeyRune, 'q', tcell.ModNone)
	if result := a.globalInputCapture(qKey); result == nil {
		t.Error("'q' while typing a new Settings entry should not quit -- it must reach addInput as literal text")
	}
	if a.settings.AllowsGlobalKeys() {
		t.Error("the view should still be in its typing sub-view after 'q' while adding")
	}
	if !a.settings.Focused() {
		t.Error("focus should still be inside the settings view after 'q' while adding")
	}
}

// TestTransportKeysNotConsumedWhileAddingSettingsEntry is the transport-
// key counterpart to TestQKeyWhileAddingSettingsEntryIsNotConsumed: a
// mark reason or tag like "single" or "stereo" contains 's', which
// doubles as the stop shortcut, so it must stay literal while addInput
// has focus. Offline-safe -- no a.client call happens, since
// AllowsGlobalKeys() is false here, so handleTransportKey is never
// reached.
func TestTransportKeysNotConsumedWhileAddingSettingsEntry(t *testing.T) {
	a := openTestAppSettingsDatabaseTab(t)
	startAddingEntry(t, a)

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

// configRows is the seam between ConfigSummary and the view: these pin
// what the Config tab ends up showing without needing the view at all.

func rowValue(rows []settingsview.Row, label string) (string, bool) {
	for _, r := range rows {
		if r.Label == label {
			return r.Value, true
		}
	}
	return "", false
}

func TestConfigRowsShowPlaceholdersForEmptyValues(t *testing.T) {
	rows := configRows(ConfigSummary{})
	for _, label := range []string{"Config File", "Database File", "Lyrics Index File", "Theme File"} {
		got, ok := rowValue(rows, label)
		if !ok {
			t.Errorf("no %q row", label)
			continue
		}
		if got != "(unknown)" {
			t.Errorf("%q with nothing configured = %q, want the placeholder", label, got)
		}
	}
	if got, _ := rowValue(rows, "Music Directory"); got != "(not configured -- lyrics feature inactive)" {
		t.Errorf("Music Directory with nothing configured = %q, want it to say the feature is inactive", got)
	}
}

// TestConfigRowsNeverShowThePasswordItself guards the one row that must
// never render a credential: ConfigSummary carries only whether a
// password is set, and this proves the value stays that way.
func TestConfigRowsNeverShowThePasswordItself(t *testing.T) {
	rows := configRows(ConfigSummary{MPDPasswordSet: true})
	got, ok := rowValue(rows, "MPD Password")
	if !ok {
		t.Fatal("no MPD Password row")
	}
	if got != "set" {
		t.Errorf("MPD Password = %q, want exactly %q", got, "set")
	}

	if got, _ := rowValue(configRows(ConfigSummary{}), "MPD Password"); got != "not set" {
		t.Errorf("MPD Password with none configured = %q, want %q", got, "not set")
	}
}

func TestConfigRowsShowResolvedValues(t *testing.T) {
	rows := configRows(ConfigSummary{
		MPDHost:              "localhost",
		MPDPort:              "6600",
		MusicDir:             "/home/u/Music",
		TrackMetadataEnabled: true,
		ConfigFilePath:       "/home/u/.config/mpdtui/config",
	})
	for _, tc := range []struct{ label, want string }{
		{"MPD Host", "localhost"},
		{"MPD Port", "6600"},
		{"Music Directory", "/home/u/Music"},
		{"Track Metadata", "yes"},
		{"Config File", "/home/u/.config/mpdtui/config"},
	} {
		if got, _ := rowValue(rows, tc.label); got != tc.want {
			t.Errorf("%q = %q, want %q", tc.label, got, tc.want)
		}
	}

	if got, _ := rowValue(configRows(ConfigSummary{}), "Track Metadata"); got != "no" {
		t.Errorf("Track Metadata when disabled = %q, want %q", got, "no")
	}
}
