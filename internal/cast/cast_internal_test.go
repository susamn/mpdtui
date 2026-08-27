package cast

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"mpdtui/internal/mpdclient"
)

// fakeProvider is a controllable CastProvider for lifecycle/discovery tests.
type fakeProvider struct {
	kind      Kind
	targets   []Target
	discErr   error
	delay     time.Duration
	playErr   error
	stopErr   error
	status    map[string]NowCasting
	playedURL map[string]string
	stops     []string
}

func (f *fakeProvider) Kind() Kind { return f.kind }

func (f *fakeProvider) Discover(ctx context.Context) ([]Target, error) {
	if f.delay > 0 {
		select {
		case <-time.After(f.delay):
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	return f.targets, f.discErr
}

func (f *fakeProvider) Play(ctx context.Context, t Target, streamURL string, meta MediaMeta) error {
	if f.playErr != nil {
		return f.playErr
	}
	if f.playedURL == nil {
		f.playedURL = map[string]string{}
	}
	f.playedURL[t.ID] = streamURL
	return nil
}

func (f *fakeProvider) Stop(ctx context.Context, t Target) error {
	f.stops = append(f.stops, t.ID)
	return f.stopErr
}

func (f *fakeProvider) Status(ctx context.Context, t Target) (NowCasting, error) {
	return f.status[t.ID], nil
}

// fakeMPD is an in-memory MPD with a settable output list.
type fakeMPD struct {
	outputs    []mpdclient.Output
	enableLog  []int
	disableLog []int
	song       mpdclient.Song
}

func (m *fakeMPD) Outputs() ([]mpdclient.Output, error) { return m.outputs, nil }

func (m *fakeMPD) HTTPDOutput() (mpdclient.Output, error) {
	for _, o := range m.outputs {
		if o.Plugin == "httpd" {
			return o, nil
		}
	}
	return mpdclient.Output{}, mpdclient.ErrNoHTTPDOutput
}

func (m *fakeMPD) EnableOutput(id int) error {
	m.enableLog = append(m.enableLog, id)
	for i := range m.outputs {
		if m.outputs[i].ID == id {
			m.outputs[i].Enabled = true
		}
	}
	return nil
}

func (m *fakeMPD) DisableOutput(id int) error {
	m.disableLog = append(m.disableLog, id)
	for i := range m.outputs {
		if m.outputs[i].ID == id {
			m.outputs[i].Enabled = false
		}
	}
	return nil
}

func (m *fakeMPD) Status() (mpdclient.Status, error)    { return mpdclient.Status{}, nil }
func (m *fakeMPD) CurrentSong() (mpdclient.Song, error) { return m.song, nil }

func testConfig(t *testing.T) Config {
	t.Helper()
	return Config{
		MPDHost:          "192.168.1.5:6600",
		DiscoveryTimeout: 200 * time.Millisecond,
		StatePath:        filepath.Join(t.TempDir(), "cast-session.json"),
	}
}

func TestDiscoverMergesDedupesAndSurvivesSlowProvider(t *testing.T) {
	fast := &fakeProvider{kind: KindChromecast, targets: []Target{
		{Kind: KindChromecast, ID: "a", Name: "Zed"},
		{Kind: KindChromecast, ID: "a", Name: "Zed dup"},
		{Kind: KindChromecast, ID: "b", Name: "Apple"},
	}}
	slow := &fakeProvider{kind: KindDLNA, delay: time.Hour, targets: []Target{{Kind: KindDLNA, ID: "x"}}}

	m := NewManager(testConfig(t), &fakeMPD{}).withProviders(fast, slow)
	got := m.Discover(context.Background())

	if len(got) != 2 {
		t.Fatalf("got %d targets, want 2 (dedupe by ID, slow provider dropped): %+v", len(got), got)
	}
	if got[0].Name != "Apple" || got[1].Name != "Zed" {
		t.Errorf("not sorted by name: %+v", got)
	}
}

func TestStartCastEnablesHTTPDPersistsThenPlays(t *testing.T) {
	mpd := &fakeMPD{outputs: []mpdclient.Output{
		{ID: 0, Name: "alsa", Plugin: "alsa", Enabled: true},
		{ID: 1, Name: "cast", Plugin: "httpd", Enabled: false},
	}}
	prov := &fakeProvider{kind: KindChromecast}
	cfg := testConfig(t)
	m := NewManager(cfg, mpd).withProviders(prov)

	target := Target{Kind: KindChromecast, ID: "tv", Name: "TV"}
	sess, err := m.StartCast(context.Background(), target, MediaMeta{})
	if err != nil {
		t.Fatalf("StartCast: %v", err)
	}

	if prov.playedURL["tv"] != "http://192.168.1.5:8000/" {
		t.Errorf("played URL = %q, want derived from MPD host + default port", prov.playedURL["tv"])
	}
	if len(mpd.enableLog) != 1 || mpd.enableLog[0] != 1 {
		t.Errorf("enableLog = %v, want [1] (httpd only; not exclusive)", mpd.enableLog)
	}
	if len(mpd.disableLog) != 0 {
		t.Errorf("disableLog = %v, want empty (exclusive off)", mpd.disableLog)
	}
	if m.Session() == nil {
		t.Fatal("Session() nil after StartCast")
	}
	persisted, _ := readState(cfg.StatePath)
	if persisted == nil || persisted.Target.ID != "tv" {
		t.Errorf("state file = %+v, want the tv session persisted", persisted)
	}
	if len(sess.PriorOutputs) != 2 {
		t.Errorf("PriorOutputs = %+v, want both outputs snapshotted", sess.PriorOutputs)
	}
}

func TestStartCastExclusiveDisablesOthers(t *testing.T) {
	mpd := &fakeMPD{outputs: []mpdclient.Output{
		{ID: 0, Plugin: "alsa", Enabled: true},
		{ID: 1, Plugin: "httpd", Enabled: false},
	}}
	cfg := testConfig(t)
	cfg.Exclusive = true
	m := NewManager(cfg, mpd).withProviders(&fakeProvider{kind: KindChromecast})

	if _, err := m.StartCast(context.Background(), Target{Kind: KindChromecast, ID: "tv"}, MediaMeta{}); err != nil {
		t.Fatalf("StartCast: %v", err)
	}
	if len(mpd.disableLog) != 1 || mpd.disableLog[0] != 0 {
		t.Errorf("disableLog = %v, want [0] (alsa disabled under exclusive)", mpd.disableLog)
	}
}

func TestStartCastRollsBackOutputsWhenPlayFails(t *testing.T) {
	mpd := &fakeMPD{outputs: []mpdclient.Output{
		{ID: 0, Plugin: "alsa", Enabled: true},
		{ID: 1, Plugin: "httpd", Enabled: false},
	}}
	cfg := testConfig(t)
	m := NewManager(cfg, mpd).withProviders(&fakeProvider{kind: KindChromecast, playErr: errors.New("boom")})

	_, err := m.StartCast(context.Background(), Target{Kind: KindChromecast, ID: "tv"}, MediaMeta{})
	if err == nil {
		t.Fatal("StartCast succeeded, want error from Play")
	}
	if mpd.outputs[1].Enabled {
		t.Error("httpd output left enabled after Play failed, want rolled back")
	}
	if m.Session() != nil {
		t.Error("Session() set after failed StartCast")
	}
	if s, _ := readState(cfg.StatePath); s != nil {
		t.Error("state file left behind after failed StartCast")
	}
}

func TestStartCastNoHTTPDOutput(t *testing.T) {
	m := NewManager(testConfig(t), &fakeMPD{outputs: []mpdclient.Output{{ID: 0, Plugin: "alsa"}}}).
		withProviders(&fakeProvider{kind: KindChromecast})
	_, err := m.StartCast(context.Background(), Target{Kind: KindChromecast, ID: "tv"}, MediaMeta{})
	if !errors.Is(err, mpdclient.ErrNoHTTPDOutput) {
		t.Fatalf("err = %v, want ErrNoHTTPDOutput", err)
	}
}

func TestStopCastFromDiskStateRestoresOutputs(t *testing.T) {
	mpd := &fakeMPD{outputs: []mpdclient.Output{
		{ID: 0, Plugin: "alsa", Enabled: false},
		{ID: 1, Plugin: "httpd", Enabled: true},
	}}
	cfg := testConfig(t)
	prov := &fakeProvider{kind: KindChromecast}
	// Simulate a cast started by a since-closed process: state file only.
	writeState(cfg.StatePath, &Session{
		Target:        Target{Kind: KindChromecast, ID: "tv", Name: "TV"},
		StreamURL:     "http://192.168.1.5:8000/",
		HTTPDOutputID: 1,
		PriorOutputs:  []OutputState{{ID: 0, Enabled: true}, {ID: 1, Enabled: false}},
		StartedAt:     time.Now(),
	})
	m := NewManager(cfg, mpd).withProviders(prov)

	if err := m.StopCast(context.Background()); err != nil {
		t.Fatalf("StopCast: %v", err)
	}
	if len(prov.stops) != 1 {
		t.Errorf("provider.Stop calls = %v, want one", prov.stops)
	}
	if !mpd.outputs[0].Enabled || mpd.outputs[1].Enabled {
		t.Errorf("outputs = %+v, want restored to prior (alsa on, httpd off)", mpd.outputs)
	}
	if s, _ := readState(cfg.StatePath); s != nil {
		t.Error("state file not cleared after StopCast")
	}
}

func TestStopCastNothingActive(t *testing.T) {
	m := NewManager(testConfig(t), &fakeMPD{outputs: []mpdclient.Output{{ID: 1, Plugin: "httpd"}}}).
		withProviders(&fakeProvider{kind: KindChromecast})
	if err := m.StopCast(context.Background()); !errors.Is(err, ErrNotCasting) {
		t.Fatalf("err = %v, want ErrNotCasting", err)
	}
}

func TestReattachAdoptsMatchingSessionAndDiscardsMismatch(t *testing.T) {
	cfg := testConfig(t)
	mpd := &fakeMPD{outputs: []mpdclient.Output{{ID: 1, Plugin: "httpd", Enabled: true}}}
	sess := &Session{
		Target:    Target{Kind: KindChromecast, ID: "tv"},
		StreamURL: "http://host:8000/",
		StartedAt: time.Now(),
	}

	// Matching: device reports playing our URL.
	writeState(cfg.StatePath, sess)
	prov := &fakeProvider{kind: KindChromecast, status: map[string]NowCasting{
		"tv": {Active: true, MediaURL: "http://host:8000/"},
	}}
	m := NewManager(cfg, mpd).withProviders(prov)
	got, err := m.Reattach(context.Background())
	if err != nil || got == nil {
		t.Fatalf("Reattach = %v, %v; want the session adopted", got, err)
	}

	// Mismatch: different URL -> cleared.
	writeState(cfg.StatePath, sess)
	prov.status["tv"] = NowCasting{Active: true, MediaURL: "http://something/else"}
	m = NewManager(cfg, mpd).withProviders(prov)
	got, _ = m.Reattach(context.Background())
	if got != nil {
		t.Error("Reattach adopted a session whose device is playing a different URL")
	}
	if s, _ := readState(cfg.StatePath); s != nil {
		t.Error("mismatched state file not cleared")
	}
}

func TestReattachIgnoresStaleSession(t *testing.T) {
	cfg := testConfig(t)
	writeState(cfg.StatePath, &Session{
		Target:    Target{Kind: KindChromecast, ID: "tv"},
		StartedAt: time.Now().Add(-48 * time.Hour),
	})
	m := NewManager(cfg, &fakeMPD{}).withProviders(&fakeProvider{kind: KindChromecast})
	got, _ := m.Reattach(context.Background())
	if got != nil {
		t.Error("Reattach adopted a 48h-old session")
	}
}

func TestDeriveStreamURL(t *testing.T) {
	tests := []struct {
		name string
		cfg  Config
		out  mpdclient.Output
		want string
		err  bool
	}{
		{"override wins", Config{StreamURL: "http://x:9/"}, mpdclient.Output{}, "http://x:9//", false},
		{"host + default port", Config{MPDHost: "10.0.0.2:6600"}, mpdclient.Output{}, "http://10.0.0.2:8000/", false},
		{"httpd reported port", Config{MPDHost: "10.0.0.2:6600"}, mpdclient.Output{Attrs: map[string]string{"port": "9999"}}, "http://10.0.0.2:9999/", false},
		{"config port", Config{MPDHost: "10.0.0.2:6600", HTTPDPort: "7777"}, mpdclient.Output{}, "http://10.0.0.2:7777/", false},
		{"stream host override", Config{MPDHost: "localhost:6600", StreamHost: "10.0.0.9"}, mpdclient.Output{}, "http://10.0.0.9:8000/", false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := deriveStreamURL(tc.cfg, tc.out)
			if (err != nil) != tc.err {
				t.Fatalf("err = %v, want err=%v", err, tc.err)
			}
			if got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestMatchTarget(t *testing.T) {
	targets := []Target{
		{ID: "id1", Name: "Living Room"},
		{ID: "id2", Name: "Kitchen"},
		{ID: "id3", Name: "Kitchen"},
	}
	if got, err := matchTarget(targets, "id2"); err != nil || got.ID != "id2" {
		t.Errorf("by id: %+v %v", got, err)
	}
	if got, err := matchTarget(targets, "living room"); err != nil || got.ID != "id1" {
		t.Errorf("by name ci: %+v %v", got, err)
	}
	if _, err := matchTarget(targets, "Kitchen"); err == nil {
		t.Error("ambiguous name should error")
	}
	if _, err := matchTarget(targets, "nope"); err == nil {
		t.Error("no match should error")
	}
}
