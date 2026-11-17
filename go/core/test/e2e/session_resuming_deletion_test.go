package e2e_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	a2atype "github.com/a2aproject/a2a-go/v2/a2a"
	a2apb "github.com/a2aproject/a2a-go/v2/a2apb/v1"
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
			ModelConfig: &v1alpha3.ModelConfigReference{Name: model.Name}, SystemPrompt: "Reply briefly.",
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
	require.Contains(t, []ateapipb.ActorState{
		ateapipb.ActorState_ACTOR_STATE_RESUMING, ateapipb.ActorState_ACTOR_STATE_RUNNING,
		ateapipb.ActorState_ACTOR_STATE_SUSPENDING, ateapipb.ActorState_ACTOR_STATE_SUSPENDED,
	}, stuck.GetStatus().GetState(), "local cancellation cleanup may already be recovering the actor")
	current, err := fixture.sessions.GetSession(fixture.ctx, &apiv1.GetSessionRequest{SessionId: fixture.sessionID})
	require.NoError(t, err)
	require.Equal(t, apiv1.RuntimeState_RUNTIME_STATE_READY, current.GetSession().GetState())
	t.Logf("actor kagent/%s is %s after message timeout; deleting Session %s", actorName, stuck.GetStatus().GetState(), fixture.sessionID)

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

func TestCanceledDispatchSuspendsActorWithoutTurn(t *testing.T) {
	t.Parallel()
	const blockedMessageID = "cancel-before-task-admission"
	for _, scenario := range []struct {
		name             string
		state            ateapipb.ActorState
		streaming        bool
		retryDuringGrace bool
	}{
		{name: "Given a running actor, when delivery is canceled, assert that cleanup suspends it", state: ateapipb.ActorState_ACTOR_STATE_RUNNING},
		{name: "Given a resuming actor, when delivery is canceled, assert that cleanup recovers and suspends it", state: ateapipb.ActorState_ACTOR_STATE_RESUMING, streaming: true},
		{name: "Given canceled delivery, when the client retries during grace, assert that the new message succeeds", state: ateapipb.ActorState_ACTOR_STATE_RUNNING, retryDuringGrace: true},
	} {
		state := scenario.state
		t.Run(scenario.name, func(t *testing.T) {
			t.Parallel()
			kube := interactionKubeClient(t)
			base := &v1alpha3.Harness{}
			require.NoError(t, kube.Get(t.Context(), ctrlclient.ObjectKey{Namespace: "kagent", Name: "byo-e2e"}, base))
			harness := &v1alpha3.Harness{ObjectMeta: metav1.ObjectMeta{GenerateName: "cancel-dispatch-", Namespace: "kagent"}, Spec: *base.Spec.DeepCopy()}
			harness.Spec.Workload.Args = []string{"--block-message-id=" + blockedMessageID}
			if state == ateapipb.ActorState_ACTOR_STATE_RESUMING {
				// Only restored startup is delayed. Cancel as soon as RESUMING is
				// visible, without waiting for the router's timeout budget.
				harness.Spec.Workload.Command = []string{"/bin/sh", "-c"}
				harness.Spec.Workload.Args = []string{`if [ -f /data/cancel-resume ]; then sleep 2; fi; touch /data/cancel-resume; exec /app --block-message-id=` + blockedMessageID}
			}
			require.NoError(t, kube.Create(t.Context(), harness))
			t.Cleanup(func() {
				if err := kube.Delete(context.Background(), harness); err != nil && !apierrors.IsNotFound(err) {
					t.Errorf("delete cancellation Harness: %v", err)
				}
			})
			template := &v1alpha3.AgentTemplate{
				ObjectMeta: metav1.ObjectMeta{GenerateName: "cancel-dispatch-", Namespace: "kagent"},
				Spec:       v1alpha3.AgentTemplateSpec{Description: "Canceled delivery without task admission"},
			}
			createAndWaitInteractionTemplateForHarness(t, kube, template, harness.Name)
			fixture := newInteractionFixtureForHarnessTemplate(t, interactionTarget(t), harness.Name, template.Name)
			actorName := substrate.ActorName(fixture.sessionID)
			initial, err := findSubstrateActor(fixture.ctx, fixture.system, "kagent", actorName)
			require.NoError(t, err)
			require.NotNil(t, initial)
			_, request := newMessageRequest(t, "This message must never start a turn")
			request.Tenant, request.Message.ContextId, request.Message.MessageId = fixture.tenant, fixture.sessionID, blockedMessageID
			ctx, cancel := context.WithCancel(fixture.ctx)
			defer cancel()
			sendDone := make(chan error, 1)
			go func() {
				if scenario.streaming {
					stream, err := fixture.client.SendStreamingMessage(ctx, request)
					for err == nil {
						_, err = stream.Recv()
					}
					sendDone <- err
					return
				}
				_, err := fixture.client.SendMessage(ctx, request)
				sendDone <- err
			}()
			require.NoError(t, wait.PollUntilContextTimeout(fixture.ctx, 50*time.Millisecond, 30*time.Second, true, func(ctx context.Context) (bool, error) {
				actor, err := findSubstrateActor(ctx, fixture.system, "kagent", actorName)
				return actor != nil && actor.GetStatus().GetState() == state, err
			}))
			cancel()
			select {
			case err := <-sendDone:
				require.Error(t, err)
			case <-time.After(5 * time.Second):
				t.Fatal("send did not return after cancellation")
			}
			if scenario.retryDuringGrace {
				_, _, task := fixture.send(t, "Retry before cleanup")
				require.Equal(t, a2atype.TaskStateCompleted, task.Status.State)
				return
			}
			require.NoError(t, wait.PollUntilContextTimeout(fixture.ctx, 100*time.Millisecond, time.Minute, true, func(ctx context.Context) (bool, error) {
				actor, err := findSubstrateActor(ctx, fixture.system, "kagent", actorName)
				return actor != nil && actor.GetMetadata().GetUid() == initial.GetMetadata().GetUid() && actor.GetStatus().GetState() == ateapipb.ActorState_ACTOR_STATE_SUSPENDED, err
			}), "canceled delivery must suspend the same actor without an accepted turn; install with sessions.dispatchCleanupDelay: 1s")
			listed, err := fixture.client.ListTasks(fixture.ctx, &a2apb.ListTasksRequest{Tenant: fixture.tenant, ContextId: fixture.contextID})
			require.NoError(t, err)
			require.Empty(t, listed.GetTasks(), "the canceled request must not have admitted a task")
			_, _, task := fixture.send(t, "A subsequent message must resume normally")
			require.Equal(t, a2atype.TaskStateCompleted, task.Status.State)
		})
	}
}
