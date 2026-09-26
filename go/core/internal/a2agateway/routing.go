package a2agateway

import (
	"context"
	"strings"

	a2a "github.com/a2aproject/a2a-go/v2/a2a"
	apiv1alpha1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1alpha1"
	"github.com/kagent-dev/kagent/go/core/internal/service/serviceerrors"
	sessionsvc "github.com/kagent-dev/kagent/go/core/internal/service/session"
	"github.com/kagent-dev/kagent/go/core/pkg/auth"
	"github.com/kagent-dev/kagent/go/pkg/logging"
	"k8s.io/apimachinery/pkg/util/validation"
)

// route resolves the Agent selected at the transport boundary. Both transports
// supply the SDK's routing metadata, so the gateway never inspects HTTP paths or
// chooses between a payload tenant and a URL.
func route(ctx context.Context) (*apiv1alpha1.ResourceReference, error) {
	tenant, _ := a2a.TenantFrom(ctx)
	namespace, name, ok := strings.Cut(tenant, "/")
	if !ok || len(validation.IsDNS1123Label(namespace)) != 0 || len(validation.IsDNS1123Subdomain(name)) != 0 {
		return nil, a2a.NewError(a2a.ErrInvalidRequest, "Agent tenant must be namespace/name")
	}
	return &apiv1alpha1.ResourceReference{Namespace: namespace, Name: name}, nil
}

func (g *Gateway) taskSession(ctx context.Context, verb auth.Verb, taskID a2a.TaskID) (*apiv1alpha1.Session, error) {
	agent, err := route(ctx)
	if err != nil {
		return nil, err
	}
	if _, ok := auth.AuthSessionFrom(ctx); !ok {
		return nil, a2a.ErrUnauthenticated
	}
	if taskID == "" {
		return nil, a2a.ErrInvalidParams
	}
	id, err := g.store.SessionForTask(ctx, string(taskID))
	if err != nil {
		return nil, g.storeError(ctx, err)
	}
	return g.storedSession(ctx, verb, agent, id)
}

func (g *Gateway) listSessions(ctx context.Context, agent *apiv1alpha1.ResourceReference, contextID string) ([]string, error) {
	if contextID != "" {
		session, err := g.storedSession(ctx, auth.VerbGet, agent, contextID)
		if err != nil {
			return nil, err
		}
		return []string{session.Id}, nil
	}
	ids := []string{}
	request := sessionsvc.ListRequest{Agent: agent, PageSize: 100}
	for {
		page, err := g.sessions.List(ctx, request)
		if err != nil {
			return nil, serviceError(ctx, err)
		}
		for _, session := range page.Sessions {
			ids = append(ids, session.Id)
		}
		if page.NextPageToken == "" {
			return ids, nil
		}
		request.PageToken = page.NextPageToken
	}
}

func serviceError(ctx context.Context, err error) error {
	switch serviceerrors.CodeOf(err) {
	case serviceerrors.CodeUnauthenticated:
		return a2a.ErrUnauthenticated
	case serviceerrors.CodePermissionDenied, serviceerrors.CodeNotFound:
		return a2a.ErrUnauthorized
	case serviceerrors.CodeInvalidArgument, serviceerrors.CodeAlreadyExists:
		return a2a.NewError(a2a.ErrInvalidRequest, serviceerrors.MessageOf(err))
	case serviceerrors.CodeFailedPrecondition, serviceerrors.CodeAborted:
		return a2a.NewError(a2a.ErrUnsupportedOperation, serviceerrors.MessageOf(err))
	default:
		logging.FromContext(ctx).ErrorContext(ctx, "agent conversation operation failed", "error", err)
		return a2a.ErrInternalError
	}
}
