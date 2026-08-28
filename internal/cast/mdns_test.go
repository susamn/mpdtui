package cast

import (
	"encoding/binary"
	"testing"
)

// dnsBuilder assembles a DNS/mDNS message for tests.
type dnsBuilder struct {
	an   int
	body []byte
}

func (d *dnsBuilder) name(s string) []byte { return appendDNSName(nil, s) }

func (d *dnsBuilder) rr(name string, typ uint16, rdata []byte) {
	d.body = append(d.body, appendDNSName(nil, name)...)
	var h [10]byte
	binary.BigEndian.PutUint16(h[0:2], typ)
	binary.BigEndian.PutUint16(h[2:4], 1) // class IN
	binary.BigEndian.PutUint32(h[4:8], 120)
	binary.BigEndian.PutUint16(h[8:10], uint16(len(rdata)))
	d.body = append(d.body, h[:]...)
	d.body = append(d.body, rdata...)
	d.an++
}

func (d *dnsBuilder) packet() []byte {
	hdr := make([]byte, 12)
	binary.BigEndian.PutUint16(hdr[0:2], 0)
	binary.BigEndian.PutUint16(hdr[2:4], 0x8400) // response, authoritative
	binary.BigEndian.PutUint16(hdr[6:8], uint16(d.an))
	return append(hdr, d.body...)
}

func srvRData(port int, target string) []byte {
	rd := make([]byte, 6)
	binary.BigEndian.PutUint16(rd[4:6], uint16(port))
	return append(rd, appendDNSName(nil, target)...)
}

func txtRData(entries ...string) []byte {
	var rd []byte
	for _, e := range entries {
		rd = append(rd, byte(len(e)))
		rd = append(rd, e...)
	}
	return rd
}

func TestMergeResponseAssemblesDevice(t *testing.T) {
	const instance = "Chromecast-1234._googlecast._tcp.local."
	const host = "abcd.local."

	b := &dnsBuilder{}
	b.rr(castService, typePTR, appendDNSName(nil, instance))
	b.rr(instance, typeSRV, srvRData(8009, host))
	b.rr(instance, typeTXT, txtRData("id=deadbeef", "fn=Living Room TV", "md=Chromecast Ultra"))
	b.rr(host, typeA, []byte{192, 168, 1, 42})

	m := map[string]*castDevice{}
	mergeResponse(b.packet(), m)

	got := completeDevices(m)
	if len(got) != 1 {
		t.Fatalf("got %d devices, want 1: %+v", len(got), got)
	}
	d := got[0]
	if d.id != "deadbeef" || d.name != "Living Room TV" || d.model != "Chromecast Ultra" {
		t.Errorf("TXT not applied: %+v", d)
	}
	if d.host != "192.168.1.42" || d.port != 8009 {
		t.Errorf("host/port = %s:%d, want 192.168.1.42:8009", d.host, d.port)
	}

	tg := d.target()
	if tg.Kind != KindChromecast || tg.ID != "deadbeef" || tg.Name != "Living Room TV" || tg.Addr != "192.168.1.42:8009" {
		t.Errorf("target() = %+v", tg)
	}
}

func TestMergeResponseAccumulatesAcrossPackets(t *testing.T) {
	const instance = "Nest._googlecast._tcp.local."
	const host = "nest.local."
	m := map[string]*castDevice{}

	p1 := &dnsBuilder{}
	p1.rr(instance, typeSRV, srvRData(8009, host))
	mergeResponse(p1.packet(), m)
	if len(completeDevices(m)) != 0 {
		t.Fatal("device reported complete with only an SRV record")
	}

	p2 := &dnsBuilder{}
	p2.rr(instance, typeTXT, txtRData("fn=Nest Mini"))
	p2.rr(host, typeA, []byte{10, 0, 0, 5})
	mergeResponse(p2.packet(), m)

	got := completeDevices(m)
	if len(got) != 1 || got[0].host != "10.0.0.5" || got[0].name != "Nest Mini" {
		t.Fatalf("accumulation failed: %+v", got)
	}
}

func TestDecodeNameFollowsCompressionPointer(t *testing.T) {
	// "local." at offset 12, then "_tcp" + pointer to offset 12.
	msg := make([]byte, 12)
	msg = append(msg, 5, 'l', 'o', 'c', 'a', 'l', 0) // offset 12: "local."
	start := len(msg)
	msg = append(msg, 4, '_', 't', 'c', 'p')
	msg = append(msg, 0xc0, 12) // pointer -> "local."

	name, next, ok := decodeName(msg, start)
	if !ok || name != "_tcp.local." {
		t.Fatalf("decodeName = %q, %v", name, ok)
	}
	if next != len(msg) {
		t.Errorf("next = %d, want %d (just past the pointer)", next, len(msg))
	}
}

func TestDecodeNameRejectsPointerLoop(t *testing.T) {
	msg := make([]byte, 14)
	msg[12], msg[13] = 0xc0, 12 // pointer to itself
	if _, _, ok := decodeName(msg, 12); ok {
		t.Fatal("decodeName accepted a self-referential pointer loop")
	}
}

func TestBuildPTRQueryShape(t *testing.T) {
	q := buildPTRQuery(castService, false)
	if binary.BigEndian.Uint16(q[4:6]) != 1 {
		t.Errorf("qdcount = %d, want 1", binary.BigEndian.Uint16(q[4:6]))
	}
	// last 4 bytes: QTYPE PTR (12), QCLASS IN (1)
	tail := q[len(q)-4:]
	if binary.BigEndian.Uint16(tail[0:2]) != 12 || binary.BigEndian.Uint16(tail[2:4]) != 1 {
		t.Errorf("question trailer = %v, want QTYPE 12 QCLASS 1", tail)
	}

	// With the unicast-response bit, QCLASS has its top bit set.
	qu := buildPTRQuery(castService, true)
	if got := binary.BigEndian.Uint16(qu[len(qu)-2:]); got != 0x8001 {
		t.Errorf("QU QCLASS = %#x, want 0x8001", got)
	}
}
