package cast

import (
	"context"
	"encoding/binary"
	"fmt"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"
)

// castService is the DNS-SD service name Chromecast / Nest devices
// advertise themselves under.
const castService = "_googlecast._tcp.local."

// mdnsAddr is the IPv4 multicast group and port for mDNS (RFC 6762).
var mdnsAddr = &net.UDPAddr{IP: net.IPv4(224, 0, 0, 251), Port: 5353}

// castDevice is one discovered Cast receiver, assembled from the PTR /
// SRV / TXT / A records in one or more mDNS responses.
type castDevice struct {
	instance string // full "<name>._googlecast._tcp.local." (PTR target)
	id       string // TXT "id=" -- the stable device UUID
	name     string // TXT "fn=" -- friendly name
	model    string // TXT "md=" -- model, e.g. "Chromecast", "Google Nest Mini"
	srvHost  string // SRV target hostname (matched against A record names)
	host     string // A record address
	port     int    // SRV port
}

func (d castDevice) target() Target {
	name := d.name
	if name == "" {
		name = strings.SplitN(d.instance, ".", 2)[0]
	}
	id := d.id
	if id == "" {
		id = d.instance
	}
	return Target{
		Kind:  KindChromecast,
		ID:    id,
		Name:  name,
		Addr:  net.JoinHostPort(d.host, strconv.Itoa(d.port)),
		Model: d.model,
	}
}

// discoverCast performs an mDNS browse for Cast devices, collecting
// responses until ctx expires. It is deliberately self-contained -- a
// small hand-written DNS-SD query rather than a general mDNS dependency:
// the query is a single well-known PTR and the only response records
// needed are PTR/SRV/TXT/A.
//
// mDNS responders answer via multicast, so we join the 224.0.0.251:5353
// group (per interface -- a machine with several NICs, or docker0
// alongside wlan, otherwise listens on the wrong one) and read the
// group. The query also sets the unicast-response bit so the first reply
// comes straight back to the sending socket, which is faster and works
// even where multicast receive is flaky.
func discoverCast(ctx context.Context) ([]castDevice, error) {
	deadline, ok := ctx.Deadline()
	if !ok {
		deadline = time.Now().Add(defaultDiscoveryTimeout)
	}

	var conns []*net.UDPConn

	// A dedicated unicast socket on an ephemeral port: the query is sent
	// from here with the unicast-response bit set, so the first reply
	// comes straight back here -- this is the path that works even when a
	// system mDNS daemon (avahi, etc.) is also bound to port 5353 and the
	// kernel hands it the multicast traffic instead of us.
	if uc, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4zero, Port: 0}); err == nil {
		_ = uc.SetReadDeadline(deadline)
		conns = append(conns, uc)
	}

	// Plus the multicast group, joined per interface, to catch responders
	// that answer multicast regardless.
	ifaces := multicastInterfaces()
	for i := range ifaces {
		if c, err := net.ListenMulticastUDP("udp4", &ifaces[i], mdnsAddr); err == nil {
			_ = c.SetReadDeadline(deadline)
			conns = append(conns, c)
		}
	}
	if len(conns) == 0 {
		return nil, fmt.Errorf("mdns: no usable socket")
	}
	defer func() {
		for _, c := range conns {
			c.Close()
		}
	}()

	quQuery := buildPTRQuery(castService, true)
	mcQuery := buildPTRQuery(castService, false)
	sendAll := func() {
		for _, c := range conns {
			q := mcQuery
			// The first socket is the ephemeral unicast one -- ask for a
			// unicast reply there.
			if la, ok := c.LocalAddr().(*net.UDPAddr); ok && la.Port != mdnsAddr.Port {
				q = quQuery
			}
			_, _ = c.WriteToUDP(q, mdnsAddr)
		}
	}
	sendAll()
	go func() {
		for _, d := range []time.Duration{200 * time.Millisecond, 700 * time.Millisecond} {
			select {
			case <-time.After(d):
				sendAll()
			case <-ctx.Done():
				return
			}
		}
	}()

	byInstance := map[string]*castDevice{}
	var mu sync.Mutex
	var wg sync.WaitGroup
	for _, c := range conns {
		wg.Add(1)
		go func(c *net.UDPConn) {
			defer wg.Done()
			buf := make([]byte, 9000)
			for {
				n, _, err := c.ReadFromUDP(buf)
				if err != nil {
					return // deadline or closed
				}
				mu.Lock()
				mergeResponse(append([]byte(nil), buf[:n]...), byInstance)
				mu.Unlock()
			}
		}(c)
	}
	wg.Wait()

	mu.Lock()
	defer mu.Unlock()
	return completeDevices(byInstance), nil
}

// multicastInterfaces returns the up, multicast-capable, non-loopback
// interfaces that have an IPv4 address -- the ones an mDNS join has any
// chance of hearing a Cast device on.
func multicastInterfaces() []net.Interface {
	all, err := net.Interfaces()
	if err != nil {
		return nil
	}
	var out []net.Interface
	for _, ifi := range all {
		if ifi.Flags&net.FlagUp == 0 || ifi.Flags&net.FlagMulticast == 0 || ifi.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, err := ifi.Addrs()
		if err != nil {
			continue
		}
		for _, a := range addrs {
			if ipn, ok := a.(*net.IPNet); ok && ipn.IP.To4() != nil {
				out = append(out, ifi)
				break
			}
		}
	}
	return out
}

func completeDevices(m map[string]*castDevice) []castDevice {
	out := make([]castDevice, 0, len(m))
	for _, d := range m {
		if d.host == "" || d.port == 0 {
			continue // never got the SRV/A pair
		}
		out = append(out, *d)
	}
	return out
}

// buildPTRQuery builds a minimal mDNS query: one question, QTYPE PTR,
// QCLASS IN, id 0 (mDNS ignores it). When unicastResponse is set, the
// QU bit (top bit of QCLASS) asks responders to reply unicast to the
// sender rather than multicast.
func buildPTRQuery(name string, unicastResponse bool) []byte {
	b := make([]byte, 12)
	binary.BigEndian.PutUint16(b[4:6], 1) // qdcount
	b = appendDNSName(b, name)
	qclass := uint16(1) // IN
	if unicastResponse {
		qclass |= 0x8000
	}
	b = append(b, 0, 12) // QTYPE PTR
	b = binary.BigEndian.AppendUint16(b, qclass)
	return b
}

func appendDNSName(b []byte, name string) []byte {
	for _, label := range strings.Split(strings.TrimSuffix(name, "."), ".") {
		if label == "" {
			continue
		}
		b = append(b, byte(len(label)))
		b = append(b, label...)
	}
	return append(b, 0)
}

// DNS record type numbers.
const (
	typeA   = 1
	typePTR = 12
	typeTXT = 16
	typeSRV = 33
)

// mergeResponse parses one mDNS response and folds its records into m,
// keyed by Cast service instance name. mDNS often splits SRV/TXT/A across
// packets, so entries accumulate across calls.
func mergeResponse(pkt []byte, m map[string]*castDevice) {
	p := &dnsParser{msg: pkt}
	hdr, ok := p.header()
	if !ok {
		return
	}
	for i := 0; i < hdr.qdcount; i++ {
		if !p.skipQuestion() {
			return
		}
	}

	total := hdr.ancount + hdr.nscount + hdr.arcount
	aRecords := map[string]string{} // hostname -> IPv4, applied after all SRVs are known

	for i := 0; i < total; i++ {
		rr, ok := p.record()
		if !ok {
			break
		}
		switch rr.typ {
		case typePTR:
			if !strings.HasSuffix(rr.name, castService) {
				continue
			}
			if target, _, ok := decodeName(pkt, rr.rdataOff); ok {
				getDevice(m, target)
			}
		case typeSRV:
			if len(rr.rdata) < 6 || !strings.HasSuffix(rr.name, castService) {
				continue
			}
			d := getDevice(m, rr.name)
			d.port = int(binary.BigEndian.Uint16(rr.rdata[4:6]))
			if host, _, ok := decodeName(pkt, rr.rdataOff+6); ok {
				d.srvHost = host
			}
		case typeTXT:
			if !strings.HasSuffix(rr.name, castService) {
				continue
			}
			applyTXT(getDevice(m, rr.name), parseTXT(rr.rdata))
		case typeA:
			if len(rr.rdata) == 4 {
				aRecords[rr.name] = net.IP(rr.rdata).String()
			}
		}
	}

	// Attribute A records to any device whose SRV target matches -- across
	// this packet and earlier ones (d.srvHost persists on the device).
	for _, d := range m {
		if ip, ok := aRecords[d.srvHost]; ok && d.srvHost != "" {
			d.host = ip
		}
	}
}

func getDevice(m map[string]*castDevice, instance string) *castDevice {
	d, ok := m[instance]
	if !ok {
		d = &castDevice{instance: instance}
		m[instance] = d
	}
	return d
}

func applyTXT(d *castDevice, kv map[string]string) {
	if v := kv["id"]; v != "" {
		d.id = v
	}
	if v := kv["fn"]; v != "" {
		d.name = v
	}
	if v := kv["md"]; v != "" {
		d.model = v
	}
}

func parseTXT(rdata []byte) map[string]string {
	kv := map[string]string{}
	for len(rdata) > 0 {
		n := int(rdata[0])
		rdata = rdata[1:]
		if n > len(rdata) {
			break
		}
		if k, v, ok := strings.Cut(string(rdata[:n]), "="); ok {
			kv[k] = v
		}
		rdata = rdata[n:]
	}
	return kv
}

// --- minimal DNS wire parser ---

type dnsHeader struct{ qdcount, ancount, nscount, arcount int }

type dnsRecord struct {
	name     string
	typ      uint16
	rdata    []byte
	rdataOff int // offset of rdata within the full message (for name decompression)
}

type dnsParser struct {
	msg []byte
	pos int
}

func (p *dnsParser) header() (dnsHeader, bool) {
	if len(p.msg) < 12 {
		return dnsHeader{}, false
	}
	p.pos = 12
	return dnsHeader{
		qdcount: int(binary.BigEndian.Uint16(p.msg[4:6])),
		ancount: int(binary.BigEndian.Uint16(p.msg[6:8])),
		nscount: int(binary.BigEndian.Uint16(p.msg[8:10])),
		arcount: int(binary.BigEndian.Uint16(p.msg[10:12])),
	}, true
}

func (p *dnsParser) skipQuestion() bool {
	if _, ok := p.readName(); !ok {
		return false
	}
	if p.pos+4 > len(p.msg) {
		return false
	}
	p.pos += 4
	return true
}

func (p *dnsParser) record() (dnsRecord, bool) {
	name, ok := p.readName()
	if !ok || p.pos+10 > len(p.msg) {
		return dnsRecord{}, false
	}
	typ := binary.BigEndian.Uint16(p.msg[p.pos : p.pos+2])
	rdlen := int(binary.BigEndian.Uint16(p.msg[p.pos+8 : p.pos+10]))
	p.pos += 10
	if p.pos+rdlen > len(p.msg) {
		return dnsRecord{}, false
	}
	rr := dnsRecord{name: name, typ: typ, rdata: p.msg[p.pos : p.pos+rdlen], rdataOff: p.pos}
	p.pos += rdlen
	return rr, true
}

func (p *dnsParser) readName() (string, bool) {
	name, newPos, ok := decodeName(p.msg, p.pos)
	if !ok {
		return "", false
	}
	p.pos = newPos
	return name, true
}

const maxNameHops = 128

// decodeName decodes a DNS name at pos, following compression pointers.
// Returns the name, the position immediately after the name in the record
// stream (not the pointer target), and ok.
func decodeName(msg []byte, pos int) (string, int, bool) {
	var labels []string
	hops := 0
	jumped := false
	next := pos

	for {
		if pos < 0 || pos >= len(msg) {
			return "", 0, false
		}
		b := msg[pos]
		switch {
		case b == 0:
			pos++
			if !jumped {
				next = pos
			}
			return strings.Join(labels, ".") + ".", next, true
		case b&0xc0 == 0xc0:
			if pos+1 >= len(msg) {
				return "", 0, false
			}
			ptr := int(binary.BigEndian.Uint16(msg[pos:pos+2]) & 0x3fff)
			if !jumped {
				next = pos + 2
			}
			jumped = true
			if hops++; hops > maxNameHops {
				return "", 0, false
			}
			pos = ptr
		default:
			n := int(b)
			pos++
			if pos+n > len(msg) {
				return "", 0, false
			}
			labels = append(labels, string(msg[pos:pos+n]))
			pos += n
		}
	}
}
