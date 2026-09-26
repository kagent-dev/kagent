package substrate

import (
	"strings"
	"testing"

	atev1alpha1 "github.com/agent-substrate/substrate/pkg/api/v1alpha1"
	"github.com/kagent-dev/kagent/go/api/v1alpha3"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestSandboxPreparationPinsCompleteInputs(t *testing.T) {
	template := &v1alpha3.SandboxTemplate{ObjectMeta: metav1.ObjectMeta{Namespace: "team-a", Name: "scratch", UID: "uid"}, Spec: v1alpha3.SandboxTemplateSpec{
		Workload:  v1alpha3.SandboxTemplateWorkload{Image: "tools@sha256:" + strings.Repeat("a", 64)},
		Substrate: v1alpha3.HarnessSubstratePolicy{WorkerPoolRef: corev1.LocalObjectReference{Name: "default"}, SnapshotPolicy: v1alpha3.HarnessSnapshotPolicy{Location: "s3://snapshots/"}},
	}}
	policy := SandboxPolicy{GuestImage: "guest@sha256:" + strings.Repeat("b", 64), CPU: "1", Memory: "1Gi"}
	actor, digest, snapshot, err := SandboxActorTemplate(template, "", policy)
	require.NoError(t, err)
	require.Contains(t, string(snapshot), policy.GuestImage)
	require.Equal(t, []string{"/run/kagent/guest/usr/local/bin/kagent-sandbox-guest"}, actor.Containers[0].Command)
	require.Equal(t, policy.GuestImage, actor.Volumes[1].Image.Reference)
	require.Equal(t, "1Gi", actor.Resources.Limits[1].Quantity)
	_, same, _, err := SandboxActorTemplate(template, atev1alpha1.SandboxClassGvisor, policy)
	require.NoError(t, err)
	require.Equal(t, digest, same)
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
	template.Spec.Env = []v1alpha3.HarnessEnvVar{{Name: "TOKEN", CredentialRef: &corev1.SecretKeySelector{}}}
	actor, _, _, err = SandboxActorTemplate(template, "", policy)
	require.ErrorContains(t, err, "credential")
	require.Nil(t, actor)
	template.Spec.Env = []v1alpha3.HarnessEnvVar{{Name: "SSL_CERT_FILE", Value: new("/untrusted")}}
	actor, _, _, err = SandboxActorTemplate(template, "", policy)
	require.ErrorContains(t, err, "reserved")
	require.Nil(t, actor)
}
