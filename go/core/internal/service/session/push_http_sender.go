package session

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"syscall"
	"time"

	"github.com/a2aproject/a2a-go/v2/a2a"
)

// HTTPPushSender sends task updates without following redirects. A redirect
// could forward the signed credential and task payload from HTTPS to HTTP.
type HTTPPushSender struct {
	client    *http.Client
	allowHTTP bool
}

func NewHTTPPushSender(timeout time.Duration, allowHTTP, allowPrivateNetworks bool) *HTTPPushSender {
	client := &http.Client{
		Timeout: timeout,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	if !allowPrivateNetworks {
		client.Transport = guardedPushTransport()
	}
	return &HTTPPushSender{client: client, allowHTTP: allowHTTP}
}

func (s *HTTPPushSender) SendPush(ctx context.Context, config *a2a.PushConfig, event a2a.Event) error {
	data, err := json.Marshal(a2a.StreamResponse{Event: event})
	if err != nil {
		return fmt.Errorf("serialize push event: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, config.URL, bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("create push request: %w", err)
	}
	if req.URL.Scheme != "https" && (req.URL.Scheme != "http" || !s.allowHTTP) {
		return fmt.Errorf("push destination must use HTTPS unless HTTP is enabled")
	}
	req.Header.Set("Content-Type", "application/json")
	if config.Auth != nil && config.Auth.Credentials != "" {
		req.Header.Set("Authorization", "Bearer "+config.Auth.Credentials)
	}
	resp, err := s.client.Do(req)
	if err != nil {
		return fmt.Errorf("send push notification: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("push notification endpoint returned non-success status: %s", resp.Status)
	}
	return nil
}

// guardedPushTransport checks the resolved address at connection time, also
// covering DNS changes between registration and delivery. Its network settings
// match the A2A SDK sender's private-network guard.
func guardedPushTransport() *http.Transport {
	dialer := &net.Dialer{Timeout: 30 * time.Second, KeepAlive: 30 * time.Second}
	dialer.Control = func(_, address string, _ syscall.RawConn) error {
		host, _, err := net.SplitHostPort(address)
		if err != nil {
			return fmt.Errorf("invalid push dial address %q: %w", address, err)
		}
		ip := net.ParseIP(host)
		if ip == nil || ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsUnspecified() || ip.IsMulticast() {
			return fmt.Errorf("push notification target resolves to a blocked address range: %s", host)
		}
		return nil
	}
	return &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		DialContext:           dialer.DialContext,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          100,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
	}
}
