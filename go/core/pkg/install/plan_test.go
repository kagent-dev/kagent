package install

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"
	ktesting "k8s.io/client-go/testing"
	"sigs.k8s.io/yaml"
)

type recordingRunner struct {
	commands []Command
	run      func(Command) (string, error)
}

var _ Runner = (*recordingRunner)(nil)

func (r *recordingRunner) Run(ctx context.Context, cmd Command) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	cmd.Args = slices.Clone(cmd.Args)
	r.commands = append(r.commands, cmd)
	if r.run != nil {
		return r.run(cmd)
	}
	if cmd.Program == "helm" && cmd.Args[0] == "template" {
		return "kind: Deployment\nspec: {template: {spec: {serviceAccountName: existing-controller}}}", nil
	}
	return "", nil
}

func testConfig() Config {
	return Config{
		Namespace:      "team",
		Kagent:         Chart{Name: "kagent", Repository: "./kagent/", Version: "v1", ValuesFiles: []string{"operator.yaml", "local.yaml"}},
		KagentCRDs:     Chart{Name: "kagent-crds", Repository: "./kagent/", Version: "v1"},
		Substrate:      Chart{Name: "substrate", Repository: "./substrate/", Version: "v2"},
		SubstrateCRDs:  Chart{Name: "substrate-crds", Repository: "./substrate/", Version: "v2"},
		PodCertificate: Chart{Name: "substrate-podcert", Repository: "./podcert/", Version: "v3"},
		ModelProvider:  "OpenAI", APIKey: "secret-provider-key",
		WorkerImage: "mirror.example/worker:v2",
		Database:    Postgres{Image: "postgres:18-alpine"},
	}
}

func TestPlanAndApply(t *testing.T) {
	for _, skip := range []bool{false, true} {
		for _, customCharts := range []bool{false, true} {
			t.Run(fmt.Sprintf("skip_database=%t/customCharts=%t", skip, customCharts), func(t *testing.T) {
				cfg := testConfig()
				cfg.SkipDatabaseSetup = skip
				cfg.Substrate.Values = []string{"global.setting=substrate-only", "rustfs.enabled=false"}
				cfg.PodCertificate.Values = []string{"global.imageRegistry=mirror.example"}
				if customCharts {
					cfg.Kagent.Name, cfg.KagentCRDs.Name = "custom-kagent", "custom-kagent-crds"
					cfg.Kagent.Values = []string{"controller.replicas=2"}
					cfg.Database.ExtraSetup = []SQLScript{{SQL: "SELECT 'application setup';", Args: []string{"--set=app_schema=app"}}}
				}
				runner := &recordingRunner{}
				kube := fake.NewSimpleClientset()
				client := NewClient(runner, kube)
				plan, err := client.Plan(t.Context(), cfg)
				require.NoError(t, err)
				require.Empty(t, kube.Actions(), "planning must not access or mutate identity pools")
				require.Len(t, runner.commands, 1, "planning only renders the controller")
				render := runner.commands[0]
				require.Equal(t, "template", render.Args[0])
				require.Contains(t, render.Args, chartReference(cfg.Kagent))
				require.Contains(t, render.Stdin, cfg.WorkerImage)

				steps := plan.Steps()
				wantDescriptions := []string{
					"Installing " + cfg.KagentCRDs.Name + " from ./kagent/",
					"Installing substrate-crds from ./substrate/",
					"Preparing Substrate prerequisites",
					"Installing substrate-podcert from ./podcert/",
				}
				if !skip {
					wantDescriptions = append(wantDescriptions, "Preparing bundled PostgreSQL")
				}
				wantDescriptions = append(wantDescriptions, "Installing substrate from ./substrate/", "Installing "+cfg.Kagent.Name+" from ./kagent/")
				var descriptions []string
				for _, step := range steps {
					descriptions = append(descriptions, step.Description())
				}
				require.Equal(t, wantDescriptions, descriptions)
				podcert := steps[3].(ChartInstall)
				require.Contains(t, podcert.Chart.Values, "global.imageRegistry=mirror.example")
				require.NotContains(t, podcert.Chart.Values, "rustfs.enabled=false")
				require.NotContains(t, podcert.Chart.Values, "global.setting=substrate-only")
				if skip {
					require.NotContains(t, strings.Join(podcert.Chart.Values, " "), "postgresClients[")
				} else {
					require.Contains(t, podcert.Chart.Values, "postgresClients[0]=team/existing-controller=kagent_owner_user")
					require.Contains(t, podcert.Chart.Values, "postgresClients[1]=team/existing-controller=kagent_readwrite_user")
				}
				substrate := steps[len(steps)-2].(ChartInstall)
				require.Contains(t, substrate.Chart.inlineValues, "spiffe://cluster.local/ns/team/sa/existing-controller")
				require.Contains(t, substrate.Chart.inlineValues, "system:serviceaccount:ate-system:ate-client")
				kagent := steps[len(steps)-1].(ChartInstall)
				require.Equal(t, "kagent", kagent.Release)
				require.Contains(t, kagent.Chart.Values, "providers.default=openAI")
				require.Contains(t, kagent.Chart.Values, "providers.openAI.apiKey=secret-provider-key")
				require.Equal(t, render.Stdin, kagent.Chart.inlineValues)
				require.Equal(t, []string{"operator.yaml", "local.yaml"}, kagent.Chart.ValuesFiles)
				if customCharts {
					require.Contains(t, kagent.Chart.Values, "controller.replicas=2")
				}

				var progress []string
				require.NoError(t, client.Apply(t.Context(), plan, func(message string) { progress = append(progress, message) }))
				require.Equal(t, wantDescriptions, progress)
				require.NotContains(t, strings.Join(progress, " "), "secret-")
				_, err = kube.CoreV1().Secrets(substrateNamespace).Get(t.Context(), "actor-id-jwt-pool", metav1.GetOptions{})
				require.NoError(t, err)

				// Verify what execution actually received, including non-Helm operations.
				next := 1
				for _, step := range steps {
					switch s := step.(type) {
					case ChartInstall:
						require.Equal(t, s.Command(), runner.commands[next])
						next++
					case Postgres:
						require.Equal(t, []string{"apply", "--server-side", "--field-manager=kagent-cli", "-f", "-"}, runner.commands[next].Args)
						require.Contains(t, runner.commands[next].Stdin, "namespace: team")
						require.Equal(t, []string{"-n", "team", "rollout", "status", "deployment/kagent-postgresql", "--timeout=5m"}, runner.commands[next+1].Args)
						require.Equal(t, s.SetupScript().SQL, runner.commands[next+2].Stdin)
						if customCharts {
							require.Contains(t, runner.commands[next+2].Args, "--set=app_schema=app")
						}
						next += 3
					}
				}
				require.Len(t, runner.commands, next)
			})
		}
	}
}

func TestPlanCopiesInputsAndSteps(t *testing.T) {
	cfg := testConfig()
	cfg.Kagent.Values = []string{"controller.replicas=1"}
	cfg.Database.ExtraSetup = []SQLScript{{SQL: "SELECT 1;", Args: []string{"--set=extra=original"}}}
	client := NewClient(&recordingRunner{}, fake.NewSimpleClientset())
	plan, err := client.Plan(t.Context(), cfg)
	require.NoError(t, err)
	cfg.Kagent.Values[0], cfg.Kagent.ValuesFiles[0] = "changed", "changed"
	cfg.Database.ExtraSetup[0].Args[0] = "changed"
	steps := plan.Steps()
	chart := steps[len(steps)-1].(ChartInstall)
	require.Contains(t, chart.Chart.Values, "controller.replicas=1")
	require.Equal(t, "operator.yaml", chart.Chart.ValuesFiles[0])
	chart.Chart.Values[0], chart.Chart.ValuesFiles[0] = "changed", "changed"
	database := steps[4].(Postgres)
	require.Equal(t, "--set=extra=original", database.ExtraSetup[0].Args[0])
	database.ExtraSetup[0].Args[0] = "changed"
	steps = plan.Steps()
	require.Contains(t, steps[len(steps)-1].(ChartInstall).Chart.Values, "providers.default=openAI")
	require.Equal(t, "operator.yaml", steps[len(steps)-1].(ChartInstall).Chart.ValuesFiles[0])
	require.Equal(t, "--set=extra=original", steps[4].(Postgres).ExtraSetup[0].Args[0])
}

func TestPlanRejectsInvalidInputsBeforeIO(t *testing.T) {
	for _, tc := range []struct {
		name string
		edit func(*Config)
	}{
		{"namespace", func(c *Config) { c.Namespace = "bad\nnamespace: injected" }},
		{"image", func(c *Config) { c.Database.Image = "postgres:18\ncommand: [sh]" }},
		{"chart", func(c *Config) { c.Kagent.Name = "" }},
		{"provider", func(c *Config) { c.ModelProvider = "" }},
		{"worker", func(c *Config) { c.WorkerImage = "" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := testConfig()
			tc.edit(&cfg)
			runner := &recordingRunner{}
			_, err := NewClient(runner, fake.NewSimpleClientset()).Plan(t.Context(), cfg)
			require.Error(t, err)
			require.Empty(t, runner.commands)
		})
	}
}

func TestApplyStopsAtFailures(t *testing.T) {
	for _, fail := range []string{"kagent-crds", "substrate-crds", "prerequisites", "substrate-podcert", "database apply", "database wait", "database SQL", "substrate", "kagent"} {
		t.Run(fail, func(t *testing.T) {
			runner := &recordingRunner{}
			kube := fake.NewSimpleClientset()
			client := NewClient(runner, kube)
			plan, err := client.Plan(t.Context(), testConfig())
			require.NoError(t, err)
			failure := errors.New("injected failure")
			failed := false
			runner.run = func(cmd Command) (string, error) {
				require.False(t, failed, "execution must stop after failure")
				matches := cmd.Program == "helm" && cmd.Args[2] == fail
				if cmd.Program == "kubectl" {
					matches = (fail == "database apply" && cmd.Args[0] == "apply") ||
						(fail == "database wait" && slices.Contains(cmd.Args, "rollout")) ||
						(fail == "database SQL" && slices.Contains(cmd.Args, "psql"))
				}
				if matches {
					failed = true
					return "", failure
				}
				return "", nil
			}
			if fail == "prerequisites" {
				kube.PrependReactor("get", "namespaces", func(ktesting.Action) (bool, runtime.Object, error) {
					failed = true
					return true, nil, failure
				})
			}
			require.ErrorIs(t, client.Apply(t.Context(), plan, nil), failure)
			require.True(t, failed)
		})
	}
}

func TestApplyCancellationAndEmptyPlan(t *testing.T) {
	runner := &recordingRunner{}
	client := NewClient(runner, fake.NewSimpleClientset())
	require.ErrorContains(t, client.Apply(t.Context(), Plan{}, nil), "empty")
	plan, err := client.Plan(t.Context(), testConfig())
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	require.ErrorIs(t, client.Apply(ctx, plan, nil), context.Canceled)
	require.Len(t, runner.commands, 1)
}

func TestPlanValuesFilesMergeWithGeneratedDefaults(t *testing.T) {
	if _, err := exec.LookPath("helm"); err != nil {
		t.Skip("Helm is required for values integration")
	}
	root := t.TempDir()
	for _, name := range []string{"kagent", "substrate"} {
		dir := filepath.Join(root, name)
		require.NoError(t, os.MkdirAll(filepath.Join(dir, "templates"), 0o700))
		require.NoError(t, os.WriteFile(filepath.Join(dir, "Chart.yaml"), []byte("apiVersion: v2\nname: "+name+"\nversion: 1.0.0\n"), 0o600))
		require.NoError(t, os.WriteFile(filepath.Join(dir, "templates", "values.yaml"), []byte("apiVersion: v1\nkind: ConfigMap\nmetadata: {name: values}\ndata:\n  values: {{ .Values | toJson | quote }}\n"), 0o600))
	}
	require.NoError(t, os.WriteFile(filepath.Join(root, "kagent", "templates", "controller-deployment.yaml"), []byte("apiVersion: apps/v1\nkind: Deployment\nmetadata: {name: controller}\nspec: {template: {spec: {serviceAccountName: {{ .Values.controller.serviceAccount.name }}}}}\n"), 0o600))
	profile, operator, substrate := filepath.Join(root, "profile.yaml"), filepath.Join(root, "operator.yaml"), filepath.Join(root, "substrate.yaml")
	require.NoError(t, os.WriteFile(profile, []byte("controller: {replicas: 1}\nk8s-agent: {enabled: false}\n"), 0o600))
	require.NoError(t, os.WriteFile(operator, []byte("controller: {replicas: 2, serviceAccount: {name: custom-controller}}\nk8s-agent: {enabled: true}\n"), 0o600))
	require.NoError(t, os.WriteFile(substrate, []byte("ateApi: {replicas: 3}\n"), 0o600))
	cfg := testConfig()
	cfg.SkipDatabaseSetup = true
	cfg.Kagent.Repository, cfg.Substrate.Repository = root, root
	cfg.Kagent.ValuesFiles = []string{profile, operator}
	cfg.Kagent.Values = []string{"controller.replicas=4"}
	cfg.Substrate.ValuesFiles = []string{substrate}
	runner := CommandRunner{}
	plan, err := NewClient(runner, fake.NewSimpleClientset()).Plan(t.Context(), cfg)
	require.NoError(t, err)
	for _, step := range plan.Steps() {
		release, ok := step.(ChartInstall)
		if !ok || (release.Release != "kagent" && release.Release != "substrate") {
			continue
		}
		command := Command{Program: "helm", Args: []string{"template", release.Release, chartReference(release.Chart), "--namespace", release.Namespace, "--show-only", "templates/values.yaml"}, Stdin: release.Chart.inlineValues}
		command.Args = append(command.Args, helmValueArgs(release.Chart)...)
		out, err := runner.Run(t.Context(), command)
		require.NoError(t, err)
		var manifest struct{ Data map[string]string }
		require.NoError(t, yaml.Unmarshal([]byte(out), &manifest))
		var values map[string]any
		require.NoError(t, json.Unmarshal([]byte(manifest.Data["values"]), &values))
		if release.Release == "kagent" {
			require.Equal(t, map[string]any{
				"replicas":                   float64(4),
				"serviceAccount":             map[string]any{"name": "custom-controller"},
				"substrateClientCertificate": map[string]any{"enabled": true},
			}, values["controller"])
			require.Equal(t, map[string]any{"enabled": true}, values["k8s-agent"])
		} else {
			require.Equal(t, map[string]any{
				"replicas": float64(3),
				"authorization": map[string]any{"bootstrapOwners": []any{
					"spiffe://cluster.local/ns/team/sa/custom-controller",
					"system:serviceaccount:ate-system:ate-client",
				}},
			}, values["ateApi"])
		}
	}
}
