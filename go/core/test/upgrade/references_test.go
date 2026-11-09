package upgrade

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// v1ReferenceManifest uses only the reference shapes the v1.0.x release writes:
// name-only model refs, and an MCP server ref with an explicit kind.
const v1ReferenceManifest = `apiVersion: api.kagent.dev/v1alpha3
kind: Harness
metadata:
  name: %[1]s
  namespace: %[2]s
spec:
  kagent:
    compaction:
      compactionInterval: 5
      summarizer:
        modelConfigRef:
          name: %[3]s
  substrate:
    workerPoolRef:
      name: kagent-default
    snapshotPolicy:
      location: gs://ate-snapshots/kagent/
---
apiVersion: api.kagent.dev/v1alpha3
kind: Agent
metadata:
  name: %[1]s
  namespace: %[2]s
spec:
  harnessRef:
    name: %[1]s
  template:
    inline:
      systemPrompt: Reply briefly.
      modelConfig:
        name: %[3]s
      tools:
      - mcp:
          server:
            kind: RemoteMCPServer
            name: %[4]s
`

// The chart creates both for the release named kagent.
const (
	v1ReferenceModelConfig = "default-model-config"
	v1ReferenceMCPServer   = "kagent-tool-server"
)

// seedV1References applies a Harness and Agent named name in the v1.0.x reference shapes.
func seedV1References(t *testing.T, env upgradeEnv, name string) {
	t.Helper()

	kubectl(t, env, time.Minute, "get", "modelconfig", v1ReferenceModelConfig, "-n", env.namespace)
	kubectl(t, env, time.Minute, "get", "remotemcpserver", v1ReferenceMCPServer, "-n", env.namespace)
	manifest := fmt.Sprintf(v1ReferenceManifest, name, env.namespace, v1ReferenceModelConfig, v1ReferenceMCPServer)
	path := filepath.Join(t.TempDir(), "ga-references.yaml")
	require.NoError(t, os.WriteFile(path, []byte(manifest), 0o600))
	kubectl(t, env, time.Minute, "apply", "-f", path)
}

// requireV1ReferencesReconciled edits the seeded Agent's prompt, then waits for the
// serving controller to report that generation resolved and ready.
// Every condition carries the generation, so a stale Ready from an earlier revision cannot pass.
func requireV1ReferencesReconciled(t *testing.T, env upgradeEnv, name, phase string) {
	t.Helper()

	patch := fmt.Sprintf(`{"spec":{"template":{"inline":{"systemPrompt":"Reply briefly (%s)."}}}}`, phase)
	generation := int64(parseInt(t, kubectl(t, env, time.Minute,
		"patch", "agent", name, "-n", env.namespace, "--type=merge", "-p", patch,
		"-o", "jsonpath={.metadata.generation}",
	), "Agent generation"))

	// Literal condition types and reasons: both the previous and current controllers must write them.
	want := []metav1.Condition{
		{Type: "ResolvedRefs", Status: metav1.ConditionTrue, Reason: "Resolved"},
		{Type: "Ready", Status: metav1.ConditionTrue, Reason: "Ready"},
	}
	require.EventuallyWithT(t, func(c *assert.CollectT) {
		out, err := kubectlOutput(t, env, time.Minute, "get", "agent", name, "-n", env.namespace, "-o", "json")
		if !assert.NoError(c, err) {
			return
		}
		var agent struct {
			Status struct {
				ObservedGeneration int64              `json:"observedGeneration"`
				Conditions         []metav1.Condition `json:"conditions"`
			} `json:"status"`
		}
		if !assert.NoError(c, json.Unmarshal([]byte(out), &agent)) {
			return
		}
		assert.Equal(c, generation, agent.Status.ObservedGeneration, "status.observedGeneration")
		for _, expected := range want {
			condition := meta.FindStatusCondition(agent.Status.Conditions, expected.Type)
			if !assert.NotNil(c, condition, "condition %s", expected.Type) {
				continue
			}
			assert.Equal(c, generation, condition.ObservedGeneration, "%s observedGeneration", expected.Type)
			assert.Equal(c, expected.Status, condition.Status, "%s status (%s: %s)", expected.Type, condition.Reason, condition.Message)
			assert.Equal(c, expected.Reason, condition.Reason, "%s reason", expected.Type)
		}
	}, 5*time.Minute, 5*time.Second, "[%s] Agent %s/%s did not reconcile its v1.0.x reference shapes", phase, env.namespace, name)
}
