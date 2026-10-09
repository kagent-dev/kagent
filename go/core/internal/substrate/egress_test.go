package substrate

import (
	"context"
	"testing"

	"github.com/agent-substrate/substrate/pkg/proto/ateapipb"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
)

func TestEnsureActorEgressPolicyRetriesLostResponse(t *testing.T) {
	fake := &egressPolicyFake{createErr: context.DeadlineExceeded}
	client := &Client{ControlClient: fake}
	policy := &ateapipb.EgressPolicy{
		Metadata: &ateapipb.ResourceMetadata{Atespace: "team-a", Name: "default"},
		Rules: []*ateapipb.EgressRule{
			{Http: &ateapipb.HTTPRule{Hostnames: []string{"api.example.com"}}},
			{Https: &ateapipb.HTTPSRule{Hostnames: []string{"api.example.com"}}},
		},
	}
	require.ErrorIs(t, client.EnsureActorEgressPolicy(t.Context(), "team-a", "actor", policy, nil), context.DeadlineExceeded)
	require.NotNil(t, fake.policy, "the server committed before the response was lost")
	fake.createErr = nil
	require.NoError(t, client.EnsureActorEgressPolicy(t.Context(), "team-a", "actor", policy, nil))
	require.NoError(t, client.EnsureActorEgressPolicy(t.Context(), "team-a", "actor", policy, nil))
	require.Equal(t, 1, fake.created)
	require.Equal(t, actorRef("team-a", "actor"), fake.actor)
	require.ErrorContains(t, client.EnsureActorEgressPolicy(t.Context(), "team-a", "actor", &ateapipb.EgressPolicy{}, nil), "does not match")
	fake.getErr = status.Error(codes.Unavailable, "unavailable")
	require.Equal(t, codes.Unavailable, status.Code(client.EnsureActorEgressPolicy(t.Context(), "team-a", "actor", policy, nil)))
}

// A new Actor starts with its template's default allowlist. When that one
// carries a binding for the golden boot only, the prepared policy replaces it;
// any other allowlist is still refused.
func TestEnsureActorEgressPolicyReplacesTheInheritedDefault(t *testing.T) {
	rule := func(effects *ateapipb.HttpRuleEffects) []*ateapipb.EgressRule {
		return []*ateapipb.EgressRule{{Https: &ateapipb.HTTPSRule{Hostnames: []string{"git.example.com"}, Effects: effects}}}
	}
	inherited := &ateapipb.EgressPolicy{Metadata: &ateapipb.ResourceMetadata{Atespace: "team-a", Name: "default"}, Rules: rule(&ateapipb.HttpRuleEffects{
		ReplaceHeaders: []*ateapipb.CredentialHeader{{Header: "authorization", Prefix: "Basic ", CredentialUri: "ate-secret://k8s.io/default/team-a/git/token"}},
	})}
	policy := &ateapipb.EgressPolicy{Metadata: &ateapipb.ResourceMetadata{Atespace: "team-a", Name: "default"}, Rules: rule(nil)}
	fake := &egressPolicyFake{policy: proto.CloneOf(inherited)}
	client := &Client{ControlClient: fake}

	require.ErrorContains(t, client.EnsureActorEgressPolicy(t.Context(), "team-a", "actor", policy, nil), "does not match", "without an inherited default nothing is replaced")
	require.Zero(t, fake.updated)
	require.NoError(t, client.EnsureActorEgressPolicy(t.Context(), "team-a", "actor", policy, inherited))
	require.Equal(t, 1, fake.updated)
	require.True(t, proto.Equal(&ateapipb.EgressPolicy{Rules: policy.Rules}, &ateapipb.EgressPolicy{Rules: fake.policy.Rules}), "the Actor holds the prepared allowlist")
	require.NoError(t, client.EnsureActorEgressPolicy(t.Context(), "team-a", "actor", policy, inherited), "a retry finds the prepared allowlist")
	require.Equal(t, 1, fake.updated)

	other := &ateapipb.EgressPolicy{Rules: []*ateapipb.EgressRule{{Https: &ateapipb.HTTPSRule{Hostnames: []string{"other.example.com"}}}}}
	fake.policy = proto.CloneOf(other)
	require.ErrorContains(t, client.EnsureActorEgressPolicy(t.Context(), "team-a", "actor", policy, inherited), "does not match")
	require.Equal(t, 1, fake.updated)
}

type egressPolicyFake struct {
	ateapipb.ControlClient
	policy            *ateapipb.EgressPolicy
	actor             *ateapipb.ObjectRef
	createErr, getErr error
	created, updated  int
}

func (f *egressPolicyFake) CreateActorEgressPolicy(_ context.Context, req *ateapipb.CreateActorEgressPolicyRequest, _ ...grpc.CallOption) (*ateapipb.EgressPolicy, error) {
	f.actor = req.Actor
	if f.policy != nil {
		return nil, status.Error(codes.AlreadyExists, "exists")
	}
	f.policy = proto.CloneOf(req.EgressPolicy)
	f.policy.Metadata.Uid = "policy-uid"
	f.policy.Metadata.Version = 1
	f.created++
	return f.policy, f.createErr
}

func (f *egressPolicyFake) GetActorEgressPolicy(_ context.Context, req *ateapipb.GetActorEgressPolicyRequest, _ ...grpc.CallOption) (*ateapipb.EgressPolicy, error) {
	f.actor = req.Actor
	return f.policy, f.getErr
}

func (f *egressPolicyFake) UpdateActorEgressPolicy(_ context.Context, req *ateapipb.UpdateActorEgressPolicyRequest, _ ...grpc.CallOption) (*ateapipb.EgressPolicy, error) {
	f.actor = req.Actor
	f.policy = proto.CloneOf(req.EgressPolicy)
	f.updated++
	return f.policy, nil
}
