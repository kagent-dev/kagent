package e2e_test

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	a2atype "github.com/a2aproject/a2a-go/v2/a2a"
	a2apb "github.com/a2aproject/a2a-go/v2/a2apb/v1"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"go.ate.dev/substrate/pkg/proto/ateapipb"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"

	apiv1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1"
	"github.com/kagent-dev/kagent/go/api/v1alpha3"
	"github.com/kagent-dev/kagent/go/core/internal/substrate"
)

// TestRuntimeRevisionLifecycle exercises actual Substrate runtimes through
// invalid edits, template retirement, checkpoint retention, and later preparation.
func TestRuntimeRevisionLifecycle(t *testing.T) {
	t.Parallel()
	forEachHarness(t, func(t *testing.T, harness testHarness) {
		// Each harness owns its revisions; overlap their periodic GC waits.
		t.Parallel()
		target := interactionTarget(t)
		modelURL := startInteractionMock(t)
		templateName := createInteractionTemplate(t, harness, modelURL)
		kube := interactionKubeClient(t)
		conn := newControllerConn(t, target)
		ctx, cancel := context.WithTimeout(metadata.AppendToOutgoingContext(t.Context(), "x-user-id", "e2e"), 6*time.Minute)
		t.Cleanup(cancel)
		sessions := apiv1.NewSessionServiceClient(conn)
		checkpoints := apiv1.NewCheckpointServiceClient(conn)
		system := apiv1.NewSystemServiceClient(conn)
		request := func(name string) *apiv1.CreateSessionRequest {
			return &apiv1.CreateSessionRequest{
				Agent:     &apiv1.ResourceReference{Namespace: "kagent", Name: name},
				RequestId: uuid.NewString(),
			}
		}
		deleteSession := func(id string) {
			t.Helper()
			cleanupCtx, cleanupCancel := context.WithTimeout(metadata.AppendToOutgoingContext(context.Background(), "x-user-id", "e2e"), time.Minute)
			defer cleanupCancel()
			require.NoError(t, deleteIdleSession(cleanupCtx, sessions, id))
		}
		send := func(id string) string {
			t.Helper()
			session, err := sessions.GetSession(ctx, &apiv1.GetSessionRequest{SessionId: id})
			require.NoError(t, err)
			ref := session.GetSession().GetAgent()
			fixture := &interactionFixture{
				ctx: ctx, client: a2apb.NewA2AServiceClient(conn), sessionID: id, contextID: id, tenant: ref.GetNamespace() + "/" + ref.GetName(),
			}
			_, _, task := fixture.send(t, "What is 2+2?")
			require.Equal(t, a2atype.TaskStateCompleted, task.Status.State, "task response: %s", taskText(task))
			require.True(t, strings.Contains(taskText(task), "The answer is 4."))
			return string(task.ID)
		}
		// No FailedPrecondition retry: Ready must already be backed by a persisted
		// successful revision when the first create request arrives.
		createRequest := request(templateName)
		created, err := sessions.CreateSession(ctx, createRequest)
		require.NoError(t, err)
		source := created.GetSession()
		t.Cleanup(func() { deleteSession(source.GetId()) })
		send(source.GetId())

		// Observe the actual runtime through the public inventory API before deleting
		// references, so an empty response cannot falsely prove cleanup later.
		actor, err := findSubstrateActor(ctx, system, "", substrate.ActorName(source.GetId()))
		require.NoError(t, err)
		require.NotNil(t, actor)
		runtimeName := actor.GetActorTemplate().GetName()
		runtimeNamespace := actor.GetActorTemplate().GetAtespace()
		require.NotEmpty(t, runtimeName)
		backend, err := system.GetSubstrateSummary(ctx, &apiv1.GetSubstrateSummaryRequest{Namespace: "kagent", Atespace: runtimeNamespace})
		require.NoError(t, err)
		require.Empty(t, backend.GetAteApiError())
		var goldenActorID string
		for _, actorTemplate := range backend.GetActorTemplates() {
			if actorTemplate.GetMetadata().GetAtespace() == runtimeNamespace && actorTemplate.GetMetadata().GetName() == runtimeName {
				goldenActorID = actorTemplate.GetMetadata().GetUid()
			}
		}
		require.NotEmpty(t, goldenActorID)

		template := &v1alpha3.AgentTemplate{}
		require.NoError(t, kube.Get(ctx, ctrlclient.ObjectKey{Namespace: "kagent", Name: templateName}, template))
		originalModelName := template.Spec.ModelConfig.Name
		template.Spec.ModelConfig.Name = "missing-" + uuid.NewString()
		require.NoError(t, kube.Update(ctx, template))
		require.NoError(t, wait.PollUntilContextTimeout(ctx, time.Second, time.Minute, true, func(ctx context.Context) (bool, error) {
			current := &v1alpha3.Agent{}
			if err := kube.Get(ctx, ctrlclient.ObjectKeyFromObject(template), current); err != nil {
				return false, err
			}
			for _, condition := range current.Status.Conditions {
				if condition.Type == v1alpha3.AgentConditionReady {
					return condition.Status == metav1.ConditionFalse, nil
				}
			}
			return false, nil
		}))
		fallback, err := sessions.CreateSession(ctx, request(templateName))
		require.NoError(t, err, "an invalid edit must preserve the last successful runtime for fresh sessions")
		fallbackID := fallback.GetSession().GetId()
		t.Cleanup(func() { deleteSession(fallbackID) })
		require.NotEqual(t, source.GetId(), fallbackID)
		require.Equal(t, source.GetPreparedRevision(), fallback.GetSession().GetPreparedRevision())
		send(fallbackID)
		deleteSession(fallbackID)
		retried, err := sessions.CreateSession(ctx, createRequest)
		require.NoError(t, err, "creation retries must retain the existing session's pinned runtime")
		require.Equal(t, source.GetId(), retried.GetSession().GetId())
		require.Equal(t, source.GetPreparedRevision(), retried.GetSession().GetPreparedRevision())
		sourceTaskID := send(source.GetId())

		checkpointResponse := createCheckpoint(t, ctx, checkpoints, &apiv1.CreateCheckpointRequest{SessionId: source.GetId(), RequestId: uuid.NewString(), ExpectedHeadTaskId: sourceTaskID})
		checkpointID := checkpointResponse.GetCheckpoint().GetId()
		t.Cleanup(func() {
			cleanupCtx, cleanupCancel := context.WithTimeout(metadata.AppendToOutgoingContext(context.Background(), "x-user-id", "e2e"), time.Minute)
			defer cleanupCancel()
			_, err := checkpoints.DeleteCheckpoint(cleanupCtx, &apiv1.DeleteCheckpointRequest{CheckpointId: checkpointID})
			if status.Code(err) != codes.NotFound {
				require.NoError(t, err)
			}
		})
		deleteSession(source.GetId())
		require.NoError(t, kube.Delete(ctx, template))
		agent := &v1alpha3.Agent{ObjectMeta: metav1.ObjectMeta{Namespace: template.Namespace, Name: template.Name}}
		require.NoError(t, kube.Get(ctx, ctrlclient.ObjectKeyFromObject(agent), agent))
		require.NoError(t, kube.Delete(ctx, agent))
		// Kubernetes deletion reaches the database through the controller. Wait for
		// retirement, releasing any sessions created before that event is observed
		// so they cannot keep the runtime alive during the GC assertion below.
		require.NoError(t, wait.PollUntilContextTimeout(ctx, time.Second, time.Minute, true, func(ctx context.Context) (bool, error) {
			created, err := sessions.CreateSession(ctx, request(templateName))
			if status.Code(err) == codes.FailedPrecondition {
				return true, nil
			}
			if err != nil {
				return false, err
			}
			id := created.GetSession().GetId()
			t.Cleanup(func() { deleteSession(id) })
			return false, deleteIdleSession(ctx, sessions, id)
		}), "Agent deletion must eventually block fresh sessions")
		forked, err := checkpoints.ForkSession(ctx, &apiv1.ForkSessionRequest{CheckpointId: checkpointID, RequestId: uuid.NewString()})
		require.NoError(t, err, "a checkpoint must retain runnable inputs after its source and template are deleted")
		forkID := forked.GetSession().GetId()
		t.Cleanup(func() { deleteSession(forkID) })
		send(forkID)
		deleteSession(forkID)
		_, err = checkpoints.DeleteCheckpoint(ctx, &apiv1.DeleteCheckpointRequest{CheckpointId: checkpointID})
		require.NoError(t, err)

		// GC must remove the template after the final
		// checkpoint disappears, without another template event to drive cleanup.
		require.NoError(t, wait.PollUntilContextTimeout(ctx, time.Second, 3*time.Minute, true, func(ctx context.Context) (bool, error) {
			backend, err := system.GetSubstrateSummary(ctx, &apiv1.GetSubstrateSummaryRequest{Namespace: "kagent", Atespace: runtimeNamespace})
			if err != nil {
				return false, err
			}
			require.Empty(t, backend.GetAteApiError())
			for _, actorTemplate := range backend.GetActorTemplates() {
				if actorTemplate.GetMetadata().GetAtespace() == runtimeNamespace && actorTemplate.GetMetadata().GetName() == runtimeName {
					return false, nil
				}
			}
			actor, err := findSubstrateActor(ctx, system, "ate-golden", goldenActorID)
			return actor == nil, err
		}), "final checkpoint deletion must eventually collect its runtime without template changes")

		// Recreating the name must prepare a new identity after collection.
		replacement := template.DeepCopy()
		replacement.ObjectMeta = metav1.ObjectMeta{Namespace: template.Namespace, Name: template.Name, Labels: template.Labels}
		replacement.Spec.ModelConfig.Name = originalModelName
		createAndWaitInteractionTemplate(t, harness, kube, replacement)
		require.NotEqual(t, template.UID, replacement.UID)
		next := replacement.Name
		nextSession, err := sessions.CreateSession(ctx, request(next))
		require.NoError(t, err)
		t.Cleanup(func() { deleteSession(nextSession.GetSession().GetId()) })
		send(nextSession.GetSession().GetId())
	})
}

func findSubstrateActor(ctx context.Context, system apiv1.SystemServiceClient, atespace, name string) (*ateapipb.Actor, error) {
	for token := ""; ; {
		page, err := system.ListSubstrateActors(ctx, &apiv1.ListSubstrateActorsRequest{
			Atespace: atespace,
			Page:     &apiv1.PageRequest{Limit: 100, PageToken: token},
		})
		if err != nil {
			return nil, err
		}
		if page.GetAteApiError() != "" {
			return nil, fmt.Errorf("Substrate actors: %s", page.GetAteApiError())
		}
		for _, actor := range page.GetActors() {
			if actor.GetMetadata().GetName() == name {
				return actor, nil
			}
		}
		token = page.GetPage().GetNextPageToken()
		if token == "" {
			return nil, nil
		}
	}
}
