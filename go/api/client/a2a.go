package client

import (
	"context"
	"time"

	a2atype "github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2aclient"
	a2agrpc "github.com/a2aproject/a2a-go/v2/a2agrpc/v1"
	a2apb "github.com/a2aproject/a2a-go/v2/a2apb/v1"
	kagenta2a "github.com/kagent-dev/kagent/go/api/a2a"
)

const userIDHeader = "x-user-id"

// A2AClient creates upstream A2A clients routed to an AgentInstance.
type A2AClient struct {
	client *BaseClient
}

// NewA2AClient creates an A2A client factory over the shared gRPC connection.
func NewA2AClient(client *BaseClient) *A2AClient {
	return &A2AClient{client: client}
}

// ForAgentInstance creates an upstream A2A client routed to one AgentInstance.
func (c *A2AClient) ForAgentInstance(ctx context.Context, namespace, id string) (*a2aclient.Client, error) {
	connection, err := c.client.grpcConnection()
	if err != nil {
		return nil, err
	}
	transport := a2agrpc.NewGRPCTransportFromClient(a2apb.NewA2AServiceClient(connection))
	return a2aclient.NewFromEndpoints(ctx, []*a2atype.AgentInterface{{
		URL:             c.client.grpc.target,
		ProtocolBinding: a2atype.TransportProtocolGRPC,
		ProtocolVersion: a2atype.Version,
	}},
		a2aclient.WithDefaultsDisabled(),
		a2aclient.WithTransport(a2atype.TransportProtocolGRPC, a2aclient.TransportFactoryFn(
			func(context.Context, *a2atype.AgentCard, *a2atype.AgentInterface) (a2aclient.Transport, error) {
				return transport, nil
			},
		)),
		a2aclient.WithCallInterceptors(&agentInstanceRoutingInterceptor{
			namespace: namespace,
			id:        id,
			userID:    c.client.UserID,
			timeout:   c.client.grpc.timeout,
		}),
	)
}

type cancelCallContextKey struct{}

type agentInstanceRoutingInterceptor struct {
	a2aclient.PassthroughInterceptor
	namespace string
	id        string
	userID    string
	timeout   time.Duration
}

func (i *agentInstanceRoutingInterceptor) Before(ctx context.Context, request *a2aclient.Request) (context.Context, any, error) {
	request.ServiceParams.Append(kagenta2a.AgentInstanceNamespaceHeader, i.namespace)
	request.ServiceParams.Append(kagenta2a.AgentInstanceIDHeader, i.id)
	if i.userID != "" {
		request.ServiceParams.Append(userIDHeader, i.userID)
	}
	if i.timeout <= 0 || isStreamingA2AMethod(request.Method) {
		return ctx, nil, nil
	}
	callContext, cancel := context.WithTimeout(ctx, i.timeout)
	return context.WithValue(callContext, cancelCallContextKey{}, cancel), nil, nil
}

func (i *agentInstanceRoutingInterceptor) After(ctx context.Context, _ *a2aclient.Response) error {
	if cancel, ok := ctx.Value(cancelCallContextKey{}).(context.CancelFunc); ok {
		cancel()
	}
	return nil
}

func isStreamingA2AMethod(method string) bool {
	return method == "SendStreamingMessage" || method == "SubscribeToTask"
}
