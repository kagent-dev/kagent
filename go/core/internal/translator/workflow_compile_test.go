package translator_test

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	runtimeconfig "github.com/kagent-dev/kagent/go/adk/pkg/config"
	configv1 "github.com/kagent-dev/kagent/go/api/gen/kagent/config/v1"
	"github.com/kagent-dev/kagent/go/api/v1alpha3"
	"github.com/kagent-dev/kagent/go/core/internal/translator"
)

const workflowYAML = `
apiVersion: adk.declarative/v0.2
kind: AgentSystem
agents:
  - name: support
    agent_class: Workflow
    entry: triage
    nodes:
      - { id: triage, type: function, function: triage }
`

func workflowConfigMap(document string) *corev1.ConfigMap {
	return &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: "workflows", Namespace: "test", UID: "workflows-uid"},
		Data:       map[string]string{"agents.yaml": document},
	}
}

func byoHarness() *v1alpha3.Harness {
	return &v1alpha3.Harness{ObjectMeta: metav1.ObjectMeta{Name: "byo", Namespace: "test"}, Spec: v1alpha3.HarnessSpec{
		BYO: &v1alpha3.BYOHarness{},
		Workload: v1alpha3.HarnessWorkload{
			Image:   new("example.com/agent@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"),
			Command: []string{"/agent"},
		},
		Substrate: v1alpha3.RuntimeSubstratePolicy{WorkerPoolRef: corev1.LocalObjectReference{Name: "default"}},
	}}
}

func workflowTemplate(mutate func(*v1alpha3.AgentTemplateSpec)) *v1alpha3.AgentTemplate {
	template := &v1alpha3.AgentTemplate{
		ObjectMeta: metav1.ObjectMeta{Name: "workflow", Namespace: "test"},
		Spec: v1alpha3.AgentTemplateSpec{
			WorkflowFrom: &v1alpha3.ConfigMapKeyReference{Name: "workflows", Key: "agents.yaml"},
		},
	}
	if mutate != nil {
		mutate(&template.Spec)
	}
	return template
}

func TestCompileAgentDeliversWorkflowToBYO(t *testing.T) {
	harness := byoHarness()
	configMap := workflowConfigMap(workflowYAML)
	template := workflowTemplate(nil)

	revision, err := compiler(t, configMap, harness).CompileAgent(t.Context(), inlineAgent(harness, template))
	require.NoError(t, err)

	var config configv1.ADKConfig
	require.NoError(t, runtimeconfig.Unmarshal(revision.ConfigJSON, &config))
	workflow := config.Agent.Workflow
	require.NotNil(t, workflow)
	require.JSONEq(t, `{"apiVersion":"adk.declarative/v0.2","kind":"AgentSystem","agents":[{"name":"support","agent_class":"Workflow","entry":"triage","nodes":[{"id":"triage","type":"function","function":"triage"}]}]}`,
		string(workflow.Document))
	require.Len(t, workflow.Sha256, 64)
	require.Contains(t, string(revision.Provenance), `"kind":"ConfigMap"`, "the source ConfigMap is recorded")

	original, err := revision.Digest()
	require.NoError(t, err)
	configMap.Data["agents.yaml"] = workflowYAML + "      - { id: extra, type: function, function: extra }\n    edges: [{ from: triage, to: extra }]\n"
	changed, err := compiler(t, configMap, harness).CompileAgent(t.Context(), inlineAgent(harness, template))
	require.NoError(t, err)
	updated, err := changed.Digest()
	require.NoError(t, err)
	require.NotEqual(t, original, updated, "a changed document must prepare a new revision")
}

func TestCompileAgentWithoutWorkflowSendsNone(t *testing.T) {
	harness := byoHarness()
	revision, err := compiler(t, harness).CompileAgent(t.Context(), inlineAgent(harness, workflowTemplate(func(s *v1alpha3.AgentTemplateSpec) {
		s.WorkflowFrom = nil
		s.SystemPrompt = "be helpful"
	})))
	require.NoError(t, err)
	var config configv1.ADKConfig
	require.NoError(t, runtimeconfig.Unmarshal(revision.ConfigJSON, &config))
	require.Nil(t, config.Agent.Workflow)
}

func TestCompileAgentRejectsInvalidWorkflow(t *testing.T) {
	harness := byoHarness()
	const invalid = `
apiVersion: adk.declarative/v0.2
kind: AgentSystem
agents:
  - name: support
    agent_class: Workflow
    nodes:
      - { id: triage, type: function }
    edges:
      - { from: START, to: triage }
      - { from: triage, to: ghost }
`
	_, err := compiler(t, workflowConfigMap(invalid), harness).CompileAgent(t.Context(), inlineAgent(harness, workflowTemplate(nil)))
	var validation *translator.ValidationError
	require.ErrorAs(t, err, &validation, "an invalid document is a compatibility problem, not a retryable lookup failure")
	require.ErrorContains(t, err, "invalid workflow")
	require.ErrorContains(t, err, "agents[0].nodes[0].function: required for node type function")
	require.ErrorContains(t, err, `agents[0].edges[1].to: unknown node "ghost"`)
}

func TestCompileAgentWorkflowSourceFailuresAreNotValidationErrors(t *testing.T) {
	harness := byoHarness()
	tests := []struct {
		name    string
		objects []any
		wantErr string
	}{
		{name: "missing ConfigMap", objects: []any{harness}, wantErr: `resolve workflow ConfigMap "workflows": not found`},
		{name: "missing key", objects: []any{harness, &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: "workflows", Namespace: "test"}}}, wantErr: `resolve workflow ConfigMap "workflows": key "agents.yaml" not found`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := compiler(t, tt.objects...).CompileAgent(t.Context(), inlineAgent(harness, workflowTemplate(nil)))
			require.ErrorContains(t, err, tt.wantErr)
			var validation *translator.ValidationError
			require.False(t, errors.As(err, &validation), "a ConfigMap that may appear later must be retried")
		})
	}
}

func TestCompileAgentRejectsWorkflowOnOtherHarnesses(t *testing.T) {
	configMap := workflowConfigMap(workflowYAML)
	for name, spec := range map[string]v1alpha3.HarnessSpec{
		"kagent": {Kagent: &v1alpha3.KagentHarness{}},
		"codex":  {Codex: &v1alpha3.CodexHarness{}},
		"claude": {Claude: &v1alpha3.ClaudeHarness{}},
	} {
		t.Run(name, func(t *testing.T) {
			spec.Substrate = v1alpha3.RuntimeSubstratePolicy{WorkerPoolRef: corev1.LocalObjectReference{Name: "default"}}
			harness := &v1alpha3.Harness{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "test"}, Spec: spec}
			// No ModelConfig: managed harnesses would also reject that, and the
			// workflow gate must be the reported reason.
			_, err := compiler(t, configMap, harness).CompileAgent(t.Context(), inlineAgent(harness, workflowTemplate(nil)))
			var validation *translator.ValidationError
			require.ErrorAs(t, err, &validation)
			require.ErrorContains(t, err, "does not run declarative workflows; use a BYO Harness")
		})
	}
}

func TestCompileAgentRejectsWorkflowOnSharedTemplates(t *testing.T) {
	harness := byoHarness()
	child := &v1alpha3.AgentTemplate{
		ObjectMeta: metav1.ObjectMeta{Name: "child", Namespace: "test"},
		Spec: v1alpha3.AgentTemplateSpec{
			WorkflowFrom: &v1alpha3.ConfigMapKeyReference{Name: "workflows", Key: "agents.yaml"},
		},
	}
	root := workflowTemplate(func(s *v1alpha3.AgentTemplateSpec) {
		s.WorkflowFrom = nil
		s.Tools = []v1alpha3.ToolBinding{{SubAgent: &v1alpha3.SubAgentToolBinding{
			Name: "child", Description: "delegate", TemplateRef: &corev1.LocalObjectReference{Name: child.Name},
		}}}
	})
	_, err := compiler(t, workflowConfigMap(workflowYAML), child, harness).CompileAgent(t.Context(), inlineAgent(harness, root))
	var validation *translator.ValidationError
	require.ErrorAs(t, err, &validation)
	require.ErrorContains(t, err, `AgentTemplate "child" sets workflowFrom, which is only supported on the root template`)
}
