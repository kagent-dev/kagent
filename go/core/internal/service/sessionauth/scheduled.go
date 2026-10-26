package sessionauth

import (
	"context"

	apiv1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1"
	"github.com/kagent-dev/kagent/go/core/internal/service/serviceerrors"
	"github.com/kagent-dev/kagent/go/core/pkg/auth"
)

// CheckScheduled authorizes access using the immutable schedule reference saved
// with a Session. It also applies to retained artifacts after source deletion.
func CheckScheduled(ctx context.Context, authorizer auth.Authorizer, verb auth.Verb, provenance *apiv1.ScheduledRunProvenance) error {
	if provenance.GetExecutionId() == "" {
		return nil
	}
	caller, ok := auth.AuthSessionFrom(ctx)
	if !ok {
		return serviceerrors.NewUnauthenticated("Failed to get authenticated principal", nil)
	}
	if _, internal := caller.(auth.ControlPlaneSession); internal {
		return nil
	}
	ref := provenance.GetRef()
	if ref.GetNamespace() == "" || ref.GetName() == "" {
		return serviceerrors.NewInternal("Session has invalid ScheduledRun provenance", nil)
	}
	kind := "ScheduledRunHistory"
	if verb != auth.VerbGet {
		kind = "ScheduledRunInvocation"
	}
	if err := authorizer.Check(ctx, caller.Principal(), verb, auth.Resource{Type: kind, Namespace: ref.Namespace, Name: ref.Name}); err != nil {
		return serviceerrors.NewPermissionDenied("Not authorized to access scheduled Session", err)
	}
	return nil
}
