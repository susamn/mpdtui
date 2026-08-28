package cast

import (
	"context"
	"fmt"
	"time"
)

// Per-operation ceilings. Play is generous because a device that was
// asleep takes several seconds to wake, switch input, and start the
// media receiver before it will accept a LOAD.
const (
	castPlayTimeout    = 45 * time.Second
	castControlTimeout = 15 * time.Second
)

// chromecastProvider casts to Google Cast / Nest devices. Discovery is a
// hand-rolled mDNS browse (_googlecast._tcp, see mdns.go); control is a
// TLS socket speaking the Cast protobuf channel protocol (see
// chromecast_channel.go).
type chromecastProvider struct {
	cfg Config
}

func newChromecastProvider(cfg Config) CastProvider {
	return &chromecastProvider{cfg: cfg}
}

func (p *chromecastProvider) Kind() Kind { return KindChromecast }

func (p *chromecastProvider) Discover(ctx context.Context) ([]Target, error) {
	devices, err := discoverCast(ctx)
	if err != nil {
		return nil, err
	}
	targets := make([]Target, 0, len(devices))
	for _, d := range devices {
		targets = append(targets, d.target())
	}
	return targets, nil
}

func (p *chromecastProvider) Play(ctx context.Context, t Target, streamURL string, meta MediaMeta) error {
	ctx, cancel := context.WithTimeout(ctx, castPlayTimeout)
	defer cancel()

	ch, err := dialChannel(ctx, t.Addr)
	if err != nil {
		return err
	}
	defer ch.close()

	transportID, err := ch.launchMedia(ctx)
	if err != nil {
		return err
	}
	return ch.loadMedia(ctx, transportID, streamURL, meta)
}

func (p *chromecastProvider) Stop(ctx context.Context, t Target) error {
	ctx, cancel := context.WithTimeout(ctx, castControlTimeout)
	defer cancel()

	ch, err := dialChannel(ctx, t.Addr)
	if err != nil {
		return err
	}
	defer ch.close()

	status, err := ch.receiverStatus(ctx)
	if err != nil {
		return err
	}
	sessionID := mediaSessionID(status)
	if sessionID == "" {
		return nil // nothing running to stop
	}
	return ch.stopSession(ctx, sessionID)
}

func (p *chromecastProvider) Status(ctx context.Context, t Target) (NowCasting, error) {
	ctx, cancel := context.WithTimeout(ctx, castControlTimeout)
	defer cancel()

	ch, err := dialChannel(ctx, t.Addr)
	if err != nil {
		return NowCasting{}, err
	}
	defer ch.close()

	status, err := ch.receiverStatus(ctx)
	if err != nil {
		return NowCasting{}, err
	}
	transportID := mediaReceiverTransportID(status)
	if transportID == "" {
		return NowCasting{Active: false}, nil
	}
	contentID, err := ch.mediaContentID(ctx, transportID)
	if err != nil {
		return NowCasting{}, err
	}
	return NowCasting{Active: contentID != "", MediaURL: contentID}, nil
}

// ensure the interface stays satisfied
var _ CastProvider = (*chromecastProvider)(nil)

// errNotImplemented is still used by the homeassistant/dlna stubs.
var errNotImplemented = fmt.Errorf("cast provider not implemented yet")
