package substrate

import (
	"context"
	"testing"

	atev1alpha1 "github.com/agent-substrate/substrate/pkg/api/v1alpha1"
	"github.com/kagent-dev/kagent/go/api/v1alpha3"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

// TestBuildActorTemplate exercises ActorTemplate generation for an AgentHarness (the
// non-SandboxAgent path), asserting the SnapshotsConfig mirrors substrate's CRD defaults
// exactly the way buildSandboxAgentActorTemplate does (see agent_lifecycle_test.go's
// TestBuildSandboxAgentActorTemplate for the equivalent SandboxAgent-side coverage).
func TestBuildActorTemplate(t *testing.T) {
	t.Parallel()

	const pinnedImage = "registry.example/kagent-dev/kagent/app@sha256:2222222222222222222222222222222222222222222222222222222222222222"
	wpKey := types.NamespacedName{Namespace: "kagent", Name: "kagent-default"}

	ah := &v1alpha3.AgentHarness{
		ObjectMeta: metav1.ObjectMeta{Name: "my-harness", Namespace: "kagent"},
		Spec: v1alpha3.AgentHarnessSpec{
			Backend: v1alpha3.AgentHarnessBackendHermes,
			Substrate: &v1alpha3.AgentHarnessSubstrateSpec{
				WorkloadImage: pinnedImage,
			},
		},
	}

	p := newTestLifecycle(t)
	tmpl, err := p.buildActorTemplate(context.Background(), ah, wpKey)
	require.NoError(t, err)

	require.Len(t, tmpl.Spec.Containers, 1)
	c := tmpl.Spec.Containers[0]
	require.Equal(t, pinnedImage, c.Image, "ActorTemplate must use the digest-pinned image")
	require.Equal(t, wpKey.Name, tmpl.Spec.WorkerSelector.MatchLabels["kagent.dev/worker-pool"])

	// SnapshotsConfig must mirror substrate's CRD defaults exactly, or kagent's spec-drift
	// check (apiequality.Semantic.DeepEqual in actorTemplateSpecEqual) will treat the
	// apiserver-defaulted stored spec as permanently different from the freshly-rebuilt
	// desired spec, and reconcileActorTemplate will delete+recreate the ActorTemplate (and
	// its golden actor) on every single reconcile.
	require.Equal(t, atev1alpha1.SnapshotScopeFull, tmpl.Spec.SnapshotsConfig.OnPause)
	require.Equal(t, atev1alpha1.SnapshotScopeFull, tmpl.Spec.SnapshotsConfig.OnCommit)
	require.Equal(t, atev1alpha1.ResumeSourceColdBoot, tmpl.Spec.SnapshotsConfig.OnResume.FromData)
}
