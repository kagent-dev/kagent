package egresspolicy

import (
	"fmt"
	"strings"
)

const ActorIdentityHeader = "x-substrate-actor-token"

// WithActorIdentity reserves a placeholder for the host-signed, origin-bound Actor JWT.
func WithActorIdentity(headers map[string]string) (map[string]string, error) {
	for name := range headers {
		if strings.EqualFold(name, ActorIdentityHeader) {
			return nil, fmt.Errorf("actor identity header is reserved")
		}
	}
	if headers == nil {
		headers = make(map[string]string, 1)
	}
	headers[ActorIdentityHeader] = "required"
	return headers, nil
}
