package substrate

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	atev1alpha1 "github.com/agent-substrate/substrate/pkg/api/v1alpha1"
	"github.com/agent-substrate/substrate/pkg/proto/ateapipb"
	"github.com/kagent-dev/kagent/go/api/v1alpha2"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestEnsureSandboxSessionActorAcrossSuspension(t *testing.T) {
	for _, tc := range []struct {
		name          string
		states        []ateapipb.Actor_Status
		noFreeWorkers bool
		wantResumes   int
	}{
		{"running actor is already reachable", []ateapipb.Actor_Status{ateapipb.Actor_STATUS_RUNNING}, false, 0},
		{"already suspended at entry", []ateapipb.Actor_Status{ateapipb.Actor_STATUS_SUSPENDED}, false, 1},
		{"suspending at entry", []ateapipb.Actor_Status{ateapipb.Actor_STATUS_SUSPENDING, ateapipb.Actor_STATUS_SUSPENDED}, false, 1},
		{"suspension starts after entry read", []ateapipb.Actor_Status{ateapipb.Actor_STATUS_RUNNING, ateapipb.Actor_STATUS_SUSPENDING, ateapipb.Actor_STATUS_SUSPENDED}, false, 1},
		{"pool exhaustion after concurrent suspend", []ateapipb.Actor_Status{ateapipb.Actor_STATUS_RUNNING, ateapipb.Actor_STATUS_SUSPENDING, ateapipb.Actor_STATUS_SUSPENDED}, true, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			agent := &v1alpha2.SandboxAgent{ObjectMeta: metav1.ObjectMeta{Namespace: "kagent", Name: "demo"}}
			tmpl := &atev1alpha1.ActorTemplate{
				ObjectMeta: metav1.ObjectMeta{Namespace: agent.Namespace, Name: "demo-template", Labels: map[string]string{SandboxAgentLabelKey: agent.Name}},
			}
			tmpl.Status.Phase = atev1alpha1.PhaseReady
			scheme := runtime.NewScheme()
			require.NoError(t, atev1alpha1.AddToScheme(scheme))
			kube := fake.NewClientBuilder().WithScheme(scheme).WithObjects(tmpl).Build()

			// Change the observed state at the RPC boundary, without timing a checkpoint.
			const sessionID = "same-session"
			id := SandboxAgentSessionActorID(agent, sessionID)
			ate := &suspendRaceControl{t: t, actorID: id, states: tc.states, noFreeWorkers: tc.noFreeWorkers}
			var healthyProbes atomic.Int32
			router := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodGet || r.URL.Path != "/health" || r.Host != ActorHost(agent.Namespace, id, "") {
					w.WriteHeader(http.StatusBadGateway)
					return
				}
				if ateapipb.Actor_Status(ate.observed.Load()) != ateapipb.Actor_STATUS_RUNNING {
					w.WriteHeader(http.StatusServiceUnavailable)
					return
				}
				healthyProbes.Add(1)
				w.WriteHeader(http.StatusOK)
			}))
			t.Cleanup(router.Close)
			backend := NewSandboxAgentActorBackend(&Client{ControlClient: ate}, kube, router.URL)

			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			got, err := backend.EnsureSessionActor(ctx, agent, sessionID)

			t.Logf("GetActor states=%v, ResumeActor calls=%d, successful GET /health=%d, error=%v",
				ate.reads, ate.resumes, healthyProbes.Load(), err)
			if tc.noFreeWorkers {
				require.ErrorIs(t, err, ErrNoFreeWorkers)
				require.Zero(t, healthyProbes.Load())
			} else {
				require.NoError(t, err)
				require.Equal(t, id, got.Handle.ID)
				require.Equal(t, agent.Namespace, got.Handle.Atespace)
				require.EqualValues(t, 1, healthyProbes.Load())
			}
			require.Equal(t, tc.wantResumes, ate.resumes)
		})
	}
}

type suspendRaceControl struct {
	ateapipb.ControlClient
	t             *testing.T
	actorID       string
	states        []ateapipb.Actor_Status
	reads         []ateapipb.Actor_Status
	observed      atomic.Int32
	resumes       int
	noFreeWorkers bool
}

func (c *suspendRaceControl) GetActor(_ context.Context, req *ateapipb.GetActorRequest, _ ...grpc.CallOption) (*ateapipb.Actor, error) {
	c.checkRef(req.GetActor())
	state := ateapipb.Actor_STATUS_RUNNING
	if c.resumes == 0 || c.noFreeWorkers {
		state = c.states[min(len(c.reads), len(c.states)-1)]
	}
	c.reads = append(c.reads, state)
	c.observed.Store(int32(state))
	return &ateapipb.Actor{Status: state}, nil
}

func (c *suspendRaceControl) ResumeActor(_ context.Context, req *ateapipb.ResumeActorRequest, _ ...grpc.CallOption) (*ateapipb.ResumeActorResponse, error) {
	c.checkRef(req.GetActor())
	require.Equal(c.t, ateapipb.Actor_STATUS_SUSPENDED, ateapipb.Actor_Status(c.observed.Load()))
	c.resumes++
	if c.noFreeWorkers {
		return nil, status.Error(codes.FailedPrecondition, "no free workers available")
	}
	c.observed.Store(int32(ateapipb.Actor_STATUS_RUNNING))
	return &ateapipb.ResumeActorResponse{Actor: &ateapipb.Actor{Status: ateapipb.Actor_STATUS_RUNNING}}, nil
}

func (c *suspendRaceControl) checkRef(ref *ateapipb.ObjectRef) {
	c.t.Helper()
	require.Equal(c.t, "kagent", ref.GetAtespace())
	require.Equal(c.t, c.actorID, ref.GetName())
}
