package translator_test

import (
	"testing"

	"github.com/stretchr/testify/require"
	ateapipb "go.ate.dev/substrate/api/ateapi/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/kagent-dev/kagent/go/api/v1alpha3"
	"github.com/kagent-dev/kagent/go/core/internal/substrate"
	"github.com/kagent-dev/kagent/go/core/internal/translator"
)

func TestCompileAgentHarnessResources(t *testing.T) {
	for _, scenario := range []struct {
		name           string
		resources      *v1alpha3.HarnessResources
		wantMillicores int64
		wantBytes      int64
	}{
		{
			name:           "Given a Harness without resources, when an Agent compiles, assert that its runtime uses the default limits",
			wantMillicores: 1000, wantBytes: 1 << 30,
		},
		{
			name:           "Given a Harness with CPU and memory limits, when an Agent compiles, assert that its runtime uses both limits",
			resources:      &v1alpha3.HarnessResources{CPU: new(resource.MustParse("2500m")), Memory: new(resource.MustParse("4Gi"))},
			wantMillicores: 2500, wantBytes: 4 << 30,
		},
		{
			name:           "Given a Harness with only a memory limit, when an Agent compiles, assert that its runtime keeps the default CPU limit",
			resources:      &v1alpha3.HarnessResources{Memory: new(resource.MustParse("512Mi"))},
			wantMillicores: 1000, wantBytes: 512 << 20,
		},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			_, actorTemplate := compileHarnessRuntime(t, scenario.resources)

			limits := actorTemplate.GetResources().GetLimits()
			require.Equal(t, scenario.wantMillicores, limits.GetCpuMillicores())
			require.Equal(t, scenario.wantBytes, limits.GetMemoryBytes())
		})
	}

	t.Run("Given an Agent on a Harness without resources, when the Harness sets a CPU limit, assert that the Agent compiles to a new revision", func(t *testing.T) {
		defaultRevision, _ := compileHarnessRuntime(t, nil)

		sizedRevision, _ := compileHarnessRuntime(t, &v1alpha3.HarnessResources{CPU: new(resource.MustParse("2"))})

		require.NotEqual(t, defaultRevision, sizedRevision)
	})
}

// compileHarnessRuntime compiles an Agent on a kagent Harness with the given
// resources and returns its revision and the ActorTemplate sent to Substrate.
func compileHarnessRuntime(t *testing.T, resources *v1alpha3.HarnessResources) (translator.RevisionID, *ateapipb.ActorTemplate) {
	t.Helper()
	harness := &v1alpha3.Harness{
		ObjectMeta: metav1.ObjectMeta{Name: "kagent", Namespace: "test"},
		Spec: v1alpha3.HarnessSpec{
			Kagent:    &v1alpha3.KagentHarness{},
			Resources: resources,
			Substrate: v1alpha3.RuntimeSubstratePolicy{
				WorkerPoolRef: corev1.LocalObjectReference{Name: "default"}, SnapshotPolicy: v1alpha3.RuntimeSnapshotPolicy{Location: "snapshots"},
			},
		},
	}
	template := &v1alpha3.AgentTemplate{
		ObjectMeta: metav1.ObjectMeta{Name: "agent", Namespace: "test"},
		Spec:       v1alpha3.AgentTemplateSpec{ModelConfig: &v1alpha3.ObjectReference{Name: "default-model"}, SystemPrompt: "help"},
	}
	result, err := compiler(t, modelConfig(), harness).CompileAgent(t.Context(), inlineAgent(harness, template))
	require.NoError(t, err)
	revisionID, err := result.Digest()
	require.NoError(t, err)
	bundle, err := result.BootstrapBundle(revisionID)
	require.NoError(t, err)
	actorTemplate, err := substrate.ActorTemplateForRevision(&result.Revision, revisionID, bundle)
	require.NoError(t, err)
	return revisionID, actorTemplate
}
