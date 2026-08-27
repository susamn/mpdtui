package castproto

import (
	"bytes"
	"reflect"
	"testing"
)

func TestMarshalKnownBytes(t *testing.T) {
	m := &CastMessage{
		ProtocolVersion: 0,
		SourceID:        "sender-0",
		DestinationID:   "receiver-0",
		Namespace:       "urn:x-cast:com.google.cast.tp.connection",
		PayloadType:     PayloadString,
		PayloadUTF8:     `{"type":"CONNECT"}`,
	}
	got := Marshal(m)

	// Built by hand from the wire format: field 1 varint 0; fields 2/3/4/6
	// length-delimited; field 5 varint 0.
	var want []byte
	want = append(want, 0x08, 0x00) // field 1 = 0
	want = append(want, 0x12, byte(len("sender-0")))
	want = append(want, "sender-0"...)
	want = append(want, 0x1a, byte(len("receiver-0")))
	want = append(want, "receiver-0"...)
	ns := "urn:x-cast:com.google.cast.tp.connection"
	want = append(want, 0x22, byte(len(ns)))
	want = append(want, ns...)
	want = append(want, 0x28, 0x00) // field 5 = 0 (STRING)
	pl := `{"type":"CONNECT"}`
	want = append(want, 0x32, byte(len(pl)))
	want = append(want, pl...)

	if !bytes.Equal(got, want) {
		t.Fatalf("Marshal mismatch\n got %x\nwant %x", got, want)
	}
}

func TestRoundTrip(t *testing.T) {
	cases := []*CastMessage{
		{SourceID: "a", DestinationID: "b", Namespace: "ns", PayloadUTF8: "hello"},
		{SourceID: "s", DestinationID: "*", Namespace: "urn:x-cast:com.google.cast.media", PayloadType: PayloadString, PayloadUTF8: `{"type":"LOAD","media":{"contentId":"http://x:8000/"}}`},
		{ProtocolVersion: 0, PayloadType: PayloadBinary, PayloadBinary: []byte{0x00, 0x01, 0xff}},
	}
	for i, in := range cases {
		out, err := Unmarshal(Marshal(in))
		if err != nil {
			t.Fatalf("case %d: Unmarshal: %v", i, err)
		}
		if !reflect.DeepEqual(in, out) {
			t.Errorf("case %d round-trip mismatch\n in %+v\nout %+v", i, in, out)
		}
	}
}

func TestUnmarshalSkipsUnknownFields(t *testing.T) {
	// A real message with an extra unknown field 9 (varint) and field 10
	// (bytes) spliced in -- decoding must ignore them, not error.
	base := Marshal(&CastMessage{SourceID: "x", Namespace: "ns", PayloadUTF8: "p"})
	withUnknown := append([]byte{}, base...)
	withUnknown = append(withUnknown, 0x48, 0x2a)             // field 9 varint = 42
	withUnknown = append(withUnknown, 0x52, 0x02, 0xaa, 0xbb) // field 10 bytes

	m, err := Unmarshal(withUnknown)
	if err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if m.SourceID != "x" || m.Namespace != "ns" || m.PayloadUTF8 != "p" {
		t.Errorf("known fields not decoded through unknowns: %+v", m)
	}
}

func TestFrameRoundTrip(t *testing.T) {
	var buf bytes.Buffer
	in := &CastMessage{SourceID: "s", DestinationID: "d", Namespace: "ns", PayloadUTF8: "payload"}
	if err := WriteFrame(&buf, in); err != nil {
		t.Fatalf("WriteFrame: %v", err)
	}
	if buf.Len() != 4+len(Marshal(in)) {
		t.Errorf("framed length = %d, want 4 + payload", buf.Len())
	}
	out, err := ReadFrame(&buf)
	if err != nil {
		t.Fatalf("ReadFrame: %v", err)
	}
	if !reflect.DeepEqual(in, out) {
		t.Errorf("frame round-trip mismatch\n in %+v\nout %+v", in, out)
	}
}

func TestReadFrameRejectsOversizeLength(t *testing.T) {
	_, err := ReadFrame(bytes.NewReader([]byte{0xff, 0xff, 0xff, 0xff}))
	if err == nil {
		t.Fatal("ReadFrame accepted a 4GB frame length")
	}
}

func TestUnmarshalTruncated(t *testing.T) {
	if _, err := Unmarshal([]byte{0x12, 0x05, 'a', 'b'}); err == nil {
		t.Fatal("Unmarshal accepted a string field shorter than its declared length")
	}
}
