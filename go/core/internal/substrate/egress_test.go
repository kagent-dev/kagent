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
			{Hostnames: &ateapipb.HostnameRule{Patterns: []string{"api.example.com"}}},
			{Cidrs: &ateapipb.CIDRRule{Cidrs: []string{"192.0.2.1/32"}}},
		},
	}
	require.ErrorIs(t, client.EnsureActorEgressPolicy(t.Context(), "team-a", "actor", policy), context.DeadlineExceeded)
	require.NotNil(t, fake.policy, "the server committed before the response was lost")
	fake.createErr = nil
	require.NoError(t, client.EnsureActorEgressPolicy(t.Context(), "team-a", "actor", policy))
	require.NoError(t, client.EnsureActorEgressPolicy(t.Context(), "team-a", "actor", policy))
	require.Equal(t, 1, fake.created)
	require.Equal(t, actorRef("team-a", "actor"), fake.actor)
	require.ErrorContains(t, client.EnsureActorEgressPolicy(t.Context(), "team-a", "actor", &ateapipb.EgressPolicy{}), "does not match")
	fake.getErr = status.Error(codes.Unavailable, "unavailable")
	require.Equal(t, codes.Unavailable, status.Code(client.EnsureActorEgressPolicy(t.Context(), "team-a", "actor", policy)))
}

type egressPolicyFake struct {
	ateapipb.ControlClient
	policy            *ateapipb.EgressPolicy
	actor             *ateapipb.ObjectRef
	createErr, getErr error
	created           int
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

func TestActorEgressPolicyAcceptsLeftmostLabelWildcards(t *testing.T) {
	policy, err := ActorEgressPolicy("team-a", []string{"*.githubusercontent.com", "GitHub.com.", "192.0.2.1"}, nil)
	require.NoError(t, err)
	require.Len(t, policy.Rules, 2)
	require.Equal(t, []string{"*.githubusercontent.com", "github.com"}, policy.Rules[0].GetHostnames().GetPatterns())
	require.Equal(t, []string{"192.0.2.1/32"}, policy.Rules[1].GetCidrs().GetCidrs())
	for _, invalid := range []string{"*", "*.", "*.*.example.com", "a.*.example.com", "*github.com", "-bad.example.com"} {
		_, err := ActorEgressPolicy("team-a", []string{invalid}, nil)
		require.Error(t, err, invalid)
	}
}
