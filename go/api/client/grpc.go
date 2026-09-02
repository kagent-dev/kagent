package client

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"os"
	"sync"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
)

const (
	// DefaultGRPCTarget is the implicit local target used when WithGRPCTarget is omitted.
	// Callers can use it to limit local connection setup to the default endpoint.
	DefaultGRPCTarget         = "localhost:8084"
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
	target          string
	timeout         time.Duration
	maxMessageBytes int
	tlsConfig       *GRPCTLSConfig
	credentials     credentials.TransportCredentials
	dialOptions     []grpc.DialOption

	mu   sync.Mutex
	conn *grpc.ClientConn
}

func newGRPCTransport() grpcTransport {
	return grpcTransport{
		target:          DefaultGRPCTarget,
		timeout:         defaultGRPCTimeout,
		maxMessageBytes: defaultGRPCMaxMessageSize,
	}
}

// WithGRPCTarget sets the native gRPC target used by migrated API clients.
func WithGRPCTarget(target string) ClientOption {
	return func(client *BaseClient) {
		client.grpc.target = target
	}
}

// WithGRPCTimeout sets the default deadline applied when a context has no
// earlier deadline. A non-positive duration disables the default deadline.
func WithGRPCTimeout(timeout time.Duration) ClientOption {
	return func(client *BaseClient) {
		client.grpc.timeout = timeout
	}
}

// WithGRPCMaxMessageSize sets the maximum size for gRPC requests, responses,
// and StructuredObject payloads. A non-positive value uses gRPC defaults.
func WithGRPCMaxMessageSize(maxMessageBytes int) ClientOption {
	return func(client *BaseClient) {
		client.grpc.maxMessageBytes = maxMessageBytes
	}
}

// WithGRPCTLS enables server-authenticated TLS for the gRPC connection.
func WithGRPCTLS(config GRPCTLSConfig) ClientOption {
	return func(client *BaseClient) {
		client.grpc.tlsConfig = &config
		client.grpc.credentials = nil
	}
}

// WithGRPCTransportCredentials sets custom gRPC transport credentials.
func WithGRPCTransportCredentials(transportCredentials credentials.TransportCredentials) ClientOption {
	return func(client *BaseClient) {
		client.grpc.credentials = transportCredentials
		client.grpc.tlsConfig = nil
	}
}

// WithGRPCDialOptions appends low-level gRPC dial options. It is primarily
// useful for custom resolvers and in-process test dialers.
func WithGRPCDialOptions(options ...grpc.DialOption) ClientOption {
	return func(client *BaseClient) {
		client.grpc.dialOptions = append(client.grpc.dialOptions, options...)
	}
}

func (c *BaseClient) grpcConnection() (*grpc.ClientConn, error) {
	c.grpc.mu.Lock()
	defer c.grpc.mu.Unlock()

	if c.grpc.conn != nil {
		return c.grpc.conn, nil
	}
	if c.grpc.target == "" {
		return nil, fmt.Errorf("gRPC target is required")
	}

	transportCredentials, err := c.grpcTransportCredentials()
	if err != nil {
		return nil, err
	}

	dialOptions := make([]grpc.DialOption, 0, len(c.grpc.dialOptions)+2)
	dialOptions = append(dialOptions, grpc.WithTransportCredentials(transportCredentials))
	if c.grpc.maxMessageBytes > 0 {
		dialOptions = append(dialOptions, grpc.WithDefaultCallOptions(
			grpc.MaxCallRecvMsgSize(c.grpc.maxMessageBytes),
			grpc.MaxCallSendMsgSize(c.grpc.maxMessageBytes),
		))
	}
	dialOptions = append(dialOptions, c.grpc.dialOptions...)

	connection, err := grpc.NewClient(c.grpc.target, dialOptions...)
	if err != nil {
		return nil, fmt.Errorf("create gRPC client for %q: %w", c.grpc.target, err)
	}
	c.grpc.conn = connection
	return connection, nil
}

func (c *BaseClient) grpcTransportCredentials() (credentials.TransportCredentials, error) {
	if c.grpc.credentials != nil {
		return c.grpc.credentials, nil
	}
	if c.grpc.tlsConfig == nil {
		return insecure.NewCredentials(), nil
	}

	tlsConfig := &tls.Config{
		MinVersion: tls.VersionTLS12,
		ServerName: c.grpc.tlsConfig.ServerName,
	}
	if c.grpc.tlsConfig.CAFile == "" {
		return credentials.NewTLS(tlsConfig), nil
	}

	caPEM, err := os.ReadFile(c.grpc.tlsConfig.CAFile)
	if err != nil {
		return nil, fmt.Errorf("read gRPC CA file: %w", err)
	}
	rootCAs, err := x509.SystemCertPool()
	if err != nil {
		rootCAs = x509.NewCertPool()
	}
	if !rootCAs.AppendCertsFromPEM(caPEM) {
		return nil, fmt.Errorf("gRPC CA file %q contains no certificates", c.grpc.tlsConfig.CAFile)
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
	if c.grpc.timeout <= 0 {
		return ctx, func() {}
	}
	return context.WithTimeout(ctx, c.grpc.timeout)
}

// Close releases the shared gRPC connection, if one was created.
func (c *BaseClient) Close() error {
	if c == nil {
		return nil
	}

	c.grpc.mu.Lock()
	defer c.grpc.mu.Unlock()
	if c.grpc.conn == nil {
		return nil
	}

	err := c.grpc.conn.Close()
	c.grpc.conn = nil
	return err
}
