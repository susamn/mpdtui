package cast

import "context"

// homeAssistantProvider casts to Home Assistant media_player entities via
// the HA REST API. Lands in a follow-up branch.
type homeAssistantProvider struct {
	baseURL string
	token   string
}

// newHomeAssistantProvider returns nil (provider disabled) unless both
// ha_url and ha_token are configured.
func newHomeAssistantProvider(cfg Config) CastProvider {
	if cfg.HABaseURL == "" || cfg.HAToken == "" {
		return nil
	}
	return &homeAssistantProvider{baseURL: cfg.HABaseURL, token: cfg.HAToken}
}

func (p *homeAssistantProvider) Kind() Kind { return KindHomeAssistant }

func (p *homeAssistantProvider) Discover(ctx context.Context) ([]Target, error) {
	return nil, nil
}

func (p *homeAssistantProvider) Play(ctx context.Context, t Target, streamURL string, meta MediaMeta) error {
	return errNotImplemented
}

func (p *homeAssistantProvider) Stop(ctx context.Context, t Target) error {
	return errNotImplemented
}

func (p *homeAssistantProvider) Status(ctx context.Context, t Target) (NowCasting, error) {
	return NowCasting{}, errNotImplemented
}
