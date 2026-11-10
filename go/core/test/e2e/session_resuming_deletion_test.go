package e2e_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	a2atype "github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2apb/v1/pbconv"
	"github.com/stretchr/testify/require"
	ateapipb "go.ate.dev/substrate/api/ateapi/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"

	apiv1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1"
	"github.com/kagent-dev/kagent/go/api/v1alpha3"
	"github.com/kagent-dev/kagent/go/core/internal/substrate"
)

func TestSessionSuspendsAfterCanceledDispatch(t *testing.T) {
	t.Parallel()
	// Given a runtime whose first restored activation blocks before accepting
	// input, while later restores use the ordinary executor and TaskStore.
	target := interactionTarget(t)
	kube := interactionKubeClient(t)
	base := &v1alpha3.Harness{}
	require.NoError(t, kube.Get(t.Context(), ctrlclient.ObjectKey{Namespace: "kagent", Name: "byo-e2e"}, base))
	harness := &v1alpha3.Harness{
		ObjectMeta: metav1.ObjectMeta{GenerateName: "canceled-dispatch-", Namespace: "kagent"},
		Spec:       *base.Spec.DeepCopy(),
	}
	harness.Spec.Workload.Args = append(harness.Spec.Workload.Args, "--block-first-restored-send")
	require.NoError(t, kube.Create(t.Context(), harness))
	t.Cleanup(func() {
		if err := kube.Delete(context.Background(), harness); err != nil && !apierrors.IsNotFound(err) {
			t.Errorf("delete canceled-dispatch Harness: %v", err)
		}
	})
	template := &v1alpha3.AgentTemplate{
		ObjectMeta: metav1.ObjectMeta{GenerateName: "canceled-dispatch-", Namespace: "kagent"},
		Spec:       v1alpha3.AgentTemplateSpec{Description: "Canceled dispatch lifecycle fixture"},
	}
	createAndWaitInteractionTemplateForHarness(t, kube, template, harness.Name)
	fixture := newInteractionFixtureForHarnessTemplate(t, target, harness.Name, template.Name)
	actorName := substrate.ActorName(fixture.sessionID)
	initial, err := findSubstrateActor(fixture.ctx, fixture.system, "kagent", actorName)
	require.NoError(t, err)
	require.NotNil(t, initial)
	require.Equal(t, ateapipb.ActorState_ACTOR_STATE_SUSPENDED, initial.GetStatus().GetState())

	// When the caller cancels after restore, no task has reached persistence.
	_, request := newMessageRequest(t, "hello")
	request.Tenant, request.Message.ContextId = fixture.tenant, fixture.sessionID
	sendCtx, cancel := context.WithCancel(fixture.ctx)
	defer cancel()
	sent := make(chan error, 1)
	go func() {
		_, err := fixture.client.SendMessage(sendCtx, request)
		sent <- err
	}()
	require.NoError(t, wait.PollUntilContextTimeout(fixture.ctx, 100*time.Millisecond, time.Minute, true, func(ctx context.Context) (bool, error) {
		select {
		case err := <-sent:
			return false, fmt.Errorf("send ended before cancellation: %v", err)
		default:
		}
		actor, err := findSubstrateActor(ctx, fixture.system, "kagent", actorName)
		if err != nil {
			return false, err
		}
		return actor != nil && actor.GetStatus().GetState() == ateapipb.ActorState_ACTOR_STATE_RUNNING, nil
	}))
	cancel()
	select {
	case err := <-sent:
		require.Equal(t, codes.Canceled, status.Code(err))
	case <-time.After(5 * time.Second):
		t.Fatal("send did not return after cancellation")
	}

	// Assert that turnless cleanup releases the allocation without inventing
	// task history, and preserves the snapshot needed for the next invocation.
	assertActorSuspended(t, fixture)
	suspended, err := findSubstrateActor(fixture.ctx, fixture.system, "kagent", actorName)
	require.NoError(t, err)
	require.NotNil(t, suspended)
	require.Nil(t, suspended.GetStatus().GetWorkerAssignment())
	listRequest, err := pbconv.ToProtoListTasksRequest(&a2atype.ListTasksRequest{Tenant: fixture.tenant, ContextID: fixture.contextID})
	require.NoError(t, err)
	listed, err := fixture.client.ListTasks(fixture.ctx, listRequest)
	require.NoError(t, err)
	require.Empty(t, listed.GetTasks())
	current, err := fixture.sessions.GetSession(fixture.ctx, &apiv1.GetSessionRequest{SessionId: fixture.sessionID})
	require.NoError(t, err)
	require.Equal(t, apiv1.RuntimeState_RUNTIME_STATE_READY, current.GetSession().GetState())

	_, _, task := fixture.send(t, "hello")
	require.Equal(t, a2atype.TaskStateCompleted, task.Status.State)
	require.Contains(t, taskText(task), "BYO agent response")
	assertActorSuspended(t, fixture)
}

// The Go ADK startup delay reproduces the router timeout independently of the
// model protocol. The installed Harness is left intact.
func TestSessionDeletionAfterInterruptedResume(t *testing.T) {
	t.Parallel()
	target := interactionTarget(t)
	kube := interactionKubeClient(t)
	base := &v1alpha3.Harness{}
	require.NoError(t, kube.Get(t.Context(), ctrlclient.ObjectKey{Namespace: "kagent", Name: "kagent"}, base))
	harness := &v1alpha3.Harness{
		ObjectMeta: metav1.ObjectMeta{GenerateName: "interrupted-resume-", Namespace: "kagent"},
		Spec:       *base.Spec.DeepCopy(),
	}
	// The golden actor boots normally and snapshots the marker. Restored startup
	// exceeds the E2E fixture's 5s parking budget and 10s backend timeout.
	harness.Spec.Workload.Command = []string{"/bin/sh", "-c"}
	harness.Spec.Workload.Args = []string{`if [ -f /data/interrupted-resume ]; then sleep 20; fi; touch /data/interrupted-resume; exec /app`}
	require.NoError(t, kube.Create(t.Context(), harness))
	t.Cleanup(func() {
		if err := kube.Delete(context.Background(), harness); err != nil && !apierrors.IsNotFound(err) {
			t.Errorf("delete interrupted-resume Harness: %v", err)
		}
	})
	model := createInteractionModel(t, kube, startInteractionMock(t), nil)
	template := &v1alpha3.AgentTemplate{
		ObjectMeta: metav1.ObjectMeta{GenerateName: "interrupted-resume-", Namespace: "kagent"},
		Spec: v1alpha3.AgentTemplateSpec{
			ModelConfig: &v1alpha3.ObjectReference{Name: model.Name}, SystemPrompt: "Reply briefly.",
		},
	}
	createAndWaitInteractionTemplateForHarness(t, kube, template, harness.Name)
	fixture := newInteractionFixtureForHarnessTemplate(t, target, harness.Name, template.Name)
	actorName := substrate.ActorName(fixture.sessionID)
	initial, err := findSubstrateActor(fixture.ctx, fixture.system, "kagent", actorName)
	require.NoError(t, err)
	require.NotNil(t, initial)
	require.Equal(t, ateapipb.ActorState_ACTOR_STATE_SUSPENDED, initial.GetStatus().GetState())

	_, request := newMessageRequest(t, "Reply OK")
	request.Tenant, request.Message.ContextId = fixture.tenant, fixture.sessionID
	// Let the router interrupt resume; the client's deadline must not cause it.
	sendCtx, cancel := context.WithTimeout(fixture.ctx, time.Minute)
	defer cancel()
	_, err = fixture.client.SendMessage(sendCtx, request)
	require.NoError(t, sendCtx.Err(), "router timeout must precede the client deadline")
	require.True(t, sendNotAccepted(err), "resume must fail before accepting the message: %v", err)
	stuck, err := findSubstrateActor(fixture.ctx, fixture.system, "kagent", actorName)
	require.NoError(t, err)
	require.NotNil(t, stuck)
	require.Equal(t, ateapipb.ActorState_ACTOR_STATE_RESUMING, stuck.GetStatus().GetState())
	current, err := fixture.sessions.GetSession(fixture.ctx, &apiv1.GetSessionRequest{SessionId: fixture.sessionID})
	require.NoError(t, err)
	require.Equal(t, apiv1.RuntimeState_RUNTIME_STATE_READY, current.GetSession().GetState())
	t.Logf("message timeout left actor kagent/%s in RESUMING; deleting Session %s", actorName, fixture.sessionID)

	var deleted *apiv1.DeleteSessionResponse
	require.NoError(t, wait.PollUntilContextTimeout(fixture.ctx, 100*time.Millisecond, 30*time.Second, true, func(ctx context.Context) (bool, error) {
		var err error
		deleted, err = fixture.sessions.DeleteSession(ctx, &apiv1.DeleteSessionRequest{SessionId: fixture.sessionID})
		if status.Code(err) != codes.Unavailable {
			return err == nil, err
		}
		// Retry backend cleanup only after deletion starts. The old suspension
		// prerequisite fails while the actor is still RESUMING.
		actor, readErr := findSubstrateActor(ctx, fixture.system, "kagent", actorName)
		if readErr != nil {
			return false, readErr
		}
		if actor != nil && actor.GetStatus().GetState() != ateapipb.ActorState_ACTOR_STATE_DELETING {
			return false, fmt.Errorf("deletion made no progress from %s: %w", actor.GetStatus().GetState(), err)
		}
		return false, nil
	}), "ordinary deletion must recover from an interrupted resume")
	require.Equal(t, apiv1.RuntimeState_RUNTIME_STATE_DELETED, deleted.GetSession().GetState())
	_, err = fixture.sessions.GetSession(fixture.ctx, &apiv1.GetSessionRequest{SessionId: fixture.sessionID})
	require.Equal(t, codes.NotFound, status.Code(err))
	actor, err := findSubstrateActor(fixture.ctx, fixture.system, "kagent", actorName)
	require.NoError(t, err)
	require.Nil(t, actor)
}
