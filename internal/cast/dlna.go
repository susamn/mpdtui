package cast

import "context"

// dlnaProvider casts to DLNA/UPnP AV renderers (SSDP discovery, SOAP
// AVTransport control). Lands in a follow-up branch.
type dlnaProvider struct {
	cfg Config
}

// newDLNAProvider is always enabled -- DLNA needs no configuration, only
// SSDP multicast on the LAN.
func newDLNAProvider(cfg Config) CastProvider {
	return &dlnaProvider{cfg: cfg}
}

func (p *dlnaProvider) Kind() Kind { return KindDLNA }

func (p *dlnaProvider) Discover(ctx context.Context) ([]Target, error) {
	return nil, nil
}

func (p *dlnaProvider) Play(ctx context.Context, t Target, streamURL string, meta MediaMeta) error {
	return errNotImplemented
}

func (p *dlnaProvider) Stop(ctx context.Context, t Target) error {
	return errNotImplemented
}

func (p *dlnaProvider) Status(ctx context.Context, t Target) (NowCasting, error) {
	return NowCasting{}, errNotImplemented
}
