package main

import (
	"crypto/sha256"
	"encoding/hex"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/kagent-dev/kagent/go/adk/pkg/declarative"
	configv1 "github.com/kagent-dev/kagent/go/api/gen/kagent/config/v1"
)

// TestAgentsDocumentBuilds guards the example document against drift from the
// registry it is served with.
func TestAgentsDocumentBuilds(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "test-key") // building an OpenAI model needs a key; no request is made
	reg, err := newRegistry()
	require.NoError(t, err)
	spec, err := reg.Parse(agentsDocument)
	require.NoError(t, err)
	system, err := declarative.Build(t.Context(), spec, reg)
	require.NoError(t, err)
	require.Equal(t, "support_triage", system.Root().Name())
}

func TestLoadDocumentPrefersTheDeliveredWorkflow(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "test-key")
	reg, err := newRegistry()
	require.NoError(t, err)

	embedded, err := loadDocument(reg, nil)
	require.NoError(t, err)
	require.Equal(t, "support_triage", embedded.Agents[0].Name)

	const document = `{"apiVersion":"adk.declarative/v0.2","kind":"AgentSystem","agents":[{"name":"delivered","agent_class":"LlmAgent","model":"stub","instruction":"hi"}]}`
	sum := sha256.Sum256([]byte(document))
	delivered, err := loadDocument(reg, &configv1.ADKWorkflowConfig{Document: []byte(document), Sha256: hex.EncodeToString(sum[:])})
	require.NoError(t, err)
	require.Equal(t, "delivered", delivered.Agents[0].Name)
}
