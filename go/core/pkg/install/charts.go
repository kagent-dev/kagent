package install

import (
	"context"
	"fmt"
	"net"
	"net/url"
	"slices"
	"strings"

	appsv1 "k8s.io/api/apps/v1"
	"sigs.k8s.io/yaml"
)

const (
	kagentRelease         = "kagent"
	kagentCRDsRelease     = "kagent-crds"
	substrateRelease      = "substrate"
	substrateCRDsRelease  = "substrate-crds"
	podCertificateRelease = "substrate-podcert"
)

// Chart names an artifact and its overrides. Name is independent of the Helm
// release name, allowing embedding applications to select different chart artifacts.
type Chart struct {
	Name        string
	Repository  string
	Version     string
	Values      []string
	ValuesFiles []string
	// Generated defaults stay separate from caller files so Helm merges their maps.
	inlineValues string
}

func cloneChart(chart Chart) Chart {
	chart.Values = slices.Clone(chart.Values)
	chart.ValuesFiles = slices.Clone(chart.ValuesFiles)
	return chart
}

func chartReference(chart Chart) string {
	return strings.TrimRight(chart.Repository, "/") + "/" + chart.Name
}

// ChartInstall is a fully resolved Helm operation. Values may contain secrets;
// callers must not print or serialize them without an explicit secret policy.
type ChartInstall struct {
	Release   string
	Namespace string
	Chart     Chart
}

var _ Step = ChartInstall{}

func (ChartInstall) installStep() {}

func (s ChartInstall) Description() string {
	return "Installing " + s.Chart.Name + " from " + s.Chart.Repository
}

// Command returns the exact invocation used to apply this release.
func (s ChartInstall) Command() Command {
	args := []string{"upgrade", "--install", s.Release, chartReference(s.Chart),
		"--version", s.Chart.Version, "--namespace", s.Namespace, "--create-namespace",
		"--wait", "--wait-for-jobs", "--history-max", "2", "--timeout", "5m"}
	args = append(args, chartTransportArgs(s.Chart)...)
	args = append(args, helmValueArgs(s.Chart)...)
	return Command{Program: "helm", Args: args, Stdin: s.Chart.inlineValues}
}

func chartTransportArgs(chart Chart) []string {
	u, err := url.Parse(chart.Repository)
	if err == nil && u.Scheme == "oci" {
		host := u.Hostname()
		if strings.EqualFold(host, "localhost") || net.ParseIP(host).IsLoopback() {
			return []string{"--plain-http"}
		}
	}
	return nil
}

// helmValueArgs keeps rendering and installation on the same values precedence.
func helmValueArgs(chart Chart) []string {
	var args []string
	for _, value := range chart.Values {
		if value != "" {
			args = append(args, "--set", value)
		}
	}
	if chart.inlineValues != "" {
		args = append(args, "-f", "-")
	}
	for _, file := range chart.ValuesFiles {
		args = append(args, "-f", file)
	}
	return args
}

func controllerServiceAccount(ctx context.Context, runner Runner, namespace string, chart Chart) (string, error) {
	args := []string{"template", kagentRelease, chartReference(chart), "--version", chart.Version,
		"--namespace", namespace, "--show-only", "templates/controller-deployment.yaml"}
	args = append(args, chartTransportArgs(chart)...)
	args = append(args, helmValueArgs(chart)...)
	manifest, err := runner.Run(ctx, Command{Program: "helm", Args: args, Stdin: chart.inlineValues})
	if err != nil {
		return "", fmt.Errorf("failed to render controller deployment: %w: %s", err, strings.TrimSpace(manifest))
	}
	var deployment appsv1.Deployment
	if err := yaml.Unmarshal([]byte(manifest), &deployment); err != nil {
		return "", fmt.Errorf("failed to decode controller deployment: %w", err)
	}
	if deployment.Kind != "Deployment" || deployment.Spec.Template.Spec.ServiceAccountName == "" {
		return "", fmt.Errorf("rendered controller deployment has no service account")
	}
	return deployment.Spec.Template.Spec.ServiceAccountName, nil
}
