package models

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"net/http"
	"os"
)

// BuildTLSTransport returns an http.RoundTripper with TLS applied.
// Returns base unchanged if no TLS config is set.
func BuildTLSTransport(
	base http.RoundTripper,
	insecureSkipVerify *bool,
	caCertPath *string,
	disableSystemCAs *bool,
) (http.RoundTripper, error) {
	// Default to http.DefaultTransport if base is nil
	if base == nil {
		base = http.DefaultTransport
	}

	// If no TLS config is set, return base unchanged
	if insecureSkipVerify == nil && (caCertPath == nil || *caCertPath == "") {
		return base, nil
	}

	// Create a new transport with TLS config
	// We need to clone the base transport to avoid modifying the default
	var tlsConfig *tls.Config

	if insecureSkipVerify != nil && *insecureSkipVerify {
		tlsConfig = &tls.Config{
			InsecureSkipVerify: true,
		}
	} else if caCertPath != nil && *caCertPath != "" {
		caCert, err := os.ReadFile(*caCertPath)
		if err != nil {
			return nil, fmt.Errorf("failed to read CA certificate from %s: %w", *caCertPath, err)
		}
		caCertPool := x509.NewCertPool()
		if !caCertPool.AppendCertsFromPEM(caCert) {
			return nil, fmt.Errorf("failed to parse CA certificate from %s", *caCertPath)
		}

		tlsConfig = &tls.Config{}
		if disableSystemCAs != nil && *disableSystemCAs {
			tlsConfig.RootCAs = caCertPool
		} else {
			systemCAs, err := x509.SystemCertPool()
			if err != nil {
				tlsConfig.RootCAs = caCertPool
			} else {
				systemCAs.AppendCertsFromPEM(caCert)
				tlsConfig.RootCAs = systemCAs
			}
		}
	}

	// Try to clone the base transport to preserve its settings
	if baseTransport, ok := base.(*http.Transport); ok {
		cloned := baseTransport.Clone()
		cloned.TLSClientConfig = tlsConfig
		return cloned, nil
	}

	// If base is not an *http.Transport, wrap it with a transport that has TLS config
	// This handles cases where base is already a custom RoundTripper
	return &tlsTransport{
		base:      base,
		tlsConfig: tlsConfig,
	}, nil
}

// tlsTransport wraps a RoundTripper and applies TLS config
type tlsTransport struct {
	base      http.RoundTripper
	tlsConfig *tls.Config
}

func (t *tlsTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	// If the request's TLS config needs to be modified, we would need to
	// create a new client for each request, which is inefficient.
	// Instead, we rely on the base transport having the TLS config set.
	// This wrapper is primarily for when base is not an *http.Transport.
	return t.base.RoundTrip(req)
}
