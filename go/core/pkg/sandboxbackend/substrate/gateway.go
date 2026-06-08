package substrate

import (
	"fmt"
	"net/url"
	"strings"
)

// DefaultAtenetRouterURL is the in-cluster HTTP endpoint for Substrate's Envoy router.
const DefaultAtenetRouterURL = "http://atenet-router.ate-system.svc:80"

// GatewayRouterTarget returns the atenet-router reverse-proxy URL and Host header for an actor.
func GatewayRouterTarget(routerURL, actorID string) (*url.URL, string, error) {
	routerURL = strings.TrimSpace(routerURL)
	if routerURL == "" {
		routerURL = DefaultAtenetRouterURL
	}
	actorID = strings.TrimSpace(actorID)
	if actorID == "" {
		return nil, "", fmt.Errorf("actor id is required")
	}
	target, err := url.Parse(routerURL)
	if err != nil {
		return nil, "", fmt.Errorf("parse atenet-router URL %q: %w", routerURL, err)
	}
	if target.Scheme == "" {
		return nil, "", fmt.Errorf("atenet-router URL %q must include a scheme (http or https)", routerURL)
	}
	host := ActorHost(actorID, "")
	return target, host, nil
}
