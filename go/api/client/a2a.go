package client

import (
	"context"
	"fmt"
	"time"

	a2atype "github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2aclient"
	a2agrpc "github.com/a2aproject/a2a-go/v2/a2agrpc/v1"
	a2apb "github.com/a2aproject/a2a-go/v2/a2apb/v1"
	apiv1alpha1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1alpha1"
)

const userIDHeader = "x-user-id"

// A2AClient creates upstream A2A clients routed to an Agent.
type A2AClient struct {
	client *baseClient
}

func newA2AClient(client *baseClient) *A2AClient {
	return &A2AClient{client: client}
}

// ForAgent creates a client for a named Agent. Sending without context or task
// creates a conversation; subsequent messages use the returned context ID.
func (c *A2AClient) ForAgent(ctx context.Context, agent *apiv1alpha1.ResourceReference) (*a2aclient.Client, error) {
	return c.forAgent(ctx, agent, "")
}

// ForAgentInstance resolves its Agent and supplies the conversation context on sends.
func (c *A2AClient) ForAgentInstance(ctx context.Context, id string) (*a2aclient.Client, error) {
	response, err := newAgentInstanceClient(c.client).GetAgentInstance(ctx, &apiv1alpha1.GetAgentInstanceRequest{AgentInstanceId: id})
	if err != nil {
		return nil, err
	}
	return c.forAgent(ctx, response.GetAgentInstance().GetAgent(), id)
}

func (c *A2AClient) forAgent(ctx context.Context, agent *apiv1alpha1.ResourceReference, contextID string) (*a2aclient.Client, error) {
	if agent.GetNamespace() == "" || agent.GetName() == "" {
		return nil, fmt.Errorf("agent namespace and name are required")
	}
	connection, err := c.client.grpcConnection()
	if err != nil {
		return nil, err
	}
	transport := a2agrpc.NewGRPCTransportFromClient(a2apb.NewA2AServiceClient(connection))
	return a2aclient.NewFromEndpoints(ctx, []*a2atype.AgentInterface{{
		URL:             c.client.transport.url,
		ProtocolBinding: a2atype.TransportProtocolGRPC,
		ProtocolVersion: a2atype.Version,
		Tenant:          agent.Namespace + "/" + agent.Name,
	}},
		a2aclient.WithDefaultsDisabled(),
		a2aclient.WithTransport(a2atype.TransportProtocolGRPC, a2aclient.TransportFactoryFn(
			func(context.Context, *a2atype.AgentCard, *a2atype.AgentInterface) (a2aclient.Transport, error) {
				return transport, nil
			},
		)),
		a2aclient.WithCallInterceptors(&agentRoutingInterceptor{
			contextID: contextID,
			userID:    c.client.userID,
			timeout:   c.client.transport.timeout,
		}),
	)
}

type cancelCallContextKey struct{}

type agentRoutingInterceptor struct {
	a2aclient.PassthroughInterceptor
	contextID string
	userID    string
	timeout   time.Duration
}

func (i *agentRoutingInterceptor) Before(ctx context.Context, request *a2aclient.Request) (context.Context, any, error) {
	if i.contextID != "" {
		switch payload := request.Payload.(type) {
		case *a2atype.SendMessageRequest:
			if payload.Message != nil && payload.Message.ContextID == "" {
				payload.Message.ContextID = i.contextID
			}
		case *a2atype.ListTasksRequest:
			if payload.ContextID == "" {
				payload.ContextID = i.contextID
			}
		}
	}
	if i.userID != "" {
		request.ServiceParams.Append(userIDHeader, i.userID)
	}
	if i.timeout <= 0 || isStreamingA2AMethod(request.Method) {
		return ctx, nil, nil
	}
	callContext, cancel := context.WithTimeout(ctx, i.timeout)
	return context.WithValue(callContext, cancelCallContextKey{}, cancel), nil, nil
}

func (i *agentRoutingInterceptor) After(ctx context.Context, _ *a2aclient.Response) error {
	if cancel, ok := ctx.Value(cancelCallContextKey{}).(context.CancelFunc); ok {
		cancel()
	}
	return nil
}

func isStreamingA2AMethod(method string) bool {
	return method == "SendStreamingMessage" || method == "SubscribeToTask"
}
