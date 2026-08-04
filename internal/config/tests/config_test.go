package tests

import (
	"testing"

	"mpdtui/internal/config"
)

func withEnv(t *testing.T, key, value string) {
	t.Helper()
	t.Setenv(key, value)
}

func TestLoadDefaults(t *testing.T) {
	withEnv(t, "MPD_HOST", "")
	withEnv(t, "MPD_PORT", "")

	c := config.Load()
	if c.Host != "localhost" || c.Port != "6600" || c.Password != "" {
		t.Fatalf("unexpected defaults: %+v", c)
	}
	if c.Addr() != "localhost:6600" {
		t.Fatalf("unexpected Addr(): %q", c.Addr())
	}
}

func TestLoadHostOnly(t *testing.T) {
	withEnv(t, "MPD_HOST", "example.org")
	withEnv(t, "MPD_PORT", "6601")

	c := config.Load()
	if c.Host != "example.org" || c.Port != "6601" || c.Password != "" {
		t.Fatalf("unexpected config: %+v", c)
	}
}

func TestLoadPasswordAtHost(t *testing.T) {
	withEnv(t, "MPD_HOST", "secret@example.org")
	withEnv(t, "MPD_PORT", "")

	c := config.Load()
	if c.Host != "example.org" || c.Password != "secret" || c.Port != "6600" {
		t.Fatalf("unexpected config: %+v", c)
	}
}
