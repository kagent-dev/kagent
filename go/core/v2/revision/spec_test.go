package revision

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
)

func TestDigestChangesWithRuntimeInput(t *testing.T) {
	spec := &Spec{Namespace: "agents", AgentTemplateName: "helper", HarnessName: "kagent", Image: "example@sha256:abc"}
	first, err := spec.Digest()
	if err != nil {
		t.Fatal(err)
	}
	spec.Environment = []corev1.EnvVar{{Name: "MODE", Value: "new"}}
	second, err := spec.Digest()
	if err != nil {
		t.Fatal(err)
	}
	if first == second {
		t.Fatal("revision did not change with environment")
	}
}
