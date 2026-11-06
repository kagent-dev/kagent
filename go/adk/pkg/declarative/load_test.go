package declarative

import (
	"crypto/sha256"
	"encoding/hex"
	"testing"

	"github.com/stretchr/testify/require"

	configv1 "github.com/kagent-dev/kagent/go/api/gen/kagent/config/v1"
)

func workflowConfig(document string) *configv1.ADKWorkflowConfig {
	sum := sha256.Sum256([]byte(document))
	return &configv1.ADKWorkflowConfig{Document: []byte(document), Sha256: hex.EncodeToString(sum[:])}
}

func TestParseConfig(t *testing.T) {
	const document = `{"apiVersion":"adk.declarative/v0.2","kind":"AgentSystem","agents":[{"name":"solo","agent_class":"LlmAgent","model":"stub","instruction":"hi"}]}`

	spec, err := NewRegistry().ParseConfig(workflowConfig(document))
	require.NoError(t, err)
	require.Equal(t, "solo", spec.Agents[0].Name)

	t.Run("nil config", func(t *testing.T) {
		_, err := NewRegistry().ParseConfig(nil)
		require.ErrorContains(t, err, "workflow config is required")
	})
	t.Run("digest mismatch", func(t *testing.T) {
		cfg := workflowConfig(document)
		cfg.Document = []byte(document + " ")
		_, err := NewRegistry().ParseConfig(cfg)
		require.ErrorContains(t, err, "does not match the recorded digest")
	})
	t.Run("document is validated against the registry", func(t *testing.T) {
		unregistered := `{"apiVersion":"adk.declarative/v0.2","kind":"AgentSystem","agents":[{"name":"solo","agent_class":"LlmAgent","model":"nope","instruction":"hi"}]}`
		_, err := NewRegistry().ParseConfig(workflowConfig(unregistered))
		require.True(t, IsValidation(err))
		require.ErrorContains(t, err, `agents[0].model: model "nope" is not registered`)
	})
}
