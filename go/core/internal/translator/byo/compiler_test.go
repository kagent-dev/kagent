package byo

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/kagent-dev/kagent/go/api/adk"
	"github.com/kagent-dev/kagent/go/api/v1alpha3"
	v2translator "github.com/kagent-dev/kagent/go/core/internal/translator"
	"github.com/stretchr/testify/require"
	"istio.io/istio/pkg/kube/krt"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestCompileOpaqueImage(t *testing.T) {
	harness := &v1alpha3.Harness{ObjectMeta: metav1.ObjectMeta{Name: "byo", Namespace: "test"}, Spec: v1alpha3.HarnessSpec{
		BYO:      &v1alpha3.BYOHarness{},
		Workload: v1alpha3.HarnessWorkload{Image: "example.com/agent@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", Command: []string{"/agent"}, Args: []string{"serve"}},
		Env:      []v1alpha3.HarnessEnvVar{{Name: "MODE", Value: new("production")}},
		Substrate: v1alpha3.HarnessSubstratePolicy{
			WorkerPoolRef: corev1.LocalObjectReference{Name: "default"}, SnapshotPolicy: v1alpha3.HarnessSnapshotPolicy{Location: "snapshots"},
		},
	}}
	template := &v1alpha3.AgentTemplate{ObjectMeta: metav1.ObjectMeta{Name: "custom-agent", Namespace: "test"}, Spec: v1alpha3.AgentTemplateSpec{
		Description: "custom A2A agent", SystemPrompt: "be helpful",
	}}

	revision, err := NewCompiler(krt.TestingDummyContext{}, v2translator.Collections{}).Compile(context.Background(), &v2translator.HarnessInput{
		Harness: harness, Root: &v2translator.AgentInput{Template: template, Instruction: template.Spec.SystemPrompt},
	})
	require.NoError(t, err)
	require.Equal(t, harness.Spec.Workload.Command, revision.Command)
	require.Equal(t, harness.Spec.Workload.Args, revision.Args)
	require.Empty(t, revision.EgressDestinations)
	require.Equal(t, []corev1.EnvVar{{Name: "MODE", Value: "production"}}, revision.Environment)

	var config adk.AgentConfig
	require.NoError(t, json.Unmarshal(revision.ConfigJSON, &config))
	require.Nil(t, config.Model)
	require.Equal(t, "be helpful", config.Instruction)
	require.Contains(t, string(revision.AgentCardJSON), `"streaming":true`)
}
