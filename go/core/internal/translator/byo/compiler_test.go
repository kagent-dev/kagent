package byo

import (
	"context"
	"encoding/json"
	"strings"
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
	require.Equal(t, []corev1.EnvVar{
		{Name: "MODE", Value: "production"},
		{Name: "PORT", Value: "80"},
	}, revision.Environment)

	var config adk.AgentConfig
	require.NoError(t, json.Unmarshal(revision.ConfigJSON, &config))
	require.Nil(t, config.Model)
	require.Equal(t, "be helpful", config.Instruction)
	require.True(t, revision.AgentCard.GetCapabilities().GetStreaming())
}

// TestCompileInjectsPort aligns the injected PORT with the agent card, which
// advertises the A2A endpoint on :80. Without it a go/adk/pkg/app image falls
// back to its 8080 default while the card points at :80 (#2758).
func TestCompileInjectsPort(t *testing.T) {
	harness := &v1alpha3.Harness{ObjectMeta: metav1.ObjectMeta{Name: "byo", Namespace: "test"}, Spec: v1alpha3.HarnessSpec{
		BYO:      &v1alpha3.BYOHarness{},
		Workload: v1alpha3.HarnessWorkload{Image: "example.com/agent@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", Command: []string{"/agent"}},
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

	cardURL := revision.AgentCard.GetSupportedInterfaces()[0].GetUrl()
	require.Contains(t, cardURL, ":80")
	cardPort := cardURL[strings.LastIndex(cardURL, ":")+1:]

	var port *string
	for i := range revision.Environment {
		if revision.Environment[i].Name == "PORT" {
			port = &revision.Environment[i].Value
		}
	}
	require.NotNil(t, port, "agent card advertises %s but the harness injects no PORT env var", cardURL)
	require.Equal(t, cardPort, *port, "agent card advertises port %s but the harness injects PORT=%q", cardPort, *port)
}

// TestCompilePortSetting covers the BYO Port setting (#2758, direction from
// #2800): it drives both the agent card and the injected PORT, wins over a
// spec.env PORT, and leaves exactly one PORT entry.
func TestCompilePortSetting(t *testing.T) {
	port := int32(8443)
	harness := &v1alpha3.Harness{ObjectMeta: metav1.ObjectMeta{Name: "byo", Namespace: "test"}, Spec: v1alpha3.HarnessSpec{
		BYO:      &v1alpha3.BYOHarness{Port: &port},
		Workload: v1alpha3.HarnessWorkload{Image: "example.com/agent@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", Command: []string{"/agent"}},
		Env:      []v1alpha3.HarnessEnvVar{{Name: "PORT", Value: new("9000")}},
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

	require.Equal(t, "http://127.0.0.1:8443", revision.AgentCard.GetSupportedInterfaces()[0].GetUrl())

	var ports []string
	for i := range revision.Environment {
		if revision.Environment[i].Name == "PORT" {
			ports = append(ports, revision.Environment[i].Value)
		}
	}
	require.Equal(t, []string{"8443"}, ports, "Port setting must win over spec.env PORT and leave one entry")
}
