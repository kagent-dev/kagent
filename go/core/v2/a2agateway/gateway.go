// Package a2agateway exposes AgentInstances through the upstream A2A service.
// The initial handler establishes authenticated routing; the durable public
// Task and event layer will wrap runtime calls here rather than enter the gRPC
// transport or binary wiring.
package a2agateway

import (
	"context"
	"errors"
	"fmt"
	"iter"

	a2atype "github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2aclient"
	"github.com/a2aproject/a2a-go/v2/a2aext"
	"github.com/a2aproject/a2a-go/v2/a2asrv"
	dbpkg "github.com/kagent-dev/kagent/go/api/database"
	apiv1alpha1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1alpha1"
	"github.com/kagent-dev/kagent/go/core/pkg/auth"
	"github.com/kagent-dev/kagent/go/core/v2/agentinstance"
	"google.golang.org/grpc/metadata"
)

type instanceStore interface {
	GetAgentInstance(context.Context, string, string, string) (*apiv1alpha1.AgentInstance, error)
}

type runtimeDialer interface {
	Dial(context.Context, *apiv1alpha1.AgentInstance) (*a2aclient.Client, error)
}

// Gateway is transport-neutral. The v0 deployment registers it on the
// controller's gRPC server, while a standalone gateway can register the same
// handler on its own server later.
type Gateway struct {
	store      instanceStore
	authorizer auth.Authorizer
	dialer     runtimeDialer
}

var _ a2asrv.RequestHandler = (*Gateway)(nil)

// New returns the upstream A2A handler independently of any listener or gRPC
// server, keeping deployment topology outside the gateway package.
func New(store instanceStore, authorizer auth.Authorizer, dialer runtimeDialer) a2asrv.RequestHandler {
	return &a2asrv.InterceptedHandler{
		Handler:      &Gateway{store: store, authorizer: authorizer, dialer: dialer},
		Interceptors: []a2asrv.CallInterceptor{a2aext.NewServerPropagator(nil)},
	}
}

func (g *Gateway) runtime(ctx context.Context, verb auth.Verb) (*a2aclient.Client, error) {
	values := metadata.ValueFromIncomingContext(ctx, ":authority")
	if len(values) != 1 {
		return nil, a2atype.NewError(a2atype.ErrInvalidRequest, "exactly one AgentInstance authority is required")
	}
	namespace, id, err := agentinstance.ParseAuthority(values[0])
	if err != nil {
		return nil, a2atype.NewError(a2atype.ErrInvalidRequest, err.Error())
	}
	session, ok := auth.AuthSessionFrom(ctx)
	if !ok {
		return nil, a2atype.NewError(a2atype.ErrUnauthenticated, "authentication is required")
	}
	principal := session.Principal()
	if err := g.authorizer.Check(ctx, principal, verb, auth.Resource{Type: "AgentInstance", Name: namespace + "/" + id}); err != nil {
		return nil, a2atype.NewError(a2atype.ErrUnauthorized, "not authorized")
	}
	instance, err := g.store.GetAgentInstance(ctx, namespace, id, principal.User.ID)
	if errors.Is(err, dbpkg.ErrNotFound) {
		return nil, a2atype.NewError(a2atype.ErrUnauthorized, "not authorized")
	}
	if err != nil {
		return nil, a2atype.NewError(a2atype.ErrInternalError, fmt.Sprintf("load AgentInstance: %v", err))
	}
	if instance.GetA2AAuthority() != agentinstance.Authority(namespace, id) {
		return nil, a2atype.NewError(a2atype.ErrInternalError, "AgentInstance has an invalid A2A authority")
	}
	if instance.GetState() != apiv1alpha1.AgentInstanceState_AGENT_INSTANCE_STATE_READY {
		return nil, a2atype.NewError(a2atype.ErrUnsupportedOperation, fmt.Sprintf("AgentInstance is %s", instance.GetState()))
	}
	client, err := g.dialer.Dial(ctx, instance)
	if err != nil {
		return nil, a2atype.NewError(a2atype.ErrInternalError, fmt.Sprintf("connect to AgentInstance runtime: %v", err))
	}
	return client, nil
}

func (g *Gateway) GetTask(ctx context.Context, req *a2atype.GetTaskRequest) (*a2atype.Task, error) {
	client, err := g.runtime(ctx, auth.VerbGet)
	if err != nil {
		return nil, err
	}
	defer client.Destroy()
	return client.GetTask(ctx, req)
}

func (g *Gateway) ListTasks(ctx context.Context, req *a2atype.ListTasksRequest) (*a2atype.ListTasksResponse, error) {
	client, err := g.runtime(ctx, auth.VerbGet)
	if err != nil {
		return nil, err
	}
	defer client.Destroy()
	return client.ListTasks(ctx, req)
}

func (g *Gateway) CancelTask(ctx context.Context, req *a2atype.CancelTaskRequest) (*a2atype.Task, error) {
	client, err := g.runtime(ctx, auth.VerbUpdate)
	if err != nil {
		return nil, err
	}
	defer client.Destroy()
	return client.CancelTask(ctx, req)
}

func (g *Gateway) SendMessage(ctx context.Context, req *a2atype.SendMessageRequest) (a2atype.SendMessageResult, error) {
	client, err := g.runtime(ctx, auth.VerbCreate)
	if err != nil {
		return nil, err
	}
	defer client.Destroy()
	return client.SendMessage(ctx, req)
}

func (g *Gateway) SubscribeToTask(ctx context.Context, req *a2atype.SubscribeToTaskRequest) iter.Seq2[a2atype.Event, error] {
	client, err := g.runtime(ctx, auth.VerbGet)
	if err != nil {
		return errorEvents(err)
	}
	return closeAfter(client, client.SubscribeToTask(ctx, req))
}

func (g *Gateway) SendStreamingMessage(ctx context.Context, req *a2atype.SendMessageRequest) iter.Seq2[a2atype.Event, error] {
	client, err := g.runtime(ctx, auth.VerbCreate)
	if err != nil {
		return errorEvents(err)
	}
	return closeAfter(client, client.SendStreamingMessage(ctx, req))
}

func (g *Gateway) GetTaskPushConfig(ctx context.Context, req *a2atype.GetTaskPushConfigRequest) (*a2atype.PushConfig, error) {
	return nil, a2atype.ErrPushNotificationNotSupported
}

func (g *Gateway) ListTaskPushConfigs(ctx context.Context, req *a2atype.ListTaskPushConfigRequest) (*a2atype.ListTaskPushConfigResponse, error) {
	return nil, a2atype.ErrPushNotificationNotSupported
}

func (g *Gateway) CreateTaskPushConfig(ctx context.Context, req *a2atype.PushConfig) (*a2atype.PushConfig, error) {
	return nil, a2atype.ErrPushNotificationNotSupported
}

func (g *Gateway) DeleteTaskPushConfig(ctx context.Context, req *a2atype.DeleteTaskPushConfigRequest) error {
	return a2atype.ErrPushNotificationNotSupported
}

func (g *Gateway) GetExtendedAgentCard(ctx context.Context, req *a2atype.GetExtendedAgentCardRequest) (*a2atype.AgentCard, error) {
	return nil, a2atype.ErrExtendedCardNotConfigured
}

func errorEvents(err error) iter.Seq2[a2atype.Event, error] {
	return func(yield func(a2atype.Event, error) bool) {
		var zero a2atype.Event
		yield(zero, err)
	}
}

func closeAfter(client *a2aclient.Client, events iter.Seq2[a2atype.Event, error]) iter.Seq2[a2atype.Event, error] {
	return func(yield func(a2atype.Event, error) bool) {
		defer client.Destroy()
		for event, err := range events {
			if !yield(event, err) {
				return
			}
		}
	}
}
