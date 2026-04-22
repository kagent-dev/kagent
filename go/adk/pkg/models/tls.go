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
	baseTransport, ok := base.(*http.Transport)
	if !ok {
		return nil, fmt.Errorf("BuildTLSTransport: base must be *http.Transport, got %T", base)
	}
	cloned := baseTransport.Clone()
	cloned.TLSClientConfig = tlsConfig
	return cloned, nil
}
