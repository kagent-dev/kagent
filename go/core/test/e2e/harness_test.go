package e2e_test

import (
	"strings"
	"testing"

	"github.com/kagent-dev/kagent/go/api/v1alpha3"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"
)

type testHarness struct {
	name         string
	runtimeLabel string
}

// Every shared AgentTemplate test runs on every harness type. BYO uses the Go
// ADK image so managed configuration is exercised; the opaque BYO contract has
// its own test. Keep exclusions in the affected test, with an explicit t.Skip
// reason. Never infer exclusions from installed fixtures or readiness failures.
func forEachHarness(t *testing.T, run func(*testing.T, testHarness)) {
	t.Helper()
	for _, harness := range []testHarness{
		{name: "kagent", runtimeLabel: "kagent"},
		{name: codexE2EHarness, runtimeLabel: "codex"},
		{name: claudeE2EHarness, runtimeLabel: "claude"},
		{name: "byo-adk-e2e", runtimeLabel: "byo-adk"},
	} {
		t.Run(harness.runtimeLabel, func(t *testing.T) {
			run(t, harness)
		})
	}
}

func (h testHarness) labels() map[string]string {
	return map[string]string{"kagent.dev/e2e-runtime": h.runtimeLabel, "kagent.dev/harness": h.name}
}

func (h testHarness) createModel(t *testing.T, kube ctrlclient.Client, modelURL string, headers map[string]string) *v1alpha3.ModelConfig {
	t.Helper()
	switch h.name {
	case "kagent", "byo-adk-e2e":
		return createInteractionModel(t, kube, modelURL, headers)
	case codexE2EHarness, claudeE2EHarness:
		if len(headers) != 0 {
			t.Fatalf("%s does not support ModelConfig defaultHeaders", h.name)
		}
		if h.name == codexE2EHarness {
			return createCodexMockModel(t, kube, modelURL)
		}
		// Anthropic clients append /v1 themselves; shared mock URLs include it
		// for OpenAI clients.
		return createClaudeMockModel(t, kube, strings.TrimSuffix(modelURL, "/v1"))
	default:
		t.Fatalf("no model fixture for harness %q", h.name)
		return nil
	}
}
