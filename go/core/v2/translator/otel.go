package translator

import (
	"os"
	"slices"
	"strings"

	corev1 "k8s.io/api/core/v1"
)

// OtelEnvFromProcess returns the controller's own OTEL_ variables so harness
// compilers can forward them to the agent runtime, which reads its telemetry
// configuration from its process environment.
func OtelEnvFromProcess() []corev1.EnvVar {
	var envVars []corev1.EnvVar
	for _, envVar := range os.Environ() {
		if !strings.HasPrefix(envVar, "OTEL_") {
			continue
		}
		name, value, found := strings.Cut(envVar, "=")
		if !found {
			continue
		}
		envVars = append(envVars, corev1.EnvVar{Name: name, Value: value})
	}
	slices.SortFunc(envVars, func(a, b corev1.EnvVar) int {
		return strings.Compare(a.Name, b.Name)
	})
	return envVars
}
