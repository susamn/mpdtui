package ui

import (
	"strings"
	"testing"

	"github.com/gdamore/tcell/v2"
)

func TestFormatConfigSummaryShowsPlaceholdersForEmptyValues(t *testing.T) {
	got := formatConfigSummary(ConfigSummary{MPDHost: "localhost", MPDPort: "6600"})
	if !strings.Contains(got, "(not configured -- lyrics feature inactive)") {
		t.Errorf("formatConfigSummary(empty MusicDir) = %q, want it to explain the lyrics feature is inactive", got)
	}
	if !strings.Contains(got, "Password: not set") {
		t.Errorf("formatConfigSummary(no password) = %q, want %q", got, "Password: not set")
	}
	if !strings.Contains(got, "Enabled:       no") {
		t.Errorf("formatConfigSummary(TrackMetadataEnabled=false) = %q, want it to say no", got)
	}
}

func TestFormatConfigSummaryNeverShowsThePasswordItself(t *testing.T) {
	// MPDPasswordSet only ever carries a bool -- there's no field a
	// caller could even pass the real password through by mistake, but
	// this guards the actual rendered output too.
	got := formatConfigSummary(ConfigSummary{MPDPasswordSet: true})
	if !strings.Contains(got, "Password: set") {
		t.Errorf("formatConfigSummary(password set) = %q, want %q", got, "Password: set")
	}
}

func TestFormatConfigSummaryShowsResolvedValues(t *testing.T) {
	got := formatConfigSummary(ConfigSummary{
		MPDHost: "192.168.1.5", MPDPort: "6601", MusicDir: "/music",
		TrackMetadataEnabled: true, ConfigFilePath: "/cfg", DBFilePath: "/db",
	})
	for _, want := range []string{"192.168.1.5", "6601", "/music", "Enabled:       yes", "/cfg", "/db"} {
		if !strings.Contains(got, want) {
			t.Errorf("formatConfigSummary(...) = %q, missing %q", got, want)
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

func TestSettingsHandleKeyTabSwitchesTabs(t *testing.T) {
	a := newTestApp()
	a.openSettings()

	tabKey := tcell.NewEventKey(tcell.KeyTab, 0, tcell.ModNone)
	if consumed := a.settings.handleKey(tabKey); !consumed {
		t.Fatal("handleKey(Tab) should report it consumed the event")
	}
	if a.settings.activeTab != settingsTabDatabase {
		t.Errorf("activeTab after Tab = %d, want settingsTabDatabase", a.settings.activeTab)
	}

	// Only two tabs, so Tab again goes back to Config.
	a.settings.handleKey(tabKey)
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

	tabKey := tcell.NewEventKey(tcell.KeyTab, 0, tcell.ModNone)
	a.settings.handleKey(tabKey)

	// Down/Up must no-op (nothing to focus) rather than panic on nil
	// input field pointers.
	downKey := tcell.NewEventKey(tcell.KeyDown, 0, tcell.ModNone)
	if consumed := a.settings.handleKey(downKey); consumed {
		t.Error("handleKey(Down) on a non-interactive Database tab should not report it consumed the event")
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
	a.settings.handleKey(tcell.NewEventKey(tcell.KeyTab, 0, tcell.ModNone))
	if !a.settings.databaseInteractive {
		t.Fatal("setup: databaseInteractive should be true with metaDB active")
	}
	if a.tv.GetFocus() != a.settings.newReasonInput {
		t.Fatalf("setup: focus after switching to Database tab = %T, want the new-reason input", a.tv.GetFocus())
	}
	return a
}

func TestSettingsSubmitNewMarkReasonAddsAndClearsField(t *testing.T) {
	a := openTestAppSettingsDatabaseTab(t)

	a.settings.newReasonInput.SetText("mark for review")
	a.settings.submitNewMarkReason()

	if got := a.settings.newReasonInput.GetText(); got != "" {
		t.Errorf("newReasonInput text after submit = %q, want cleared", got)
	}
	reasons, err := a.metaDB.ListMarkReasons()
	if err != nil {
		t.Fatalf("ListMarkReasons: %v", err)
	}
	if len(reasons) != 2 || reasons[1].Reason != "mark for review" {
		t.Errorf("ListMarkReasons() = %+v, want the new reason appended", reasons)
	}
	if got := a.settings.markReasonsList.GetText(true); !strings.Contains(got, "mark for review") {
		t.Errorf("markReasonsList text = %q, want it to include the new reason", got)
	}
}

func TestSettingsSubmitNewMarkReasonIgnoresBlankInput(t *testing.T) {
	a := openTestAppSettingsDatabaseTab(t)

	a.settings.newReasonInput.SetText("   ")
	a.settings.submitNewMarkReason()

	reasons, err := a.metaDB.ListMarkReasons()
	if err != nil {
		t.Fatalf("ListMarkReasons: %v", err)
	}
	if len(reasons) != 1 {
		t.Errorf("ListMarkReasons() = %+v, want no row added for blank input", reasons)
	}
}

func TestSettingsSubmitNewTagAddsAndClearsField(t *testing.T) {
	a := openTestAppSettingsDatabaseTab(t)

	a.settings.newTagInput.SetText("french")
	a.settings.submitNewTag()

	if got := a.settings.newTagInput.GetText(); got != "" {
		t.Errorf("newTagInput text after submit = %q, want cleared", got)
	}
	tags, err := a.metaDB.ListTags()
	if err != nil {
		t.Fatalf("ListTags: %v", err)
	}
	if len(tags) != 4 || tags[3].Tagname != "french" {
		t.Errorf("ListTags() = %+v, want the new tag appended", tags)
	}
}

func TestSettingsHandleKeyDownUpMovesBetweenDatabaseFields(t *testing.T) {
	a := openTestAppSettingsDatabaseTab(t) // focus starts on newReasonInput

	a.settings.handleKey(tcell.NewEventKey(tcell.KeyDown, 0, tcell.ModNone))
	if a.tv.GetFocus() != a.settings.newTagInput {
		t.Errorf("focus after Down = %T, want the new-tag input", a.tv.GetFocus())
	}

	a.settings.handleKey(tcell.NewEventKey(tcell.KeyUp, 0, tcell.ModNone))
	if a.tv.GetFocus() != a.settings.newReasonInput {
		t.Errorf("focus after Up = %T, want the new-reason input", a.tv.GetFocus())
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

// TestSettingsAddMarkReasonDoesNotAffectQueueRatingKeys is a light
// sanity check that typing digits into the new-reason field (which
// share runes with the Queue's own '1'-'5' rating shortcut) is treated
// as literal text, not routed into handleRateSelectedTrack -- since
// settings.handleKey only claims Tab/Backtab/Down/Up, everything else
// (including digit runes) must fall through to the focused InputField.
func TestSettingsAddMarkReasonAcceptsDigitsAsLiteralText(t *testing.T) {
	a := openTestAppSettingsDatabaseTab(t)

	a.settings.newReasonInput.SetText("skip 2 tracks")
	a.settings.submitNewMarkReason()

	reasons, err := a.metaDB.ListMarkReasons()
	if err != nil {
		t.Fatalf("ListMarkReasons: %v", err)
	}
	if len(reasons) != 2 || reasons[1].Reason != "skip 2 tracks" {
		t.Errorf("ListMarkReasons() = %+v, want %q added verbatim", reasons, "skip 2 tracks")
	}
}
