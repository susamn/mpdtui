// Package cast sends MPD's audio to external players -- Chromecast / Nest
// devices, Home Assistant media_player entities, and DLNA/UPnP renderers.
//
// The model is "sender only": mpdtui enables MPD's httpd audio output,
// tells the target device to play that HTTP stream URL, then steps back.
// MPD stays the source of truth for what's playing, and normal transport
// controls (pause/seek/next) keep working because the device is just an
// HTTP client of the httpd stream. When mpdtui exits the device keeps
// playing; the next launch re-discovers it and re-attaches by matching
// the device's current media URL against the persisted session.
//
// Everything casting-related lives in this package. The rest of the
// codebase touches it only through Manager (constructed in cmd/mpdtui)
// and, for the TUI, the small castController interface in internal/ui.
package cast

import (
	"context"
	"errors"
	"sort"
	"sync"
	"time"

	"mpdtui/internal/mpdclient"
)

// Kind identifies a casting backend.
type Kind string

const (
	KindChromecast    Kind = "chromecast"
	KindHomeAssistant Kind = "homeassistant"
	KindDLNA          Kind = "dlna"
)

// Target is a discovered device one can cast to. ID is provider-stable
// across discoveries (Cast UUID / HA entity_id / UPnP UDN) and is what
// the persisted session and `-cast-to` matching key on.
type Target struct {
	Kind  Kind
	ID    string
	Name  string
	Addr  string // host:port or base URL, provider-specific
	Model string
}

// MediaMeta is the now-playing information handed to a device so its UI
// can show a title/artist/artwork instead of a bare URL. All fields are
// best-effort -- a device is told to play the stream regardless.
type MediaMeta struct {
	Title   string
	Artist  string
	Artwork string
}

// NowCasting is what a provider reports a device is currently doing, used
// for re-attach detection: MediaURL is the URL the device says it's
// playing (compared against the persisted session's stream URL).
type NowCasting struct {
	Active   bool
	MediaURL string
}

// CastProvider is one backend. Discover is best-effort and returns
// whatever it found before ctx expired rather than failing outright.
type CastProvider interface {
	Kind() Kind
	Discover(ctx context.Context) ([]Target, error)
	Play(ctx context.Context, t Target, streamURL string, meta MediaMeta) error
	Stop(ctx context.Context, t Target) error
	Status(ctx context.Context, t Target) (NowCasting, error)
}

// MPD is the slice of *mpdclient.Client this package needs. Narrow on
// purpose -- tests substitute a fake, and it documents exactly how
// casting touches MPD (enable httpd, optionally disable others, restore).
type MPD interface {
	Outputs() ([]mpdclient.Output, error)
	HTTPDOutput() (mpdclient.Output, error)
	EnableOutput(id int) error
	DisableOutput(id int) error
	Status() (mpdclient.Status, error)
	CurrentSong() (mpdclient.Song, error)
}

// ErrNotCasting is returned by StopCast when nothing is active.
var ErrNotCasting = errors.New("not currently casting")

// Manager owns discovery, the cast lifecycle, and the single active
// session (this build casts to one target at a time). It is safe for
// concurrent use; the mutex is only ever held for in-memory snapshots,
// never across network or MPD I/O.
type Manager struct {
	cfg       Config
	mpd       MPD
	providers []CastProvider

	mu      sync.Mutex
	session *Session
}

// NewManager builds a Manager with every provider its config enables.
// Chromecast and DLNA are always on (discovery-only, no config needed);
// Home Assistant only when ha_url + ha_token are set.
func NewManager(cfg Config, mpd MPD) *Manager {
	m := &Manager{cfg: cfg, mpd: mpd}
	m.providers = append(m.providers, newChromecastProvider(cfg))
	if p := newHomeAssistantProvider(cfg); p != nil {
		m.providers = append(m.providers, p)
	}
	if p := newDLNAProvider(cfg); p != nil {
		m.providers = append(m.providers, p)
	}
	return m
}

// withProviders is a test seam: replace the provider set.
func (m *Manager) withProviders(ps ...CastProvider) *Manager {
	m.providers = ps
	return m
}

// Session returns a snapshot of the active session, or nil.
func (m *Manager) Session() *Session {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.session == nil {
		return nil
	}
	s := *m.session
	return &s
}

func (m *Manager) setSession(s *Session) {
	m.mu.Lock()
	m.session = s
	m.mu.Unlock()
}

// Discover fans out to every provider concurrently under one deadline,
// de-dupes by (Kind, ID), and returns a stable-sorted list. A provider
// that errors or times out just contributes nothing.
func (m *Manager) Discover(ctx context.Context) []Target {
	timeout := m.cfg.DiscoveryTimeout
	if timeout <= 0 {
		timeout = defaultDiscoveryTimeout
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	var wg sync.WaitGroup
	results := make(chan []Target, len(m.providers))
	for _, p := range m.providers {
		wg.Add(1)
		go func(p CastProvider) {
			defer wg.Done()
			found, _ := p.Discover(ctx)
			results <- found
		}(p)
	}
	go func() { wg.Wait(); close(results) }()

	seen := make(map[string]bool)
	var out []Target
	for batch := range results {
		for _, t := range batch {
			key := string(t.Kind) + "\x00" + t.ID
			if seen[key] {
				continue
			}
			seen[key] = true
			out = append(out, t)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Kind != out[j].Kind {
			return out[i].Kind < out[j].Kind
		}
		return out[i].Name < out[j].Name
	})
	return out
}

func (m *Manager) providerFor(k Kind) CastProvider {
	for _, p := range m.providers {
		if p.Kind() == k {
			return p
		}
	}
	return nil
}

// now is overridable in tests.
var now = time.Now
