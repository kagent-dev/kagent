package translator_test

import (
	"testing"

	"github.com/stretchr/testify/require"
	atev1 "go.ate.dev/substrate/api/infra/k8s/ate/v1"
	"istio.io/istio/pkg/kube/krt"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	"github.com/kagent-dev/kagent/go/api/v1alpha3"
	"github.com/kagent-dev/kagent/go/core/internal/translator"
	kagenttranslator "github.com/kagent-dev/kagent/go/core/internal/translator/kagent"
)

func TestResolveWorkerPoolAcrossNamespaces(t *testing.T) {
	shared := &atev1.WorkerPool{ObjectMeta: metav1.ObjectMeta{Namespace: "ate-workers", Name: "shared"}}
	ref := v1alpha3.WorkerPoolReference{Name: "shared", Namespace: "ate-workers"}
	for _, tt := range []struct {
		name       string
		namespaces []string
		ok         bool
	}{
		{name: "not configured"},
		{name: "other namespaces", namespaces: []string{"pools-b"}},
		{name: "listed", namespaces: []string{"pools-b", "ate-workers"}, ok: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			collections := mockCollections(t, shared)
			collections.WorkerPoolNamespaces = tt.namespaces
			pool, err := translator.ResolveWorkerPool(krt.TestingDummyContext{}, collections, "team-a", ref)
			if !tt.ok {
				require.EqualError(t, err, `reference from namespace "team-a" to WorkerPool "ate-workers/shared" denied: namespace "ate-workers" is not in substrate.workerPoolNamespaces`)
				require.Nil(t, pool)
				return
			}
			require.NoError(t, err)
			require.Equal(t, types.NamespacedName{Namespace: "ate-workers", Name: "shared"}, types.NamespacedName{Namespace: pool.Namespace, Name: pool.Name})
		})
	}

	t.Run("same namespace needs no configuration", func(t *testing.T) {
		collections := mockCollections(t, &atev1.WorkerPool{ObjectMeta: metav1.ObjectMeta{Namespace: "team-a", Name: "local"}})
		pool, err := translator.ResolveWorkerPool(krt.TestingDummyContext{}, collections, "team-a", v1alpha3.WorkerPoolReference{Name: "local"})
		require.NoError(t, err)
		require.Equal(t, "team-a", pool.Namespace)
	})

	t.Run("missing in an allowed namespace", func(t *testing.T) {
		collections := mockCollections(t)
		collections.WorkerPoolNamespaces = []string{"ate-workers"}
		_, err := translator.ResolveWorkerPool(krt.TestingDummyContext{}, collections, "team-a", ref)
		var missing *translator.WorkerPoolNotFoundError
		require.ErrorAs(t, err, &missing)
		require.Equal(t, types.NamespacedName{Namespace: "ate-workers", Name: "shared"}, missing.WorkerPool)
	})
}

func TestRevisionPoolNamespaceIdentity(t *testing.T) {
	local := translator.Revision{Namespace: "team-a", AgentName: "helper", WorkerPoolName: "shared", CPU: "1", Memory: "1Gi"}
	explicitLocal := local
	explicitLocal.WorkerPoolNamespace = "team-a"
	shared := local
	shared.WorkerPoolNamespace = "ate-workers"

	localID, err := local.Digest()
	require.NoError(t, err)
	explicitID, err := explicitLocal.Digest()
	require.NoError(t, err)
	sharedID, err := shared.Digest()
	require.NoError(t, err)
	require.Equal(t, localID, explicitID, "an unset pool namespace must keep existing revision identities")
	require.NotEqual(t, localID, sharedID, "moving to another pool namespace must prepare a new revision")
}

func TestCompileAgentPlacesOnPoolInAllowedNamespace(t *testing.T) {
	harness := &v1alpha3.Harness{
		ObjectMeta: metav1.ObjectMeta{Namespace: "test", Name: "kagent"},
		Spec: v1alpha3.HarnessSpec{
			Kagent: &v1alpha3.KagentHarness{},
			Substrate: v1alpha3.RuntimeSubstratePolicy{
				WorkerPoolRef: v1alpha3.WorkerPoolReference{Name: "shared", Namespace: "ate-workers"}, SnapshotPolicy: v1alpha3.RuntimeSnapshotPolicy{Location: "snapshots"},
			},
		},
	}
	template := &v1alpha3.AgentTemplate{
		ObjectMeta: metav1.ObjectMeta{Namespace: "test", Name: "assistant"},
		Spec:       v1alpha3.AgentTemplateSpec{ModelConfig: &v1alpha3.ModelConfigReference{Name: "default-model"}, SystemPrompt: "help"},
	}
	model := modelConfig()
	model.Spec.APIKeySecret, model.Spec.APIKeySecretKey = "model-auth", "api-key"
	collections := mockCollections(t, model, harness,
		&corev1.Secret{ObjectMeta: metav1.ObjectMeta{Namespace: "test", Name: "model-auth"}, Data: map[string][]byte{"api-key": []byte("secret")}},
		&atev1.WorkerPool{ObjectMeta: metav1.ObjectMeta{Namespace: "ate-workers", Name: "shared"}, Spec: atev1.WorkerPoolSpec{SandboxClasses: []atev1.WorkerPoolSandboxClass{{Name: atev1.SandboxClassMicroVM}}}},
	)
	collections.WorkerPoolNamespaces = []string{"ate-workers"}
	ctx := krt.TestingDummyContext{}
	compiler := translator.NewCompiler(ctx, collections, map[translator.HarnessType]translator.HarnessCompiler{
		translator.HarnessTypeKagent: kagenttranslator.NewCompiler(ctx, collections, testRuntimeDefaults()),
	}, testHarnessImages, testRuntimeDefaults())

	result, err := compiler.CompileAgent(t.Context(), inlineAgent(harness, template))

	require.NoError(t, err)
	require.Equal(t, "test", result.Namespace, "the Agent keeps its namespace and Atespace")
	require.Equal(t, "ate-workers", result.WorkerPoolNamespace)
	require.Equal(t, "shared", result.WorkerPoolName)
	require.Equal(t, atev1.SandboxClassMicroVM, result.SandboxClass, "the referenced pool supplies the sandbox class")
}
