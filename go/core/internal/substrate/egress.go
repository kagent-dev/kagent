package substrate

import (
	"context"
	"fmt"

	"github.com/agent-substrate/substrate/pkg/proto/ateapipb"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
)

// EnsureActorEgressPolicy can be retried after a lost create response. A prepared
// revision is immutable, so an existing policy must match; never accept or
// overwrite a different allowlist, except inherited: the template's default a
// new Actor starts with, which is replaced before the Actor first runs.
// Substrate deletes the policy with its Actor.
func (c *Client) EnsureActorEgressPolicy(ctx context.Context, atespace, name string, policy, inherited *ateapipb.EgressPolicy) error {
	ctx, cancel := c.callCtx(ctx)
	defer cancel()
	actor := actorRef(atespace, name)
	_, err := c.CreateActorEgressPolicy(ctx, &ateapipb.CreateActorEgressPolicyRequest{
		Actor: actor, EgressPolicy: policy,
	})
	if status.Code(err) != codes.AlreadyExists {
		return err
	}
	existing, err := c.GetActorEgressPolicy(ctx, &ateapipb.GetActorEgressPolicyRequest{Actor: actor})
	if err != nil {
		return err
	}
	if sameRules(existing, policy) {
		return nil
	}
	if inherited == nil || !sameRules(existing, inherited) {
		return fmt.Errorf("existing Actor egress policy does not match the prepared revision")
	}
	// The existing metadata carries the uid and version preconditions.
	replacement := &ateapipb.EgressPolicy{Metadata: existing.GetMetadata(), Rules: policy.GetRules()}
	_, err = c.UpdateActorEgressPolicy(ctx, &ateapipb.UpdateActorEgressPolicyRequest{Actor: actor, EgressPolicy: replacement})
	return err
}

func sameRules(a, b *ateapipb.EgressPolicy) bool {
	return proto.Equal(&ateapipb.EgressPolicy{Rules: a.GetRules()}, &ateapipb.EgressPolicy{Rules: b.GetRules()})
}

// ReplaceActorEgressPolicy gives an Actor the allowlist of the revision it is
// repointed to: it creates the policy, or replaces the rules of the one it has.
func (c *Client) ReplaceActorEgressPolicy(ctx context.Context, atespace, name string, policy *ateapipb.EgressPolicy) error {
	ctx, cancel := c.callCtx(ctx)
	defer cancel()
	actor := actorRef(atespace, name)
	_, err := c.CreateActorEgressPolicy(ctx, &ateapipb.CreateActorEgressPolicyRequest{
		Actor: actor, EgressPolicy: policy,
	})
	if status.Code(err) != codes.AlreadyExists {
		return err
	}
	existing, err := c.GetActorEgressPolicy(ctx, &ateapipb.GetActorEgressPolicyRequest{Actor: actor})
	if err != nil {
		return err
	}
	if sameRules(existing, policy) {
		return nil
	}
	// The existing metadata carries the uid and version preconditions.
	replacement := &ateapipb.EgressPolicy{Metadata: existing.GetMetadata(), Rules: policy.GetRules()}
	_, err = c.UpdateActorEgressPolicy(ctx, &ateapipb.UpdateActorEgressPolicyRequest{Actor: actor, EgressPolicy: replacement})
	return err
}
