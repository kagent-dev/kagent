package substrate

import (
	"context"
	"testing"

	"github.com/agent-substrate/substrate/pkg/proto/ateapipb"
	"github.com/kagent-dev/kagent/go/api/v1alpha2"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// statusActorClient is a configurable fake ate-api ControlClient. It holds full actor records
// (id, status, source template) so it can serve ListActors/GetActor and record DeleteActor.
// The embedded nil interface panics on any RPC not overridden below.
type statusActorClient struct {
	ateapipb.ControlClient
	actors   map[string]*ateapipb.Actor
	deleted  []string
	suspends []string
	resumes  []string
}

func (c *statusActorClient) add(a *ateapipb.Actor) { c.actors[actorName(a)] = a }

func (c *statusActorClient) GetActor(_ context.Context, in *ateapipb.GetActorRequest, _ ...grpc.CallOption) (*ateapipb.Actor, error) {
	a, ok := c.actors[in.GetActor().GetName()]
	if !ok {
		return nil, status.Error(codes.NotFound, "actor not found")
	}
	return a, nil
}

func (c *statusActorClient) DeleteActor(_ context.Context, in *ateapipb.DeleteActorRequest, _ ...grpc.CallOption) (*ateapipb.Actor, error) {
	id := in.GetActor().GetName()
	c.deleted = append(c.deleted, id)
	delete(c.actors, id)
	return &ateapipb.Actor{}, nil
}

func (c *statusActorClient) ListActors(context.Context, *ateapipb.ListActorsRequest, ...grpc.CallOption) (*ateapipb.ListActorsResponse, error) {
	out := make([]*ateapipb.Actor, 0, len(c.actors))
	for _, a := range c.actors {
		out = append(out, a)
	}
	return &ateapipb.ListActorsResponse{Actors: out}, nil
}

func (c *statusActorClient) SuspendActor(_ context.Context, in *ateapipb.SuspendActorRequest, _ ...grpc.CallOption) (*ateapipb.SuspendActorResponse, error) {
	c.suspends = append(c.suspends, in.GetActor().GetName())
	return &ateapipb.SuspendActorResponse{}, nil
}

func (c *statusActorClient) ResumeActor(_ context.Context, in *ateapipb.ResumeActorRequest, _ ...grpc.CallOption) (*ateapipb.ResumeActorResponse, error) {
	a, ok := c.actors[in.GetActor().GetName()]
	if !ok {
		return nil, status.Error(codes.NotFound, "actor not found")
	}
	a.Status = ateapipb.Actor_STATUS_RUNNING
	c.resumes = append(c.resumes, in.GetActor().GetName())
	return &ateapipb.ResumeActorResponse{Actor: a}, nil
}

// TestDeleteSandboxAgentSessionActor covers the one-session-one-actor delete: a single
// deterministic id covers the session's whole life, regardless of rollouts it survived.
func TestDeleteSandboxAgentSessionActor(t *testing.T) {
	t.Parallel()
	sa := reapAgent()
	actorID := SandboxAgentSessionActorID(sa, "sess-1")

	rec := &statusActorClient{actors: map[string]*ateapipb.Actor{}}
	rec.add(&ateapipb.Actor{Metadata: &ateapipb.ResourceMetadata{Name: actorID}, Status: ateapipb.Actor_STATUS_SUSPENDED})
	b := &SandboxAgentActorBackend{client: &Client{ControlClient: rec}}

	// deleteActor performs at most one mutating step per call; requeue until done.
	var done bool
	var err error
	for range 3 {
		done, err = b.DeleteSandboxAgentSessionActor(context.Background(), sa, "sess-1")
		require.NoError(t, err)
		if done {
			break
		}
	}
	require.True(t, done)
	require.Equal(t, []string{actorID}, rec.deleted)

	// Missing actor is already done.
	done, err = b.DeleteSandboxAgentSessionActor(context.Background(), sa, "sess-never")
	require.NoError(t, err)
	require.True(t, done)
}

// reapAgent is the SandboxAgent used by the reap tests.
func reapAgent() *v1alpha2.SandboxAgent {
	return &v1alpha2.SandboxAgent{ObjectMeta: metav1.ObjectMeta{Name: "agent", Namespace: "kagent"}}
}
