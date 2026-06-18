package substrate

import (
	"context"
	"strings"
	"testing"

	atev1alpha1 "github.com/agent-substrate/substrate/pkg/api/v1alpha1"
	"github.com/kagent-dev/kagent/go/api/v1alpha2"
	"github.com/kagent-dev/kagent/go/core/pkg/consts"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestShortConfigHash(t *testing.T) {
	t.Parallel()
	// Matches the translator's decimal uint64 annotation; rendered as hex.
	require.Equal(t, "ff", shortConfigHash("255"))
	require.Equal(t, "", shortConfigHash(""))
	require.Equal(t, "", shortConfigHash("not-a-number"))
	require.NotEqual(t, shortConfigHash("100"), shortConfigHash("101"))
}

func TestSandboxAgentActorTemplateNameWithHash(t *testing.T) {
	t.Parallel()
	sa := &v1alpha2.SandboxAgent{ObjectMeta: metav1.ObjectMeta{Name: "my-agent", Namespace: "kagent"}}

	// Distinct configs → distinct template names → distinct golden snapshots.
	n1 := sandboxAgentActorTemplateName(sa, "abc123")
	n2 := sandboxAgentActorTemplateName(sa, "def456")
	require.Equal(t, "my-agent-abc123", n1)
	require.NotEqual(t, n1, n2)
	require.LessOrEqual(t, len(n1), 63)

	// Empty hash falls back to the stable base name (preserves prior behavior).
	require.Equal(t, "my-agent", sandboxAgentActorTemplateName(sa, ""))

	// Long agent names stay within the DNS-1123 budget once the hash suffix is added.
	long := &v1alpha2.SandboxAgent{ObjectMeta: metav1.ObjectMeta{Name: strings.Repeat("a", 80)}}
	require.LessOrEqual(t, len(sandboxAgentActorTemplateName(long, "deadbeefdeadbeef")), 63)
}

func TestSandboxAgentSessionActorIDVariesWithHash(t *testing.T) {
	t.Parallel()
	sa := &v1alpha2.SandboxAgent{ObjectMeta: metav1.ObjectMeta{Name: "my-agent", Namespace: "kagent"}}

	id1 := SandboxAgentSessionActorID(sa, "abc123", "sess-1")
	id2 := SandboxAgentSessionActorID(sa, "def456", "sess-1")
	require.NotEqual(t, id1, id2, "config change must yield a new actor id so a fresh actor is created")

	// Same hash + session is stable so repeated messages resume the warm actor.
	require.Equal(t, id1, SandboxAgentSessionActorID(sa, "abc123", "sess-1"))

	// Keeps the per-agent prefix so DeleteAll / reaping still match by prefix.
	prefix := sandboxAgentActorPrefix(sa)
	require.True(t, strings.HasPrefix(id1, prefix+"-"))
}

func TestBuildActorTemplateStampsConfigHash(t *testing.T) {
	t.Parallel()
	p := newTestLifecycle(t)
	sa := &v1alpha2.SandboxAgent{
		ObjectMeta: metav1.ObjectMeta{Name: "py-agent", Namespace: "kagent"},
		Spec: v1alpha2.SandboxAgentSpec{
			Platform:  v1alpha2.SandboxPlatformSubstrate,
			AgentSpec: v1alpha2.AgentSpec{Type: v1alpha2.AgentType_Declarative, Declarative: &v1alpha2.DeclarativeAgentSpec{Runtime: v1alpha2.DeclarativeRuntime_Python}},
		},
	}
	pod := corev1.PodTemplateSpec{
		ObjectMeta: metav1.ObjectMeta{Annotations: map[string]string{consts.ConfigHashAnnotation: "255"}},
		Spec: corev1.PodSpec{Containers: []corev1.Container{{
			Name:  defaultKagentContainer,
			Image: "registry.example/app@sha256:1111111111111111111111111111111111111111111111111111111111111111",
		}}},
	}
	wpKey := types.NamespacedName{Namespace: "kagent", Name: "kagent-default"}
	tmpl, err := p.buildSandboxAgentActorTemplate(sa, wpKey, pod)
	require.NoError(t, err)
	require.Equal(t, "py-agent-ff", tmpl.Name, "template name must carry the config-hash suffix")
	require.Equal(t, "ff", tmpl.Annotations[consts.ConfigHashAnnotation])
}

func TestResolveCurrentActorTemplate(t *testing.T) {
	t.Parallel()
	scheme := runtime.NewScheme()
	utilruntime.Must(atev1alpha1.AddToScheme(scheme))

	// Old template is Ready (serving); newer one is still building. Blue-green: serve the old
	// Ready golden until the new is Ready, so the resolver must prefer the Ready one even though
	// it's older.
	oldReady := &atev1alpha1.ActorTemplate{ObjectMeta: metav1.ObjectMeta{
		Name: "my-agent-old", Namespace: "kagent",
		Labels:            map[string]string{SandboxAgentLabelKey: "my-agent"},
		CreationTimestamp: metav1.Unix(100, 0),
	}, Status: atev1alpha1.ActorTemplateStatus{Phase: atev1alpha1.PhaseReady}}
	newerBuilding := &atev1alpha1.ActorTemplate{ObjectMeta: metav1.ObjectMeta{
		Name: "my-agent-new", Namespace: "kagent",
		Labels:            map[string]string{SandboxAgentLabelKey: "my-agent"},
		CreationTimestamp: metav1.Unix(200, 0),
	}, Status: atev1alpha1.ActorTemplateStatus{Phase: atev1alpha1.PhaseResumeGoldenActor}}
	other := &atev1alpha1.ActorTemplate{ObjectMeta: metav1.ObjectMeta{
		Name: "other-agent", Namespace: "kagent",
		Labels: map[string]string{SandboxAgentLabelKey: "other-agent"},
	}}
	cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(oldReady, newerBuilding, other).Build()

	got, err := ResolveCurrentActorTemplate(context.Background(), cl, "kagent", "my-agent")
	require.NoError(t, err)
	require.NotNil(t, got)
	require.Equal(t, "my-agent-old", got.Name, "must prefer the newest READY template (no downtime during rebuild)")

	none, err := ResolveCurrentActorTemplate(context.Background(), cl, "kagent", "absent")
	require.NoError(t, err)
	require.Nil(t, none)

	// When none is Ready yet (first build), fall back to the newest.
	firstBuild := fake.NewClientBuilder().WithScheme(scheme).WithObjects(newerBuilding).Build()
	got, err = ResolveCurrentActorTemplate(context.Background(), firstBuild, "kagent", "my-agent")
	require.NoError(t, err)
	require.NotNil(t, got)
	require.Equal(t, "my-agent-new", got.Name)
}

func TestRetireSupersededTemplates(t *testing.T) {
	t.Parallel()
	scheme := runtime.NewScheme()
	utilruntime.Must(atev1alpha1.AddToScheme(scheme))

	lbl := map[string]string{SandboxAgentLabelKey: "my-agent"}
	mk := func(name string, created int64, phase atev1alpha1.PhaseType) *atev1alpha1.ActorTemplate {
		return &atev1alpha1.ActorTemplate{
			ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "kagent", Labels: lbl, CreationTimestamp: metav1.Unix(created, 0)},
			Status:     atev1alpha1.ActorTemplateStatus{Phase: phase}, // GoldenActorID empty → no ate-api call
		}
	}
	oldReady := mk("my-agent-old", 100, atev1alpha1.PhaseReady)        // superseded → retire
	activeReady := mk("my-agent-active", 150, atev1alpha1.PhaseReady)  // newest Ready → keep (serving)
	newestBuilding := mk("my-agent-new", 200, atev1alpha1.PhaseResumeGoldenActor) // desired/building → keep

	cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(oldReady, activeReady, newestBuilding).Build()
	p := &Lifecycle{Client: cl}
	sa := &v1alpha2.SandboxAgent{ObjectMeta: metav1.ObjectMeta{Name: "my-agent", Namespace: "kagent"}}

	// The superseded oldReady (Ready, golden Suspended) is retired now; the serving activeReady and
	// the building newest are kept. No retirement is pending (the building one is kept, not retired),
	// so done==true — the controller's ActorTemplate watch re-triggers retirement of activeReady once
	// newest becomes Ready.
	done, err := p.RetireSupersededTemplates(context.Background(), sa)
	require.NoError(t, err)
	require.True(t, done, "no retirement pending: oldReady removed, active+building kept")

	remaining, err := listSandboxAgentActorTemplates(context.Background(), cl, "kagent", "my-agent")
	require.NoError(t, err)
	names := map[string]bool{}
	for _, t := range remaining {
		names[t.Name] = true
	}
	require.False(t, names["my-agent-old"], "superseded Ready template must be retired")
	require.True(t, names["my-agent-active"], "newest Ready (serving) template must be kept")
	require.True(t, names["my-agent-new"], "newest (building) template must be kept")
}
