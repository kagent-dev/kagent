// Copyright 2026 The kagent Authors
// SPDX-License-Identifier: Apache-2.0

package e2e_test

import (
	"bytes"
	"context"
	"encoding/json"
	"maps"
	"net/http"
	"testing"

	a2atype "github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/kagent-dev/kagent/go/api/v1alpha3"
	"github.com/kagent-dev/kagent/go/pkg/telemetry/conv"
	"github.com/kagent-dev/mockllm"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/baggage"
	"google.golang.org/grpc/metadata"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"
)

// TestRuntimeGenAIBaggage verifies that the Go runtime's model and MCP calls
// continue the caller's trace, and carry the request's GenAI identity as
// baggage only when the runtime's OTEL_PROPAGATORS includes baggage. The MCP
// session's own requests carry neither.
func TestRuntimeGenAIBaggage(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name        string
		propagators string
		wantBaggage bool
	}{
		{name: "default propagators"},
		{name: "baggage propagator", propagators: "tracecontext,baggage", wantBaggage: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			target := interactionTarget(t)
			kube := interactionKubeClient(t)

			mcpURL, mcpServer := startMCPMock(t)
			cfg, err := mockllm.LoadConfigFromFile("mocks/invoke_mcp_agent.json", interactionMocks)
			require.NoError(t, err)
			prompt := "Add 3 and 5 using the configured MCP server."
			require.NoError(t, json.Unmarshal([]byte(`{"role":"user","content":"`+prompt+`"}`), &cfg.OpenAI[0].Match.Message))
			recorder := startModelRecorder(t, startMockLLMConfig(t, cfg), nil)
			model := createInteractionModel(t, kube, reachableModelURL(t, recorder.URL), map[string]string{"X-Kagent-E2E-Model": "agent"})

			harness := createPropagatorsHarness(t, kube, tc.propagators)
			template := createGenAIBaggageTemplate(t, kube, harness.Name, model.Name, mcpURL)
			fixture := newInteractionFixtureForHarnessTemplate(t, target, harness.Name, template)
			// The caller's trace continues through every hop whether or not the
			// installation exports traces.
			const callerTraceID = "4bf92f3577b34da6a3ce929d0e0e4736"
			fixture.ctx = metadata.AppendToOutgoingContext(fixture.ctx,
				"traceparent", "00-"+callerTraceID+"-00f067aa0ba902b7-01",
				"baggage", "gen_ai.conversation.id=spoofed",
			)

			_, _, task := fixture.send(t, prompt)
			require.Equalf(t, a2atype.TaskStateCompleted, task.Status.State, "task ended in %s: %s", task.Status.State, taskText(task))
			require.Contains(t, taskText(task), "result is 8")

			identity := map[string]string{
				"gen_ai.agent.name":      template,
				"gen_ai.conversation.id": fixture.sessionID,
				"a2a.task.id":            string(task.ID),
			}
			assertBaggage := func(t *testing.T, header string, want map[string]string) {
				t.Helper()
				if !tc.wantBaggage {
					require.Empty(t, header, "baggage sent without the baggage propagator")
					return
				}
				requireBaggageMembers(t, header, want)
			}

			modelCalls := recorder.Requests("X-Kagent-E2E-Model", "agent")
			require.NotEmpty(t, modelCalls, "the mock model received no request")
			for _, request := range modelCalls {
				require.Contains(t, request.Header.Get("traceparent"), callerTraceID, "model request does not continue the caller's trace")
				assertBaggage(t, request.Header.Get("baggage"), identity)
			}

			var toolCall bool
			for _, request := range mcpServer.Requests() {
				if request.Method != http.MethodPost {
					require.Emptyf(t, request.Headers.Get("traceparent"), "MCP session %s carried traceparent", request.Method)
					require.Emptyf(t, request.Headers.Get("baggage"), "MCP session %s carried baggage", request.Method)
					continue
				}
				if !bytes.Contains(request.Body, []byte(`"method":"tools/call"`)) {
					continue
				}
				toolCall = true
				require.Contains(t, request.Headers.Get("traceparent"), callerTraceID, "MCP tool call does not continue the caller's trace")
				toolIdentity := maps.Clone(identity)
				toolIdentity[string(conv.GenAIToolNameKey)] = "add_numbers"
				toolIdentity[string(conv.GenAIToolCallIDKey)] = "call_1"
				assertBaggage(t, request.Headers.Get("baggage"), toolIdentity)
			}
			require.True(t, toolCall, "mock MCP server did not receive a tool call")
		})
	}
}

// requireBaggageMembers asserts that a baggage header holds the expected
// members. Other members, such as a caller's baggage the installation
// propagates, may be present too.
func requireBaggageMembers(t *testing.T, header string, want map[string]string) {
	t.Helper()
	bag, err := baggage.Parse(header)
	require.NoErrorf(t, err, "parse baggage %q", header)
	for key, value := range want {
		require.Equalf(t, value, bag.Member(key).Value(), "baggage member %s in %q", key, header)
	}
}

// createPropagatorsHarness clones the suite's kagent Harness, setting the
// runtime's OTEL_PROPAGATORS when propagators is not empty.
func createPropagatorsHarness(t *testing.T, kube ctrlclient.Client, propagators string) *v1alpha3.Harness {
	t.Helper()
	base := &v1alpha3.Harness{}
	if err := kube.Get(t.Context(), ctrlclient.ObjectKey{Namespace: "kagent", Name: "kagent"}, base); err != nil {
		t.Fatalf("get kagent Harness: %v", err)
	}
	harness := &v1alpha3.Harness{
		ObjectMeta: metav1.ObjectMeta{GenerateName: "propagators-", Namespace: "kagent"},
		Spec:       *base.Spec.DeepCopy(),
	}
	if propagators != "" {
		harness.Spec.Env = append(harness.Spec.Env, v1alpha3.RuntimeEnvVar{Name: "OTEL_PROPAGATORS", Value: propagators})
	}
	if err := kube.Create(t.Context(), harness); err != nil {
		t.Fatalf("create propagators Harness: %v", err)
	}
	t.Cleanup(func() {
		if err := kube.Delete(context.Background(), harness); err != nil && !apierrors.IsNotFound(err) {
			t.Errorf("delete propagators Harness: %v", err)
		}
	})
	return harness
}

func createGenAIBaggageTemplate(t *testing.T, kube ctrlclient.Client, harnessName, modelName, mcpURL string) string {
	t.Helper()
	server := &v1alpha3.RemoteMCPServer{
		ObjectMeta: metav1.ObjectMeta{GenerateName: "genai-baggage-mcp-", Namespace: "kagent"},
		Spec: v1alpha3.RemoteMCPServerSpec{
			Description: "GenAI baggage E2E fixture",
			Protocol:    v1alpha3.RemoteMCPServerProtocolStreamableHttp,
			URL:         mcpURL,
		},
	}
	if err := kube.Create(t.Context(), server); err != nil {
		t.Fatalf("create GenAI baggage RemoteMCPServer: %v", err)
	}
	t.Cleanup(func() {
		if err := kube.Delete(context.Background(), server); err != nil && !apierrors.IsNotFound(err) {
			t.Errorf("delete GenAI baggage RemoteMCPServer: %v", err)
		}
	})
	template := &v1alpha3.AgentTemplate{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: "genai-baggage-", Namespace: "kagent",
			Labels: map[string]string{"kagent.dev/harness": harnessName},
		},
		Spec: v1alpha3.AgentTemplateSpec{
			ModelConfig:  &corev1.LocalObjectReference{Name: modelName},
			Description:  "GenAI baggage E2E fixture",
			SystemPrompt: "Use the configured MCP tool. Do not calculate the answer yourself.",
			Tools: []v1alpha3.ToolBinding{{MCP: &v1alpha3.MCPToolBinding{
				Server: corev1.TypedLocalObjectReference{Kind: "RemoteMCPServer", Name: server.Name},
			}}},
		},
	}
	createAndWaitInteractionTemplateForHarness(t, kube, template, harnessName)
	return template.Name
}
