package commands

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	apiv1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1"
	"github.com/kagent-dev/kagent/go/api/structuredobject"
	apiv1alpha3 "github.com/kagent-dev/kagent/go/api/v1alpha3"
	clioutput "github.com/kagent-dev/kagent/go/core/cli/internal/output"
)

func TestListModelConfigs(t *testing.T) {
	response := func() *apiv1.ListModelConfigsResponse {
		return &apiv1.ListModelConfigsResponse{ModelConfigs: []*apiv1.ModelConfig{
			modelConfig(t, "team-a", "fast", apiv1alpha3.ModelProviderOpenAI, "gpt-4.1-mini"),
			modelConfig(t, "team-b", "deep", apiv1alpha3.ModelProviderAnthropic, "claude-opus-4"),
		}}
	}

	t.Run("Given ModelConfigs in two namespaces, when listed in team-a, assert that only team-a's appear with provider and model", func(t *testing.T) {
		var output bytes.Buffer

		require.NoError(t, writeModelConfigs(&output, clioutput.FormatTable, "team-a", response()))
		assert.Regexp(t, `fast\s+\S\s+OpenAI\s+\S\s+gpt-4.1-mini`, output.String())
		assert.NotContains(t, output.String(), "deep")
	})

	t.Run("Given ModelConfigs in two namespaces, when listed in team-b as JSON, assert that only team-b's appear", func(t *testing.T) {
		var output bytes.Buffer

		require.NoError(t, writeModelConfigs(&output, clioutput.FormatJSON, "team-b", response()))
		assert.Contains(t, output.String(), `"name":"deep"`)
		assert.NotContains(t, output.String(), `"name":"fast"`)
	})
}

func modelConfig(t *testing.T, namespace, name string, provider apiv1alpha3.ModelProvider, model string) *apiv1.ModelConfig {
	t.Helper()
	config := &apiv1alpha3.ModelConfig{
		ObjectMeta: metav1.ObjectMeta{Namespace: namespace, Name: name},
		Spec:       apiv1alpha3.ModelConfigSpec{Provider: provider, Model: model},
	}
	resource, err := structuredobject.FromGo(config, apiv1alpha3.GroupVersion.String(), apiv1alpha3.ModelConfigGroupKind.Kind, 0)
	require.NoError(t, err)
	return &apiv1.ModelConfig{Ref: &apiv1.ResourceReference{Namespace: namespace, Name: name}, Resource: resource}
}
