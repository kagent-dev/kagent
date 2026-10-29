package install

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
	"k8s.io/client-go/kubernetes/fake"
)

func TestChartCommand(t *testing.T) {
	step := ChartInstall{Release: "kagent", Namespace: "team", Chart: Chart{
		Name: "custom-kagent", Repository: "./charts/", Version: "v1",
		Values: []string{"controller.replicas=1"}, ValuesFiles: []string{"operator.yaml", "local.yaml"},
		inlineValues: "controller:\n  serviceAccount: {create: false}\n",
	}}
	require.Equal(t, Command{Program: "helm", Args: []string{
		"upgrade", "--install", "kagent", "./charts/custom-kagent", "--version", "v1",
		"--namespace", "team", "--create-namespace", "--wait", "--wait-for-jobs", "--history-max", "2", "--timeout", "5m",
		"--set", "controller.replicas=1", "-f", "-", "-f", "operator.yaml", "-f", "local.yaml",
	}, Stdin: step.Chart.inlineValues}, step.Command())
	for _, tc := range []struct {
		repository string
		want       []string
	}{
		{"oci://localhost:5001/charts", []string{"--plain-http"}},
		{"oci://127.0.0.1:5001/charts", []string{"--plain-http"}},
		{"oci://[::1]:5001/charts", []string{"--plain-http"}},
		{"oci://registry.example/charts", nil},
		{"./charts", nil},
	} {
		t.Run(tc.repository, func(t *testing.T) {
			require.Equal(t, tc.want, chartTransportArgs(Chart{Repository: tc.repository}))
		})
	}
}

func TestControllerServiceAccount(t *testing.T) {
	for _, tc := range []struct {
		name, manifest, want, wantErr string
	}{
		{"default", "kind: Deployment\nspec: {template: {spec: {serviceAccountName: kagent-controller}}}", "kagent-controller", ""},
		{"custom", "kind: Deployment\nspec: {template: {spec: {serviceAccountName: existing-controller}}}", "existing-controller", ""},
		{"missing", "kind: Deployment", "", "no service account"},
		{"malformed", "[", "", "decode controller deployment"},
		{"render fails", "fail", "", "render controller deployment"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			runner := &recordingRunner{run: func(Command) (string, error) {
				if tc.manifest == "fail" {
					return "", errors.New("render failed")
				}
				return tc.manifest, nil
			}}
			chart := Chart{Name: "custom-kagent", Repository: "./charts/", Version: "v1", Values: []string{"controller.replicas=1"}, ValuesFiles: []string{"operator.yaml", "local.yaml"}, inlineValues: "controller: {}\n"}
			got, err := NewClient(runner, fake.NewSimpleClientset()).controllerServiceAccount(t.Context(), "team", chart)
			if tc.wantErr != "" {
				require.ErrorContains(t, err, tc.wantErr)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tc.want, got)
			require.Equal(t, []string{"template", "kagent", "./charts/custom-kagent", "--version", "v1", "--namespace", "team", "--show-only", "templates/controller-deployment.yaml", "--set", "controller.replicas=1", "-f", "-", "-f", "operator.yaml", "-f", "local.yaml"}, runner.commands[0].Args)
			require.Equal(t, chart.inlineValues, runner.commands[0].Stdin)
		})
	}
}
