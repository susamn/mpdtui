package cast

import (
	"context"
	"errors"
)

// errNotImplemented is returned by provider methods that aren't wired up
// yet. Chromecast lands in a follow-up commit; the interface and the
// Manager lifecycle are in place first.
var errNotImplemented = errors.New("cast provider not implemented yet")

// chromecastProvider casts to Google Cast / Nest devices. Discovery is
// mDNS (_googlecast._tcp); control is a TLS socket speaking the Cast
// protobuf channel protocol.
type chromecastProvider struct {
	cfg Config
}

func newChromecastProvider(cfg Config) CastProvider {
	return &chromecastProvider{cfg: cfg}
}

func (p *chromecastProvider) Kind() Kind { return KindChromecast }

func (p *chromecastProvider) Discover(ctx context.Context) ([]Target, error) {
	return nil, nil
}

func (p *chromecastProvider) Play(ctx context.Context, t Target, streamURL string, meta MediaMeta) error {
	return errNotImplemented
}

func (p *chromecastProvider) Stop(ctx context.Context, t Target) error {
	return errNotImplemented
}

func (p *chromecastProvider) Status(ctx context.Context, t Target) (NowCasting, error) {
	return NowCasting{}, errNotImplemented
}
