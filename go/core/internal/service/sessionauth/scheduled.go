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
	kind, verb := scheduledResource(verb)
	resource := auth.Resource{Type: kind, Namespace: ref.Namespace, Name: ref.Name}
	if err := authorizer.Check(ctx, caller.Principal(), verb, resource); err != nil {
		// Without history access the Session is indistinguishable from a missing one.
		if kind != scheduledRunHistory && authorizer.Check(ctx, caller.Principal(), auth.VerbGet, auth.Resource{Type: scheduledRunHistory, Namespace: ref.Namespace, Name: ref.Name}) == nil {
			return serviceerrors.NewPermissionDenied("Not authorized to access scheduled Session", err)
		}
		return serviceerrors.NewNotFound("Session not found", err)
	}
	return nil
}

const (
	scheduledRunHistory    = "ScheduledRunHistory"
	scheduledRunInvocation = "ScheduledRunInvocation"
)

// scheduledResource maps a Session operation to the schedule permission that
// governs it. Reads need history. Anything that starts or steers work, whether
// a new turn, a task reply, a cancellation, or a rename, is an invocation;
// removing retained data is an invocation delete.
func scheduledResource(verb auth.Verb) (string, auth.Verb) {
	switch verb {
	case auth.VerbGet, auth.VerbList:
		return scheduledRunHistory, auth.VerbGet
	case auth.VerbDelete:
		return scheduledRunInvocation, auth.VerbDelete
	default:
		return scheduledRunInvocation, auth.VerbCreate
	}
}
