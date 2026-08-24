package tests

import (
	"os"
	"path/filepath"
	"testing"

	"mpdtui/internal/theme"
)

func TestLoadFromReadsFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "colors.toml")
	toml := `mode = "dark"

accent = "#7d82d9"
selection = "#252e56"
muted = "#6d7db6"

background = "#060B1E"
foreground = "#ffcead"

red = "#ED5B5A"
green = "#92a593"
`
	if err := os.WriteFile(path, []byte(toml), 0o644); err != nil {
		t.Fatal(err)
	}

	got, live := theme.LoadFrom(path)
	if !live {
		t.Error("LoadFrom() live = false, want true (file exists and is readable)")
	}
	if got.Mode != "dark" {
		t.Errorf("Mode = %q, want dark", got.Mode)
	}
	if got.Accent != "#7d82d9" {
		t.Errorf("Accent = %q, want #7d82d9", got.Accent)
	}
	if got.Background != "#060B1E" {
		t.Errorf("Background = %q, want #060B1E", got.Background)
	}
	if got.Red != "#ED5B5A" {
		t.Errorf("Red = %q, want #ED5B5A", got.Red)
	}
	// Fields the sample file above omits (e.g. Orange) should keep
	// Default()'s value rather than come back empty.
	if got.Orange != theme.Default().Orange {
		t.Errorf("Orange = %q, want Default() value %q", got.Orange, theme.Default().Orange)
	}
}

func TestLoadFromReadsArbitraryPath(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "matugen-colors.toml")
	toml := "accent = \"#7d82d9\"\ngreen = \"#92a593\"\n"
	if err := os.WriteFile(path, []byte(toml), 0o644); err != nil {
		t.Fatal(err)
	}

	got, live := theme.LoadFrom(path)
	if !live {
		t.Error("LoadFrom() live = false, want true (file exists and is readable)")
	}
	if got.Accent != "#7d82d9" {
		t.Errorf("Accent = %q, want #7d82d9", got.Accent)
	}
	if got.Green != "#92a593" {
		t.Errorf("Green = %q, want #92a593", got.Green)
	}
	// A field the sample above omits keeps Default()'s value.
	if got.Blue != theme.Default().Blue {
		t.Errorf("Blue = %q, want Default() value %q", got.Blue, theme.Default().Blue)
	}
}

func TestLoadFromEmptyOrMissingPathFallsBackToDefault(t *testing.T) {
	got, live := theme.LoadFrom("")
	if live {
		t.Error("LoadFrom(\"\") live = true, want false")
	}
	if got != theme.Default() {
		t.Errorf("LoadFrom(\"\") = %+v, want Default() %+v", got, theme.Default())
	}

	got, live = theme.LoadFrom(filepath.Join(t.TempDir(), "does-not-exist.toml"))
	if live {
		t.Error("LoadFrom(nonexistent) live = true, want false")
	}
	if got != theme.Default() {
		t.Errorf("LoadFrom(nonexistent) = %+v, want Default() %+v", got, theme.Default())
	}
}

func TestLoadFromIgnoresMalformedLines(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "colors.toml")
	toml := "not a key value line\naccent = \"#123456\"\n[some_table]\n"
	if err := os.WriteFile(path, []byte(toml), 0o644); err != nil {
		t.Fatal(err)
	}

	got, _ := theme.LoadFrom(path)
	if got.Accent != "#123456" {
		t.Errorf("Accent = %q, want #123456", got.Accent)
	}
}

// TestSerializeRoundTripsThroughLoadFrom covers Serialize/LoadFrom's
// contract with each other: internal/config.EnsureConfigFiles writes
// Serialize(theme.Default())'s output straight to disk as mpdtui's own
// default color file, and every subsequent run reads it back via
// LoadFrom -- if the two ever disagreed on the format, that file would
// silently stop round-tripping (falling back to Default() again instead
// of reflecting whatever was actually written, including any hand
// edits a user made to it).
func TestSerializeRoundTripsThroughLoadFrom(t *testing.T) {
	want := theme.Default()
	dir := t.TempDir()
	path := filepath.Join(dir, "colors.toml")
	if err := os.WriteFile(path, []byte(theme.Serialize(want)), 0o644); err != nil {
		t.Fatal(err)
	}

	got, live := theme.LoadFrom(path)
	if !live {
		t.Fatal("LoadFrom() live = false, want true")
	}
	if got != want {
		t.Errorf("LoadFrom(Serialize(Default())) = %+v, want %+v", got, want)
	}
}
