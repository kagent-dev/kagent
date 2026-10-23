package e2e_test

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"sync/atomic"
	"testing"
	"time"

	"github.com/a2aproject/a2a-go/v2/a2a"
	a2apb "github.com/a2aproject/a2a-go/v2/a2apb/v1"
	"github.com/kagent-dev/mockllm"
	"github.com/stretchr/testify/require"
	ateapipb "go.ate.dev/substrate/api/ateapi/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"

	apiv1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1"
	configv1 "github.com/kagent-dev/kagent/go/api/gen/kagent/config/v1"
	"github.com/kagent-dev/kagent/go/core/internal/substrate"
)

func TestSessionRevertCrashedRuntime(t *testing.T) {
	target := interactionTarget(t)
	setSessionPolicy(t, target, func(cfg *configv1.SessionsConfig) { cfg.AutomaticRecovery = false })
	forEachHarness(t, func(t *testing.T, harness testHarness) {
		cfg, err := mockllm.LoadConfigFromFile("mocks/invoke_agent.json", interactionMocks)
		require.NoError(t, err)
		upstream, err := url.Parse(startMockLLMConfig(t, cfg))
		require.NoError(t, err)
		proxy := httputil.NewSingleHostReverseProxy(upstream)
		// Only an interaction runs a Session's Actor; quiescence suspends it once
		// the turn settles. The gate holds the second turn's model call open so
		// the Actor is still running when its worker is deleted.
		var blockNext atomic.Bool
		blocked := make(chan struct{}, 1)
		release := make(chan struct{})
		gate := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method == http.MethodPost && blockNext.CompareAndSwap(true, false) {
				blocked <- struct{}{}
				select {
				case <-r.Context().Done():
				case <-release:
				}
				return
			}
			proxy.ServeHTTP(w, r)
		}))
		require.NoError(t, gate.Listener.Close())
		gate.Listener, err = net.Listen("tcp", "0.0.0.0:0")
		require.NoError(t, err)
		gate.Start()
		t.Cleanup(gate.Close)
		fixture := newInteractionFixture(t, harness, target, reachableModelURL(t, gate.URL))
		defer func() {
			close(release)
			gate.CloseClientConnections()
		}()
		_, _, task := fixture.send(t, "What is 2+2?")
		require.Equal(t, a2a.TaskStateCompleted, task.Status.State)
		assertActorSuspended(t, fixture)
		getTask := &a2apb.GetTaskRequest{Tenant: fixture.tenant, Id: string(task.ID)}
		before, err := fixture.client.GetTask(fixture.ctx, getTask)
		require.NoError(t, err)
		session, err := fixture.sessions.GetSession(fixture.ctx, &apiv1.GetSessionRequest{SessionId: fixture.sessionID})
		require.NoError(t, err)
		namespace, name := session.Session.Agent.Namespace, substrate.ActorName(fixture.sessionID)
		blockNext.Store(true)
		_, turn := newMessageRequest(t, "What is 2+2?")
		turn.Tenant, turn.Message.ContextId = fixture.tenant, fixture.sessionID
		turnCtx, stopTurn := context.WithCancel(fixture.ctx)
		defer stopTurn()
		sendDone := make(chan error, 1)
		go func() { _, err := sendMessageWithRetry(turnCtx, fixture.client, turn); sendDone <- err }()
		select {
		case <-blocked:
		case <-time.After(time.Minute):
			t.Fatal("second turn never reached the blocking model")
		}
		running, err := findSubstrateActor(fixture.ctx, fixture.system, namespace, name)
		require.NoError(t, err)
		require.NotNil(t, running)
		require.Equal(t, ateapipb.ActorState_ACTOR_STATE_RUNNING, running.GetStatus().GetState())
		require.NotEmpty(t, substrate.SnapshotURI(substrate.LatestDurableActorSnapshot(running)))
		request := &apiv1.RevertSessionRequest{SessionId: fixture.sessionID}
		_, err = fixture.sessions.RevertSession(fixture.ctx, request)
		require.Equal(t, codes.FailedPrecondition, status.Code(err), "healthy runtimes cannot revert")
		assignment := running.GetStatus().GetWorkerAssignment()
		require.Equal(t, namespace, assignment.GetWorkerNamespace())
		require.NotEmpty(t, assignment.GetWorkerPod())
		for token := ""; ; {
			page, err := fixture.system.ListSubstrateActors(fixture.ctx, &apiv1.ListSubstrateActorsRequest{
				Page: &apiv1.PageRequest{Limit: 100, PageToken: token},
			})
			require.NoError(t, err)
			require.Empty(t, page.GetAteApiError())
			for _, actor := range page.GetActors() {
				other := actor.GetStatus().GetWorkerAssignment()
				if actor.GetMetadata().GetUid() != running.GetMetadata().GetUid() {
					require.False(t, other.GetWorkerNamespace() == assignment.GetWorkerNamespace() && other.GetWorkerPod() == assignment.GetWorkerPod(),
						"refuse to delete a worker shared with another Actor")
				}
			}
			token = page.GetPage().GetNextPageToken()
			if token == "" {
				break
			}
		}
		kube := interactionKubeClient(t)
		pod := &corev1.Pod{}
		require.NoError(t, kube.Get(fixture.ctx, ctrlclient.ObjectKey{
			Namespace: assignment.GetWorkerNamespace(), Name: assignment.GetWorkerPod(),
		}, pod))
		require.NotEmpty(t, pod.UID)
		require.NoError(t, kube.Delete(fixture.ctx, pod, &ctrlclient.DeleteOptions{
			GracePeriodSeconds: new(int64(0)), Preconditions: &metav1.Preconditions{UID: &pod.UID},
		}))
		err = wait.PollUntilContextTimeout(fixture.ctx, 100*time.Millisecond, time.Minute, true, func(ctx context.Context) (bool, error) {
			actor, err := findSubstrateActor(ctx, fixture.system, namespace, name)
			return actor.GetStatus().GetState() == ateapipb.ActorState_ACTOR_STATE_CRASHED, err
		})
		require.NoError(t, err)
		observed, err := fixture.sessions.GetSession(fixture.ctx, &apiv1.GetSessionRequest{SessionId: fixture.sessionID})
		require.NoError(t, err)
		require.True(t, observed.Session.GetRecovery().GetNeeded())
		require.False(t, observed.Session.GetRecovery().GetAutomatic())
		reverted, err := fixture.sessions.RevertSession(fixture.ctx, request)
		require.NoError(t, err)
		require.Equal(t, apiv1.RuntimeState_RUNTIME_STATE_READY, reverted.Session.State)
		recovered, err := findSubstrateActor(fixture.ctx, fixture.system, namespace, name)
		require.NoError(t, err)
		require.Equal(t, running.GetMetadata().GetUid(), recovered.GetMetadata().GetUid())
		require.Equal(t, ateapipb.ActorState_ACTOR_STATE_SUSPENDED, recovered.GetStatus().GetState())
		require.Equal(t, substrate.SnapshotURI(substrate.LatestDurableActorSnapshot(running)), substrate.SnapshotURI(substrate.LatestActorSnapshot(recovered)))
		retained, err := fixture.client.GetTask(fixture.ctx, getTask)
		require.NoError(t, err)
		require.True(t, proto.Equal(before, retained), "recovery preserves the completed task and its history")
		stopTurn()
		select {
		case <-sendDone:
		case <-time.After(10 * time.Second):
			t.Fatal("interrupted caller did not stop")
		}
		_, _, next := fixture.send(t, "What is 2+2?")
		require.Equal(t, a2a.TaskStateCompleted, next.Status.State)
		require.Contains(t, taskText(next), "4")
	})
}
