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
	dir := t.TempDir()
	withEnv(t, "XDG_CONFIG_HOME", dir)
	writeConfigFile(t, dir, "music_dir = /mnt/music\n")

	if got, want := config.LoadMusicDir(), "/mnt/music"; got != want {
		t.Errorf("LoadMusicDir() = %q, want %q", got, want)
	}
}

func TestLoadMusicDirIgnoresCommentsAndBlankLines(t *testing.T) {
	dir := t.TempDir()
	withEnv(t, "XDG_CONFIG_HOME", dir)
	writeConfigFile(t, dir, "# a comment\n\n  music_dir   =   /mnt/music  \n")

	if got, want := config.LoadMusicDir(), "/mnt/music"; got != want {
		t.Errorf("LoadMusicDir() = %q, want %q", got, want)
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

func TestLoadMusicDirExpandsLeadingTilde(t *testing.T) {
	dir := t.TempDir()
	withEnv(t, "XDG_CONFIG_HOME", dir)
	writeConfigFile(t, dir, "music_dir = ~/Music\n")

	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("no home directory available in this environment")
	}
	if got, want := config.LoadMusicDir(), filepath.Join(home, "Music"); got != want {
		t.Errorf("LoadMusicDir() = %q, want %q", got, want)
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
