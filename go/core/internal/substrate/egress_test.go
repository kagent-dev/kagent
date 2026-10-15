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

	"github.com/kagent-dev/kagent/go/core/internal/egress"
)

type egressResolverFunc func(context.Context, string) (egress.Access, error)

func (f egressResolverFunc) ResolveEgress(ctx context.Context, revision string) (egress.Access, error) {
	return f(ctx, revision)
}

var _ egress.Resolver = egressResolverFunc(nil)

func TestEnsureActorEgressPolicyUpdatesCurrentGrants(t *testing.T) {
	fake := &egressPolicyFake{createErr: context.DeadlineExceeded}
	access := egress.Access{Destinations: []string{"https://api.example.com:443"}}
	client := &Client{ControlClient: fake, cfg: Config{EgressResolver: egressResolverFunc(func(_ context.Context, revision string) (egress.Access, error) {
		require.Equal(t, "pinned-revision", revision)
		return access, nil
	})}}
	reconcile := func() error { return client.EnsureActorEgressPolicy(t.Context(), "team-a", "actor", "pinned-revision") }
	require.ErrorIs(t, reconcile(), context.DeadlineExceeded)
	require.NotNil(t, fake.policy, "the server committed before the response was lost")
	fake.createErr = nil
	require.NoError(t, reconcile())
	require.NoError(t, reconcile())
	require.Equal(t, 1, fake.created)
	require.Zero(t, fake.updated)
	require.Equal(t, actorRef("team-a", "actor"), fake.actor)

	access = egress.Access{}
	require.NoError(t, reconcile())
	require.Empty(t, fake.policy.Rules, "removing the grant revokes the existing actor")
	require.Equal(t, 1, fake.updated)
	access = egress.Access{Destinations: []string{"https://api.example.com:443"}, Credentials: []egress.Credential{{Origin: "https://api.example.com:443", Header: "authorization", URI: "ate-secret://k8s.io/default/team-a/auth/token", Prefix: "Bearer "}}}
	require.NoError(t, reconcile())
	require.Equal(t, "Bearer ", fake.policy.Rules[0].GetHttps().GetEffects().GetReplaceHeaders()[0].Prefix)
	access.Credentials[0].URI = "ate-secret://k8s.io/default/team-a/rotated/token"
	require.NoError(t, reconcile())
	require.Equal(t, access.Credentials[0].URI, fake.policy.Rules[0].GetHttps().GetEffects().GetReplaceHeaders()[0].CredentialUri)
	fake.getErr = status.Error(codes.Unavailable, "unavailable")
	require.Equal(t, codes.Unavailable, status.Code(reconcile()))
}

func TestEnsureActorEgressPolicyReResolvesAfterConflict(t *testing.T) {
	fake := &egressPolicyFake{}
	access := egress.Access{}
	client := &Client{ControlClient: fake, cfg: Config{EgressResolver: egressResolverFunc(func(context.Context, string) (egress.Access, error) { return access, nil })}}
	reconcile := func() error { return client.EnsureActorEgressPolicy(t.Context(), "team-a", "actor", "revision") }
	require.NoError(t, reconcile())
	access.Destinations = []string{"https://revoked.example.com:443"}
	fake.beforeUpdate = func() {
		// Another reconciliation committed revocation after this attempt read the
		// backend version. Retrying must resolve current grants, not replay the allow.
		access = egress.Access{}
		fake.policy.Metadata.Version++
	}
	require.NoError(t, reconcile())
	require.Empty(t, fake.policy.Rules)
	require.Zero(t, fake.updated)
}

type egressPolicyFake struct {
	ateapipb.ControlClient
	policy            *ateapipb.EgressPolicy
	actor             *ateapipb.ObjectRef
	createErr, getErr error
	created, updated  int
	beforeUpdate      func()
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
	return proto.CloneOf(f.policy), f.createErr
}

func (f *egressPolicyFake) GetActorEgressPolicy(_ context.Context, req *ateapipb.GetActorEgressPolicyRequest, _ ...grpc.CallOption) (*ateapipb.EgressPolicy, error) {
	f.actor = req.Actor
	if f.getErr != nil {
		return nil, f.getErr
	}
	if f.policy == nil {
		return nil, status.Error(codes.NotFound, "missing")
	}
	return proto.CloneOf(f.policy), nil
}

func (f *egressPolicyFake) UpdateActorEgressPolicy(_ context.Context, req *ateapipb.UpdateActorEgressPolicyRequest, _ ...grpc.CallOption) (*ateapipb.EgressPolicy, error) {
	if f.beforeUpdate != nil {
		f.beforeUpdate()
		f.beforeUpdate = nil
	}
	if req.EgressPolicy.Metadata.Version != f.policy.Metadata.Version || req.EgressPolicy.Metadata.Uid != f.policy.Metadata.Uid {
		return nil, status.Error(codes.Aborted, "version conflict")
	}
	f.policy = proto.CloneOf(req.EgressPolicy)
	f.policy.Metadata.Version++
	f.updated++
	return proto.CloneOf(f.policy), nil
}
