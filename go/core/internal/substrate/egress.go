package substrate

import (
	"context"
	"fmt"
	"slices"

	ateapipb "go.ate.dev/substrate/api/ateapi/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"

	"github.com/kagent-dev/kagent/go/core/internal/egress"
)

// CheckActorEgressPolicy compiles the revision's policy without applying it, so
// a compiler failure stops lifecycle work before it is issued.
func (c *Client) CheckActorEgressPolicy(ctx context.Context, atespace, revision string) error {
	if c.cfg.EgressPolicies == nil {
		return fmt.Errorf("actor egress policy store is required")
	}
	access, err := c.cfg.EgressPolicies.GetRuntimeEgressPolicy(ctx, revision)
	if err != nil {
		return fmt.Errorf("load intended actor egress: %w", err)
	}
	_, err = compileActorEgressPolicy(ctx, atespace, access, c.cfg.ControlPlaneURL, c.cfg.EgressPolicyCompilers)
	return err
}

// EnsureActorEgressPolicy installs current authorization without restarting the
// actor. Read the backend version before the controller's persisted inputs so
// concurrent applications cannot overwrite a newer policy with an old one.
// Conflicts reload both; lifecycle paths never translate Kubernetes.
// Substrate deletes the policy with its Actor.
func (c *Client) EnsureActorEgressPolicy(ctx context.Context, atespace, name, revision string) error {
	ctx, cancel := c.callCtx(ctx)
	defer cancel()
	if c.cfg.EgressPolicies == nil {
		return fmt.Errorf("actor egress policy store is required")
	}
	actor := actorRef(atespace, name)
	for range 5 {
		existing, err := c.GetActorEgressPolicy(ctx, &ateapipb.GetActorEgressPolicyRequest{Actor: actor, Name: "default"})
		if err != nil && status.Code(err) != codes.NotFound {
			return err
		}
		access, err := c.cfg.EgressPolicies.GetRuntimeEgressPolicy(ctx, revision)
		if err != nil {
			return fmt.Errorf("load intended actor egress: %w", err)
		}
		policy, chainErr := compileActorEgressPolicy(ctx, atespace, access, c.cfg.ControlPlaneURL, c.cfg.EgressPolicyCompilers)
		if chainErr != nil {
			// No rules denies all; extensions may have removed built-in access.
			policy = &ateapipb.EgressPolicy{Metadata: &ateapipb.ResourceMetadata{Atespace: atespace, Name: "default"}}
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
			return err
		}
		if chainErr != nil {
			return fmt.Errorf("denied actor egress after extension failure: %w", chainErr)
		}
		// A source update may have discovered this instance before its Actor
		// existed. Recheck after applying so creation cannot miss that update.
		latest, err := c.cfg.EgressPolicies.GetRuntimeEgressPolicy(ctx, revision)
		if err != nil {
			return fmt.Errorf("reload intended actor egress: %w", err)
		}
		if slices.Equal(access.Destinations, latest.Destinations) && slices.EqualFunc(access.Credentials, latest.Credentials, egress.Credential.Equal) &&
			slices.Equal(access.TokenPassthroughTargets, latest.TokenPassthroughTargets) {
			return nil
		}
	}
	return status.Error(codes.Aborted, "actor egress policy changed during reconciliation")
}
