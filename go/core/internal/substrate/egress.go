package substrate

import (
	"context"
	"fmt"

	"github.com/agent-substrate/substrate/pkg/proto/ateapipb"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
)

// EnsureActorEgressPolicy installs current authorization without restarting the
// actor. Read the backend version before resolving grants through authoritative
// Kubernetes reads. The version precondition orders competing policy writes; it
// cannot make cached Kubernetes state current. Conflicts retry both reads.
// Substrate deletes the policy with its Actor.
func (c *Client) EnsureActorEgressPolicy(ctx context.Context, atespace, name, revision string) error {
	ctx, cancel := c.callCtx(ctx)
	defer cancel()
	if c.cfg.EgressResolver == nil {
		return fmt.Errorf("actor egress resolver is required")
	}
	actor := actorRef(atespace, name)
	for range 5 {
		existing, err := c.GetActorEgressPolicy(ctx, &ateapipb.GetActorEgressPolicyRequest{Actor: actor})
		if err != nil && status.Code(err) != codes.NotFound {
			return err
		}
		access, err := c.cfg.EgressResolver.ResolveEgress(ctx, revision)
		if err != nil {
			return fmt.Errorf("resolve actor egress: %w", err)
		}
		policy, err := ActorEgressPolicy(atespace, access.Destinations, access.Credentials)
		if err != nil {
			return err
		}
		if existing == nil {
			_, err = c.CreateActorEgressPolicy(ctx, &ateapipb.CreateActorEgressPolicyRequest{Actor: actor, EgressPolicy: policy})
		} else {
			if proto.Equal(&ateapipb.EgressPolicy{Rules: existing.GetRules()}, &ateapipb.EgressPolicy{Rules: policy.GetRules()}) {
				return nil
			}
			policy.Metadata = proto.CloneOf(existing.GetMetadata())
			_, err = c.UpdateActorEgressPolicy(ctx, &ateapipb.UpdateActorEgressPolicyRequest{Actor: actor, EgressPolicy: policy})
		}
		if status.Code(err) != codes.AlreadyExists && status.Code(err) != codes.Aborted {
			return err
		}
	}
	return status.Error(codes.Aborted, "actor egress policy changed during reconciliation")
}
