package substrate

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	atev1alpha1 "github.com/agent-substrate/substrate/pkg/api/v1alpha1"
	"github.com/agent-substrate/substrate/pkg/proto/ateapipb"
	"github.com/kagent-dev/kagent/go/api/v1alpha2"
	"github.com/kagent-dev/kagent/go/core/pkg/consts"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
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

func (c *statusActorClient) add(a *ateapipb.Actor) { c.actors[a.GetActorId()] = a }

func (c *statusActorClient) GetActor(_ context.Context, in *ateapipb.GetActorRequest, _ ...grpc.CallOption) (*ateapipb.GetActorResponse, error) {
	a, ok := c.actors[in.GetActorRef().GetName()]
	if !ok {
		return nil, status.Error(codes.NotFound, "actor not found")
	}
	return &ateapipb.GetActorResponse{Actor: a}, nil
}

func (c *statusActorClient) DeleteActor(_ context.Context, in *ateapipb.DeleteActorRequest, _ ...grpc.CallOption) (*ateapipb.DeleteActorResponse, error) {
	id := in.GetActorRef().GetName()
	c.deleted = append(c.deleted, id)
	delete(c.actors, id)
	return &ateapipb.DeleteActorResponse{}, nil
}

func (c *statusActorClient) ListActors(context.Context, *ateapipb.ListActorsRequest, ...grpc.CallOption) (*ateapipb.ListActorsResponse, error) {
	out := make([]*ateapipb.Actor, 0, len(c.actors))
	for _, a := range c.actors {
		out = append(out, a)
	}
	return &ateapipb.ListActorsResponse{Actors: out}, nil
}

func (c *statusActorClient) SuspendActor(_ context.Context, in *ateapipb.SuspendActorRequest, _ ...grpc.CallOption) (*ateapipb.SuspendActorResponse, error) {
	c.suspends = append(c.suspends, in.GetActorRef().GetName())
	return &ateapipb.SuspendActorResponse{}, nil
}

func (c *statusActorClient) ResumeActor(_ context.Context, in *ateapipb.ResumeActorRequest, _ ...grpc.CallOption) (*ateapipb.ResumeActorResponse, error) {
	a, ok := c.actors[in.GetActorRef().GetName()]
	if !ok {
		return nil, status.Error(codes.NotFound, "actor not found")
	}
	a.Status = ateapipb.Actor_STATUS_RUNNING
	c.resumes = append(c.resumes, in.GetActorRef().GetName())
	return &ateapipb.ResumeActorResponse{Actor: a}, nil
}

// TestDeleteSandboxAgentSessionActor covers the one-session-one-actor delete: a single
// deterministic id covers the session's whole life, regardless of rollouts it survived.
func TestDeleteSandboxAgentSessionActor(t *testing.T) {
	t.Parallel()
	sa := reapAgent()
	actorID := SandboxAgentSessionActorID(sa, "sess-1")

	rec := &statusActorClient{actors: map[string]*ateapipb.Actor{}}
	rec.add(&ateapipb.Actor{ActorId: actorID, Status: ateapipb.Actor_STATUS_SUSPENDED})
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

// reapTemplate builds a labeled, config-hashed ActorTemplate for the reap-test agent.
func reapTemplate(hash string, gen int64, phase atev1alpha1.PhaseType) *atev1alpha1.ActorTemplate {
	return &atev1alpha1.ActorTemplate{ObjectMeta: metav1.ObjectMeta{
		Name: "agent-" + hash, Namespace: "kagent",
		Labels: map[string]string{SandboxAgentLabelKey: "agent"},
		Annotations: map[string]string{
			consts.ConfigHashAnnotation: hash,
			desiredGenerationAnnotation: strconv.FormatInt(gen, 10),
		},
	}, Status: atev1alpha1.ActorTemplateStatus{Phase: phase}}
}

// TestFetchLocalSessionEvents covers the events read-through: resume when suspended, read the
// runtime's local-events route via the router with actor Host routing, and suspend afterwards
// ONLY when the read woke the actor (never checkpoint an in-flight chat). A 404 from the actor
// (image without the route) must fail loud, not return an empty list.
func TestFetchLocalSessionEvents(t *testing.T) {
	t.Parallel()
	rows := `[{"id":"e1","data":"{\"author\":\"user\"}","created_at":"2026-07-06T10:00:00Z"}]`

	for _, tc := range []struct {
		name         string
		status       ateapipb.Actor_Status
		routeMissing bool
		wantResume   bool
		wantSuspend  bool
		wantErrIs    error
	}{
		{
			name:        "suspended actor: resumed, read, suspended again",
			status:      ateapipb.Actor_STATUS_SUSPENDED,
			wantResume:  true,
			wantSuspend: true,
		},
		{
			name:        "running actor: read without touching its lifecycle",
			status:      ateapipb.Actor_STATUS_RUNNING,
			wantResume:  false,
			wantSuspend: false,
		},
		{
			name:         "actor without the local route: fails loud, still suspended",
			status:       ateapipb.Actor_STATUS_SUSPENDED,
			routeMissing: true,
			wantResume:   true,
			wantSuspend:  true,
			wantErrIs:    ErrLocalSessionEventsUnsupported,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			scheme := runtime.NewScheme()
			utilruntime.Must(atev1alpha1.AddToScheme(scheme))
			cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(
				reapTemplate("cur", 1, atev1alpha1.PhaseReady),
			).Build()
			sa := reapAgent()
			actorID := SandboxAgentSessionActorID(sa, "sess-1")

			rec := &statusActorClient{actors: map[string]*ateapipb.Actor{}}
			rec.add(&ateapipb.Actor{ActorId: actorID, Status: tc.status, ActorTemplateNamespace: "kagent", ActorTemplateName: "agent-cur"})

			var gotHost string
			router := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch {
				case r.URL.Path == "/health":
					w.WriteHeader(http.StatusOK)
				case r.URL.Path == "/local/sessions/sess-1/events" && !tc.routeMissing:
					gotHost = r.Host
					require.Equal(t, "jm@solo.io", r.URL.Query().Get("user_id"))
					_, _ = w.Write([]byte(rows))
				default:
					http.NotFound(w, r)
				}
			}))
			defer router.Close()

			backend := NewSandboxAgentActorBackend(&Client{ControlClient: rec}, cl, router.URL)
			body, err := backend.FetchLocalSessionEvents(context.Background(), sa, "sess-1", "jm@solo.io")

			if tc.wantErrIs != nil {
				require.ErrorIs(t, err, tc.wantErrIs)
			} else {
				require.NoError(t, err)
				require.JSONEq(t, rows, string(body))
				require.Equal(t, ActorHost(actorID, ""), gotHost, "events fetch must use actor Host routing")
			}
			if tc.wantResume {
				require.Equal(t, []string{actorID}, rec.resumes)
			} else {
				require.Empty(t, rec.resumes)
			}
			if tc.wantSuspend {
				require.Equal(t, []string{actorID}, rec.suspends, "read must re-suspend the actor it woke")
			} else {
				require.Empty(t, rec.suspends, "read must not suspend an actor it did not wake")
			}
		})
	}
}
