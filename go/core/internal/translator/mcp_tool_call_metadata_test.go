package translator_test

import (
	"slices"
	"testing"

	a2apb "github.com/a2aproject/a2a-go/v2/a2apb/v1"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	apia2a "github.com/kagent-dev/kagent/go/api/a2a"
	"github.com/kagent-dev/kagent/go/api/v1alpha3"
	"github.com/kagent-dev/kagent/go/core/internal/translator"
)

const metadataWorkerPoolName = "default"

func TestCompiledRevisionAdvertisesToolCallMetadataForDefaultGoWorkload(t *testing.T) {
	for _, scenario := range []struct {
		name     string
		workload v1alpha3.HarnessWorkload
		want     bool
	}{
		{name: "default Go workload", want: true},
		{name: "explicit image", workload: v1alpha3.HarnessWorkload{Image: new(testHarnessImages.GetKagent())}},
	} {
		t.Run("Given "+scenario.name+", when an Agent is compiled, assert that its persisted card declares only known runtime support", func(t *testing.T) {
			model := modelConfig()
			harness := &v1alpha3.Harness{
				ObjectMeta: metav1.ObjectMeta{Name: string(translator.HarnessTypeKagent), Namespace: model.Namespace},
				Spec:       v1alpha3.HarnessSpec{Kagent: &v1alpha3.KagentHarness{}, Workload: scenario.workload},
			}
			harness.Spec.Substrate.WorkerPoolRef.Name = metadataWorkerPoolName
			template := &v1alpha3.AgentTemplate{
				ObjectMeta: metav1.ObjectMeta{Name: "metadata-agent", Namespace: model.Namespace},
				Spec:       v1alpha3.AgentTemplateSpec{ModelConfig: &v1alpha3.ModelConfigReference{Name: model.Name}},
			}
			revision, err := compiler(t, model, harness).CompileAgent(t.Context(), inlineAgent(harness, template))
			require.NoError(t, err)
			declared := slices.ContainsFunc(revision.AgentCard.GetCapabilities().GetExtensions(), func(extension *a2apb.AgentExtension) bool {
				return extension.GetUri() == apia2a.MCPToolCallMetadataExtensionURI && !extension.GetRequired()
			})
			require.Equal(t, scenario.want, declared)
		})
	}
}
