package substrate

import (
	"context"
	"fmt"
	"slices"

	"github.com/agent-substrate/substrate/pkg/proto/ateapipb"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"

	"github.com/kagent-dev/kagent/go/core/internal/egress"
)

// EnsureActorEgressPolicy installs current authorization without restarting the
// actor and returns the effective policy it applied, including withheld-grant
// diagnostics. Read the backend version before the controller's persisted intended
// policy so concurrent applications cannot overwrite a newer policy with an old
// one. Conflicts reload both; lifecycle paths never translate Kubernetes.
// Substrate deletes the policy with its Actor.
func (c *Client) EnsureActorEgressPolicy(ctx context.Context, atespace, name, revision string) (egress.Policy, error) {
	ctx, cancel := c.callCtx(ctx)
	defer cancel()
	if c.cfg.EgressPolicies == nil {
		return egress.Policy{}, fmt.Errorf("actor egress policy store is required")
	}
	actor := actorRef(atespace, name)
	for range 5 {
		existing, err := c.GetActorEgressPolicy(ctx, &ateapipb.GetActorEgressPolicyRequest{Actor: actor})
		if err != nil && status.Code(err) != codes.NotFound {
			return egress.Policy{}, err
		}
		access, err := c.cfg.EgressPolicies.GetRuntimeEgressPolicy(ctx, revision)
		if err != nil {
			return egress.Policy{}, fmt.Errorf("load intended actor egress: %w", err)
		}
		policy, err := ActorEgressPolicy(atespace, access.Destinations, access.Credentials)
		if err != nil {
			return egress.Policy{}, err
		}
		if existing == nil {
			_, err = c.CreateActorEgressPolicy(ctx, &ateapipb.CreateActorEgressPolicyRequest{Actor: actor, EgressPolicy: policy})
		} else {
			if !proto.Equal(&ateapipb.EgressPolicy{Rules: existing.GetRules()}, &ateapipb.EgressPolicy{Rules: policy.GetRules()}) {
				policy.Metadata = proto.CloneOf(existing.GetMetadata())
				_, err = c.UpdateActorEgressPolicy(ctx, &ateapipb.UpdateActorEgressPolicyRequest{Actor: actor, EgressPolicy: policy})
			}
		}
		if status.Code(err) == codes.AlreadyExists || status.Code(err) == codes.Aborted {
			continue
		}
		if err != nil {
			return egress.Policy{}, err
		}
		// A source update may have discovered this instance before its Actor
		// existed. Recheck after applying so creation cannot miss that update.
		latest, err := c.cfg.EgressPolicies.GetRuntimeEgressPolicy(ctx, revision)
		if err != nil {
			return egress.Policy{}, fmt.Errorf("reload intended actor egress: %w", err)
		}
		if slices.Equal(access.Destinations, latest.Destinations) && slices.Equal(access.Credentials, latest.Credentials) {
			return latest, nil
		}
	}
	return egress.Policy{}, status.Error(codes.Aborted, "actor egress policy changed during reconciliation")
}
