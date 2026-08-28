package cast

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"net"
	"sync/atomic"
	"time"

	"mpdtui/internal/cast/castproto"
)

// Cast channel namespaces and the default media receiver app id.
const (
	nsConnection = "urn:x-cast:com.google.cast.tp.connection"
	nsHeartbeat  = "urn:x-cast:com.google.cast.tp.heartbeat"
	nsReceiver   = "urn:x-cast:com.google.cast.receiver"
	nsMedia      = "urn:x-cast:com.google.cast.media"

	defaultMediaReceiverAppID = "CC1AD845"

	senderID           = "sender-0"
	platformReceiverID = "receiver-0"

	// castDialTimeout bounds the TCP+TLS connect so an unreachable device
	// (e.g. one behind AP client-isolation) fails in seconds rather than
	// hanging on the caller's full operation budget.
	castDialTimeout = 8 * time.Second
)

// channel is a single TLS connection to a Cast device speaking the
// framed-protobuf channel protocol. Not safe for concurrent use; each
// provider call opens and closes its own.
type channel struct {
	conn   net.Conn
	nextID int64
}

// dialChannel opens the TLS channel and performs the platform-level
// CONNECT. Cast devices present a self-signed certificate, so
// verification is necessarily skipped -- there is no CA to trust for a
// LAN appliance, and the connection carries only playback-control
// messages (a stream URL the device then fetches over plain HTTP anyway),
// not secrets. This is the same posture every Cast sender library takes.
func dialChannel(ctx context.Context, addr string) (*channel, error) {
	d := &tls.Dialer{
		NetDialer: &net.Dialer{Timeout: castDialTimeout},
		Config:    &tls.Config{InsecureSkipVerify: true}, //nolint:gosec // see doc comment
	}
	conn, err := d.DialContext(ctx, "tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("cast dial %s: %w", addr, err)
	}
	ch := &channel{conn: conn}
	// Callers always pass a ctx with a deadline (see the provider
	// methods); use it as the socket deadline so every read/write in the
	// session is bounded even though mDNS gave us no other signal.
	dl, ok := ctx.Deadline()
	if !ok {
		dl = time.Now().Add(castPlayTimeout)
	}
	_ = conn.SetDeadline(dl)

	if err := ch.send(platformReceiverID, nsConnection, map[string]any{
		"type":   "CONNECT",
		"origin": map[string]any{},
	}); err != nil {
		conn.Close()
		return nil, err
	}
	return ch, nil
}

func (ch *channel) close() {
	// Best-effort polite close on the connection namespace.
	_ = ch.send(platformReceiverID, nsConnection, map[string]string{"type": "CLOSE"})
	_ = ch.conn.Close()
}

func (ch *channel) requestID() int {
	return int(atomic.AddInt64(&ch.nextID, 1))
}

// send marshals payload as JSON and writes one frame.
func (ch *channel) send(dest, namespace string, payload any) error {
	data, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	return castproto.WriteFrame(ch.conn, &castproto.CastMessage{
		SourceID:      senderID,
		DestinationID: dest,
		Namespace:     namespace,
		PayloadType:   castproto.PayloadString,
		PayloadUTF8:   string(data),
	})
}

// readUntil reads frames until match returns true for a decoded payload,
// or the deadline/ctx fires. Heartbeat PINGs are answered inline.
func (ch *channel) readUntil(ctx context.Context, match func(msg map[string]any) bool) (map[string]any, error) {
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		frame, err := castproto.ReadFrame(ch.conn)
		if err != nil {
			return nil, err
		}
		if frame.Namespace == nsHeartbeat {
			var hb map[string]any
			if json.Unmarshal([]byte(frame.PayloadUTF8), &hb) == nil && hb["type"] == "PING" {
				_ = ch.send(frame.SourceID, nsHeartbeat, map[string]string{"type": "PONG"})
			}
			continue
		}
		var payload map[string]any
		if err := json.Unmarshal([]byte(frame.PayloadUTF8), &payload); err != nil {
			continue
		}
		if match(payload) {
			return payload, nil
		}
	}
}

// launchMedia makes sure the default media receiver is running and
// returns its transport (session) id.
//
// A device that was asleep answers LAUNCH with a series of
// RECEIVER_STATUS messages over several seconds (waking, switching
// input, ...) and only the last one carries the app and its
// transportId. So this asks for status first (the app may already be
// up), sends LAUNCH if not, and keeps reading RECEIVER_STATUS until one
// has a media-receiver transportId -- bounded by the socket deadline
// dialChannel set from the caller's context.
func (ch *channel) launchMedia(ctx context.Context) (string, error) {
	if err := ch.send(platformReceiverID, nsReceiver, map[string]any{
		"type":      "GET_STATUS",
		"requestId": ch.requestID(),
	}); err != nil {
		return "", err
	}

	sentLaunch := false
	for {
		msg, err := ch.readUntil(ctx, func(m map[string]any) bool {
			t, _ := m["type"].(string)
			return t == "RECEIVER_STATUS" || t == "LAUNCH_ERROR"
		})
		if err != nil {
			return "", fmt.Errorf("waiting for the media receiver to start: %w", err)
		}
		if t, _ := msg["type"].(string); t == "LAUNCH_ERROR" {
			return "", fmt.Errorf("cast device refused to launch the media receiver")
		}
		if id := mediaReceiverTransportID(msg); id != "" {
			return id, nil
		}
		if !sentLaunch {
			sentLaunch = true
			if err := ch.send(platformReceiverID, nsReceiver, map[string]any{
				"type":      "LAUNCH",
				"requestId": ch.requestID(),
				"appId":     defaultMediaReceiverAppID,
			}); err != nil {
				return "", err
			}
		}
	}
}

// mediaReceiverTransportID digs the default media receiver's transportId
// out of a RECEIVER_STATUS payload.
func mediaReceiverTransportID(status map[string]any) string {
	st, _ := status["status"].(map[string]any)
	apps, _ := st["applications"].([]any)
	for _, a := range apps {
		app, _ := a.(map[string]any)
		if app["appId"] == defaultMediaReceiverAppID {
			if id, ok := app["transportId"].(string); ok {
				return id
			}
		}
	}
	return ""
}

// receiverStatus sends GET_STATUS on the receiver namespace and returns
// the decoded RECEIVER_STATUS payload.
func (ch *channel) receiverStatus(ctx context.Context) (map[string]any, error) {
	if err := ch.send(platformReceiverID, nsReceiver, map[string]any{
		"type":      "GET_STATUS",
		"requestId": ch.requestID(),
	}); err != nil {
		return nil, err
	}
	return ch.readUntil(ctx, func(m map[string]any) bool { return m["type"] == "RECEIVER_STATUS" })
}

// mediaSessionID digs the running media receiver's sessionId out of a
// RECEIVER_STATUS payload (needed to STOP it).
func mediaSessionID(status map[string]any) string {
	st, _ := status["status"].(map[string]any)
	apps, _ := st["applications"].([]any)
	for _, a := range apps {
		app, _ := a.(map[string]any)
		if app["appId"] == defaultMediaReceiverAppID {
			if id, ok := app["sessionId"].(string); ok {
				return id
			}
		}
	}
	return ""
}

// loadMedia connects to the media receiver's transport and issues LOAD.
// It returns once the receiver acknowledges with a MEDIA_STATUS, or
// errors on LOAD_FAILED / an invalid request.
func (ch *channel) loadMedia(ctx context.Context, transportID, streamURL string, meta MediaMeta) error {
	if err := ch.send(transportID, nsConnection, map[string]string{"type": "CONNECT"}); err != nil {
		return err
	}
	load := map[string]any{
		"type":      "LOAD",
		"requestId": ch.requestID(),
		"autoplay":  true,
		"media": map[string]any{
			"contentId":   streamURL,
			"streamType":  "LIVE",
			"contentType": "audio/mpeg",
			"metadata": map[string]any{
				"metadataType": 3, // MusicTrackMediaMetadata
				"title":        meta.Title,
				"artist":       meta.Artist,
			},
		},
	}
	if err := ch.send(transportID, nsMedia, load); err != nil {
		return err
	}
	resp, err := ch.readUntil(ctx, func(m map[string]any) bool {
		switch m["type"] {
		case "MEDIA_STATUS", "LOAD_FAILED", "LOAD_CANCELLED", "INVALID_REQUEST":
			return true
		}
		return false
	})
	if err != nil {
		return err
	}
	if t, _ := resp["type"].(string); t != "MEDIA_STATUS" {
		return fmt.Errorf("cast device rejected the stream (%s) -- check the httpd encoder is one it supports (MP3/AAC/FLAC/Vorbis) and the stream URL is reachable from the device", t)
	}
	return nil
}

// mediaContentID connects to the media transport, requests its status and
// returns the contentId the device reports playing (empty if idle).
func (ch *channel) mediaContentID(ctx context.Context, transportID string) (string, error) {
	if err := ch.send(transportID, nsConnection, map[string]string{"type": "CONNECT"}); err != nil {
		return "", err
	}
	if err := ch.send(transportID, nsMedia, map[string]any{
		"type":      "GET_STATUS",
		"requestId": ch.requestID(),
	}); err != nil {
		return "", err
	}
	status, err := ch.readUntil(ctx, func(m map[string]any) bool { return m["type"] == "MEDIA_STATUS" })
	if err != nil {
		return "", err
	}
	list, _ := status["status"].([]any)
	for _, s := range list {
		entry, _ := s.(map[string]any)
		media, _ := entry["media"].(map[string]any)
		if id, ok := media["contentId"].(string); ok {
			return id, nil
		}
	}
	return "", nil
}

// stopSession asks the platform receiver to STOP the running media app,
// then waits briefly for its acknowledging RECEIVER_STATUS so the caller
// doesn't drop the connection before the device has processed the STOP.
// A device that sends no acknowledgement is not an error -- STOP is
// one-way.
func (ch *channel) stopSession(ctx context.Context, sessionID string) error {
	if err := ch.send(platformReceiverID, nsReceiver, map[string]any{
		"type":      "STOP",
		"requestId": ch.requestID(),
		"sessionId": sessionID,
	}); err != nil {
		return err
	}
	_ = ch.conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	_, _ = ch.readUntil(ctx, func(m map[string]any) bool {
		return m["type"] == "RECEIVER_STATUS"
	})
	return nil
}
