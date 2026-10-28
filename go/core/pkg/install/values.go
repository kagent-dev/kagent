package install

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"sigs.k8s.io/yaml"
)

// Values can contain credentials. Keep subprocess diagnostics available through
// Unwrap without including their potentially sensitive contents in CLI errors.
type valuesError struct{ cause error }

func (e *valuesError) Error() string { return "invalid Helm values or chart input" }
func (e *valuesError) Unwrap() error { return e.cause }

func writeValuesChart(dir string) error {
	if err := os.MkdirAll(filepath.Join(dir, "templates"), 0o700); err != nil {
		return fmt.Errorf("failed to create values chart: %w", err)
	}
	files := map[string]string{
		"Chart.yaml":            "apiVersion: v2\nname: kagent-install-values\nversion: 0.0.0\n",
		"templates/values.yaml": "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: kagent-install-values\ndata:\n  values: {{ .Values | toJson | quote }}\n",
	}
	for path, data := range files {
		if err := os.WriteFile(filepath.Join(dir, path), []byte(data), 0o600); err != nil {
			return fmt.Errorf("failed to write values chart: %w", err)
		}
	}
	return nil
}

// Let the installed Helm version own merge, --set parsing and YAML semantics.
// The synthetic chart supplies no defaults and never contacts Kubernetes.
func mergeValues(ctx context.Context, runner Runner, chart Chart, valuesChart string) (map[string]any, error) {
	args := append([]string{"template", "kagent-install-values", valuesChart}, helmValueArgs(chart)...)
	out, err := runner.Run(ctx, Command{Program: "helm", Args: args, Stdin: chart.inlineValues})
	if err != nil {
		return nil, fmt.Errorf("failed to merge chart values: %w", &valuesError{cause: err})
	}
	var manifest struct {
		Data struct {
			Values string `json:"values"`
		} `json:"data"`
	}
	if err := yaml.Unmarshal([]byte(out), &manifest); err != nil {
		return nil, fmt.Errorf("failed to decode merged values: %w", &valuesError{cause: err})
	}
	var values map[string]any
	if err := json.Unmarshal([]byte(manifest.Data.Values), &values); err != nil {
		return nil, fmt.Errorf("failed to decode merged values: %w", &valuesError{cause: err})
	}
	if values == nil {
		return nil, fmt.Errorf("merged Helm values must be an object")
	}
	return values, nil
}
