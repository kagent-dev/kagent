package byo

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
	"istio.io/istio/pkg/kube/krt"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	runtimeconfig "github.com/kagent-dev/kagent/go/adk/pkg/config"
	configv1 "github.com/kagent-dev/kagent/go/api/gen/kagent/config/v1"
	"github.com/kagent-dev/kagent/go/api/v1alpha3"
	"github.com/kagent-dev/kagent/go/core/internal/substrate"
	"github.com/kagent-dev/kagent/go/core/internal/translator"
)

func TestCompileOpaqueImage(t *testing.T) {
	harness := &translator.HarnessConfiguration{Name: "byo", Namespace: "test", Source: metav1.ObjectMeta{Name: "byo", Namespace: "test"}, Spec: v1alpha3.HarnessSpec{
		BYO:      &v1alpha3.BYOHarness{},
		Workload: v1alpha3.HarnessWorkload{Image: new("example.com/agent@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"), Command: []string{"/agent"}, Args: []string{"serve"}},
		Env:      []v1alpha3.RuntimeEnvVar{{Name: "MODE", Value: "production"}},
		Substrate: v1alpha3.RuntimeSubstratePolicy{
			WorkerPoolRef: corev1.LocalObjectReference{Name: "default"}, SnapshotPolicy: v1alpha3.RuntimeSnapshotPolicy{Location: "snapshots"},
		},
	}}
	template := &translator.TemplateConfiguration{Name: "custom-agent", Namespace: "test", Source: &metav1.ObjectMeta{Name: "custom-agent", Namespace: "test"}, Spec: v1alpha3.AgentTemplateSpec{
		Description: "custom A2A agent", SystemPrompt: "be helpful",
	}}

	revision, err := NewCompiler(krt.TestingDummyContext{}, translator.Collections{}, testRuntimeDefaults()).Compile(context.Background(), &translator.HarnessInput{
		AgentName: "runnable-agent",
		Harness:   harness, Root: &translator.AgentInput{Template: template, Instruction: template.Spec.SystemPrompt},
	})
	require.NoError(t, err)
	require.Equal(t, harness.Spec.Workload.Command, revision.Command)
	require.Equal(t, harness.Spec.Workload.Args, revision.Args)
	require.Equal(t, []string{"http://kagent-controller.kagent:8083"}, revision.EgressDestinations)
	require.Equal(t, []corev1.EnvVar{
		{Name: "MODE", Value: "production"},
	}, revision.Environment)

	var config configv1.ADKConfig
	require.NoError(t, runtimeconfig.Unmarshal(revision.ConfigJSON, &config))
	require.Nil(t, config.Agent.Model)
	require.Equal(t, "be helpful", config.Agent.Instruction)
	require.Equal(t, "sqlite+aiosqlite:////data/sessions.db", config.Agent.SessionDbUrl)
	require.True(t, revision.AgentCard.GetCapabilities().GetStreaming())
}

func TestCompileOpaqueImageKeepsItsOwnTelemetry(t *testing.T) {
	settings := testRuntimeDefaults()
	settings.Telemetry = &configv1.TelemetryConfig{Traces: &configv1.OTLPConfig{Endpoint: "http://collector:4317", Protocol: new("grpc")}}
	harness := &translator.HarnessConfiguration{Name: "byo", Namespace: "test", Source: metav1.ObjectMeta{Name: "byo", Namespace: "test"}, Spec: v1alpha3.HarnessSpec{
		BYO:      &v1alpha3.BYOHarness{},
		Workload: v1alpha3.HarnessWorkload{Image: new("example.com/agent@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")},
		Env: []v1alpha3.RuntimeEnvVar{
			{Name: "OTEL_SERVICE_NAME", Value: "my-langgraph"},
			{Name: "OTEL_EXPORTER_OTLP_ENDPOINT", Value: "https://otlp.example.com"},
		},
		Substrate: v1alpha3.RuntimeSubstratePolicy{
			WorkerPoolRef: corev1.LocalObjectReference{Name: "default"}, SnapshotPolicy: v1alpha3.RuntimeSnapshotPolicy{Location: "snapshots"},
		},
	}}
	template := &translator.TemplateConfiguration{Name: "custom-agent", Namespace: "test", Source: &metav1.ObjectMeta{Name: "custom-agent", Namespace: "test"}}

	revision, err := NewCompiler(krt.TestingDummyContext{}, translator.Collections{}, settings).Compile(context.Background(), &translator.HarnessInput{
		AgentName: "runnable-agent",
		Harness:   harness, Root: &translator.AgentInput{Template: template},
	})
	require.NoError(t, err)
	environment := map[string]string{}
	for _, variable := range revision.Environment {
		environment[variable.Name] = variable.Value
	}
	require.Equal(t, "my-langgraph", environment["OTEL_SERVICE_NAME"])
	require.Equal(t, "https://otlp.example.com", environment["OTEL_EXPORTER_OTLP_ENDPOINT"])
	require.Equal(t, "otlp", environment["OTEL_TRACES_EXPORTER"])
	require.Contains(t, environment["OTEL_RESOURCE_ATTRIBUTES"], "gen_ai.main_agent.name=runnable-agent")
}

func testRuntimeDefaults() *configv1.RuntimeDefaultsConfig {
	return &configv1.RuntimeDefaultsConfig{ControlPlaneUrl: new("http://kagent-controller.kagent:8083"), GatewayUrl: new("http://kagent-controller.kagent:8083"), Logging: &configv1.LoggingConfig{Level: new("info")}}
}

func TestCompiledTelemetryFitsTheActorEnvironmentBudget(t *testing.T) {
	settings := testRuntimeDefaults()
	settings.Telemetry = &configv1.TelemetryConfig{
		Traces:  &configv1.OTLPConfig{Endpoint: "http://collector:4317", Protocol: new("grpc")},
		Metrics: &configv1.OTLPConfig{Endpoint: "http://collector:4317", Protocol: new("grpc")},
		Logs:    &configv1.OTLPConfig{Endpoint: "http://collector:4317", Protocol: new("grpc")},
	}
	harness := &translator.HarnessConfiguration{Name: "byo", Namespace: "test", Source: metav1.ObjectMeta{Name: "byo", Namespace: "test"}, Spec: v1alpha3.HarnessSpec{
		BYO:       &v1alpha3.BYOHarness{},
		Workload:  v1alpha3.HarnessWorkload{Image: new("example.com/agent@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"), Command: []string{"/agent"}},
		Substrate: v1alpha3.RuntimeSubstratePolicy{WorkerPoolRef: corev1.LocalObjectReference{Name: "default"}, SnapshotPolicy: v1alpha3.RuntimeSnapshotPolicy{Location: "snapshots"}},
	}}
	// All three signals must leave room for fourteen user settings. The final
	// ActorTemplate also includes credentials/trust and bootstrap variables.
	for i := range 14 {
		harness.Spec.Env = append(harness.Spec.Env, v1alpha3.RuntimeEnvVar{Name: fmt.Sprintf("CUSTOM_%d", i), Value: "value"})
	}
	template := &translator.TemplateConfiguration{Name: "custom-agent", Namespace: "test", Source: &metav1.ObjectMeta{Name: "custom-agent", Namespace: "test"}}
	revision, err := NewCompiler(krt.TestingDummyContext{}, translator.Collections{}, settings).Compile(t.Context(), &translator.HarnessInput{
		AgentName: "runnable-agent", Harness: harness, Root: &translator.AgentInput{Template: template},
	})
	require.NoError(t, err)
	// Placement and limits normally come from the central runtime compiler.
	revision.CPU, revision.Memory = "1", "1Gi"
	id, err := revision.Digest()
	require.NoError(t, err)
	actor, err := substrate.ActorTemplateForRevision(&revision.Revision, id)
	require.NoError(t, err)
	require.Len(t, actor.GetContainers()[0].GetEnv(), 32)
	require.Contains(t, revision.EgressDestinations, "http://collector:4317")
	// Keep the actual Substrate boundary enforced after compaction.
	revision.Environment = append(revision.Environment, corev1.EnvVar{Name: "ONE_MORE", Value: "value"})
	_, err = substrate.ActorTemplateForRevision(&revision.Revision, id)
	require.ErrorContains(t, err, "Substrate supports at most 32")
}
