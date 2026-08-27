// Package castproto implements just enough of the Google Cast channel
// protocol's CastMessage to open a connection and control the default
// media receiver.
//
// The full protocol defines CastMessage in a .proto file and ships a
// generated Go binding. That message has six fields we ever set, all
// scalar, and the protocol version is frozen -- so rather than take on
// google.golang.org/protobuf (a large module) and a code-generation
// step for one tiny fixed message, this hand-encodes the protobuf wire
// format directly. The wire format is simple and stable; Marshal/Unmarshal
// here are round-trip tested against known byte sequences.
//
// Wire reference: https://developers.google.com/cast/docs/media (channel
// protocol) and the CastMessage definition in the Chromium source
// (components/cast_channel/proto/cast_channel.proto).
package castproto

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
)

// PayloadType mirrors CastMessage.PayloadType. Only STRING is used here
// (every namespace we talk to exchanges JSON), but BINARY is decoded so
// an unexpected binary frame is reported rather than silently mangled.
type PayloadType int32

const (
	PayloadString PayloadType = 0
	PayloadBinary PayloadType = 1
)

// CastMessage is one frame on a Cast channel.
type CastMessage struct {
	// ProtocolVersion is always 0 (CASTV2_1_0); kept as a field so Marshal
	// emits it explicitly, which the receiver expects.
	ProtocolVersion int32
	SourceID        string
	DestinationID   string
	Namespace       string
	PayloadType     PayloadType
	PayloadUTF8     string
	PayloadBinary   []byte
}

// Field numbers from cast_channel.proto.
const (
	fieldProtocolVersion = 1
	fieldSourceID        = 2
	fieldDestinationID   = 3
	fieldNamespace       = 4
	fieldPayloadType     = 5
	fieldPayloadUTF8     = 6
	fieldPayloadBinary   = 7
)

const (
	wireVarint = 0
	wireBytes  = 2
)

// Marshal encodes m in protobuf wire format. Fields are emitted in field-
// number order. protocol_version and payload_type are always written
// (they're required in the proto); the payload fields are written when
// non-empty.
func Marshal(m *CastMessage) []byte {
	var b []byte
	b = appendVarintField(b, fieldProtocolVersion, uint64(m.ProtocolVersion))
	b = appendStringField(b, fieldSourceID, m.SourceID)
	b = appendStringField(b, fieldDestinationID, m.DestinationID)
	b = appendStringField(b, fieldNamespace, m.Namespace)
	b = appendVarintField(b, fieldPayloadType, uint64(m.PayloadType))
	if m.PayloadUTF8 != "" {
		b = appendStringField(b, fieldPayloadUTF8, m.PayloadUTF8)
	}
	if len(m.PayloadBinary) > 0 {
		b = appendBytesField(b, fieldPayloadBinary, m.PayloadBinary)
	}
	return b
}

var errTruncated = errors.New("castproto: truncated message")

// Unmarshal decodes a protobuf-wire CastMessage. Unknown fields are
// skipped so a protocol extension doesn't break decoding.
func Unmarshal(data []byte) (*CastMessage, error) {
	m := &CastMessage{}
	for len(data) > 0 {
		tag, n := binary.Uvarint(data)
		if n <= 0 {
			return nil, errTruncated
		}
		data = data[n:]
		field := tag >> 3
		wire := tag & 0x7

		switch wire {
		case wireVarint:
			v, n := binary.Uvarint(data)
			if n <= 0 {
				return nil, errTruncated
			}
			data = data[n:]
			switch field {
			case fieldProtocolVersion:
				m.ProtocolVersion = int32(v)
			case fieldPayloadType:
				m.PayloadType = PayloadType(v)
			}
		case wireBytes:
			l, n := binary.Uvarint(data)
			if n <= 0 {
				return nil, errTruncated
			}
			data = data[n:]
			if uint64(len(data)) < l {
				return nil, errTruncated
			}
			val := data[:l]
			data = data[l:]
			switch field {
			case fieldSourceID:
				m.SourceID = string(val)
			case fieldDestinationID:
				m.DestinationID = string(val)
			case fieldNamespace:
				m.Namespace = string(val)
			case fieldPayloadUTF8:
				m.PayloadUTF8 = string(val)
			case fieldPayloadBinary:
				m.PayloadBinary = append([]byte(nil), val...)
			}
		default:
			return nil, fmt.Errorf("castproto: unsupported wire type %d for field %d", wire, field)
		}
	}
	return m, nil
}

// WriteFrame writes m to w with the 4-byte big-endian length prefix the
// Cast channel uses to delimit frames.
func WriteFrame(w io.Writer, m *CastMessage) error {
	payload := Marshal(m)
	var hdr [4]byte
	binary.BigEndian.PutUint32(hdr[:], uint32(len(payload)))
	if _, err := w.Write(hdr[:]); err != nil {
		return err
	}
	_, err := w.Write(payload)
	return err
}

// ReadFrame reads one length-prefixed CastMessage from r.
func ReadFrame(r io.Reader) (*CastMessage, error) {
	var hdr [4]byte
	if _, err := io.ReadFull(r, hdr[:]); err != nil {
		return nil, err
	}
	n := binary.BigEndian.Uint32(hdr[:])
	if n > maxFrameSize {
		return nil, fmt.Errorf("castproto: frame of %d bytes exceeds %d limit", n, maxFrameSize)
	}
	buf := make([]byte, n)
	if _, err := io.ReadFull(r, buf); err != nil {
		return nil, err
	}
	return Unmarshal(buf)
}

// maxFrameSize guards against a malformed length prefix asking for a
// huge allocation. Cast control messages are small (a few KB at most).
const maxFrameSize = 1 << 20

func appendVarintField(b []byte, field int, v uint64) []byte {
	b = binary.AppendUvarint(b, uint64(field)<<3|wireVarint)
	return binary.AppendUvarint(b, v)
}

func appendStringField(b []byte, field int, s string) []byte {
	return appendBytesField(b, field, []byte(s))
}

func appendBytesField(b []byte, field int, v []byte) []byte {
	b = binary.AppendUvarint(b, uint64(field)<<3|wireBytes)
	b = binary.AppendUvarint(b, uint64(len(v)))
	return append(b, v...)
}
