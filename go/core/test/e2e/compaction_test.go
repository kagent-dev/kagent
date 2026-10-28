// Copyright 2026 The kagent Authors
// SPDX-License-Identifier: Apache-2.0

package e2e_test

import (
	"context"
	"embed"
	"testing"

	a2atype "github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/stretchr/testify/require"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/kagent-dev/kagent/go/api/v1alpha3"
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
	recorder := startModelRecorder(t, mockLLMProxy(t, startMockLLMServer(t, compactionMocks, "mocks/invoke_golang_compaction.json")), nil)
	kube := interactionKubeClient(t)
	modelURL := reachableModelURL(t, recorder.URL)
	agentModel := createInteractionModel(t, kube, modelURL, map[string]string{"X-Kagent-E2E-Model": "agent"})
	summarizerModel := createInteractionModel(t, kube, modelURL, map[string]string{"X-Kagent-E2E-Model": "summarizer"})
	harness := createCompactionHarness(t, kube, summarizerModel.Name, &v1alpha3.KagentHarnessCompaction{CompactionInterval: new(2)})
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

// With no interval configured, these cases require the threshold and tail
// settings to reach the runtime. Recorded model requests establish which
// events the summarizer replaces and which events remain in the raw tail.
func TestSessionTokenThresholdCompaction(t *testing.T) {
	t.Parallel()
	target := interactionTarget(t)
	turns := []struct{ prompt, reply string }{
		{"Turn one: the codeword is ALPHA.", "Noted: the codeword is ALPHA."},
		{"Turn two: the port is 8443.", "Noted: the port is 8443."},
		{"Turn three: repeat the codeword and the port.", "The codeword is ALPHA and the port is 8443."},
	}
	for _, tc := range []struct {
		name                string
		tokenThreshold      int
		retentionSize       int
		turnCount           int
		firstCompactionTurn int
	}{
		{"short tail", 10, 1, 2, 2},
		{"larger tail", 10, 3, 3, 3},
		{"below threshold", 1_000_000, 1, 3, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			recorder := startModelRecorder(t, mockLLMProxy(t, startMockLLMServer(t, compactionMocks, "mocks/invoke_golang_compaction.json")), nil)
			kube := interactionKubeClient(t)
			modelURL := reachableModelURL(t, recorder.URL)
			agentModel := createInteractionModel(t, kube, modelURL, map[string]string{"X-Kagent-E2E-Model": "agent"})
			summarizerModel := createInteractionModel(t, kube, modelURL, map[string]string{"X-Kagent-E2E-Model": "summarizer"})
			// Nonstreaming responses carry the fixture's token usage. This
			// keeps the threshold independent of prompt-size estimation.
			before := agentModel.DeepCopy()
			agentModel.Spec.Stream = new(false)
			require.NoError(t, kube.Patch(t.Context(), agentModel, ctrlclient.MergeFrom(before)))
			harness := createCompactionHarness(t, kube, summarizerModel.Name, &v1alpha3.KagentHarnessCompaction{
				TokenThreshold: new(tc.tokenThreshold), EventRetentionSize: new(tc.retentionSize),
			})
			fixture := newInteractionFixtureForHarnessTemplate(t, target, harness.Name, createCompactionInteractionTemplate(t, kube, harness.Name, agentModel.Name))

			for i, turn := range turns[:tc.turnCount] {
				_, _, task := fixture.send(t, turn.prompt)
				require.Equalf(t, a2atype.TaskStateCompleted, task.Status.State, "turn %q ended in %s: %s", turn.prompt, task.Status.State, taskText(task))
				require.Contains(t, taskText(task), turn.reply)
				requests := recorder.Requests("X-Kagent-E2E-Model", "agent")
				require.Len(t, requests, i+1, "one agent model call per completed turn")
				require.Contains(t, string(requests[i].Body), turn.prompt, "the current question remains raw")
				if tc.firstCompactionTurn == 0 || i+1 < tc.firstCompactionTurn {
					require.Empty(t, recorder.Requests("X-Kagent-E2E-Model", "summarizer"), "no compaction before the threshold leaves a compactable prefix")
				}
			}

			requests := recorder.Requests("X-Kagent-E2E-Model", "agent")
			if tc.firstCompactionTurn == 0 {
				for _, request := range requests[1:] {
					require.Contains(t, string(request.Body), turns[0].prompt)
					require.Contains(t, string(request.Body), turns[0].reply)
					require.NotContains(t, string(request.Body), compactionSummary)
				}
				require.Contains(t, string(requests[2].Body), turns[1].prompt)
				require.Contains(t, string(requests[2].Body), turns[1].reply)
				return
			}

			summaries := recorder.Requests("X-Kagent-E2E-Model", "summarizer")
			require.Len(t, summaries, 1, "one summary replaces the old prefix")
			summary := string(summaries[0].Body)
			require.Contains(t, summary, compactionSummaryPrompt)
			require.Contains(t, summary, turns[0].prompt)
			require.NotContains(t, summary, turns[0].reply, "the retained answer stays outside the summary")
			require.NotContains(t, summary, turns[1].prompt, "the retained tail stays outside the summary")
			compacted := string(requests[1].Body)
			if tc.retentionSize == 3 {
				require.Contains(t, compacted, turns[0].prompt, "a larger tail delays compaction")
				require.Contains(t, compacted, turns[0].reply)
				compacted = string(requests[2].Body)
				require.Contains(t, compacted, turns[1].prompt)
				require.Contains(t, compacted, turns[1].reply)
				require.NotContains(t, summary, turns[1].reply)
				require.NotContains(t, summary, turns[2].prompt)
			}
			require.Contains(t, compacted, compactionSummary)
			require.NotContains(t, compacted, turns[0].prompt, "the summary replaces the old question")
			require.Contains(t, compacted, turns[0].reply, "the retained prior answer remains raw")
		})
	}
}

// createCompactionHarness clones the suite's kagent Harness with the requested
// policy and a dedicated summarizer model. Model headers distinguish summary
// requests from agent requests. It admits the templates labeled for it only.
func createCompactionHarness(t *testing.T, kube ctrlclient.Client, summarizerModel string, policy *v1alpha3.KagentHarnessCompaction) *v1alpha3.Harness {
	t.Helper()
	base := &v1alpha3.Harness{}
	if err := kube.Get(t.Context(), ctrlclient.ObjectKey{Namespace: "kagent", Name: "kagent"}, base); err != nil {
		t.Fatalf("get kagent Harness: %v", err)
	}
	harness := &v1alpha3.Harness{
		ObjectMeta: metav1.ObjectMeta{GenerateName: "compaction-", Namespace: "kagent"},
		Spec:       *base.Spec.DeepCopy(),
	}
	harness.Spec.Kagent.Compaction = policy
	harness.Spec.Kagent.Compaction.Summarizer = &v1alpha3.KagentHarnessSummarizer{
		ModelConfigRef: &v1alpha3.ObjectReference{Name: summarizerModel},
		PromptTemplate: compactionSummaryPrompt + "\n\n{conversation_history}",
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
			ModelConfig:  &v1alpha3.ObjectReference{Name: agentModel},
			Description:  "Context compaction E2E fixture",
			SystemPrompt: "Reply briefly.",
		},
	}
	createAndWaitInteractionTemplateForHarness(t, kube, template, harnessName)
	return template.Name
}
