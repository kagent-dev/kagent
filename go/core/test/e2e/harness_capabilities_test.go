package e2e_test

import (
	"context"
	"encoding/json"
	"net/http/httputil"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	a2atype "github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/kagent-dev/mockllm"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/kagent-dev/kagent/go/api/v1alpha3"
)

// One harness is enough: the central compiler and the Substrate adapter apply
// capabilities for every runtime variant.
func TestHarnessCapabilities(t *testing.T) {
	t.Parallel()
	target := interactionTarget(t)
	kube := interactionKubeClient(t)
	base := &v1alpha3.Harness{}
	require.NoError(t, kube.Get(t.Context(), ctrlclient.ObjectKey{Namespace: "kagent", Name: "kagent"}, base))
	createHarness := func(t *testing.T, add ...corev1.Capability) *v1alpha3.Harness {
		t.Helper()
		harness := &v1alpha3.Harness{
			ObjectMeta: metav1.ObjectMeta{GenerateName: "capabilities-", Namespace: "kagent"},
			Spec:       *base.Spec.DeepCopy(),
		}
		harness.Spec.SecurityContext = &v1alpha3.HarnessSecurityContext{Capabilities: &corev1.Capabilities{Add: add}}
		require.NoError(t, kube.Create(t.Context(), harness))
		t.Cleanup(func() {
			if err := kube.Delete(context.Background(), harness); err != nil && !apierrors.IsNotFound(err) {
				t.Errorf("delete Harness: %v", err)
			}
		})
		return harness
	}

	t.Run("Given a Harness that adds SETUID and SETGID, when its agent reads its own capabilities in a Session, assert that the bounding set holds both", func(t *testing.T) {
		t.Parallel()
		harness := createHarness(t, "SETUID", "SETGID")
		recorder := startCapabilitiesMock(t)
		model := createInteractionModel(t, kube, reachableModelURL(t, recorder.URL), nil)
		template := &v1alpha3.AgentTemplate{
			ObjectMeta: metav1.ObjectMeta{GenerateName: "capabilities-", Namespace: "kagent"},
			Spec: v1alpha3.AgentTemplateSpec{
				ModelConfig:  &v1alpha3.ModelConfigReference{Name: model.Name},
				Description:  "Harness capabilities E2E fixture",
				SystemPrompt: "Report your capabilities when asked.",
			},
		}
		createAndWaitInteractionTemplateForHarness(t, kube, template, harness.Name)
		fixture := newInteractionFixtureForHarnessTemplate(t, target, harness.Name, template.Name)

		_, _, task := fixture.send(t, "Report your capabilities.")

		require.Equal(t, a2atype.TaskStateCompleted, task.Status.State, taskText(task))
		bounding := boundingSetReportedToModel(t, recorder)
		// Linux numbers SETGID 6 and SETUID 7.
		require.NotZero(t, bounding&(1<<6), "bounding set %#x lacks SETGID", bounding)
		require.NotZero(t, bounding&(1<<7), "bounding set %#x lacks SETUID", bounding)
	})

	t.Run("Given a Harness that adds a capability Substrate does not allow, when an Agent selects it, assert that the Agent is not Compatible and its status names the capability", func(t *testing.T) {
		t.Parallel()
		harness := createHarness(t, "NET_ADMIN")
		model := createInteractionModel(t, kube, startInteractionMock(t), nil)
		agent := &v1alpha3.Agent{
			ObjectMeta: metav1.ObjectMeta{GenerateName: "capabilities-", Namespace: "kagent"},
			Spec: v1alpha3.AgentSpec{
				HarnessRef: corev1.LocalObjectReference{Name: harness.Name},
				Template: v1alpha3.AgentTemplateSource{Inline: &v1alpha3.AgentTemplateSpec{
					ModelConfig: &v1alpha3.ModelConfigReference{Name: model.Name}, SystemPrompt: "Reply briefly.",
				}},
			},
		}

		require.NoError(t, kube.Create(t.Context(), agent))
		t.Cleanup(func() {
			if err := kube.Delete(context.Background(), agent); err != nil && !apierrors.IsNotFound(err) {
				t.Errorf("delete Agent: %v", err)
			}
		})

		var compatible *metav1.Condition
		err := wait.PollUntilContextTimeout(t.Context(), time.Second, time.Minute, true, func(ctx context.Context) (bool, error) {
			if err := kube.Get(ctx, ctrlclient.ObjectKeyFromObject(agent), agent); err != nil {
				return false, err
			}
			compatible = apimeta.FindStatusCondition(agent.Status.Conditions, v1alpha3.AgentConditionCompatible)
			return compatible != nil && compatible.ObservedGeneration == agent.Generation && compatible.Status == metav1.ConditionFalse, nil
		})
		require.NoError(t, err, "last conditions: %+v", agent.Status.Conditions)
		require.Equal(t, "ActorTemplateInvalid", compatible.Reason)
		require.Contains(t, compatible.Message, `capability "NET_ADMIN" cannot be added`)
		require.False(t, apimeta.IsStatusConditionTrue(agent.Status.Conditions, v1alpha3.AgentConditionReady))
	})
}

// startCapabilitiesMock scripts a model that has the agent print the bounding
// set of its own process and then finishes the turn.
func startCapabilitiesMock(t *testing.T) *modelRecorder {
	t.Helper()
	arguments, err := json.Marshal(map[string]string{"command": `echo "CAP_BND=$(awk '/^CapBnd:/{print $2}' /proc/self/status)"`})
	require.NoError(t, err)
	step := func(name string, match, message map[string]any, finish string) map[string]any {
		return map[string]any{
			"name":  name,
			"match": map[string]any{"match_type": "contains", "message": match},
			"response": map[string]any{
				"id": name, "object": "chat.completion", "created": 1677652288, "model": "gpt-4.1-mini",
				"choices": []map[string]any{{"index": 0, "message": message, "finish_reason": finish}},
			},
		}
	}
	raw, err := json.Marshal(map[string]any{"openai": []map[string]any{
		step("read", map[string]any{"role": "user", "content": "Report your capabilities."}, map[string]any{
			"role": "assistant", "content": "",
			"tool_calls": []map[string]any{{"id": "call_read_capabilities", "type": "function", "function": map[string]any{"name": "bash", "arguments": string(arguments)}}},
		}, "tool_calls"),
		step("reported", map[string]any{"role": "tool", "content": "CAP_BND="}, map[string]any{"role": "assistant", "content": "CAPABILITIES_REPORTED"}, "stop"),
	}})
	require.NoError(t, err)
	var config mockllm.Config
	require.NoError(t, json.Unmarshal(raw, &config))
	upstream, err := url.Parse(startMockLLMConfig(t, config))
	require.NoError(t, err)
	return startModelRecorder(t, httputil.NewSingleHostReverseProxy(upstream), nil)
}

// boundingSetReportedToModel returns the bounding set in the tool result the
// runtime sent back to the model.
func boundingSetReportedToModel(t *testing.T, recorder *modelRecorder) uint64 {
	t.Helper()
	for _, request := range recorder.Requests("", "") {
		last, err := lastEgressModelInput(request.Body)
		require.NoError(t, err)
		var message struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		}
		if len(last) == 0 || json.Unmarshal(last, &message) != nil || message.Role != "tool" {
			continue
		}
		_, value, found := strings.Cut(message.Content, "CAP_BND=")
		if !found {
			continue
		}
		bounding, err := strconv.ParseUint(strings.TrimSpace(strings.SplitN(value, "\n", 2)[0]), 16, 64)
		require.NoError(t, err, "tool output %q", message.Content)
		return bounding
	}
	t.Fatal("the model never received the agent's bounding set")
	return 0
}
