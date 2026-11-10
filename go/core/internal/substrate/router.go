package substrate

import (
	"fmt"
	"net/url"

	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"

	configv1 "github.com/kagent-dev/kagent/go/api/gen/kagent/config/v1"
)

// RouterTransport uses the same trust bundle and rotating client certificate as
// ate-api for actor ingress, including A2A and sandbox guest calls.
func RouterTransport(cfg *configv1.SubstrateConfig) (string, credentials.TransportCredentials, error) {
	router, err := url.Parse(cfg.GetRouterUrl())
	if err != nil || router.Host == "" {
		return "", nil, fmt.Errorf("invalid Substrate router URL %q", cfg.GetRouterUrl())
	}
	switch router.Scheme {
	case "http":
		return router.Host, insecure.NewCredentials(), nil
	case "https":
		tlsConfig, err := clientTLSConfig(cfg.GetCaFile(), cfg.GetClientCertFile())
		if err != nil {
			return "", nil, err
		}
		tlsConfig.ServerName = router.Hostname()
		return router.Host, credentials.NewTLS(tlsConfig), nil
	default:
		return "", nil, fmt.Errorf("substrate router URL must use http or https")
	}
}
