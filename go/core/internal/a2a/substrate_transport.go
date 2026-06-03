package a2a

import (
	"net/http"
	"net/url"
	"strings"
)

// substrateAgentRoundTripper proxies A2A HTTP to an agent actor via atenet-router using Host routing.
type substrateAgentRoundTripper struct {
	router   *url.URL
	actorHost string
	base     http.RoundTripper
}

func newSubstrateAgentRoundTripper(routerURL, actorHost string, base http.RoundTripper) (http.RoundTripper, error) {
	routerURL = strings.TrimSpace(routerURL)
	if routerURL == "" {
		routerURL = "http://atenet-router.ate-system.svc:80"
	}
	u, err := url.Parse(routerURL)
	if err != nil {
		return nil, err
	}
	if base == nil {
		base = http.DefaultTransport
	}
	return &substrateAgentRoundTripper{router: u, actorHost: strings.TrimSpace(actorHost), base: base}, nil
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
