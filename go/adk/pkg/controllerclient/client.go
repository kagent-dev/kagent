package controllerclient

import (
	"context"
	"crypto/tls"
	"fmt"
	"net/url"
	"sync"
	"time"

	"github.com/kagent-dev/kagent/go/adk/pkg/auth"
	apiv1alpha1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1alpha1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
)

const (
	defaultTimeout        = 30 * time.Second
	defaultMaxMessageSize = 16 << 20
)

type TokenProvider interface {
	GetToken() string
}

type Config struct {
	APIURL               string
	AgentName            string
	TokenProvider        TokenProvider
	Timeout              time.Duration
	MaxMessageBytes      int
	TransportCredentials credentials.TransportCredentials
	DialOptions          []grpc.DialOption
}

type Client struct {
	connection      *grpc.ClientConn
	timeout         time.Duration
	maxMessageBytes int
	agentName       string
	tokenProvider   TokenProvider
	memoryService   apiv1alpha1.MemoryServiceClient
	closeOnce       sync.Once
	closeErr        error
}

func New(config Config) (*Client, error) {
	target, secure, err := targetFromURL(config.APIURL)
	if err != nil {
		return nil, err
	}
	if config.Timeout == 0 {
		config.Timeout = defaultTimeout
	}
	if config.MaxMessageBytes == 0 {
		config.MaxMessageBytes = defaultMaxMessageSize
	}
	transportCredentials := config.TransportCredentials
	if transportCredentials == nil {
		if secure {
			transportCredentials = credentials.NewTLS(&tls.Config{MinVersion: tls.VersionTLS12})
		} else {
			transportCredentials = insecure.NewCredentials()
		}
	}
	dialOptions := make([]grpc.DialOption, 0, len(config.DialOptions)+2)
	dialOptions = append(dialOptions, grpc.WithTransportCredentials(transportCredentials))
	if config.MaxMessageBytes > 0 {
		dialOptions = append(dialOptions, grpc.WithDefaultCallOptions(
			grpc.MaxCallRecvMsgSize(config.MaxMessageBytes),
			grpc.MaxCallSendMsgSize(config.MaxMessageBytes),
		))
	}
	dialOptions = append(dialOptions, config.DialOptions...)

	connection, err := grpc.NewClient(target, dialOptions...)
	if err != nil {
		return nil, fmt.Errorf("create controller API client for %q: %w", config.APIURL, err)
	}
	return &Client{
		connection:      connection,
		timeout:         config.Timeout,
		maxMessageBytes: config.MaxMessageBytes,
		agentName:       config.AgentName,
		tokenProvider:   config.TokenProvider,
		memoryService:   apiv1alpha1.NewMemoryServiceClient(connection),
	}, nil
}

func targetFromURL(rawURL string) (string, bool, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return "", false, fmt.Errorf("parse controller API URL %q: %w", rawURL, err)
	}
	if u.Host == "" || (u.Path != "" && u.Path != "/") || u.RawQuery != "" || u.Fragment != "" {
		return "", false, fmt.Errorf("controller API URL %q must contain only a scheme and authority", rawURL)
	}
	switch u.Scheme {
	case "http":
		return "passthrough:///" + u.Host, false, nil
	case "https":
		return "passthrough:///" + u.Host, true, nil
	default:
		return "", false, fmt.Errorf("controller API URL %q must use http or https", rawURL)
	}
}

func (client *Client) MemoryService() apiv1alpha1.MemoryServiceClient {
	return client.memoryService
}

func (client *Client) MaxMessageBytes() int {
	return client.maxMessageBytes
}

func (client *Client) CallContext(ctx context.Context, userID string) (context.Context, context.CancelFunc) {
	if userID == "" {
		userID = auth.UserIDFromContext(ctx)
	}
	metadataValues := make([]string, 0, 6)
	if client.tokenProvider != nil {
		if token := client.tokenProvider.GetToken(); token != "" {
			metadataValues = append(metadataValues, "authorization", "Bearer "+token)
		}
	}
	if client.agentName != "" {
		metadataValues = append(metadataValues, "x-agent-name", client.agentName)
	}
	if userID != "" {
		metadataValues = append(metadataValues, "x-user-id", userID)
	}
	if len(metadataValues) > 0 {
		ctx = metadata.AppendToOutgoingContext(ctx, metadataValues...)
	}
	if client.timeout <= 0 {
		return ctx, func() {}
	}
	return context.WithTimeout(ctx, client.timeout)
}

func (client *Client) Close() error {
	if client == nil || client.connection == nil {
		return nil
	}
	client.closeOnce.Do(func() {
		client.closeErr = client.connection.Close()
	})
	return client.closeErr
}
