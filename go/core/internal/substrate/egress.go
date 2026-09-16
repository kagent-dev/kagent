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
// overwrite a different allowlist. Substrate deletes the policy with its Actor.
func (c *Client) EnsureActorEgressPolicy(ctx context.Context, atespace, name string, policy *ateapipb.EgressPolicy) error {
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
	if !proto.Equal(&ateapipb.EgressPolicy{Rules: existing.GetRules()}, &ateapipb.EgressPolicy{Rules: policy.Rules}) {
		return fmt.Errorf("existing Actor egress policy does not match the prepared revision")
	}
	return nil
}
