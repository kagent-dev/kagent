package install

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"
	ktesting "k8s.io/client-go/testing"
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
				cfg.ProfileValues = "agents: {enabled: false}\n"
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
				require.Contains(t, substrate.Chart.InlineValues, "spiffe://cluster.local/ns/team/sa/existing-controller")
				require.Contains(t, substrate.Chart.InlineValues, "system:serviceaccount:ate-system:ate-client")
				kagent := steps[len(steps)-1].(ChartInstall)
				require.Equal(t, "kagent", kagent.Release)
				require.Contains(t, kagent.Chart.Values, "providers.default=openAI")
				require.Contains(t, kagent.Chart.Values, "providers.openAI.apiKey=secret-provider-key")
				require.Equal(t, render.Stdin, kagent.Chart.InlineValues)
				require.Contains(t, kagent.Chart.InlineValues, "agents: {enabled: false}")
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
