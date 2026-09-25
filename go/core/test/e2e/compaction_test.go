// Copyright 2026 The kagent Authors
// SPDX-License-Identifier: Apache-2.0

package e2e_test

import (
	"context"
	"embed"
	"testing"

	a2atype "github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/kagent-dev/kagent/go/api/v1alpha3"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"
)

//go:embed mocks/invoke_golang_compaction.json
var compactionMocks embed.FS

// compactionSummaryPrompt is the custom summarizer prompt of the fixture. The
// mock LLM answers a summarization request by this text rather than by the
// runtime's default prompt, whose wording is not a contract.
const compactionSummaryPrompt = "Summarize the conversation for the compaction test."

// compactionSummary is what the mock summarizer model answers. It stands in
// for the compacted turns in every later prompt.
const compactionSummary = "Compacted summary: the codeword is ALPHA and the port is 8443."

// The compaction Harness admits only templates carrying this label, so the
// suite's other kagent templates keep running uncompacted.
const (
	compactionRuntimeLabel = "kagent.dev/e2e-runtime"
	compactionRuntime      = "compaction"
)

// TestSessionContextCompaction verifies that a Harness's
// spec.kagent.compaction reaches the Go runtime and that the runtime compacts
// with it: after the configured number of turns the runtime asks the dedicated
// summarizer model for a summary, and the next turn's model request carries
// that summary in place of the compacted turns. Both models are the same mock
// LLM behind a recording proxy; a default header on each ModelConfig tells the
// two apart.
func TestSessionContextCompaction(t *testing.T) {
	t.Parallel()
	target := interactionTarget(t)
	recorder := startModelRecorder(t, startMockLLMServer(t, compactionMocks, "mocks/invoke_golang_compaction.json"), nil)
	kube := interactionKubeClient(t)
	modelURL := reachableModelURL(t, recorder.URL)
	agentModel := createInteractionModel(t, kube, modelURL, map[string]string{"X-Kagent-E2E-Model": "agent"})
	summarizerModel := createInteractionModel(t, kube, modelURL, map[string]string{"X-Kagent-E2E-Model": "summarizer"})
	harness := createCompactionHarness(t, kube, summarizerModel.Name)
	fixture := newInteractionFixtureForHarnessTemplate(t, target, harness.Name, createCompactionInteractionTemplate(t, kube, harness.Name, agentModel.Name))

	turns := []struct{ prompt, reply string }{
		{"Turn one: the codeword is ALPHA.", "Noted: the codeword is ALPHA."},
		{"Turn two: the port is 8443.", "Noted: the port is 8443."},
		{"Turn three: repeat the codeword and the port.", "The codeword is ALPHA and the port is 8443."},
	}
	for _, turn := range turns {
		_, _, task := fixture.send(t, turn.prompt)
		require.Equalf(t, a2atype.TaskStateCompleted, task.Status.State, "turn %q ended in %s: %s", turn.prompt, task.Status.State, taskText(task))
		require.Contains(t, taskText(task), turn.reply)
	}

	// The sliding window fires once the second invocation is complete, before
	// the runtime answers the turn, and covers both turns so far.
	summaries := recorder.Requests("X-Kagent-E2E-Model", "summarizer")
	require.Len(t, summaries, 1, "the summarizer model is called once after the second turn")
	require.Contains(t, string(summaries[0].Body), compactionSummaryPrompt)
	require.Contains(t, string(summaries[0].Body), turns[0].prompt)
	require.Contains(t, string(summaries[0].Body), turns[1].prompt)

	requests := recorder.Requests("X-Kagent-E2E-Model", "agent")
	require.Len(t, requests, 3, "one agent model call per turn")
	second := string(requests[1].Body)
	require.Contains(t, second, turns[0].prompt, "before the window fires the raw turn is still in the prompt")
	third := string(requests[2].Body)
	require.Contains(t, third, compactionSummary, "the summary stands in for the compacted turns")
	require.Contains(t, third, turns[2].prompt)
	for _, compacted := range []string{turns[0].prompt, turns[0].reply, turns[1].prompt, turns[1].reply} {
		require.NotContains(t, third, compacted, "compacted turn still in the prompt")
	}
}

// createCompactionHarness clones the suite's kagent Harness into one whose
// sliding window fires after two invocations and whose summaries are written
// by the given ModelConfig, so the test can tell the summarizer's requests from
// the agent's. It admits the templates labeled for it only.
func createCompactionHarness(t *testing.T, kube ctrlclient.Client, summarizerModel string) *v1alpha3.Harness {
	t.Helper()
	base := &v1alpha3.Harness{}
	if err := kube.Get(t.Context(), ctrlclient.ObjectKey{Namespace: "kagent", Name: "kagent"}, base); err != nil {
		t.Fatalf("get kagent Harness: %v", err)
	}
	harness := &v1alpha3.Harness{
		ObjectMeta: metav1.ObjectMeta{GenerateName: "compaction-", Namespace: "kagent"},
		Spec:       *base.Spec.DeepCopy(),
	}
	harness.Spec.Kagent.Compaction = &v1alpha3.KagentHarnessCompaction{
		CompactionInterval: new(2),
		Summarizer: &v1alpha3.KagentHarnessSummarizer{
			ModelConfigRef: &corev1.LocalObjectReference{Name: summarizerModel},
			PromptTemplate: compactionSummaryPrompt + "\n\n{conversation_history}",
		},
	}
	if err := kube.Create(t.Context(), harness); err != nil {
		t.Fatalf("create compaction Harness: %v", err)
	}
	t.Cleanup(func() {
		if err := kube.Delete(context.Background(), harness); err != nil && !apierrors.IsNotFound(err) {
			t.Errorf("delete compaction Harness: %v", err)
		}
	})
	return harness
}

// createCompactionInteractionTemplate creates a plain AgentTemplate admitted by
// the compaction Harness; the compaction policy lives on the Harness.
func createCompactionInteractionTemplate(t *testing.T, kube ctrlclient.Client, harnessName, agentModel string) string {
	t.Helper()
	template := &v1alpha3.AgentTemplate{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: "compaction-", Namespace: "kagent",
			Labels: map[string]string{compactionRuntimeLabel: compactionRuntime, "kagent.dev/harness": harnessName},
		},
		Spec: v1alpha3.AgentTemplateSpec{
			ModelConfig:  &corev1.LocalObjectReference{Name: agentModel},
			Description:  "Context compaction E2E fixture",
			SystemPrompt: "Reply briefly.",
		},
	}
	createAndWaitInteractionTemplateForHarness(t, kube, template, harnessName)
	return template.Name
}
