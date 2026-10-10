package translator_test

import (
	"fmt"
	"testing"

	"github.com/kagent-dev/kagent/go/api/v1alpha3"
	"github.com/kagent-dev/kagent/go/core/internal/translator"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestCompileDefaultImageRevision(t *testing.T) {
	for _, variant := range []struct {
		name  string
		spec  v1alpha3.HarnessSpec
		image string
	}{
		{"kagent", v1alpha3.HarnessSpec{Kagent: &v1alpha3.KagentHarness{}}, testBuiltinImages.Kagent},
		{"claude", v1alpha3.HarnessSpec{Claude: &v1alpha3.ClaudeHarness{}}, testBuiltinImages.Claude},
		{"codex", v1alpha3.HarnessSpec{Codex: &v1alpha3.CodexHarness{}}, testBuiltinImages.Codex},
	} {
		for _, inline := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/inline=%t", variant.name, inline), func(t *testing.T) {
				harness := &v1alpha3.Harness{ObjectMeta: metav1.ObjectMeta{Name: "runtime", Namespace: "test"}, Spec: *variant.spec.DeepCopy()}
				harness.Spec.Substrate.WorkerPoolRef.Name = "default"
				template := &v1alpha3.AgentTemplate{ObjectMeta: metav1.ObjectMeta{Name: "assistant", Namespace: "test"}, Spec: v1alpha3.AgentTemplateSpec{ModelConfig: &corev1.LocalObjectReference{Name: "default-model"}}}
				agent := inlineAgent(harness, template)
				if !inline {
					agent.Spec.Harness = nil
					agent.Spec.HarnessRef = &corev1.LocalObjectReference{Name: harness.Name}
				}
				model := modelConfig()
				if variant.name == "codex" {
					model.Spec.OpenAI = &v1alpha3.OpenAIConfig{APIFormat: new(v1alpha3.OpenAIAPIFormatResponses)}
					model.Spec.APIKeySecret = "auth"
					model.Spec.APIKeySecretKey = "key"
				}
				if variant.name == "claude" {
					model.Spec.Provider = v1alpha3.ModelProviderAnthropic
					model.Spec.Model = "claude-sonnet-4-5"
					model.Spec.APIKeySecret = "auth"
					model.Spec.APIKeySecretKey = "key"
				}
				secret := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "auth", Namespace: "test"}, Data: map[string][]byte{"key": []byte("secret")}}
				compile := func(images translator.BuiltinImages) *translator.CompileResult {
					t.Helper()
					result, err := compilerWithImages(t, images, model, secret, harness).CompileAgent(t.Context(), agent)
					require.NoError(t, err)
					return result
				}
				first := compile(testBuiltinImages)
				require.Equal(t, variant.image, first.Image)
				before, err := first.Digest()
				require.NoError(t, err)
				require.Nil(t, harness.Spec.Workload.Image, "compilation must not mutate source resources")
				if inline {
					require.Nil(t, agent.Spec.Harness.Workload.Image)
				}
				changed := "mirror/runtime@sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
				images := translator.BuiltinImages{Kagent: changed, Claude: changed, Codex: changed}
				second := compile(images)
				require.Equal(t, changed, second.Image)
				after, err := second.Digest()
				require.NoError(t, err)
				require.NotEqual(t, before, after)
				require.Equal(t, variant.image, first.Image, "already compiled revisions stay immutable")
				harness.Spec.Workload.Image = new(variant.image)
				if inline {
					agent.Spec.Harness = harness.Spec.DeepCopy()
				}
				explicit := compile(translator.BuiltinImages{})
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
}
