package session

import (
	"context"
	"fmt"

	apiv1alpha1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1alpha1"
	kagentv1alpha3 "github.com/kagent-dev/kagent/go/api/v1alpha3"
	"github.com/kagent-dev/kagent/go/core/internal/service/serviceerrors"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
)

// refuseUnresolvedHarness refuses new work for the Agent while the Harness it
// references does not resolve. Removing a Harness switches its runtime off:
// the controller then reports the Agent's references unresolved, but the
// Agent's last prepared revision stays ready, so without this check new
// conversations and turns would go on running on it. Work already running is
// left alone; the refusal ends as soon as the Harness resolves again. An Agent
// that declares its Harness inline, or that is gone, is left to the checks
// that already cover it, and without an Agent reader nothing is checked.
func (s *Service) refuseUnresolvedHarness(ctx context.Context, ref *apiv1alpha1.ResourceReference) error {
	if s.agents == nil {
		return nil
	}
	agent := &kagentv1alpha3.Agent{}
	err := s.agents.Get(ctx, types.NamespacedName{Namespace: ref.GetNamespace(), Name: ref.GetName()}, agent)
	if apierrors.IsNotFound(err) {
		return nil
	}
	if err != nil {
		return serviceerrors.NewUnavailable("Failed to check the Agent's Harness", err)
	}
	if agent.Spec.HarnessRef == nil {
		return nil
	}
	key := types.NamespacedName{Namespace: agent.Namespace, Name: agent.Spec.HarnessRef.Name}
	err = s.agents.Get(ctx, key, &kagentv1alpha3.Harness{})
	if apierrors.IsNotFound(err) {
		return serviceerrors.NewFailedPrecondition(fmt.Sprintf(
			"Agent %s/%s cannot start new work: its Harness %q does not resolve (%s=False, ReferenceResolutionFailed)",
			agent.Namespace, agent.Name, key.String(), kagentv1alpha3.AgentConditionResolvedRefs), err)
	}
	if err != nil {
		return serviceerrors.NewUnavailable("Failed to check the Agent's Harness", err)
	}
	return nil
}
