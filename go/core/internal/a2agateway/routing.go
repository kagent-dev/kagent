package a2agateway

import (
	"context"
	"errors"
	"strings"

	a2a "github.com/a2aproject/a2a-go/v2/a2a"
	apiv1alpha1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1alpha1"
	"github.com/kagent-dev/kagent/go/core/internal/service/agentinstance"
	"github.com/kagent-dev/kagent/go/core/internal/service/serviceerrors"
	"github.com/kagent-dev/kagent/go/core/pkg/auth"
	"github.com/kagent-dev/kagent/go/pkg/logging"
	"k8s.io/apimachinery/pkg/util/validation"
)

type httpAgentKey struct{}

// route uses the URL for HTTP and the standard A2A tenant for gRPC. An HTTP
// payload cannot change the Agent selected by its URL.
func route(ctx context.Context, tenant string) (*apiv1alpha1.ResourceReference, error) {
	if urlAgent, ok := ctx.Value(httpAgentKey{}).(string); ok {
		if tenant != "" && tenant != urlAgent {
			return nil, a2a.NewError(a2a.ErrInvalidRequest, "tenant does not match the Agent URL")
		}
		tenant = urlAgent
	} else if tenant == "" {
		tenant, _ = a2a.TenantFrom(ctx)
	}
	namespace, name, ok := strings.Cut(tenant, "/")
	if !ok || len(validation.IsDNS1123Label(namespace)) != 0 || len(validation.IsDNS1123Subdomain(name)) != 0 {
		return nil, a2a.NewError(a2a.ErrInvalidRequest, "Agent tenant must be namespace/name")
	}
	return &apiv1alpha1.ResourceReference{Namespace: namespace, Name: name}, nil
}

func (g *Gateway) taskInstance(ctx context.Context, verb auth.Verb, tenant string, taskID a2a.TaskID) (*apiv1alpha1.AgentInstance, error) {
	agent, err := route(ctx, tenant)
	if err != nil {
		return nil, err
	}
	if _, ok := auth.AuthSessionFrom(ctx); !ok {
		return nil, a2a.ErrUnauthenticated
	}
	if taskID == "" {
		return nil, a2a.ErrInvalidParams
	}
	id, err := g.store.AgentInstanceForTask(ctx, string(taskID))
	if err != nil {
		return nil, g.storeError(ctx, err)
	}
	return g.storedInstance(ctx, verb, agent, id)
}

func (g *Gateway) listInstances(ctx context.Context, agent *apiv1alpha1.ResourceReference, contextID string) ([]string, error) {
	if contextID != "" {
		instance, err := g.storedInstance(ctx, auth.VerbGet, agent, contextID)
		if err != nil {
			return nil, err
		}
		return []string{instance.Id}, nil
	}
	// An instance share grants no access to other conversations of the Agent.
	if share, ok := auth.ShareContextFrom(ctx); ok {
		instance, err := g.storedInstance(ctx, auth.VerbGet, agent, share.AgentInstanceID)
		if err != nil {
			return nil, err
		}
		return []string{instance.Id}, nil
	}
	ids := []string{}
	request := agentinstance.ListRequest{Agent: agent, PageSize: 100}
	for {
		page, err := g.instances.List(ctx, request)
		if err != nil {
			return nil, serviceError(ctx, err)
		}
		for _, instance := range page.Instances {
			if _, err := g.storedInstance(ctx, auth.VerbGet, agent, instance.Id); err != nil {
				if errors.Is(err, a2a.ErrUnauthorized) {
					continue
				}
				return nil, err
			}
			ids = append(ids, instance.Id)
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
