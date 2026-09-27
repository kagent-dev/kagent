// Package a2agateway routes public A2A calls to actors and persisted Session state.
package a2agateway

import (
	"context"
	"iter"
	"strings"

	a2a "github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2aclient"
	"github.com/a2aproject/a2a-go/v2/a2aext"
	"github.com/a2aproject/a2a-go/v2/a2asrv"
	"github.com/google/uuid"
	apiv1alpha1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1alpha1"
	sessionsvc "github.com/kagent-dev/kagent/go/core/internal/service/session"
	"k8s.io/apimachinery/pkg/types"
)

type interactionService interface {
	GetTask(context.Context, types.NamespacedName, *a2a.GetTaskRequest) (*a2a.Task, error)
	ListTasks(context.Context, types.NamespacedName, *a2a.ListTasksRequest) (*a2a.ListTasksResponse, error)
	PrepareSend(context.Context, types.NamespacedName, *a2a.SendMessageRequest) (*sessionsvc.PreparedSend, error)
	PrepareCancelTask(context.Context, types.NamespacedName, *a2a.CancelTaskRequest) (*apiv1alpha1.Session, *a2a.Task, error)
	PrepareTaskSubscription(context.Context, types.NamespacedName, *a2a.SubscribeToTaskRequest) (*apiv1alpha1.Session, *a2a.Task, error)
	RevokeSend(context.Context, types.NamespacedName, *a2a.Message, uuid.UUID) (bool, error)
	GetTaskByMessage(context.Context, types.NamespacedName, *a2a.Message) (*a2a.Task, error)
	GetSendResult(context.Context, types.NamespacedName, *a2a.Message, a2a.TaskID, *int) (*a2a.Task, error)
	GetCancelResult(context.Context, types.NamespacedName, a2a.TaskID) (*a2a.Task, error)
	GetSettledTask(context.Context, types.NamespacedName, string, a2a.TaskID, *int) (*a2a.Task, error)
	GetAgentCard(context.Context, types.NamespacedName) (*a2a.AgentCard, error)
}

type runtimeDialer interface {
	Dial(context.Context, *apiv1alpha1.Session) (*a2aclient.Client, error)
}

// Gateway owns actor dispatch and live observation for both public transports.
// Session policy, resolution, and persistence stay behind the service boundary.
type Gateway struct {
	interactions interactionService
	dialer       runtimeDialer
	gatewayURL   string
}

var _ runtimeDialer = (*RuntimeDialer)(nil)
var _ a2asrv.RequestHandler = (*Gateway)(nil)
var _ interactionService = (*sessionsvc.InteractionService)(nil)

func New(interactions interactionService, dialer runtimeDialer, gatewayURL string) a2asrv.RequestHandler {
	return &a2asrv.InterceptedHandler{
		Handler:      &Gateway{interactions: interactions, dialer: dialer, gatewayURL: gatewayURL},
		Interceptors: []a2asrv.CallInterceptor{a2aext.NewServerPropagator(nil)},
	}
}

func (g *Gateway) GetTask(ctx context.Context, req *a2a.GetTaskRequest) (*a2a.Task, error) {
	agent, err := route(ctx)
	if err != nil {
		return nil, err
	}
	return g.interactions.GetTask(ctx, agent, req)
}

func (g *Gateway) ListTasks(ctx context.Context, req *a2a.ListTasksRequest) (*a2a.ListTasksResponse, error) {
	agent, err := route(ctx)
	if err != nil {
		return nil, err
	}
	return g.interactions.ListTasks(ctx, agent, req)
}

func (g *Gateway) CancelTask(ctx context.Context, req *a2a.CancelTaskRequest) (*a2a.Task, error) {
	agent, err := route(ctx)
	if err != nil {
		return nil, err
	}
	return g.cancelTask(ctx, agent, req)
}

func (g *Gateway) SendMessage(ctx context.Context, req *a2a.SendMessageRequest) (a2a.SendMessageResult, error) {
	agent, err := route(ctx)
	if err != nil {
		return nil, err
	}
	return g.sendMessage(ctx, agent, req)
}

func (g *Gateway) SendStreamingMessage(ctx context.Context, req *a2a.SendMessageRequest) iter.Seq2[a2a.Event, error] {
	agent, err := route(ctx)
	if err != nil {
		return func(yield func(a2a.Event, error) bool) { yield(nil, err) }
	}
	return g.sendStreamingMessage(ctx, agent, req)
}

func (g *Gateway) SubscribeToTask(ctx context.Context, req *a2a.SubscribeToTaskRequest) iter.Seq2[a2a.Event, error] {
	agent, err := route(ctx)
	if err != nil {
		return func(yield func(a2a.Event, error) bool) { yield(nil, err) }
	}
	return g.subscribeToTask(ctx, agent, req)
}

func (g *Gateway) GetTaskPushConfig(ctx context.Context, req *a2a.GetTaskPushConfigRequest) (*a2a.PushConfig, error) {
	return nil, a2a.ErrPushNotificationNotSupported
}

func (g *Gateway) ListTaskPushConfigs(ctx context.Context, req *a2a.ListTaskPushConfigRequest) (*a2a.ListTaskPushConfigResponse, error) {
	return nil, a2a.ErrPushNotificationNotSupported
}

func (g *Gateway) CreateTaskPushConfig(ctx context.Context, req *a2a.PushConfig) (*a2a.PushConfig, error) {
	return nil, a2a.ErrPushNotificationNotSupported
}

func (g *Gateway) DeleteTaskPushConfig(ctx context.Context, req *a2a.DeleteTaskPushConfigRequest) error {
	return a2a.ErrPushNotificationNotSupported
}

func (g *Gateway) GetExtendedAgentCard(ctx context.Context, req *a2a.GetExtendedAgentCardRequest) (*a2a.AgentCard, error) {
	if req == nil {
		return nil, a2a.ErrInvalidParams
	}
	ref, err := route(ctx)
	if err != nil {
		return nil, err
	}
	card, err := g.interactions.GetAgentCard(ctx, ref)
	if err != nil {
		return nil, err
	}
	// The compiled card provides immutable template metadata. Public transport,
	// security, and signatures belong to the gateway instead of the private
	// runtime that produced that card.
	card.SupportedInterfaces = []*a2a.AgentInterface{
		a2a.NewAgentInterface(strings.TrimRight(g.gatewayURL, "/")+HTTPPathPrefix+ref.Namespace+"/"+ref.Name, a2a.TransportProtocolJSONRPC),
		a2a.NewAgentInterface(g.gatewayURL, a2a.TransportProtocolGRPC),
	}
	// HTTP identifies the Agent in its URL; only the shared gRPC endpoint needs
	// clients to send a tenant.
	card.SupportedInterfaces[1].Tenant = ref.Namespace + "/" + ref.Name
	// Extensions are the exception, and replacing the whole capabilities struct
	// used to drop them. They describe what the runtime behind this gateway can
	// negotiate — human-in-the-loop among them — which is not the gateway's to
	// erase. A client discovers HITL by reading this card, so wiping it made
	// answering an agent's question undiscoverable while the card still rendered
	// perfectly.
	extensions := card.Capabilities.Extensions
	card.Capabilities = a2a.AgentCapabilities{Streaming: true, ExtendedAgentCard: true, Extensions: extensions}
	card.SecurityRequirements = nil
	card.SecuritySchemes = nil
	card.Signatures = nil
	return card, nil
}
