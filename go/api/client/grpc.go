package client

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"net/url"
	"os"
	"sync"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
)

const (
	// DefaultAPIURL is the local control-plane API endpoint.
	DefaultAPIURL = "http://localhost:8083"
	// DefaultGatewayURL is the local A2A and MCP gateway endpoint.
	DefaultGatewayURL         = "http://localhost:8083"
	defaultGRPCTimeout        = 30 * time.Second
	defaultGRPCMaxMessageSize = 16 << 20
)

// GRPCTLSConfig configures server-authenticated TLS for gRPC connections.
// An empty CAFile uses the host's system certificate pool.
type GRPCTLSConfig struct {
	CAFile     string
	ServerName string
}

type grpcTransport struct {
	url             string
	target          string
	timeout         time.Duration
	maxMessageBytes int
	tlsConfig       *GRPCTLSConfig
	credentials     credentials.TransportCredentials
	dialOptions     []grpc.DialOption

	mu   sync.Mutex
	conn *grpc.ClientConn
}

func newGRPCTransport(rawURL string) (*grpcTransport, error) {
	target, secure, err := parseGRPCURL(rawURL)
	if err != nil {
		return nil, err
	}
	transport := grpcTransport{
		url:             rawURL,
		target:          target,
		timeout:         defaultGRPCTimeout,
		maxMessageBytes: defaultGRPCMaxMessageSize,
	}
	if secure {
		transport.tlsConfig = &GRPCTLSConfig{}
	}
	return &transport, nil
}

func parseGRPCURL(rawURL string) (string, bool, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return "", false, fmt.Errorf("parse URL %q: %w", rawURL, err)
	}
	if u.Host == "" || (u.Path != "" && u.Path != "/") || u.RawQuery != "" || u.Fragment != "" {
		return "", false, fmt.Errorf("URL %q must contain only a scheme and authority", rawURL)
	}
	switch u.Scheme {
	case "http":
		return "passthrough:///" + u.Host, false, nil
	case "https":
		return "passthrough:///" + u.Host, true, nil
	default:
		return "", false, fmt.Errorf("URL %q must use http or https", rawURL)
	}
}

// WithGRPCTarget overrides both native gRPC targets for in-process tests.
func WithGRPCTarget(target string) ClientOption {
	return func(client *BaseClient) {
		client.api.target, client.gateway.target = target, target
	}
}

// WithGRPCTimeout sets the default deadline applied when a context has no
// earlier deadline. A non-positive duration disables the default deadline.
func WithGRPCTimeout(timeout time.Duration) ClientOption {
	return func(client *BaseClient) {
		client.api.timeout, client.gateway.timeout = timeout, timeout
	}
}

// WithGRPCMaxMessageSize sets the maximum size for gRPC requests, responses,
// and StructuredObject payloads. A non-positive value uses gRPC defaults.
func WithGRPCMaxMessageSize(maxMessageBytes int) ClientOption {
	return func(client *BaseClient) {
		client.api.maxMessageBytes, client.gateway.maxMessageBytes = maxMessageBytes, maxMessageBytes
	}
}

// WithGRPCTLS configures server-authenticated TLS for HTTPS endpoints.
func WithGRPCTLS(config GRPCTLSConfig) ClientOption {
	return func(client *BaseClient) {
		for _, transport := range []*grpcTransport{client.api, client.gateway} {
			if transport.tlsConfig != nil {
				transport.tlsConfig = &config
				transport.credentials = nil
			}
		}
	}
}

// WithGRPCTransportCredentials sets custom gRPC transport credentials.
func WithGRPCTransportCredentials(transportCredentials credentials.TransportCredentials) ClientOption {
	return func(client *BaseClient) {
		client.api.credentials, client.gateway.credentials = transportCredentials, transportCredentials
		client.api.tlsConfig, client.gateway.tlsConfig = nil, nil
	}
}

// WithGRPCDialOptions appends low-level gRPC dial options. It is primarily
// useful for custom resolvers and in-process test dialers.
func WithGRPCDialOptions(options ...grpc.DialOption) ClientOption {
	return func(client *BaseClient) {
		client.api.dialOptions = append(client.api.dialOptions, options...)
		client.gateway.dialOptions = append(client.gateway.dialOptions, options...)
	}
}

func (c *BaseClient) grpcConnection(transport *grpcTransport) (*grpc.ClientConn, error) {
	transport.mu.Lock()
	defer transport.mu.Unlock()

	if transport.conn != nil {
		return transport.conn, nil
	}
	if transport.target == "" {
		return nil, fmt.Errorf("gRPC target is required")
	}

	transportCredentials, err := grpcTransportCredentials(transport)
	if err != nil {
		return nil, err
	}

	dialOptions := make([]grpc.DialOption, 0, len(transport.dialOptions)+2)
	dialOptions = append(dialOptions, grpc.WithTransportCredentials(transportCredentials))
	if transport.maxMessageBytes > 0 {
		dialOptions = append(dialOptions, grpc.WithDefaultCallOptions(
			grpc.MaxCallRecvMsgSize(transport.maxMessageBytes),
			grpc.MaxCallSendMsgSize(transport.maxMessageBytes),
		))
	}
	dialOptions = append(dialOptions, transport.dialOptions...)

	connection, err := grpc.NewClient(transport.target, dialOptions...)
	if err != nil {
		return nil, fmt.Errorf("create gRPC client for %q: %w", transport.target, err)
	}
	transport.conn = connection
	return connection, nil
}

func grpcTransportCredentials(transport *grpcTransport) (credentials.TransportCredentials, error) {
	if transport.credentials != nil {
		return transport.credentials, nil
	}
	if transport.tlsConfig == nil {
		return insecure.NewCredentials(), nil
	}

	tlsConfig := &tls.Config{
		MinVersion: tls.VersionTLS12,
		ServerName: transport.tlsConfig.ServerName,
	}
	if transport.tlsConfig.CAFile == "" {
		return credentials.NewTLS(tlsConfig), nil
	}

	caPEM, err := os.ReadFile(transport.tlsConfig.CAFile)
	if err != nil {
		return nil, fmt.Errorf("read gRPC CA file: %w", err)
	}
	rootCAs, err := x509.SystemCertPool()
	if err != nil {
		rootCAs = x509.NewCertPool()
	}
	if !rootCAs.AppendCertsFromPEM(caPEM) {
		return nil, fmt.Errorf("gRPC CA file %q contains no certificates", transport.tlsConfig.CAFile)
	}
	tlsConfig.RootCAs = rootCAs
	return credentials.NewTLS(tlsConfig), nil
}

func (c *BaseClient) grpcCallContext(ctx context.Context) (context.Context, context.CancelFunc) {
	return c.grpcCallContextForUser(ctx, c.UserID)
}

func (c *BaseClient) grpcCallContextForUser(ctx context.Context, userID string) (context.Context, context.CancelFunc) {
	if userID != "" {
		ctx = metadata.AppendToOutgoingContext(ctx, "x-user-id", userID)
	}
	if c.api.timeout <= 0 {
		return ctx, func() {}
	}
	return context.WithTimeout(ctx, c.api.timeout)
}

// Close releases the shared gRPC connection, if one was created.
func (c *BaseClient) Close() error {
	if c == nil {
		return nil
	}

	var errs []error
	for _, transport := range []*grpcTransport{c.api, c.gateway} {
		transport.mu.Lock()
		if transport.conn != nil {
			errs = append(errs, transport.conn.Close())
			transport.conn = nil
		}
		transport.mu.Unlock()
	}
	return errors.Join(errs...)
}
