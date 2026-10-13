package translator

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	configv1 "github.com/kagent-dev/kagent/go/api/gen/kagent/config/v1"
	"github.com/kagent-dev/kagent/go/api/v1alpha3"
)

func TestHarnessImage(t *testing.T) {
	const digest = "@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	images := &configv1.HarnessImagesConfig{Kagent: "registry/kagent" + digest, Claude: "registry/claude" + digest, Codex: "registry/codex" + digest}
	for _, tt := range []struct {
		name string
		spec v1alpha3.HarnessSpec
		want string
	}{
		{"kagent", v1alpha3.HarnessSpec{Kagent: &v1alpha3.KagentHarness{}}, images.GetKagent()},
		{"claude", v1alpha3.HarnessSpec{Claude: &v1alpha3.ClaudeHarness{}}, images.GetClaude()},
		{"codex", v1alpha3.HarnessSpec{Codex: &v1alpha3.CodexHarness{}}, images.GetCodex()},
	} {
		t.Run(tt.name, func(t *testing.T) {
			harness := &HarnessConfiguration{Spec: tt.spec}
			got, err := harnessImage(harness, images)
			require.NoError(t, err)
			require.Equal(t, tt.want, got)
			require.Nil(t, harness.Spec.Workload.Image)
			_, err = harnessImage(harness, nil)
			require.ErrorContains(t, err, "no default image configured for "+tt.name)
			harness.Spec.Workload.Image = new("mirror/override" + digest)
			got, err = harnessImage(harness, nil)
			require.NoError(t, err)
			require.Equal(t, *harness.Spec.Workload.Image, got)
			for _, invalid := range []string{"", "registry/image:latest", "registry/image@sha256:" + strings.Repeat("a", 63)} {
				harness.Spec.Workload.Image = new(invalid)
				_, err = harnessImage(harness, images)
				require.ErrorContains(t, err, "must be pinned by sha256 digest")
			}
		})
	}
	t.Run("BYO requires an explicit image", func(t *testing.T) {
		harness := &HarnessConfiguration{Spec: v1alpha3.HarnessSpec{BYO: &v1alpha3.BYOHarness{}}}
		_, err := harnessImage(harness, images)
		require.ErrorContains(t, err, "BYO harnesses must specify workload.image")
		harness.Spec.Workload.Image = new("registry/byo" + digest)
		got, err := harnessImage(harness, images)
		require.NoError(t, err)
		require.Equal(t, *harness.Spec.Workload.Image, got)
	})
	t.Run("only the selected default is needed", func(t *testing.T) {
		partial := &configv1.HarnessImagesConfig{Kagent: images.GetKagent(), Claude: "invalid"}
		_, err := harnessImage(&HarnessConfiguration{Spec: v1alpha3.HarnessSpec{Kagent: &v1alpha3.KagentHarness{}}}, partial)
		require.NoError(t, err)
		_, err = harnessImage(&HarnessConfiguration{Spec: v1alpha3.HarnessSpec{Claude: &v1alpha3.ClaudeHarness{}}}, partial)
		require.ErrorContains(t, err, "must be pinned by sha256 digest")
	})
}
