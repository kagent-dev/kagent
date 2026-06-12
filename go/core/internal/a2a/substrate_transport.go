package a2a

import (
	"net/http"
	"net/url"

	"github.com/kagent-dev/kagent/go/core/pkg/sandboxbackend/substrate"
)

// substrateAgentRoundTripper proxies A2A HTTP to an agent actor via atenet-router using Host routing.
type substrateAgentRoundTripper struct {
	router    *url.URL
	actorHost string
	base      http.RoundTripper
}

func newSubstrateAgentRoundTripper(routerURL, actorID string, base http.RoundTripper) (http.RoundTripper, error) {
	target, host, err := substrate.GatewayRouterTarget(routerURL, actorID)
	if err != nil {
		return nil, err
	}
	if base == nil {
		base = http.DefaultTransport
	}
	return &substrateAgentRoundTripper{router: target, actorHost: host, base: base}, nil
}

func (t *substrateAgentRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	if t == nil || t.router == nil {
		return nil, http.ErrSkipAltProtocol
	}
	req = req.Clone(req.Context())
	req.URL.Scheme = t.router.Scheme
	req.URL.Host = t.router.Host
	if t.actorHost != "" {
		req.Host = t.actorHost
	}
	return t.base.RoundTrip(req)
}
