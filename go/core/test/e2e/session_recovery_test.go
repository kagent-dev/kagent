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

	a2atype "github.com/a2aproject/a2a-go/v2/a2a"
	a2apb "github.com/a2aproject/a2a-go/v2/a2apb/v1"
	"github.com/agent-substrate/substrate/pkg/proto/ateapipb"
	"github.com/kagent-dev/mockllm"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"

	apiv1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1"
	"github.com/kagent-dev/kagent/go/core/internal/substrate"
)

// This test deletes a worker, so run it sequentially and check that no other
// Actor currently shares that worker before deleting the exact Pod UID.
func TestSessionWorkerLossRecovery(t *testing.T) {
	target := interactionTarget(t)
	forEachHarness(t, func(t *testing.T, harness testHarness) {
		cfg, err := mockllm.LoadConfigFromFile("mocks/invoke_agent.json", interactionMocks)
		require.NoError(t, err)
		upstream, err := url.Parse(startMockLLMConfig(t, cfg))
		require.NoError(t, err)
		proxy := httputil.NewSingleHostReverseProxy(upstream)
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
		_, _, first := fixture.send(t, "What is 2+2?")
		require.Equal(t, a2atype.TaskStateCompleted, first.Status.State)
		assertActorSuspended(t, fixture)
		firstRequest := &a2apb.GetTaskRequest{Tenant: fixture.tenant, Id: string(first.ID)}
		storedFirst, err := fixture.client.GetTask(fixture.ctx, firstRequest)
		require.NoError(t, err)
		require.NotEmpty(t, storedFirst.GetHistory())
		instance, err := fixture.sessions.GetSession(fixture.ctx, &apiv1.GetSessionRequest{SessionId: fixture.sessionID})
		require.NoError(t, err)
		namespace := instance.GetSession().GetAgent().GetNamespace()
		actorName := substrate.ActorName(fixture.sessionID)
		checkpoint, err := findSubstrateActor(fixture.ctx, fixture.system, namespace, actorName)
		require.NoError(t, err)
		require.NotNil(t, checkpoint)
		require.NotEmpty(t, checkpoint.GetStatus().GetExternalSnapshot().GetSnapshotUri())

		blockNext.Store(true)
		_, request := newMessageRequest(t, "What is 2+2?")
		request.Tenant, request.Message.ContextId = fixture.tenant, fixture.sessionID
		turnCtx, stopTurn := context.WithCancel(fixture.ctx)
		defer stopTurn()
		sendDone := make(chan error, 1)
		go func() { _, err := sendMessageWithRetry(turnCtx, fixture.client, request); sendDone <- err }()
		select {
		case <-blocked:
		case <-time.After(time.Minute):
			t.Fatal("second turn never reached the blocking model")
		}
		page, err := fixture.client.ListTasks(fixture.ctx, &a2apb.ListTasksRequest{
			Tenant: fixture.tenant, ContextId: fixture.contextID, PageSize: new(int32(100)),
		})
		require.NoError(t, err)
		var interrupted *a2apb.Task
		for _, task := range page.GetTasks() {
			if task.GetStatus().GetState() == a2apb.TaskState_TASK_STATE_WORKING {
				require.Nil(t, interrupted)
				interrupted = task
			}
		}
		require.NotNil(t, interrupted, "the blocked turn must be durably accepted")
		running, err := findSubstrateActor(fixture.ctx, fixture.system, namespace, actorName)
		require.NoError(t, err)
		require.NotNil(t, running)
		require.Equal(t, checkpoint.GetMetadata().GetUid(), running.GetMetadata().GetUid())
		require.Equal(t, ateapipb.ActorState_ACTOR_STATE_RUNNING, running.GetStatus().GetState())
		assignment := running.GetStatus().GetWorkerAssignment()
		require.Equal(t, namespace, assignment.GetWorkerNamespace())
		require.NotEmpty(t, assignment.GetWorkerPod())
		for token := ""; ; {
			actors, err := fixture.system.ListSubstrateActors(fixture.ctx, &apiv1.ListSubstrateActorsRequest{
				Page: &apiv1.PageRequest{Limit: 100, PageToken: token},
			})
			require.NoError(t, err)
			require.Empty(t, actors.GetAteApiError())
			for _, actor := range actors.GetActors() {
				other := actor.GetStatus().GetWorkerAssignment()
				if actor.GetMetadata().GetUid() != running.GetMetadata().GetUid() {
					require.False(t, other.GetWorkerNamespace() == assignment.GetWorkerNamespace() && other.GetWorkerPod() == assignment.GetWorkerPod(),
						"refuse to delete a worker shared with another Actor")
				}
			}
			token = actors.GetPage().GetNextPageToken()
			if token == "" {
				break
			}
		}
		kube := interactionKubeClient(t)
		pod := &corev1.Pod{}
		podKey := ctrlclient.ObjectKey{Namespace: assignment.GetWorkerNamespace(), Name: assignment.GetWorkerPod()}
		require.NoError(t, kube.Get(fixture.ctx, podKey, pod))
		require.NotEmpty(t, pod.UID)
		t.Logf("deleting worker %s/%s uid=%s for accepted task %s", pod.Namespace, pod.Name, pod.UID, interrupted.GetId())
		require.NoError(t, kube.Delete(fixture.ctx, pod, &ctrlclient.DeleteOptions{
			GracePeriodSeconds: new(int64(0)), Preconditions: &metav1.Preconditions{UID: &pod.UID},
		}))
		err = wait.PollUntilContextTimeout(fixture.ctx, 100*time.Millisecond, time.Minute, true, func(ctx context.Context) (bool, error) {
			err := kube.Get(ctx, podKey, &corev1.Pod{})
			if apierrors.IsNotFound(err) {
				return true, nil
			}
			return false, err
		})
		require.NoError(t, err, "the assigned worker must actually be deleted")

		// Recovery may pass through CRASHED between observations. The durable
		// task failure and restored Actor prove recovery without slowing it down.
		err = wait.PollUntilContextTimeout(fixture.ctx, 100*time.Millisecond, 3*time.Minute, true, func(ctx context.Context) (bool, error) {
			stored, err := fixture.client.GetTask(ctx, &a2apb.GetTaskRequest{Tenant: fixture.tenant, Id: interrupted.GetId()})
			if err != nil {
				return false, err
			}
			current, err := fixture.sessions.GetSession(ctx, &apiv1.GetSessionRequest{SessionId: fixture.sessionID})
			if err != nil {
				return false, err
			}
			return stored.GetStatus().GetState() == a2apb.TaskState_TASK_STATE_FAILED && current.GetSession().GetState() == apiv1.RuntimeState_RUNTIME_STATE_READY, nil
		})
		require.NoError(t, err, "the interrupted task must fail and the Session must recover")
		stopTurn()
		select {
		case err := <-sendDone:
			t.Logf("interrupted send returned grpc status %s", status.Code(err))
		case <-time.After(10 * time.Second):
			t.Fatal("interrupted caller did not stop")
		}
		recovered, err := findSubstrateActor(fixture.ctx, fixture.system, namespace, actorName)
		require.NoError(t, err)
		require.NotNil(t, recovered)
		require.Equal(t, checkpoint.GetMetadata().GetUid(), recovered.GetMetadata().GetUid())
		require.Equal(t, ateapipb.ActorState_ACTOR_STATE_SUSPENDED, recovered.GetStatus().GetState())
		require.True(t, proto.Equal(checkpoint.GetStatus().GetExternalSnapshot(), recovered.GetStatus().GetExternalSnapshot()))
		_, _, next := fixture.send(t, "What is 2+2?")
		require.Equal(t, a2atype.TaskStateCompleted, next.Status.State)
		require.Contains(t, taskText(next), "4")
		retained, err := fixture.client.GetTask(fixture.ctx, firstRequest)
		require.NoError(t, err)
		require.True(t, proto.Equal(storedFirst, retained), "the completed task and its history must survive worker loss")
	})
}
