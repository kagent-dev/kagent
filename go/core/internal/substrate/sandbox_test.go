package substrate

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	atev1alpha1 "go.ate.dev/substrate/pkg/api/v1alpha1"
	"go.ate.dev/substrate/pkg/proto/ateapipb"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/kagent-dev/kagent/go/api/v1alpha3"
)

func TestSandboxPreparationPinsCompleteInputs(t *testing.T) {
	template := &v1alpha3.SandboxTemplate{ObjectMeta: metav1.ObjectMeta{Namespace: "team-a", Name: "scratch", UID: "uid"}, Spec: v1alpha3.SandboxTemplateSpec{
		Workload:  v1alpha3.SandboxTemplateWorkload{Image: "tools@sha256:" + strings.Repeat("a", 64)},
		Substrate: v1alpha3.RuntimeSubstratePolicy{WorkerPoolRef: corev1.LocalObjectReference{Name: "default"}, SnapshotPolicy: v1alpha3.RuntimeSnapshotPolicy{Location: "s3://snapshots/"}},
	}}
	policy := SandboxPolicy{GuestImage: "guest@sha256:" + strings.Repeat("b", 64), CPU: "1", Memory: "1Gi"}
	actor, digest, snapshot, err := SandboxActorTemplate(template, "", policy)
	require.NoError(t, err)
	require.NotEqual(t, "5bf14bc98818a54bf4e59e5aca83de9bde5df22e9efa206b65e1389069a188c3", digest,
		"namespace-scoped placement must create a fresh immutable ActorTemplate")
	testWorkerPoolSelection(t, actor, template.Namespace, template.Spec.Substrate.WorkerPoolRef.Name)
	require.Contains(t, string(snapshot), policy.GuestImage)
	require.Equal(t, []string{"/run/kagent/guest/usr/local/bin/kagent-sandbox-guest"}, actor.Containers[0].Command)
	require.Equal(t, policy.GuestImage, actor.Volumes[1].Image.Reference)
	require.Equal(t, "1Gi", actor.Resources.Limits[1].Quantity)
	require.Equal(t, ateapipb.SnapshotFidelity_SNAPSHOT_FIDELITY_VOLUMES, actor.GetSnapshotConfig().GetPreferredFidelity())
	require.Equal(t, ateapipb.GoldenSnapshotMode_GOLDEN_SNAPSHOT_MODE_ENABLED, actor.GetSnapshotConfig().GetGoldenSnapshotConfig().GetMode())
	trust := actor.Volumes[2].GetSystemInfo().GetDataSources()[0].GetTrustBundle()
	require.Equal(t, []string{"egress-mitm.ate.dev"}, trust.GetNames())
	require.Equal(t, "trust-bundle.pem", trust.GetPath())
	_, same, _, err := SandboxActorTemplate(template, atev1alpha1.SandboxClassGvisor, policy)
	require.NoError(t, err)
	require.Equal(t, digest, same)
	template.Spec.EgressPolicyRefs = []corev1.LocalObjectReference{{Name: "live"}}
	_, unchanged, source, err := SandboxActorTemplate(template, "", policy)
	require.NoError(t, err)
	require.Equal(t, digest, unchanged, "policy references are live authorization")
	var recorded sandboxInputs
	require.NoError(t, json.Unmarshal(source, &recorded))
	require.Equal(t, template.Spec, recorded.Spec, "provenance retains the complete source spec")
	for _, change := range []struct {
		name   string
		mutate func(*v1alpha3.SandboxTemplate, *SandboxPolicy)
		class  atev1alpha1.SandboxClass
	}{
		{"guest", func(_ *v1alpha3.SandboxTemplate, p *SandboxPolicy) {
			p.GuestImage = "guest@sha256:" + strings.Repeat("c", 64)
		}, ""},
		{"identity", func(s *v1alpha3.SandboxTemplate, _ *SandboxPolicy) { s.UID = "new" }, ""},
		{"resources", func(_ *v1alpha3.SandboxTemplate, p *SandboxPolicy) { p.Memory = "2Gi" }, ""},
		{"class", func(_ *v1alpha3.SandboxTemplate, _ *SandboxPolicy) {}, atev1alpha1.SandboxClassMicroVM},
	} {
		t.Run(change.name, func(t *testing.T) {
			input, settings := template.DeepCopy(), policy
			change.mutate(input, &settings)
			_, changed, _, err := SandboxActorTemplate(input, change.class, settings)
			require.NoError(t, err)
			require.NotEqual(t, digest, changed)
		})
	}
	template.Spec.Env = []v1alpha3.RuntimeEnvVar{{Name: "SSL_CERT_FILE", Value: "/untrusted"}}
	actor, _, _, err = SandboxActorTemplate(template, "", policy)
	require.ErrorContains(t, err, "reserved")
	require.Nil(t, actor)
}
