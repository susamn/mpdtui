// Package config resolves MPD connection settings from the environment.
package config

import (
	"os"
	"strings"
)

const (
	defaultHost = "localhost"
	defaultPort = "6600"
)

// Config holds the connection settings for reaching an MPD server.
type Config struct {
	Host     string
	Port     string
	Password string
}

// Load reads MPD_HOST and MPD_PORT from the environment, following mpc's
// convention that MPD_HOST may be "password@host".
func Load() Config {
	host := envOr("MPD_HOST", defaultHost)
	port := envOr("MPD_PORT", defaultPort)

	password := ""
	if at := strings.IndexByte(host, '@'); at >= 0 {
		password = host[:at]
		host = host[at+1:]
	}

	return Config{Host: host, Port: port, Password: password}
}

// Addr returns the host:port pair for dialing.
func (c Config) Addr() string {
	return c.Host + ":" + c.Port
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
