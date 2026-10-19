package kagent

import (
	"testing"

	"github.com/kagent-dev/kagent/go/core/pkg/env"
	corev1 "k8s.io/api/core/v1"
)

func TestApplyOperatorOnlyTraceContextPolicy(t *testing.T) {
	name := env.KagentTraceContextKeys.Name()
	for _, operatorValue := range []string{"", "user.id"} {
		t.Run("operator="+operatorValue, func(t *testing.T) {
			t.Setenv(name, operatorValue)
			values := applyOperatorOnlyEnv([]corev1.EnvVar{
				{Name: "OTHER", Value: "kept"},
				{Name: name, Value: "private_token"},
				{Name: name, ValueFrom: &corev1.EnvVarSource{
					SecretKeyRef: &corev1.SecretKeySelector{Key: "tenant-policy"},
				}},
			}, env.KagentTraceContextKeys)
			count := 0
			for _, value := range values {
				if value.Name != name {
					continue
				}
				count++
				if value.Value != operatorValue || value.ValueFrom != nil {
					t.Fatalf("tenant policy survived: %#v", value)
				}
			}
			want := 0
			if operatorValue != "" {
				want = 1
			}
			if count != want || values[0].Name != "OTHER" || values[0].Value != "kept" {
				t.Fatalf("environment = %#v, want %d operator policy entries and OTHER kept", values, want)
			}
		})
	}
}
