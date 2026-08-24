package agent

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
)

func TestMergeEnv_UserValueWins(t *testing.T) {
	userEnv := []corev1.EnvVar{{Name: "OTEL_SERVICE_NAME", Value: "my-agent"}}
	sharedEnv := []corev1.EnvVar{
		{Name: "OTEL_SERVICE_NAME", Value: "controller-default"},
		{Name: "KAGENT_NAME", Value: "agent-1"},
	}

	got := mergeEnv(userEnv, sharedEnv)

	want := map[string]string{
		"OTEL_SERVICE_NAME": "my-agent",
		"KAGENT_NAME":       "agent-1",
	}
	if len(got) != len(want) {
		t.Fatalf("mergeEnv() = %v, want %d entries", got, len(want))
	}
	for _, e := range got {
		if e.Value != want[e.Name] {
			t.Errorf("mergeEnv() entry %s = %q, want %q", e.Name, e.Value, want[e.Name])
		}
	}
}

func TestBuildConfigSecretData(t *testing.T) {
	data := buildConfigSecretData(`{"app":"ok"}`, `{"card":"ok"}`)
	if data["config.json"] == "" || data["agent-card.json"] == "" {
		t.Fatal("config and agent card must be present")
	}
}
