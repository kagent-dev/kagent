package install

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/kagent-dev/kagent/go/api/v1alpha3"
)

func TestCommandRunner(t *testing.T) {
	runner := CommandRunner{}
	out, err := runner.Run(t.Context(), Command{Program: "sh", Args: []string{"-c", "cat; echo warning >&2"}, Stdin: "kind: Deployment\n"})
	require.NoError(t, err)
	require.Equal(t, "kind: Deployment\n", out, "successful render output must exclude stderr")
	_, err = runner.Run(t.Context(), Command{Program: "sh", Args: []string{"-c", "echo refused >&2; exit 1"}})
	require.ErrorContains(t, err, "refused")
}

func TestProviderAPIKeys(t *testing.T) {
	for _, tc := range []struct {
		provider v1alpha3.ModelProvider
		want     []string
	}{
		{v1alpha3.ModelProviderOpenAI, []string{"OPENAI_API_KEY"}},
		{v1alpha3.ModelProviderAnthropic, []string{"ANTHROPIC_API_KEY"}},
		{v1alpha3.ModelProviderAzureOpenAI, []string{"AZURE_OPENAI_API_KEY"}},
		{v1alpha3.ModelProviderOllama, []string{"OLLAMA_API_KEY"}},
		{v1alpha3.ModelProviderGemini, []string{"GOOGLE_API_KEY", "GEMINI_API_KEY"}},
		{v1alpha3.ModelProviderBedrock, nil},
		{v1alpha3.ModelProvider("unknown"), nil},
	} {
		t.Run(string(tc.provider), func(t *testing.T) {
			var names []string
			for _, key := range ProviderAPIKeys(tc.provider) {
				names = append(names, key.Name())
			}
			require.Equal(t, tc.want, names)
		})
	}
}
