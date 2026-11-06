package e2e_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/kagent-dev/kagent/go/api/v1alpha3"
)

const (
	validWorkflowDocument = `
apiVersion: adk.declarative/v0.2
kind: AgentSystem
agents:
  - name: support
    agent_class: Workflow
    entry: triage
    nodes:
      - { id: triage, type: function, function: triage }
`
	invalidWorkflowDocument = `
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
)

// TestAgentWorkflowFromValidation covers the controller's judgment of a
// workflow document before any runtime exists: an invalid document and an
// unsupported harness both fail compilation with Compatible=False and name the
// problem. Neither case starts an Actor.
func TestAgentWorkflowFromValidation(t *testing.T) {
	t.Parallel()
	interactionTarget(t) // skips without a cluster
	for _, tt := range []struct {
		name     string
		harness  string
		document string
		want     []string
	}{
		{
			name: "invalid document on a BYO harness", harness: "byo-adk-e2e", document: invalidWorkflowDocument,
			want: []string{
				"invalid workflow",
				"agents[0].nodes[0].function: required for node type function",
				`agents[0].edges[1].to: unknown node "ghost"`,
			},
		},
		{
			name: "valid document on a managed harness", harness: "kagent", document: validWorkflowDocument,
			want: []string{`Harness runtime "kagent" does not run declarative workflows; use a BYO Harness`},
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			kube := interactionKubeClient(t)
			configMap := &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{GenerateName: "workflow-", Namespace: "kagent"},
				Data:       map[string]string{"agents.yaml": tt.document},
			}
			require.NoError(t, kube.Create(t.Context(), configMap))
			deleteOnCleanup(t, kube, configMap)

			template := &v1alpha3.AgentTemplate{
				ObjectMeta: metav1.ObjectMeta{GenerateName: "workflow-", Namespace: "kagent"},
				Spec: v1alpha3.AgentTemplateSpec{
					Description:  "Workflow validation E2E fixture",
					WorkflowFrom: &v1alpha3.ConfigMapKeyReference{Name: configMap.Name, Key: "agents.yaml"},
				},
			}
			require.NoError(t, kube.Create(t.Context(), template))
			deleteOnCleanup(t, kube, template)

			agent := &v1alpha3.Agent{
				ObjectMeta: metav1.ObjectMeta{Name: template.Name, Namespace: template.Namespace},
				Spec: v1alpha3.AgentSpec{
					Template:   v1alpha3.AgentTemplateSource{Ref: &corev1.LocalObjectReference{Name: template.Name}},
					HarnessRef: corev1.LocalObjectReference{Name: tt.harness},
				},
			}
			require.NoError(t, kube.Create(t.Context(), agent))
			deleteOnCleanup(t, kube, agent)

			compatible := waitForIncompatible(t, kube, agent)
			for _, want := range tt.want {
				require.Contains(t, compatible.Message, want)
			}
			require.Empty(t, agent.Status.LatestSuccessfulRevision, "no revision is prepared for a rejected document")
		})
	}
}

func deleteOnCleanup(t *testing.T, kube ctrlclient.Client, object ctrlclient.Object) {
	t.Helper()
	t.Cleanup(func() {
		if err := kube.Delete(context.Background(), object); err != nil && !apierrors.IsNotFound(err) {
			t.Errorf("delete %T %s/%s: %v", object, object.GetNamespace(), object.GetName(), err)
		}
	})
}

// waitForIncompatible waits for the controller to reject the Agent's current
// generation with Compatible=False for an unsupported configuration, and returns
// that condition.
func waitForIncompatible(t *testing.T, kube ctrlclient.Client, agent *v1alpha3.Agent) *metav1.Condition {
	t.Helper()
	var compatible *metav1.Condition
	err := wait.PollUntilContextTimeout(t.Context(), time.Second, 2*time.Minute, true, func(ctx context.Context) (bool, error) {
		if err := kube.Get(ctx, ctrlclient.ObjectKeyFromObject(agent), agent); err != nil {
			return false, err
		}
		compatible = apimeta.FindStatusCondition(agent.Status.Conditions, v1alpha3.AgentConditionCompatible)
		return compatible != nil && compatible.ObservedGeneration == agent.Generation && compatible.Status == metav1.ConditionFalse, nil
	})
	require.NoError(t, err, "last conditions: %+v", agent.Status.Conditions)
	require.Equal(t, "UnsupportedConfiguration", compatible.Reason)
	return compatible
}
