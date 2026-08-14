package tests

import (
	"os"
	"path/filepath"
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
