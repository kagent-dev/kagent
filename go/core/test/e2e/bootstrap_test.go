package e2e_test

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	a2atype "github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/metadata"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"

	apiv1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1"
	"github.com/kagent-dev/kagent/go/api/v1alpha3"
)

// Large config and card payloads must reach the model across golden startup,
// suspension/restore and a checkpoint fork, without the environment-size limit.
func TestLargeConfigurationBootstrap(t *testing.T) {
	target := interactionTarget(t)
	forEachHarness(t, func(t *testing.T, harness testHarness) {
		kube := interactionKubeClient(t)
		recorder := startModelRecorder(t, mockLLMProxy(t, startMockLLMServer(t, interactionMocks, "mocks/invoke_agent.json")), nil)
		model := harness.createModel(t, kube, reachableModelURL(t, recorder.URL), nil)
		const marker = "bootstrap-large-prompt-end"
		prompt := strings.Repeat("Helpful context. ", 8000) + marker
		template := &v1alpha3.AgentTemplate{ObjectMeta: metav1.ObjectMeta{GenerateName: "bootstrap-large-", Namespace: "kagent", Labels: harness.labels()}, Spec: v1alpha3.AgentTemplateSpec{ModelConfig: &v1alpha3.ObjectReference{Name: model.Name}, SystemPrompt: prompt, Description: strings.Repeat("description ", 4000)}}
		createAndWaitInteractionTemplate(t, harness, kube, template)
		fixture := newInteractionFixtureForTemplate(t, harness, target, template.Name)
		_, _, task := fixture.send(t, "What is 2+2?")
		require.Equal(t, a2atype.TaskStateCompleted, task.Status.State, "task failed: %s", taskText(task))
		assertActorSuspended(t, fixture)
		_, _, resumed := fixture.send(t, "What is 2+2?")
		require.Equal(t, a2atype.TaskStateCompleted, resumed.Status.State, "restored task failed: %s", taskText(resumed))
		checkpoint := createCheckpoint(t, fixture.ctx, fixture.checkpoints, &apiv1.CreateCheckpointRequest{SessionId: fixture.sessionID, RequestId: uuid.NewString(), ExpectedHeadTaskId: string(resumed.ID)}).GetCheckpoint()
		t.Cleanup(func() {
			ctx, cancel := context.WithTimeout(metadata.AppendToOutgoingContext(context.Background(), "x-user-id", "e2e"), time.Minute)
			defer cancel()
			_, err := fixture.checkpoints.DeleteCheckpoint(ctx, &apiv1.DeleteCheckpointRequest{CheckpointId: checkpoint.Id})
			require.NoError(t, err)
		})
		forked, err := fixture.checkpoints.ForkSession(fixture.ctx, &apiv1.ForkSessionRequest{CheckpointId: checkpoint.Id, RequestId: uuid.NewString()})
		require.NoError(t, err)
		fork := *fixture
		fork.sessionID, fork.contextID = forked.Session.Id, forked.Session.ContextId
		t.Cleanup(func() {
			ctx, cancel := context.WithTimeout(metadata.AppendToOutgoingContext(context.Background(), "x-user-id", "e2e"), time.Minute)
			defer cancel()
			require.NoError(t, deleteIdleSession(ctx, fixture.sessions, fork.sessionID))
		})
		_, _, forkTask := fork.send(t, "What is 2+2?")
		require.Equal(t, a2atype.TaskStateCompleted, forkTask.Status.State, "fork task failed: %s", taskText(forkTask))
		recorder.mu.Lock()
		defer recorder.mu.Unlock()
		modelCalls := 0
		for _, request := range recorder.requests {
			// Claude also probes the endpoint without a model request body.
			if request.Method != http.MethodPost {
				continue
			}
			modelCalls++
			require.True(t, bytes.Contains(request.Body, []byte(marker)), "every model invocation must retain the pinned large prompt")
		}
		require.GreaterOrEqual(t, modelCalls, 3, "startup, restore, and fork must each invoke the model")
	})
}

func TestADKSharedConfigurationBootstrap(t *testing.T) {
	target := interactionTarget(t)
	kube := interactionKubeClient(t)
	harness := testHarness{name: "kagent", runtimeLabel: "kagent"}
	recorder := startModelRecorder(t, mockLLMProxy(t, startMockLLMServer(t, interactionMocks, "mocks/invoke_shared_agent.json")), nil)
	template := largeSharedADKTemplate(t, kube, harness, reachableModelURL(t, recorder.URL), 3)
	// Given three 56 KiB shared agents and a large card, when the compiled
	// runtime boots and invokes a child, assert that its full instruction arrives.
	createAndWaitInteractionTemplate(t, harness, kube, template)
	fixture := newInteractionFixtureForTemplate(t, harness, target, template.Name)
	_, _, task := fixture.send(t, "Ask the specialist")
	require.Equal(t, a2atype.TaskStateCompleted, task.Status.State, "%s", taskText(task))
	require.Contains(t, taskText(task), "Answer from the shared specialist.")
	assertActorSuspended(t, fixture)
	_, _, task = fixture.send(t, "Ask the specialist")
	require.Equal(t, a2atype.TaskStateCompleted, task.Status.State, "%s", taskText(task))
	requests := recorder.Requests("X-Kagent-E2E-Agent", "child")
	require.NotEmpty(t, requests, "the shared ADK child must actually execute")
	for _, request := range requests {
		require.Greater(t, len(request.Body), 56<<10)
		require.Contains(t, string(request.Body), "large-shared-child-instruction-end")
	}
}

func TestADKOversizedConfigurationRejected(t *testing.T) {
	interactionTarget(t)
	kube := interactionKubeClient(t)
	harness := testHarness{name: "kagent", runtimeLabel: "kagent"}
	template := largeSharedADKTemplate(t, kube, harness, startInteractionMock(t), 4)
	// Given four 56 KiB shared agents, when the Agent is created, assert that
	// its oversized compiled bundle is rejected with a useful compatibility error.
	createSharedTemplate(t, kube, template)
	agent := &v1alpha3.Agent{ObjectMeta: metav1.ObjectMeta{Name: template.Name, Namespace: template.Namespace}, Spec: v1alpha3.AgentSpec{HarnessRef: corev1.LocalObjectReference{Name: harness.name}, Template: v1alpha3.AgentTemplateSource{Ref: &corev1.LocalObjectReference{Name: template.Name}}}}
	require.NoError(t, kube.Create(t.Context(), agent))
	t.Cleanup(func() { require.NoError(t, kube.Delete(context.Background(), agent)) })
	err := wait.PollUntilContextTimeout(t.Context(), time.Second, time.Minute, true, func(ctx context.Context) (bool, error) {
		if err := kube.Get(ctx, ctrlclient.ObjectKeyFromObject(agent), agent); err != nil {
			return false, err
		}
		condition := meta.FindStatusCondition(agent.Status.Conditions, v1alpha3.AgentConditionCompatible)
		return condition != nil && condition.ObservedGeneration == agent.Generation && condition.Status == metav1.ConditionFalse && condition.Reason == "ActorTemplateInvalid", nil
	})
	require.NoError(t, err, "last conditions: %+v", agent.Status.Conditions)
	condition := meta.FindStatusCondition(agent.Status.Conditions, v1alpha3.AgentConditionCompatible)
	require.Contains(t, condition.Message, "configuration and Agent Card exceed")
	require.Empty(t, agent.Status.DesiredRevision)
	require.Empty(t, agent.Status.LatestSuccessfulRevision)
}

func largeSharedADKTemplate(t *testing.T, kube ctrlclient.Client, harness testHarness, modelURL string, children int) *v1alpha3.AgentTemplate {
	t.Helper()
	rootModel := harness.createModel(t, kube, modelURL, map[string]string{"X-Kagent-E2E-Agent": "root"})
	childModel := harness.createModel(t, kube, modelURL, map[string]string{"X-Kagent-E2E-Agent": "child"})
	root := &v1alpha3.AgentTemplate{ObjectMeta: metav1.ObjectMeta{GenerateName: "bootstrap-large-shared-", Namespace: "kagent", Labels: harness.labels()}, Spec: v1alpha3.AgentTemplateSpec{ModelConfig: &v1alpha3.ObjectReference{Name: rootModel.Name}, SystemPrompt: "Delegate every request to the specialist.", Description: strings.Repeat("d", 40<<10)}}
	for i := range children {
		child := &v1alpha3.AgentTemplate{ObjectMeta: metav1.ObjectMeta{GenerateName: "bootstrap-large-child-", Namespace: "kagent", Labels: harness.labels()}, Spec: v1alpha3.AgentTemplateSpec{ModelConfig: &v1alpha3.ObjectReference{Name: childModel.Name}, Description: "Shared specialist", SystemPrompt: strings.Repeat("c", 56<<10) + "large-shared-child-instruction-end"}}
		createSharedTemplate(t, kube, child)
		name := "specialist"
		if i > 0 {
			name = fmt.Sprintf("additional_specialist_%d", i)
		}
		root.Spec.Tools = append(root.Spec.Tools, v1alpha3.ToolBinding{SubAgent: &v1alpha3.SubAgentToolBinding{Name: name, Description: "Handles specialist requests", TemplateRef: &corev1.LocalObjectReference{Name: child.Name}}})
	}
	return root
}
