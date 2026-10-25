package translator_test

import (
	"testing"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	configv1 "github.com/kagent-dev/kagent/go/api/gen/kagent/config/v1"
	"github.com/kagent-dev/kagent/go/api/v1alpha3"
	"github.com/kagent-dev/kagent/go/core/internal/translator"
)

func TestCompileDefaultImageRevision(t *testing.T) {
	for _, variant := range []struct {
		name  string
		spec  v1alpha3.HarnessSpec
		image string
	}{
		{"kagent", v1alpha3.HarnessSpec{Kagent: &v1alpha3.KagentHarness{}}, testHarnessImages.GetKagent()},
		{"claude", v1alpha3.HarnessSpec{Claude: &v1alpha3.ClaudeHarness{}}, testHarnessImages.GetClaude()},
		{"codex", v1alpha3.HarnessSpec{Codex: &v1alpha3.CodexHarness{}}, testHarnessImages.GetCodex()},
	} {
		t.Run(variant.name, func(t *testing.T) {
			harness := &v1alpha3.Harness{ObjectMeta: metav1.ObjectMeta{Name: "runtime", Namespace: "test"}, Spec: *variant.spec.DeepCopy()}
			harness.Spec.Substrate.WorkerPoolRef.Name = "default"
			template := &v1alpha3.AgentTemplate{ObjectMeta: metav1.ObjectMeta{Name: "assistant", Namespace: "test"}, Spec: v1alpha3.AgentTemplateSpec{ModelConfig: &corev1.LocalObjectReference{Name: "default-model"}}}
			agent := inlineAgent(harness, template)
			model := modelConfig()
			if variant.name == "codex" {
				model.Spec.OpenAI = &v1alpha3.OpenAIConfig{APIFormat: new(v1alpha3.OpenAIAPIFormatResponses)}
				model.Spec.Headers = []v1alpha3.Header{{Name: "Authorization", Prefix: new("Bearer "), ValueFrom: v1alpha3.HeaderValueSource{SecretKeyRef: &corev1.SecretKeySelector{LocalObjectReference: corev1.LocalObjectReference{Name: "auth"}, Key: "key"}}}}
			}
			if variant.name == "claude" {
				model.Spec.Provider = v1alpha3.ModelProviderAnthropic
				model.Spec.Model = "claude-sonnet-4-5"
				model.Spec.Headers = []v1alpha3.Header{{Name: "Authorization", Prefix: new("Bearer "), ValueFrom: v1alpha3.HeaderValueSource{SecretKeyRef: &corev1.SecretKeySelector{LocalObjectReference: corev1.LocalObjectReference{Name: "auth"}, Key: "key"}}}}
			}
			secret := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "auth", Namespace: "test"}, Data: map[string][]byte{"key": []byte("secret")}}
			compile := func(images *configv1.HarnessImagesConfig) *translator.CompileResult {
				t.Helper()
				result, err := compilerWithImages(t, images, model, secret, harness).CompileAgent(t.Context(), agent)
				require.NoError(t, err)
				return result
			}
			first := compile(testHarnessImages)
			require.Equal(t, variant.image, first.Image)
			before, err := first.Digest()
			require.NoError(t, err)
			require.Nil(t, harness.Spec.Workload.Image, "compilation must not mutate source resources")
			changed := "mirror/runtime@sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
			images := &configv1.HarnessImagesConfig{Kagent: changed, Claude: changed, Codex: changed}
			second := compile(images)
			require.Equal(t, changed, second.Image)
			require.JSONEq(t, string(first.Provenance), string(second.Provenance), "release defaults are not Kubernetes inputs")
			after, err := second.Digest()
			require.NoError(t, err)
			require.NotEqual(t, before, after)
			require.Equal(t, variant.image, first.Image, "already compiled revisions stay immutable")
			harness.Spec.Workload.Image = new(variant.image)
			explicit := compile(nil)
			require.Equal(t, variant.image, explicit.Image)
			unrelated := compile(images)
			explicitID, err := explicit.Digest()
			require.NoError(t, err)
			unrelatedID, err := unrelated.Digest()
			require.NoError(t, err)
			require.Equal(t, explicitID, unrelatedID, "release defaults cannot change an explicit override")
		})
	}
}
