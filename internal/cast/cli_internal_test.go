package cast

import (
	"bytes"
	"strings"
	"testing"

	"mpdtui/internal/mpdclient"
)

func cliManager(t *testing.T, prov *fakeProvider) *Manager {
	t.Helper()
	mpd := &fakeMPD{outputs: []mpdclient.Output{
		{ID: 0, Plugin: "alsa", Enabled: true},
		{ID: 1, Plugin: "httpd", Enabled: false},
	}}
	return NewManager(testConfig(t), mpd).withProviders(prov)
}

func TestCLIList(t *testing.T) {
	prov := &fakeProvider{kind: KindChromecast, targets: []Target{
		{Kind: KindChromecast, ID: "1", Name: "Living Room", Addr: "10.0.0.2:8009"},
	}}
	var buf bytes.Buffer
	if err := List(&buf, cliManager(t, prov)); err != nil {
		t.Fatalf("List: %v", err)
	}
	if !strings.Contains(buf.String(), "Living Room") || !strings.Contains(buf.String(), "10.0.0.2:8009") {
		t.Errorf("List output missing device info:\n%s", buf.String())
	}
}

func TestCLIListEmpty(t *testing.T) {
	var buf bytes.Buffer
	if err := List(&buf, cliManager(t, &fakeProvider{kind: KindChromecast})); err != nil {
		t.Fatalf("List: %v", err)
	}
	if !strings.Contains(buf.String(), "no cast targets found") {
		t.Errorf("empty List output = %q", buf.String())
	}
}

func TestCLIStartByName(t *testing.T) {
	prov := &fakeProvider{kind: KindChromecast, targets: []Target{
		{Kind: KindChromecast, ID: "id-1", Name: "Kitchen"},
	}}
	m := cliManager(t, prov)
	var buf bytes.Buffer
	if err := Start(&buf, m, "kitchen"); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if len(prov.playedURL) != 1 {
		t.Errorf("expected one Play call, got %v", prov.playedURL)
	}
	if !strings.Contains(buf.String(), "casting to Kitchen") {
		t.Errorf("Start output = %q", buf.String())
	}
	if m.Session() == nil {
		t.Error("no session after Start")
	}
}

func TestCLIStartNoMatch(t *testing.T) {
	prov := &fakeProvider{kind: KindChromecast, targets: []Target{{Kind: KindChromecast, ID: "a", Name: "Office"}}}
	if err := Start(&bytes.Buffer{}, cliManager(t, prov), "nope"); err == nil {
		t.Fatal("Start with no matching target should error")
	}
}

func TestCLIStopNothingActive(t *testing.T) {
	var buf bytes.Buffer
	if err := Stop(&buf, cliManager(t, &fakeProvider{kind: KindChromecast})); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	if !strings.Contains(buf.String(), "no active cast found") {
		t.Errorf("Stop output = %q", buf.String())
	}
}

func TestCLIStopActive(t *testing.T) {
	prov := &fakeProvider{kind: KindChromecast, targets: []Target{{Kind: KindChromecast, ID: "id-1", Name: "Den"}}}
	m := cliManager(t, prov)
	if err := Start(&bytes.Buffer{}, m, "id-1"); err != nil {
		t.Fatalf("Start: %v", err)
	}
	var buf bytes.Buffer
	if err := Stop(&buf, m); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	if len(prov.stops) != 1 || !strings.Contains(buf.String(), "cast stopped") {
		t.Errorf("Stop: stops=%v output=%q", prov.stops, buf.String())
	}
}
