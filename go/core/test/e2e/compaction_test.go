// Copyright 2026 The kagent Authors
// SPDX-License-Identifier: Apache-2.0

package e2e_test

import (
	"embed"
	"testing"

	a2atype "github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/kagent-dev/kagent/go/api/v1alpha3"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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

// TestAgentInstanceContextCompaction verifies that spec.context.compaction
// reaches the Go runtime and that the runtime compacts with it: after the
// configured number of turns the runtime asks the dedicated summarizer model
// for a summary, and the next turn's model request carries that summary in
// place of the compacted turns. Both models are the same mock LLM behind a
// recording proxy; a default header on each ModelConfig tells the two apart.
func TestAgentInstanceContextCompaction(t *testing.T) {
	t.Parallel()
	target := interactionTarget(t)
	recorder := startModelRecorder(t, startMockLLMServer(t, compactionMocks, "mocks/invoke_golang_compaction.json"), nil)
	fixture := newInteractionFixtureForTemplate(t, target, createCompactionInteractionTemplate(t, reachableModelURL(t, recorder.URL)))

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

// createCompactionInteractionTemplate creates an AgentTemplate whose sliding
// window fires after two invocations and whose summaries are written by a
// dedicated ModelConfig, so the test can tell the summarizer's requests from
// the agent's.
func createCompactionInteractionTemplate(t *testing.T, modelURL string) string {
	t.Helper()
	kube := interactionKubeClient(t)
	agentModel := createInteractionModel(t, kube, modelURL, map[string]string{"X-Kagent-E2E-Model": "agent"})
	summarizerModel := createInteractionModel(t, kube, modelURL, map[string]string{"X-Kagent-E2E-Model": "summarizer"})
	template := &v1alpha3.AgentTemplate{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: "compaction-", Namespace: "kagent",
			Labels: map[string]string{"kagent.dev/e2e-runtime": "kagent", "kagent.dev/harness": "kagent"},
		},
		Spec: v1alpha3.AgentTemplateSpec{
			ModelConfig:  &corev1.LocalObjectReference{Name: agentModel.Name},
			Description:  "Context compaction E2E fixture",
			SystemPrompt: "Reply briefly.",
			Context: &v1alpha3.AgentTemplateContextSpec{Compaction: &v1alpha3.AgentTemplateCompactionSpec{
				CompactionInterval: new(2),
				Summarizer: &v1alpha3.AgentTemplateSummarizerSpec{
					ModelConfig:    &corev1.LocalObjectReference{Name: summarizerModel.Name},
					PromptTemplate: compactionSummaryPrompt + "\n\n{conversation_history}",
				},
			}},
		},
	}
	createAndWaitInteractionTemplate(t, kube, template)
	return template.Name
}
