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

func TestActorEgressPolicy(t *testing.T) {
	policy, err := ActorEgressPolicy("team-a", []string{"API.Example.com.", "api.example.com", "192.0.2.1", "2001:db8::1", "::ffff:192.0.2.1"})
	require.NoError(t, err)
	require.Equal(t, &ateapipb.ResourceMetadata{Atespace: "team-a", Name: "default"}, policy.Metadata)
	require.Len(t, policy.Rules, 2)
	require.Equal(t, []string{"api.example.com"}, policy.Rules[0].GetHostnames().GetPatterns())
	require.Equal(t, []string{"192.0.2.1/32", "2001:db8::1/128"}, policy.Rules[1].GetCidrs().GetCidrs())
	policy, err = ActorEgressPolicy("team-a", nil)
	require.NoError(t, err)
	require.Empty(t, policy.Rules, "no destinations must deny all egress")
	for _, destination := range []string{"", "*", "https://api.example.com", "api.example.com:443", "192.0.2.0/24", "fe80::1%eth0"} {
		t.Run(destination, func(t *testing.T) {
			_, err := ActorEgressPolicy("team-a", []string{destination})
			require.Error(t, err)
		})
	}
}

func TestEnsureActorEgressPolicyRetriesLostResponse(t *testing.T) {
	fake := &egressPolicyFake{createErr: context.DeadlineExceeded}
	client := &Client{ControlClient: fake}
	policy, err := ActorEgressPolicy("team-a", []string{"api.example.com", "192.0.2.1"})
	require.NoError(t, err)
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
