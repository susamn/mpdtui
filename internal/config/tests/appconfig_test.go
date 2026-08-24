package tests

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mpdtui/internal/config"
)

func TestConfigDirUsesXDGConfigHomeWhenSet(t *testing.T) {
	withEnv(t, "XDG_CONFIG_HOME", "/xdg")
	if got, want := config.ConfigDir(), filepath.Join("/xdg", "mpdtui"); got != want {
		t.Errorf("ConfigDir() = %q, want %q", got, want)
	}
}

func TestConfigDirFallsBackToDotConfig(t *testing.T) {
	withEnv(t, "XDG_CONFIG_HOME", "")
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("no home directory available in this environment")
	}
	if got, want := config.ConfigDir(), filepath.Join(home, ".config", "mpdtui"); got != want {
		t.Errorf("ConfigDir() = %q, want %q", got, want)
	}
}

func TestLoadMusicDirMissingFileReturnsEmpty(t *testing.T) {
	withEnv(t, "XDG_CONFIG_HOME", t.TempDir())
	if got := config.LoadMusicDir(); got != "" {
		t.Errorf("LoadMusicDir() with no config file = %q, want empty", got)
	}
}

func TestLoadMusicDirReadsKey(t *testing.T) {
	xdgHome := t.TempDir()
	withEnv(t, "XDG_CONFIG_HOME", xdgHome)
	musicDir := t.TempDir() // must actually exist -- LoadMusicDir now verifies that
	writeConfigFile(t, xdgHome, "music_dir = "+musicDir+"\n")

	if got, want := config.LoadMusicDir(), musicDir; got != want {
		t.Errorf("LoadMusicDir() = %q, want %q", got, want)
	}
}

func TestLoadMusicDirIgnoresCommentsAndBlankLines(t *testing.T) {
	xdgHome := t.TempDir()
	withEnv(t, "XDG_CONFIG_HOME", xdgHome)
	musicDir := t.TempDir()
	writeConfigFile(t, xdgHome, "# a comment\n\n  music_dir   =   "+musicDir+"  \n")

	if got, want := config.LoadMusicDir(), musicDir; got != want {
		t.Errorf("LoadMusicDir() = %q, want %q", got, want)
	}
}

// TestLoadMusicDirNonexistentPathReturnsEmpty covers a configured but
// broken setup -- a stale path, a typo, an unmounted drive -- which must
// behave exactly like "not configured at all" (see LoadMusicDir's own
// doc comment): every consumer of this value only ever checks for "",
// so a real-but-unreadable path leaking through here would otherwise
// show up as confusing, half-working behavior instead of the feature
// just staying cleanly inactive.
func TestLoadMusicDirNonexistentPathReturnsEmpty(t *testing.T) {
	dir := t.TempDir()
	withEnv(t, "XDG_CONFIG_HOME", dir)
	writeConfigFile(t, dir, "music_dir = "+filepath.Join(dir, "does-not-exist")+"\n")

	if got := config.LoadMusicDir(); got != "" {
		t.Errorf("LoadMusicDir() with a nonexistent music_dir = %q, want empty", got)
	}
}

// TestLoadMusicDirPathIsAFileReturnsEmpty covers music_dir pointing at an
// existing file rather than a directory -- also a broken configuration
// that must resolve to "", not a path lyrics.Candidates would fail to
// os.ReadDir on later.
func TestLoadMusicDirPathIsAFileReturnsEmpty(t *testing.T) {
	dir := t.TempDir()
	withEnv(t, "XDG_CONFIG_HOME", dir)
	filePath := filepath.Join(dir, "not-a-directory")
	if err := os.WriteFile(filePath, []byte("x"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	writeConfigFile(t, dir, "music_dir = "+filePath+"\n")

	if got := config.LoadMusicDir(); got != "" {
		t.Errorf("LoadMusicDir() with music_dir pointing at a file = %q, want empty", got)
	}
}

func TestLoadMusicDirNoMatchingKeyReturnsEmpty(t *testing.T) {
	dir := t.TempDir()
	withEnv(t, "XDG_CONFIG_HOME", dir)
	writeConfigFile(t, dir, "some_other_key = value\n")

	if got := config.LoadMusicDir(); got != "" {
		t.Errorf("LoadMusicDir() with no music_dir key = %q, want empty", got)
	}
}

// TestLoadMusicDirExpandsLeadingTilde points $HOME at a temp dir (rather
// than relying on the real environment's actual home directory, or a
// subdirectory under it, existing) so the test is self-contained and
// still exercises the real existence check: fakeHome/Music is created for
// real, so tilde-expansion has to land on exactly that path for the
// directory to be found.
func TestLoadMusicDirExpandsLeadingTilde(t *testing.T) {
	fakeHome := t.TempDir()
	withEnv(t, "HOME", fakeHome)
	musicDir := filepath.Join(fakeHome, "Music")
	if err := os.Mkdir(musicDir, 0o755); err != nil {
		t.Fatalf("Mkdir: %v", err)
	}

	xdgHome := t.TempDir()
	withEnv(t, "XDG_CONFIG_HOME", xdgHome)
	writeConfigFile(t, xdgHome, "music_dir = ~/Music\n")

	if got, want := config.LoadMusicDir(), musicDir; got != want {
		t.Errorf("LoadMusicDir() = %q, want %q", got, want)
	}
}

func TestLoadThemeFileMissingFileFallsBackToDefaultColorsFile(t *testing.T) {
	withEnv(t, "XDG_CONFIG_HOME", t.TempDir())
	if got, want := config.LoadThemeFile(), config.DefaultColorsFile(); got != want {
		t.Errorf("LoadThemeFile() with no config file = %q, want DefaultColorsFile() %q", got, want)
	}
}

func TestLoadThemeFileReadsKey(t *testing.T) {
	xdgHome := t.TempDir()
	withEnv(t, "XDG_CONFIG_HOME", xdgHome)
	writeConfigFile(t, xdgHome, "theme_file = /some/where/colors.toml\n")

	if got, want := config.LoadThemeFile(), "/some/where/colors.toml"; got != want {
		t.Errorf("LoadThemeFile() = %q, want %q", got, want)
	}
}

// TestLoadThemeFileDoesNotRequireExistence covers LoadThemeFile's own
// deliberate divergence from LoadMusicDir: a configured-but-unreadable
// path is passed through as-is rather than collapsed to "", since
// internal/theme's own Load/LoadFrom already treat an unreadable file
// as "no live theme" and fall back to mpdtui's static colors on their
// own -- there's no separate "is this a real file" check to duplicate
// here.
func TestLoadThemeFileDoesNotRequireExistence(t *testing.T) {
	xdgHome := t.TempDir()
	withEnv(t, "XDG_CONFIG_HOME", xdgHome)
	writeConfigFile(t, xdgHome, "theme_file = /does/not/exist.toml\n")

	if got, want := config.LoadThemeFile(), "/does/not/exist.toml"; got != want {
		t.Errorf("LoadThemeFile() = %q, want %q", got, want)
	}
}

func TestLoadThemeFileExpandsLeadingTilde(t *testing.T) {
	fakeHome := t.TempDir()
	withEnv(t, "HOME", fakeHome)
	xdgHome := t.TempDir()
	withEnv(t, "XDG_CONFIG_HOME", xdgHome)
	writeConfigFile(t, xdgHome, "theme_file = ~/.cache/mpdtui/colors.toml\n")

	want := filepath.Join(fakeHome, ".cache", "mpdtui", "colors.toml")
	if got := config.LoadThemeFile(); got != want {
		t.Errorf("LoadThemeFile() = %q, want %q", got, want)
	}
}

// TestLoadThemeFileResolvesRelativePathAgainstConfigDir covers
// EnsureConfigFiles' own default value ("./colors.toml"): a relative
// theme_file resolves against ConfigDir(), not the process's current
// working directory, so it keeps working no matter where mpdtui is
// launched from.
func TestLoadThemeFileResolvesRelativePathAgainstConfigDir(t *testing.T) {
	xdgHome := t.TempDir()
	withEnv(t, "XDG_CONFIG_HOME", xdgHome)
	writeConfigFile(t, xdgHome, "theme_file = ./colors.toml\n")

	want := filepath.Join(xdgHome, "mpdtui", "colors.toml")
	if got := config.LoadThemeFile(); got != want {
		t.Errorf("LoadThemeFile() = %q, want %q", got, want)
	}
}

// TestLoadThemeFileResolvesBareRelativeFilename covers a theme_file
// value with no "./" prefix at all (just a bare filename) -- same
// resolution as the "./"-prefixed form above.
func TestLoadThemeFileResolvesBareRelativeFilename(t *testing.T) {
	xdgHome := t.TempDir()
	withEnv(t, "XDG_CONFIG_HOME", xdgHome)
	writeConfigFile(t, xdgHome, "theme_file = matugen-colors.toml\n")

	want := filepath.Join(xdgHome, "mpdtui", "matugen-colors.toml")
	if got := config.LoadThemeFile(); got != want {
		t.Errorf("LoadThemeFile() = %q, want %q", got, want)
	}
}

func TestLoadTrackMetadataEnabledDefaultsToFalse(t *testing.T) {
	withEnv(t, "XDG_CONFIG_HOME", t.TempDir())
	if config.LoadTrackMetadataEnabled() {
		t.Error("LoadTrackMetadataEnabled() with no config file = true, want false")
	}
}

func TestLoadTrackMetadataEnabledTrue(t *testing.T) {
	dir := t.TempDir()
	withEnv(t, "XDG_CONFIG_HOME", dir)
	writeConfigFile(t, dir, "track_metadata = true\n")

	if !config.LoadTrackMetadataEnabled() {
		t.Error("LoadTrackMetadataEnabled() = false, want true")
	}
}

// TestLoadTrackMetadataEnabledRejectsAnythingButExactlyTrue guards the
// deliberately strict match ("== \"true\"", not a general boolean
// parse): a typo or a "yes"/"1"/"True" should stay inactive rather than
// silently activating a feature that starts writing a local database
// file on every play/pause tick.
func TestLoadTrackMetadataEnabledRejectsAnythingButExactlyTrue(t *testing.T) {
	for _, value := range []string{"True", "yes", "1", "enabled", ""} {
		dir := t.TempDir()
		withEnv(t, "XDG_CONFIG_HOME", dir)
		writeConfigFile(t, dir, "track_metadata = "+value+"\n")

		if config.LoadTrackMetadataEnabled() {
			t.Errorf("LoadTrackMetadataEnabled() with track_metadata = %q = true, want false", value)
		}
	}
}

func TestLoadMusicDirAndTrackMetadataFromTheSameFile(t *testing.T) {
	dir := t.TempDir()
	withEnv(t, "XDG_CONFIG_HOME", dir)
	musicDir := t.TempDir()
	writeConfigFile(t, dir, "music_dir = "+musicDir+"\ntrack_metadata = true\n")

	if got, want := config.LoadMusicDir(), musicDir; got != want {
		t.Errorf("LoadMusicDir() = %q, want %q", got, want)
	}
	if !config.LoadTrackMetadataEnabled() {
		t.Error("LoadTrackMetadataEnabled() = false, want true")
	}
}

func TestDBFileIsInsideConfigDir(t *testing.T) {
	withEnv(t, "XDG_CONFIG_HOME", "/xdg")
	if got, want := config.DBFile(), filepath.Join("/xdg", "mpdtui", "mpdtui.db"); got != want {
		t.Errorf("DBFile() = %q, want %q", got, want)
	}
}

func TestEnsureConfigFilesCreatesBothFiles(t *testing.T) {
	withEnv(t, "XDG_CONFIG_HOME", t.TempDir())

	if err := config.EnsureConfigFiles(); err != nil {
		t.Fatalf("EnsureConfigFiles(): %v", err)
	}

	if _, err := os.Stat(config.DefaultColorsFile()); err != nil {
		t.Errorf("DefaultColorsFile() not created: %v", err)
	}
	if _, err := os.Stat(config.ConfigFile()); err != nil {
		t.Errorf("ConfigFile() not created: %v", err)
	}
	if got := config.LoadThemeFile(); got != config.DefaultColorsFile() {
		t.Errorf("LoadThemeFile() after EnsureConfigFiles() = %q, want %q", got, config.DefaultColorsFile())
	}
}

// TestEnsureConfigFilesWritesRelativeThemeFile covers the actual
// written content, not just LoadThemeFile()'s resolved result: the
// config file itself must contain a relative theme_file value
// ("./colors.toml"), not an absolute path baked in at creation time --
// so it stays correct if ConfigDir() (e.g. $XDG_CONFIG_HOME) ever
// changes later, and reads cleanly if the config directory itself is
// copied/moved somewhere else.
func TestEnsureConfigFilesWritesRelativeThemeFile(t *testing.T) {
	withEnv(t, "XDG_CONFIG_HOME", t.TempDir())

	if err := config.EnsureConfigFiles(); err != nil {
		t.Fatalf("EnsureConfigFiles(): %v", err)
	}

	data, err := os.ReadFile(config.ConfigFile())
	if err != nil {
		t.Fatalf("ReadFile(ConfigFile()): %v", err)
	}
	if !strings.Contains(string(data), "theme_file = ./colors.toml") {
		t.Errorf("ConfigFile() content = %q, want it to contain \"theme_file = ./colors.toml\"", data)
	}
}

// TestEnsureConfigFilesNeverOverwritesExistingFiles covers the
// "mandatory but non-destructive" contract: EnsureConfigFiles always
// attempts to create what's missing, but must never touch a file
// that's already there -- a hand-edited config or color file surviving
// across every future run is the whole point of checking existence
// first rather than always writing.
func TestEnsureConfigFilesNeverOverwritesExistingFiles(t *testing.T) {
	withEnv(t, "XDG_CONFIG_HOME", t.TempDir())

	writeConfigFile(t, os.Getenv("XDG_CONFIG_HOME"), "theme_file = /my/own/colors.toml\n")
	if err := os.WriteFile(config.DefaultColorsFile(), []byte("accent = \"#123456\"\n"), 0o644); err != nil {
		t.Fatalf("seed DefaultColorsFile(): %v", err)
	}

	if err := config.EnsureConfigFiles(); err != nil {
		t.Fatalf("EnsureConfigFiles(): %v", err)
	}

	if got, want := config.LoadThemeFile(), "/my/own/colors.toml"; got != want {
		t.Errorf("LoadThemeFile() = %q, want %q (config file was overwritten)", got, want)
	}
	data, err := os.ReadFile(config.DefaultColorsFile())
	if err != nil {
		t.Fatalf("ReadFile(DefaultColorsFile()): %v", err)
	}
	if string(data) != "accent = \"#123456\"\n" {
		t.Errorf("DefaultColorsFile() content = %q, want it unchanged (was overwritten)", data)
	}
}

func writeConfigFile(t *testing.T, xdgConfigHome, content string) {
	t.Helper()
	dir := filepath.Join(xdgConfigHome, "mpdtui")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("MkdirAll(%q): %v", dir, err)
	}
	if err := os.WriteFile(filepath.Join(dir, "config"), []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
}
