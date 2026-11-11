package e2e_test

import (
	"bytes"
	"context"
	"fmt"
	"net"
	"net/url"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	a2atype "github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2apb/v1/pbconv"
	"github.com/stretchr/testify/require"
	ateapipb "go.ate.dev/substrate/api/ateapi/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
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
	require.Equal(t, initial.GetMetadata().GetUid(), suspended.GetMetadata().GetUid(), "recovery must preserve the actor")
	require.Nil(t, suspended.GetStatus().GetWorkerAssignment())
	require.Greater(t, substrate.LatestActorSnapshot(suspended).GetGeneration(), substrate.LatestActorSnapshot(initial).GetGeneration(),
		"recovery must persist a new suspension snapshot")
	listRequest, err := pbconv.ToProtoListTasksRequest(&a2atype.ListTasksRequest{Tenant: fixture.tenant, ContextID: fixture.contextID})
	require.NoError(t, err)
	listed, err := fixture.client.ListTasks(fixture.ctx, listRequest)
	require.NoError(t, err)
	require.Empty(t, listed.GetTasks())
	current, err := fixture.sessions.GetSession(fixture.ctx, &apiv1.GetSessionRequest{SessionId: fixture.sessionID})
	require.NoError(t, err)
	require.Equal(t, apiv1.RuntimeState_RUNTIME_STATE_READY, current.GetSession().GetState())
	require.Equal(t, apiv1.RuntimeOperation_RUNTIME_OPERATION_NONE, current.GetSession().GetOperation())

	_, _, task := fixture.send(t, "hello")
	require.Equal(t, a2atype.TaskStateCompleted, task.Status.State)
	require.Contains(t, taskText(task), "BYO agent response")
	assertActorSuspended(t, fixture)
}

// canceledResumeStartup blocks readiness at a test-controlled HTTP gate only on
// the first restore. The callback proves guest startup reached the container;
// RESUMING plus a worker assignment alone can precede microVM initialization.
const canceledResumeStartup = `set -eu
resume_marker="$1"
startup_gate="$2"
shift 2
if [ -f "$resume_marker" ] && [ ! -f "$resume_marker.released" ]; then
  wget -q -T 30 -O /dev/null "$startup_gate"
  touch "$resume_marker.released"
fi
touch "$resume_marker"
exec "$@"`

func TestCanceledResumeStartupGate(t *testing.T) {
	// Given the same startup wrapper used inside the E2E guest, golden startup
	// must work without the gate and first restore must block until released.
	gateURL, started, release := startBlockingMockServer(t, nil)
	defer release()
	parsed, err := url.Parse(gateURL)
	require.NoError(t, err)
	parsed.Host = net.JoinHostPort("127.0.0.1", parsed.Port())
	marker := filepath.Join(t.TempDir(), "resume")
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	startup := func(gate string) *exec.Cmd {
		return exec.CommandContext(ctx, "/bin/sh", "-c", canceledResumeStartup, "canceled-resume", marker, gate, "/bin/true")
	}
	const unavailableGate = "http://127.0.0.1:1"
	output, err := startup(unavailableGate).CombinedOutput()
	require.NoError(t, err, "%s", output)

	// When the first restore reaches the gate, assert that readiness stays
	// blocked until release and later restores no longer need the gate.
	command := startup(parsed.String())
	var restoredOutput bytes.Buffer
	command.Stdout, command.Stderr = &restoredOutput, &restoredOutput
	finished := make(chan error, 1)
	go func() {
		defer close(finished)
		finished <- command.Run()
	}()
	t.Cleanup(func() {
		release()
		cancel()
		<-finished
	})
	select {
	case <-started:
	case err := <-finished:
		t.Fatalf("restore exited before reaching gate: %v: %s", err, restoredOutput.String())
	case <-ctx.Done():
		t.Fatal("restore did not reach gate")
	}
	select {
	case err := <-finished:
		t.Fatalf("restore exited before release: %v", err)
	default:
	}
	release()
	select {
	case err := <-finished:
		require.NoError(t, err, "%s", restoredOutput.String())
	case <-ctx.Done():
		t.Fatal("restore did not finish after release")
	}
	output, err = startup(unavailableGate).CombinedOutput()
	require.NoError(t, err, "%s", output)
}

func TestSessionSuspendsAfterCanceledResume(t *testing.T) {
	t.Parallel()
	// Given a golden runtime that starts normally, but holds readiness on its
	// first restore until the test releases it after canceling the message.
	target := interactionTarget(t)
	kube := interactionKubeClient(t)
	gateURL, guestStarted, releaseReadiness := startBlockingMockServer(t, nil)
	defer releaseReadiness()
	gateURL = reachableServerURL(t, gateURL, "")
	policy := &v1alpha3.EgressPolicy{
		ObjectMeta: metav1.ObjectMeta{GenerateName: "canceled-resume-", Namespace: "kagent"},
		Spec:       v1alpha3.EgressPolicySpec{Destinations: []v1alpha3.EgressDestination{{Origin: gateURL}}},
	}
	require.NoError(t, kube.Create(t.Context(), policy))
	t.Cleanup(func() {
		if err := kube.Delete(context.Background(), policy); err != nil && !apierrors.IsNotFound(err) {
			t.Errorf("delete canceled-resume EgressPolicy: %v", err)
		}
	})
	base := &v1alpha3.Harness{}
	require.NoError(t, kube.Get(t.Context(), ctrlclient.ObjectKey{Namespace: "kagent", Name: "byo-e2e"}, base))
	harness := &v1alpha3.Harness{
		ObjectMeta: metav1.ObjectMeta{GenerateName: "canceled-resume-", Namespace: "kagent"},
		Spec:       *base.Spec.DeepCopy(),
	}
	harness.Spec.Workload.Command = []string{"/bin/sh", "-c"}
	harness.Spec.Workload.Args = []string{canceledResumeStartup, "canceled-resume", "/data/e2e-canceled-resume", gateURL, "/app"}
	require.NoError(t, kube.Create(t.Context(), harness))
	t.Cleanup(func() {
		if err := kube.Delete(context.Background(), harness); err != nil && !apierrors.IsNotFound(err) {
			t.Errorf("delete canceled-resume Harness: %v", err)
		}
	})
	template := &v1alpha3.AgentTemplate{
		ObjectMeta: metav1.ObjectMeta{GenerateName: "canceled-resume-", Namespace: "kagent"},
		Spec:       v1alpha3.AgentTemplateSpec{Description: "Canceled resume lifecycle fixture"},
	}
	createAndWaitInteractionTemplateForHarness(t, kube, template, harness.Name)
	agent := &v1alpha3.Agent{}
	require.NoError(t, kube.Get(t.Context(), ctrlclient.ObjectKeyFromObject(template), agent))
	agent.Spec.EgressPolicyRefs = []corev1.LocalObjectReference{{Name: policy.Name}}
	require.NoError(t, kube.Update(t.Context(), agent))
	require.NoError(t, wait.PollUntilContextTimeout(t.Context(), 100*time.Millisecond, time.Minute, true, func(ctx context.Context) (bool, error) {
		if err := kube.Get(ctx, ctrlclient.ObjectKeyFromObject(agent), agent); err != nil {
			return false, err
		}
		condition := apimeta.FindStatusCondition(agent.Status.Conditions, v1alpha3.AgentConditionEgressPoliciesApplied)
		return condition != nil && condition.ObservedGeneration == agent.Generation && condition.Status == metav1.ConditionTrue, nil
	}))
	fixture := newInteractionFixtureForHarnessTemplate(t, target, harness.Name, template.Name)
	actorName := substrate.ActorName(fixture.sessionID)
	initial, err := findSubstrateActor(fixture.ctx, fixture.system, "kagent", actorName)
	require.NoError(t, err)
	require.NotNil(t, initial)
	require.Equal(t, ateapipb.ActorState_ACTOR_STATE_SUSPENDED, initial.GetStatus().GetState())

	// When the guest reaches the gate, cancel while readiness is still blocked.
	// Canceling at assignment can interrupt VM construction instead of readiness
	// and trigger a separate Substrate startup failure before the guest even runs.
	_, request := newMessageRequest(t, "hello")
	request.Tenant, request.Message.ContextId = fixture.tenant, fixture.sessionID
	sendCtx, cancel := context.WithCancel(fixture.ctx)
	defer cancel()
	sent := make(chan error, 1)
	go func() {
		_, err := fixture.client.SendMessage(sendCtx, request)
		sent <- err
	}()
	select {
	case <-guestStarted:
	case err := <-sent:
		t.Fatalf("send ended before guest startup: %v", err)
	case <-time.After(30 * time.Second):
		t.Fatal("restored guest did not reach the readiness gate")
	}
	resuming, err := findSubstrateActor(fixture.ctx, fixture.system, "kagent", actorName)
	require.NoError(t, err)
	require.Equal(t, ateapipb.ActorState_ACTOR_STATE_RESUMING, resuming.GetStatus().GetState())
	require.NotNil(t, resuming.GetStatus().GetWorkerAssignment())
	cancel()
	select {
	case err := <-sent:
		require.Equal(t, codes.Canceled, status.Code(err))
	case <-time.After(5 * time.Second):
		t.Fatal("send did not return after cancellation")
	}
	releaseReadiness()
	interrupted, err := findSubstrateActor(fixture.ctx, fixture.system, "kagent", actorName)
	require.NoError(t, err)
	require.NotNil(t, interrupted)
	listRequest, err := pbconv.ToProtoListTasksRequest(&a2atype.ListTasksRequest{Tenant: fixture.tenant, ContextID: fixture.contextID})
	require.NoError(t, err)
	listed, err := fixture.client.ListTasks(fixture.ctx, listRequest)
	require.NoError(t, err)
	t.Logf("canceled Session %s actor kagent/%s: state=%s worker_assigned=%t tasks=%d", fixture.sessionID, actorName,
		interrupted.GetStatus().GetState(), interrupted.GetStatus().GetWorkerAssignment() != nil, len(listed.GetTasks()))
	require.Empty(t, listed.GetTasks())

	// Assert that automatic cleanup finishes the interrupted restore and suspends
	// it without accepting input or changing the Session's logical availability.
	lastActor := interrupted
	err = wait.PollUntilContextTimeout(fixture.ctx, 100*time.Millisecond, 30*time.Second, true, func(ctx context.Context) (bool, error) {
		actor, err := findSubstrateActor(ctx, fixture.system, "kagent", actorName)
		if err != nil {
			return false, err
		}
		if actor == nil {
			return false, fmt.Errorf("actor disappeared during canceled-resume cleanup")
		}
		if actor.GetStatus().GetState() != lastActor.GetStatus().GetState() {
			t.Logf("Session %s actor kagent/%s: state=%s worker_assigned=%t", fixture.sessionID, actorName,
				actor.GetStatus().GetState(), actor.GetStatus().GetWorkerAssignment() != nil)
		}
		lastActor = actor
		if actor.GetStatus().GetState() == ateapipb.ActorState_ACTOR_STATE_CRASHED {
			return false, fmt.Errorf("actor crashed during canceled-resume cleanup: %s", actor.GetStatus().GetCrash().GetMessage())
		}
		snapshot := substrate.LatestActorSnapshot(actor)
		return actor.GetStatus().GetState() == ateapipb.ActorState_ACTOR_STATE_SUSPENDED && actor.GetStatus().GetWorkerAssignment() == nil &&
			substrate.SnapshotURI(snapshot) != "" && snapshot.GetFidelity() == ateapipb.SnapshotFidelity_SNAPSHOT_FIDELITY_VOLUMES, nil
	})
	t.Logf("cleanup result for Session %s actor kagent/%s: state=%s worker_assigned=%t error=%v", fixture.sessionID, actorName,
		lastActor.GetStatus().GetState(), lastActor.GetStatus().GetWorkerAssignment() != nil, err)
	listed, listErr := fixture.client.ListTasks(fixture.ctx, listRequest)
	require.NoError(t, listErr)
	t.Logf("Session %s tasks after canceled-resume cleanup: %d", fixture.sessionID, len(listed.GetTasks()))
	require.NoError(t, err, "canceled restore must automatically reach Suspended with a durable snapshot")
	require.Equal(t, initial.GetMetadata().GetUid(), lastActor.GetMetadata().GetUid(), "recovery must preserve the actor")
	require.Greater(t, substrate.LatestActorSnapshot(lastActor).GetGeneration(), substrate.LatestActorSnapshot(initial).GetGeneration(),
		"recovery must persist a new suspension snapshot")
	require.Empty(t, listed.GetTasks())
	current, err := fixture.sessions.GetSession(fixture.ctx, &apiv1.GetSessionRequest{SessionId: fixture.sessionID})
	require.NoError(t, err)
	require.Equal(t, apiv1.RuntimeState_RUNTIME_STATE_READY, current.GetSession().GetState())
	require.Equal(t, apiv1.RuntimeOperation_RUNTIME_OPERATION_NONE, current.GetSession().GetOperation())
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
	require.Equal(t, ateapipb.ActorState_ACTOR_STATE_RESUMING, stuck.GetStatus().GetState())
	current, err := fixture.sessions.GetSession(fixture.ctx, &apiv1.GetSessionRequest{SessionId: fixture.sessionID})
	require.NoError(t, err)
	require.Equal(t, apiv1.RuntimeState_RUNTIME_STATE_READY, current.GetSession().GetState())
	t.Logf("message timeout left actor kagent/%s in RESUMING; deleting Session %s", actorName, fixture.sessionID)

	var deleted *apiv1.DeleteSessionResponse
	require.NoError(t, wait.PollUntilContextTimeout(fixture.ctx, 100*time.Millisecond, 30*time.Second, true, func(ctx context.Context) (bool, error) {
		var err error
		deleted, err = fixture.sessions.DeleteSession(ctx, &apiv1.DeleteSessionRequest{SessionId: fixture.sessionID})
		// Dispatch cleanup must not delay deletion admission.
		if status.Code(err) != codes.Unavailable {
			return err == nil, err
		}
		// Deletion must already be durable, but an issued cleanup resume can
		// still hold Substrate's actor lease. Retry once that call yields it.
		pending, readErr := fixture.sessions.GetSession(ctx, &apiv1.GetSessionRequest{SessionId: fixture.sessionID})
		if readErr != nil {
			return false, readErr
		}
		if pending.GetSession().GetState() != apiv1.RuntimeState_RUNTIME_STATE_DELETING ||
			pending.GetSession().GetOperation() != apiv1.RuntimeOperation_RUNTIME_OPERATION_DELETE {
			return false, fmt.Errorf("deletion was not admitted: %w", err)
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
