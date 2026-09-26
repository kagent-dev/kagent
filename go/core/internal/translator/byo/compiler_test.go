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
	harness := &v2translator.HarnessConfiguration{Name: "byo", Namespace: "test", Source: &metav1.ObjectMeta{Name: "byo", Namespace: "test"}, Spec: v1alpha3.HarnessSpec{
		BYO:      &v1alpha3.BYOHarness{},
		Workload: v1alpha3.HarnessWorkload{Image: "example.com/agent@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", Command: []string{"/agent"}, Args: []string{"serve"}},
		Env:      []v1alpha3.HarnessEnvVar{{Name: "MODE", Value: new("production")}},
		Substrate: v1alpha3.HarnessSubstratePolicy{
			WorkerPoolRef: corev1.LocalObjectReference{Name: "default"}, SnapshotPolicy: v1alpha3.HarnessSnapshotPolicy{Location: "snapshots"},
		},
	}}
	template := &v2translator.TemplateConfiguration{Name: "custom-agent", Namespace: "test", Source: &metav1.ObjectMeta{Name: "custom-agent", Namespace: "test"}, Spec: v1alpha3.AgentTemplateSpec{
		Description: "custom A2A agent", SystemPrompt: "be helpful",
	}}

	revision, err := NewCompiler(krt.TestingDummyContext{}, v2translator.Collections{}).Compile(context.Background(), &v2translator.HarnessInput{
		AgentName: "runnable-agent",
		Harness:   harness, Root: &v2translator.AgentInput{Template: template, Instruction: template.Spec.SystemPrompt},
	})
	require.NoError(t, err)
	require.Equal(t, harness.Spec.Workload.Command, revision.Command)
	require.Equal(t, harness.Spec.Workload.Args, revision.Args)
	require.Equal(t, []string{"kagent-controller.kagent"}, revision.EgressDestinations)
	require.Equal(t, []corev1.EnvVar{
		{Name: "MODE", Value: "production"},
		{Name: "KAGENT_API_URL", Value: "http://kagent-controller.kagent:8083"},
	}, revision.Environment)

	var config adk.AgentConfig
	require.NoError(t, json.Unmarshal(revision.ConfigJSON, &config))
	require.Nil(t, config.Model)
	require.Equal(t, "be helpful", config.Instruction)
	require.Equal(t, "sqlite+aiosqlite:////data/sessions.db", config.SessionDBURL)
	require.True(t, revision.AgentCard.GetCapabilities().GetStreaming())
}

func TestCompileOpaqueImageKeepsItsOwnTelemetry(t *testing.T) {
	t.Setenv("OTEL_TRACES_EXPORTER", "otlp")
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "http://collector:4317")
	harness := &v2translator.HarnessConfiguration{Name: "byo", Namespace: "test", Source: &metav1.ObjectMeta{Name: "byo", Namespace: "test"}, Spec: v1alpha3.HarnessSpec{
		BYO:      &v1alpha3.BYOHarness{},
		Workload: v1alpha3.HarnessWorkload{Image: "example.com/agent@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"},
		Env: []v1alpha3.HarnessEnvVar{
			{Name: "OTEL_SERVICE_NAME", Value: new("my-langgraph")},
			{Name: "OTEL_EXPORTER_OTLP_ENDPOINT", Value: new("https://otlp.example.com")},
		},
		Substrate: v1alpha3.HarnessSubstratePolicy{
			WorkerPoolRef: corev1.LocalObjectReference{Name: "default"}, SnapshotPolicy: v1alpha3.HarnessSnapshotPolicy{Location: "snapshots"},
		},
	}}
	template := &v2translator.TemplateConfiguration{Name: "custom-agent", Namespace: "test", Source: &metav1.ObjectMeta{Name: "custom-agent", Namespace: "test"}}

	revision, err := NewCompiler(krt.TestingDummyContext{}, v2translator.Collections{}).Compile(context.Background(), &v2translator.HarnessInput{
		AgentName: "runnable-agent",
		Harness:   harness, Root: &v2translator.AgentInput{Template: template},
	})
	require.NoError(t, err)
	environment := map[string]string{}
	for _, variable := range revision.Environment {
		environment[variable.Name] = variable.Value
	}
	require.Equal(t, "my-langgraph", environment["OTEL_SERVICE_NAME"])
	require.Equal(t, "https://otlp.example.com", environment["OTEL_EXPORTER_OTLP_ENDPOINT"])
	require.Equal(t, "otlp", environment["OTEL_TRACES_EXPORTER"])
	require.Contains(t, environment["OTEL_RESOURCE_ATTRIBUTES"], "gen_ai.agent.name=runnable-agent")
}
