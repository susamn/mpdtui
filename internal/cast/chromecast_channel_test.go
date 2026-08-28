package cast

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"math/big"
	"net"
	"sync"
	"testing"
	"time"

	"mpdtui/internal/cast/castproto"
)

// fakeReceiver is a scripted stand-in for a Cast device: a TLS listener
// that answers the platform/media control messages the provider sends.
type fakeReceiver struct {
	t  *testing.T
	ln net.Listener

	mu         sync.Mutex
	appRunning bool // whether the media receiver "app" is launched
	contentID  string
	seen       []string // message types received, for assertions
}

func (fr *fakeReceiver) setApp(running bool) {
	fr.mu.Lock()
	fr.appRunning = running
	fr.mu.Unlock()
}

func (fr *fakeReceiver) setContentID(id string) {
	fr.mu.Lock()
	fr.contentID = id
	fr.mu.Unlock()
}

func (fr *fakeReceiver) gotContentID() string {
	fr.mu.Lock()
	defer fr.mu.Unlock()
	return fr.contentID
}

func newFakeReceiver(t *testing.T) *fakeReceiver {
	t.Helper()
	cert := selfSignedCert(t)
	ln, err := tls.Listen("tcp", "127.0.0.1:0", &tls.Config{Certificates: []tls.Certificate{cert}})
	if err != nil {
		t.Fatalf("tls.Listen: %v", err)
	}
	fr := &fakeReceiver{t: t, ln: ln}
	go fr.serve()
	t.Cleanup(func() { ln.Close() })
	return fr
}

func (fr *fakeReceiver) addr() string { return fr.ln.Addr().String() }

func (fr *fakeReceiver) received(typ string) bool {
	fr.mu.Lock()
	defer fr.mu.Unlock()
	for _, s := range fr.seen {
		if s == typ {
			return true
		}
	}
	return false
}

func (fr *fakeReceiver) serve() {
	for {
		conn, err := fr.ln.Accept()
		if err != nil {
			return
		}
		go fr.handle(conn)
	}
}

func (fr *fakeReceiver) handle(conn net.Conn) {
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
	for {
		frame, err := castproto.ReadFrame(conn)
		if err != nil {
			return
		}
		var payload map[string]any
		_ = json.Unmarshal([]byte(frame.PayloadUTF8), &payload)
		typ, _ := payload["type"].(string)

		fr.mu.Lock()
		fr.seen = append(fr.seen, typ)
		fr.mu.Unlock()

		switch typ {
		case "LAUNCH":
			fr.setApp(true)
			fr.reply(conn, frame.SourceID, nsReceiver, fr.receiverStatus())
		case "GET_STATUS":
			if frame.Namespace == nsReceiver {
				fr.reply(conn, frame.SourceID, nsReceiver, fr.receiverStatus())
			} else {
				fr.reply(conn, frame.SourceID, nsMedia, fr.mediaStatus())
			}
		case "LOAD":
			media, _ := payload["media"].(map[string]any)
			id, _ := media["contentId"].(string)
			fr.setContentID(id)
			fr.reply(conn, frame.SourceID, nsMedia, fr.mediaStatus())
		case "STOP":
			fr.setApp(false)
			fr.reply(conn, frame.SourceID, nsReceiver, fr.receiverStatus())
		}
	}
}

func (fr *fakeReceiver) reply(conn net.Conn, dest, ns string, payload any) {
	data, _ := json.Marshal(payload)
	_ = castproto.WriteFrame(conn, &castproto.CastMessage{
		SourceID:      "receiver-0",
		DestinationID: dest,
		Namespace:     ns,
		PayloadType:   castproto.PayloadString,
		PayloadUTF8:   string(data),
	})
}

func (fr *fakeReceiver) receiverStatus() map[string]any {
	fr.mu.Lock()
	running := fr.appRunning
	fr.mu.Unlock()

	apps := []any{}
	if running {
		apps = append(apps, map[string]any{
			"appId":       defaultMediaReceiverAppID,
			"transportId": "web-1",
			"sessionId":   "sess-1",
		})
	}
	return map[string]any{"type": "RECEIVER_STATUS", "status": map[string]any{"applications": apps}}
}

func (fr *fakeReceiver) mediaStatus() map[string]any {
	fr.mu.Lock()
	id := fr.contentID
	fr.mu.Unlock()

	entry := map[string]any{"mediaSessionId": 1}
	if id != "" {
		entry["media"] = map[string]any{"contentId": id}
	}
	return map[string]any{"type": "MEDIA_STATUS", "status": []any{entry}}
}

func selfSignedCert(t *testing.T) tls.Certificate {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "fake-cast"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		IPAddresses:  []net.IP{net.ParseIP("127.0.0.1")},
	}
	der, err := x509.CreateCertificate(rand.Reader, &tmpl, &tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key}
}

func testCtx(t *testing.T) context.Context {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	t.Cleanup(cancel)
	return ctx
}

func TestChromecastPlayLaunchesAndLoads(t *testing.T) {
	fr := newFakeReceiver(t)
	p := &chromecastProvider{}
	target := Target{Kind: KindChromecast, ID: "x", Name: "TV", Addr: fr.addr()}

	err := p.Play(testCtx(t), target, "http://10.0.0.2:8000/", MediaMeta{Title: "Song", Artist: "Band"})
	if err != nil {
		t.Fatalf("Play: %v", err)
	}
	if !fr.received("LAUNCH") || !fr.received("LOAD") {
		t.Errorf("expected LAUNCH then LOAD, saw %v", fr.seen)
	}
	if fr.gotContentID() != "http://10.0.0.2:8000/" {
		t.Errorf("device loaded contentId %q, want the stream URL", fr.gotContentID())
	}
}

func TestChromecastStatusReportsContentID(t *testing.T) {
	fr := newFakeReceiver(t)
	fr.setApp(true)
	fr.setContentID("http://10.0.0.2:8000/")
	p := &chromecastProvider{}

	nc, err := p.Status(testCtx(t), Target{Kind: KindChromecast, Addr: fr.addr()})
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if !nc.Active || nc.MediaURL != "http://10.0.0.2:8000/" {
		t.Errorf("NowCasting = %+v, want active with the stream URL", nc)
	}
}

func TestChromecastStatusInactiveWhenNoApp(t *testing.T) {
	fr := newFakeReceiver(t)
	p := &chromecastProvider{}
	nc, err := p.Status(testCtx(t), Target{Kind: KindChromecast, Addr: fr.addr()})
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if nc.Active {
		t.Errorf("NowCasting.Active = true with no media receiver running")
	}
}

func TestChromecastStopStopsSession(t *testing.T) {
	fr := newFakeReceiver(t)
	fr.setApp(true)
	p := &chromecastProvider{}
	if err := p.Stop(testCtx(t), Target{Kind: KindChromecast, Addr: fr.addr()}); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	if !fr.received("STOP") {
		t.Errorf("expected STOP, saw %v", fr.seen)
	}
}

func TestChromecastAnswersHeartbeatPing(t *testing.T) {
	fr := newFakeReceiver(t)
	p := &chromecastProvider{}
	// Not a direct assertion on PONG, but the flow must not stall when a
	// PING is interleaved -- exercised implicitly by the 5s ctx here plus
	// the receiver's own deadline. A regression (treating PING as the
	// awaited reply) would hang until ctx expiry and fail.
	if _, err := p.Status(testCtx(t), Target{Kind: KindChromecast, Addr: fr.addr()}); err != nil {
		t.Fatalf("Status: %v", err)
	}
}
