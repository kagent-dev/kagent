package controllerclient

import (
	"context"
	"crypto/tls"
	"fmt"
	"net/url"
	"sync"
	"time"

	"go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"

	"github.com/kagent-dev/kagent/go/adk/pkg/auth"
	"github.com/kagent-dev/kagent/go/api/client"
	runtimev1 "github.com/kagent-dev/kagent/go/api/gen/kagent/runtime/v1"
)

const (
	defaultTimeout = 30 * time.Second
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
	apiURL           string
	connection       *grpc.ClientConn
	timeout          time.Duration
	maxMessageBytes  int
	agentName        string
	tokenProvider    TokenProvider
	memoryService    runtimev1.MemoryServiceClient
	taskStoreService runtimev1.TaskStoreServiceClient
	closeOnce        sync.Once
	closeErr         error
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
		config.MaxMessageBytes = client.DefaultGRPCMaxMessageSize
	}
	transportCredentials := config.TransportCredentials
	if transportCredentials == nil {
		if secure {
			transportCredentials = credentials.NewTLS(&tls.Config{MinVersion: tls.VersionTLS12})
		} else {
			transportCredentials = insecure.NewCredentials()
		}
	}
	dialOptions := make([]grpc.DialOption, 0, len(config.DialOptions)+3)
	dialOptions = append(dialOptions,
		grpc.WithTransportCredentials(transportCredentials),
		grpc.WithStatsHandler(otelgrpc.NewClientHandler()),
	)
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
		apiURL:           config.APIURL,
		connection:       connection,
		timeout:          config.Timeout,
		maxMessageBytes:  config.MaxMessageBytes,
		agentName:        config.AgentName,
		tokenProvider:    config.TokenProvider,
		memoryService:    runtimev1.NewMemoryServiceClient(connection),
		taskStoreService: runtimev1.NewTaskStoreServiceClient(connection),
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

// GetAPIURL returns the validated control-plane origin shared by its HTTP and gRPC APIs.
func (client *Client) GetAPIURL() string { return client.apiURL }

func (client *Client) MemoryService() runtimev1.MemoryServiceClient {
	return client.memoryService
}

func (client *Client) TaskStoreService() runtimev1.TaskStoreServiceClient {
	return client.taskStoreService
}

func (client *Client) MaxMessageBytes() int {
	return client.maxMessageBytes
}

func (client *Client) CallContext(ctx context.Context, userID string) (context.Context, context.CancelFunc) {
	if userID == "" {
		userID = auth.UserIDFromContext(ctx)
	}
	metadataValues, _ := metadata.FromOutgoingContext(ctx)
	metadataValues = metadataValues.Copy()
	// Substrate replaces the placeholder at the controller's runtime origin.
	token := "placeholder"
	if client.tokenProvider != nil {
		if supplied := client.tokenProvider.GetToken(); supplied != "" {
			token = supplied
		}
	}
	metadataValues.Set("authorization", "Bearer "+token)
	if client.agentName != "" {
		metadataValues.Set("x-agent-name", client.agentName)
	}
	if userID != "" {
		metadataValues.Set("x-user-id", userID)
	}
	ctx = metadata.NewOutgoingContext(ctx, metadataValues)
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
