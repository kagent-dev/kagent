package e2e_test

import (
	"context"
	"strings"
	"testing"
	"time"

	a2atype "github.com/a2aproject/a2a-go/v2/a2a"
	a2apb "github.com/a2aproject/a2a-go/v2/a2apb/v1"
	"github.com/google/uuid"
	apiv1alpha1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1alpha1"
	"github.com/kagent-dev/kagent/go/api/v1alpha3"
	"github.com/kagent-dev/kagent/go/core/internal/substrate"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"
)

// TestRuntimeRevisionLifecycle exercises actual Substrate runtimes through
// invalid edits, template retirement, checkpoint retention, and later preparation.
func TestRuntimeRevisionLifecycle(t *testing.T) {
	testRuntimeRevisionLifecycle(t, nil)
}

func testRuntimeRevisionLifecycle(t *testing.T, afterCleanup func(context.Context, time.Time)) {
	t.Helper()
	target := interactionTarget(t)
	modelURL := startInteractionMock(t)
	templateName := createInteractionTemplate(t, modelURL)
	kube := interactionKubeClient(t)
	conn, err := grpc.NewClient(target, grpc.WithTransportCredentials(insecure.NewCredentials()))
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.Close() })
	ctx, cancel := context.WithTimeout(metadata.AppendToOutgoingContext(t.Context(), "x-user-id", "e2e"), 6*time.Minute)
	t.Cleanup(cancel)
	instances := apiv1alpha1.NewAgentInstanceServiceClient(conn)
	checkpoints := apiv1alpha1.NewCheckpointServiceClient(conn)
	system := apiv1alpha1.NewSystemServiceClient(conn)
	request := func(name string) *apiv1alpha1.CreateAgentInstanceRequest {
		return &apiv1alpha1.CreateAgentInstanceRequest{
			AgentTemplate: &apiv1alpha1.ResourceReference{Namespace: "kagent", Name: name},
			Harness:       &apiv1alpha1.ResourceReference{Namespace: "kagent", Name: "kagent"}, RequestId: uuid.NewString(),
		}
	}
	deleteInstance := func(id string) {
		t.Helper()
		cleanupCtx, cleanupCancel := context.WithTimeout(metadata.AppendToOutgoingContext(context.Background(), "x-user-id", "e2e"), time.Minute)
		defer cleanupCancel()
		_, err := instances.DeleteAgentInstance(cleanupCtx, &apiv1alpha1.DeleteAgentInstanceRequest{AgentInstanceId: id})
		if status.Code(err) != codes.NotFound {
			require.NoError(t, err)
		}
	}
	send := func(id string) {
		t.Helper()
		fixture := &interactionFixture{
			ctx: metadata.AppendToOutgoingContext(ctx, "x-kagent-agent-instance-id", id), client: a2apb.NewA2AServiceClient(conn),
		}
		_, _, task := fixture.send(t, "What is 2+2?")
		require.Equal(t, a2atype.TaskStateCompleted, task.Status.State)
		require.True(t, strings.Contains(taskText(task), "The answer is 4."))
	}
	// No FailedPrecondition retry: Ready must already be backed by a persisted
	// successful revision when the first create request arrives.
	created, err := instances.CreateAgentInstance(ctx, request(templateName))
	require.NoError(t, err)
	source := created.GetAgentInstance()
	t.Cleanup(func() { deleteInstance(source.GetId()) })
	send(source.GetId())

	// Observe the actual runtime through the public status API before deleting
	// references, so an empty response cannot falsely prove cleanup later.
	backend, err := system.GetSubstrateStatus(ctx, &apiv1alpha1.GetSubstrateStatusRequest{Namespace: "kagent"})
	require.NoError(t, err)
	require.Empty(t, backend.GetAteApiError())
	var runtimeName, runtimeNamespace, goldenActorID string
	for _, actor := range backend.GetActors() {
		if actor.GetActorId() == substrate.ActorName(source.GetId()) {
			runtimeName, runtimeNamespace = actor.GetActorTemplateName(), actor.GetActorTemplateNamespace()
		}
	}
	require.NotEmpty(t, runtimeName)
	for _, actorTemplate := range backend.GetActorTemplates() {
		if actorTemplate.GetNamespace() == runtimeNamespace && actorTemplate.GetName() == runtimeName {
			goldenActorID = actorTemplate.GetGoldenActorId()
		}
	}
	require.NotEmpty(t, goldenActorID)

	template := &v1alpha3.AgentTemplate{}
	require.NoError(t, kube.Get(ctx, ctrlclient.ObjectKey{Namespace: "kagent", Name: templateName}, template))
	originalModelName := template.Spec.ModelConfig.Name
	template.Spec.ModelConfig.Name = "missing-" + uuid.NewString()
	require.NoError(t, kube.Update(ctx, template))
	require.NoError(t, wait.PollUntilContextTimeout(ctx, time.Second, time.Minute, true, func(ctx context.Context) (bool, error) {
		current := &v1alpha3.AgentTemplate{}
		if err := kube.Get(ctx, ctrlclient.ObjectKeyFromObject(template), current); err != nil {
			return false, err
		}
		for _, harness := range current.Status.Harnesses {
			if harness.Harness != "kagent" {
				continue
			}
			for _, condition := range harness.Conditions {
				if condition.Type == v1alpha3.AgentTemplateConditionReady && condition.ObservedGeneration == template.Generation {
					return condition.Status == metav1.ConditionFalse, nil
				}
			}
		}
		return false, nil
	}))
	fallback, err := instances.CreateAgentInstance(ctx, request(templateName))
	require.NoError(t, err, "an invalid edit must preserve the current UID's last-good runtime")
	t.Cleanup(func() { deleteInstance(fallback.GetAgentInstance().GetId()) })
	require.Equal(t, source.GetPreparedRevision(), fallback.GetAgentInstance().GetPreparedRevision())
	send(fallback.GetAgentInstance().GetId())
	deleteInstance(fallback.GetAgentInstance().GetId())

	checkpointResponse, err := checkpoints.CreateCheckpoint(ctx, &apiv1alpha1.CreateCheckpointRequest{AgentInstanceId: source.GetId(), RequestId: uuid.NewString()})
	require.NoError(t, err)
	checkpointID := checkpointResponse.GetCheckpoint().GetId()
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(metadata.AppendToOutgoingContext(context.Background(), "x-user-id", "e2e"), time.Minute)
		defer cleanupCancel()
		_, err := checkpoints.DeleteCheckpoint(cleanupCtx, &apiv1alpha1.DeleteCheckpointRequest{CheckpointId: checkpointID})
		if status.Code(err) != codes.NotFound {
			require.NoError(t, err)
		}
	})
	deleteInstance(source.GetId())
	require.NoError(t, kube.Delete(ctx, template))
	// Wait for the controller to retire the pair, using the same public create
	// path as a client. Dispose of any instance created before retirement wins.
	require.NoError(t, wait.PollUntilContextTimeout(ctx, time.Second, time.Minute, true, func(ctx context.Context) (bool, error) {
		response, err := instances.CreateAgentInstance(ctx, request(templateName))
		if status.Code(err) == codes.FailedPrecondition {
			return true, nil
		}
		if err != nil {
			return false, err
		}
		deleteInstance(response.GetAgentInstance().GetId())
		return false, nil
	}))
	forked, err := checkpoints.ForkAgentInstance(ctx, &apiv1alpha1.ForkAgentInstanceRequest{CheckpointId: checkpointID, RequestId: uuid.NewString()})
	require.NoError(t, err, "a checkpoint must retain runnable inputs after its source and template are deleted")
	forkID := forked.GetAgentInstance().GetId()
	t.Cleanup(func() { deleteInstance(forkID) })
	send(forkID)
	deleteInstance(forkID)
	releasedAt := time.Now()
	_, err = checkpoints.DeleteCheckpoint(ctx, &apiv1alpha1.DeleteCheckpointRequest{CheckpointId: checkpointID})
	require.NoError(t, err)

	// GC must remove both the template and golden actor after the final
	// checkpoint disappears, without another template event to drive cleanup.
	require.NoError(t, wait.PollUntilContextTimeout(ctx, time.Second, 3*time.Minute, true, func(ctx context.Context) (bool, error) {
		backend, err := system.GetSubstrateStatus(ctx, &apiv1alpha1.GetSubstrateStatusRequest{Namespace: "kagent"})
		if err != nil {
			return false, err
		}
		require.Empty(t, backend.GetAteApiError())
		for _, actorTemplate := range backend.GetActorTemplates() {
			if actorTemplate.GetNamespace() == runtimeNamespace && actorTemplate.GetName() == runtimeName {
				return false, nil
			}
		}
		for _, actor := range backend.GetActors() {
			if actor.GetAtespace() == "ate-golden" && actor.GetActorId() == goldenActorID {
				return false, nil
			}
		}
		return true, nil
	}), "final checkpoint deletion must eventually collect its runtime without template changes")
	if afterCleanup != nil {
		afterCleanup(ctx, releasedAt)
	}

	// Recreating the name must prepare a new identity after collection.
	replacement := template.DeepCopy()
	replacement.ObjectMeta = metav1.ObjectMeta{Namespace: template.Namespace, Name: template.Name, Labels: template.Labels}
	replacement.Spec.ModelConfig.Name = originalModelName
	createAndWaitInteractionTemplate(t, kube, replacement)
	require.NotEqual(t, template.UID, replacement.UID)
	next := replacement.Name
	nextInstance, err := instances.CreateAgentInstance(ctx, request(next))
	require.NoError(t, err)
	t.Cleanup(func() { deleteInstance(nextInstance.GetAgentInstance().GetId()) })
	send(nextInstance.GetAgentInstance().GetId())
}
