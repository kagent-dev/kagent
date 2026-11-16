package e2e_test

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/kagent-dev/kagent/go/api/v1alpha3"
)

// runtimeEntrypoints is the ENTRYPOINT of each fixture image, so a wrapper
// command can start the runtime the image would have started.
var runtimeEntrypoints = map[string]string{
	"kagent":  "/app",
	"codex":   "/usr/local/bin/kagent-codex",
	"claude":  "/usr/local/bin/kagent-claude",
	"byo-adk": "/app",
}

// TestHarnessWorkloadOverridesReachTheRuntime checks that every harness runs
// spec.workload.command and args instead of the image entrypoint. A compiler
// that drops them makes the failing wrapper case report Ready.
func TestHarnessWorkloadOverridesReachTheRuntime(t *testing.T) {
	interactionTarget(t)
	forEachHarness(t, func(t *testing.T, runtime testHarness) {
		entrypoint, ok := runtimeEntrypoints[runtime.runtimeLabel]
		require.True(t, ok, "no entrypoint for runtime %q", runtime.runtimeLabel)
		kube := interactionKubeClient(t)
		model := runtime.createModel(t, kube, startInteractionMock(t), nil)

		t.Run(fmt.Sprintf("Given a %s Harness whose command exits before the runtime starts, when an Agent references it, assert that the golden build fails", runtime.runtimeLabel), func(t *testing.T) {
			t.Parallel()
			agent := createAgentOnOverriddenHarness(t, kube, runtime, model, []string{"exit 3"})
			waitForGoldenBuildFailure(t, kube, agent)
		})

		t.Run(fmt.Sprintf("Given a %s Harness whose command wraps the image entrypoint, when an Agent references it, assert that the Agent becomes ready", runtime.runtimeLabel), func(t *testing.T) {
			t.Parallel()
			agent := createAgentOnOverriddenHarness(t, kube, runtime, model, []string{"exec " + entrypoint})
			waitForAgentRevision(t, kube, agent)
		})
	})
}

// createAgentOnOverriddenHarness copies the installed fixture Harness, runs
// args through /bin/sh as its workload command, and creates an Agent on it.
func createAgentOnOverriddenHarness(t *testing.T, kube ctrlclient.Client, runtime testHarness, model *v1alpha3.ModelConfig, args []string) *v1alpha3.Agent {
	t.Helper()
	source := &v1alpha3.Harness{}
	require.NoError(t, kube.Get(t.Context(), ctrlclient.ObjectKey{Namespace: "kagent", Name: runtime.name}, source))
	harness := &v1alpha3.Harness{ObjectMeta: metav1.ObjectMeta{GenerateName: "workload-override-", Namespace: source.Namespace}, Spec: *source.Spec.DeepCopy()}
	harness.Spec.Workload.Command = []string{"/bin/sh", "-c"}
	harness.Spec.Workload.Args = args
	require.NoError(t, kube.Create(t.Context(), harness))
	t.Cleanup(func() { require.NoError(t, ctrlclient.IgnoreNotFound(kube.Delete(context.Background(), harness))) })

	agent := &v1alpha3.Agent{
		ObjectMeta: metav1.ObjectMeta{GenerateName: "workload-override-", Namespace: source.Namespace},
		Spec: v1alpha3.AgentSpec{
			HarnessRef: corev1.LocalObjectReference{Name: harness.Name},
			Template:   v1alpha3.AgentTemplateSource{Inline: &v1alpha3.AgentTemplateSpec{ModelConfig: &v1alpha3.ModelConfigReference{Name: model.Name}, SystemPrompt: "Be helpful."}},
		},
	}
	require.NoError(t, kube.Create(t.Context(), agent))
	t.Cleanup(func() { require.NoError(t, ctrlclient.IgnoreNotFound(kube.Delete(context.Background(), agent))) })
	return agent
}

// waitForGoldenBuildFailure waits until the Agent reports that its golden actor
// failed to start, and fails as soon as the Agent reports Ready instead.
func waitForGoldenBuildFailure(t *testing.T, kube ctrlclient.Client, agent *v1alpha3.Agent) {
	t.Helper()
	err := wait.PollUntilContextTimeout(t.Context(), time.Second, 3*time.Minute, true, func(ctx context.Context) (bool, error) {
		if err := kube.Get(ctx, ctrlclient.ObjectKeyFromObject(agent), agent); err != nil {
			return false, err
		}
		condition := meta.FindStatusCondition(agent.Status.Conditions, v1alpha3.AgentConditionReady)
		if condition == nil || condition.ObservedGeneration != agent.Generation {
			return false, nil
		}
		if condition.Status == metav1.ConditionTrue {
			return false, errors.New("the Agent became ready, so the runtime ignored the workload command")
		}
		// The controller reports a failed golden build with this reason.
		return condition.Reason == "ActorTemplateRetrying", nil
	})
	require.NoError(t, err, "last conditions: %+v", agent.Status.Conditions)
}
