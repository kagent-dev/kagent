package preparation

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
)

func TestRevisionChangesWithRuntimeInput(t *testing.T) {
	bundle := &Bundle{Namespace: "agents", AgentTemplateName: "helper", HarnessName: "kagent", Image: "example@sha256:abc"}
	first, err := bundle.Revision()
	if err != nil {
		t.Fatal(err)
	}
	bundle.Environment = []corev1.EnvVar{{Name: "MODE", Value: "new"}}
	second, err := bundle.Revision()
	if err != nil {
		t.Fatal(err)
	}
	if first == second {
		t.Fatal("revision did not change with environment")
	}
}
