package translator

import (
	"strings"
	"testing"

	"github.com/kagent-dev/kagent/go/api/v1alpha3"
	"github.com/stretchr/testify/require"
)

func TestHarnessImage(t *testing.T) {
	const digest = "@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	images := BuiltinImages{Kagent: "registry/kagent" + digest, Claude: "registry/claude" + digest, Codex: "registry/codex" + digest}
	for _, tt := range []struct {
		name string
		spec v1alpha3.HarnessSpec
		want string
	}{
		{"kagent", v1alpha3.HarnessSpec{Kagent: &v1alpha3.KagentHarness{}}, images.Kagent},
		{"claude", v1alpha3.HarnessSpec{Claude: &v1alpha3.ClaudeHarness{}}, images.Claude},
		{"codex", v1alpha3.HarnessSpec{Codex: &v1alpha3.CodexHarness{}}, images.Codex},
	} {
		t.Run(tt.name, func(t *testing.T) {
			harness := &HarnessConfiguration{Spec: tt.spec}
			got, err := images.image(harness)
			require.NoError(t, err)
			require.Equal(t, tt.want, got)
			require.Nil(t, harness.Spec.Workload.Image)
			_, err = (BuiltinImages{}).image(harness)
			require.ErrorContains(t, err, "no default image configured for "+tt.name)
			harness.Spec.Workload.Image = new("mirror/override" + digest)
			got, err = (BuiltinImages{}).image(harness)
			require.NoError(t, err)
			require.Equal(t, *harness.Spec.Workload.Image, got)
			for _, invalid := range []string{"", "registry/image:latest", "registry/image@sha256:" + strings.Repeat("a", 63)} {
				harness.Spec.Workload.Image = new(invalid)
				_, err = images.image(harness)
				require.ErrorContains(t, err, "must be pinned by sha256 digest")
			}
		})
	}
	t.Run("BYO requires an explicit image", func(t *testing.T) {
		harness := &HarnessConfiguration{Spec: v1alpha3.HarnessSpec{BYO: &v1alpha3.BYOHarness{}}}
		_, err := images.image(harness)
		require.ErrorContains(t, err, "BYO harnesses must specify workload.image")
		harness.Spec.Workload.Image = new("registry/byo" + digest)
		got, err := images.image(harness)
		require.NoError(t, err)
		require.Equal(t, *harness.Spec.Workload.Image, got)
	})
	t.Run("only the selected default is needed", func(t *testing.T) {
		partial := BuiltinImages{Kagent: images.Kagent, Claude: "invalid"}
		_, err := partial.image(&HarnessConfiguration{Spec: v1alpha3.HarnessSpec{Kagent: &v1alpha3.KagentHarness{}}})
		require.NoError(t, err)
		_, err = partial.image(&HarnessConfiguration{Spec: v1alpha3.HarnessSpec{Claude: &v1alpha3.ClaudeHarness{}}})
		require.ErrorContains(t, err, "must be pinned by sha256 digest")
	})
}
