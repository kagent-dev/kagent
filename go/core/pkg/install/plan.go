package install

import (
	"context"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"k8s.io/apimachinery/pkg/util/validation"

	kagentsetup "github.com/kagent-dev/kagent/go/core/pkg/postgressetup"
)

// Config contains resolved application settings. The caller selects chart names,
// repositories, credentials, values files and worker image explicitly.
type Config struct {
	Namespace         string
	Kagent            Chart
	KagentCRDs        Chart
	Substrate         Chart
	SubstrateCRDs     Chart
	PodCertificate    Chart
	ModelProvider     string
	APIKey            string
	WorkerImage       string
	SkipDatabaseSetup bool
	Database          Postgres
}

// Step is one of the concrete operations supported by the installer.
type Step interface {
	Description() string
	installStep()
}

// SubstratePrerequisites creates namespaces, signing pools and authentication
// configuration, preserving existing private keys. Keys are generated only by Apply.
type SubstratePrerequisites struct {
	Namespace string
}

var _ Step = SubstratePrerequisites{}

func (SubstratePrerequisites) installStep() {}

func (SubstratePrerequisites) Description() string { return "Preparing Substrate prerequisites" }

// Plan stores operations in dependency order. Building a plan does not mutate the
// cluster; chart rendering may read local values files and fetch chart artifacts.
type Plan struct {
	steps []Step
}

// Steps returns an independent copy of the planned operations in execution order.
func (p Plan) Steps() []Step {
	steps := make([]Step, 0, len(p.steps))
	for _, step := range p.steps {
		switch s := step.(type) {
		case ChartInstall:
			s.Chart = cloneChart(s.Chart)
			steps = append(steps, s)
		case Postgres:
			steps = append(steps, clonePostgres(s))
		case SubstratePrerequisites:
			steps = append(steps, s)
		}
	}
	return steps
}

// Plan resolves the controller identity using the same chart and overrides that
// Apply will install. It performs no Kubernetes writes or key generation.
func (c *Client) Plan(ctx context.Context, cfg Config) (Plan, error) {
	if problems := validation.IsDNS1123Label(cfg.Namespace); len(problems) != 0 {
		return Plan{}, fmt.Errorf("invalid namespace %q: %s", cfg.Namespace, strings.Join(problems, ", "))
	}
	for _, chart := range []Chart{cfg.Kagent, cfg.KagentCRDs, cfg.Substrate, cfg.SubstrateCRDs, cfg.PodCertificate} {
		if chart.Name == "" || chart.Repository == "" || chart.Version == "" {
			return Plan{}, fmt.Errorf("chart name, repository and version are required")
		}
	}
	if cfg.ModelProvider == "" || cfg.WorkerImage == "" {
		return Plan{}, fmt.Errorf("model provider and worker image are required")
	}
	kagent := cloneChart(cfg.Kagent)
	provider := strings.ToLower(cfg.ModelProvider[:1]) + cfg.ModelProvider[1:]
	kagent.Values = append([]string{"providers.default=" + provider,
		"providers." + provider + ".apiKey=" + cfg.APIKey}, kagent.Values...)
	kagent.inlineValues = `controller:
  substrateClientCertificate:
    enabled: true
config:
  substrate:
    apiEndpoint: dns:///api.ate-system.svc:443
    routerUrl: http://atenet-router.ate-system.svc:80
` + fmt.Sprintf("\nsubstrateWorkerPool:\n  create: true\n  workerImage: %s\n", strconv.Quote(cfg.WorkerImage))
	if !cfg.SkipDatabaseSetup {
		kagent.Values = append(kagent.Values, "database.postgres.clientCertificate.enabled=true")
	}
	database := clonePostgres(cfg.Database)
	database.Namespace = cfg.Namespace
	if !cfg.SkipDatabaseSetup {
		if _, err := database.Manifest(); err != nil {
			return Plan{}, err
		}
	}
	serviceAccount, err := c.controllerServiceAccount(ctx, cfg.Namespace, kagent)
	if err != nil {
		return Plan{}, err
	}
	substrate := cloneChart(cfg.Substrate)
	substrate.inlineValues = substrateInstallValues(cfg.Namespace, serviceAccount)
	podcert := cloneChart(cfg.PodCertificate)
	podcert.Values = append(postgresClientValues(cfg.Namespace, serviceAccount, !cfg.SkipDatabaseSetup), podcert.Values...)
	if !cfg.SkipDatabaseSetup {
		substrate.Values = append(substrate.Values, "postgres.clientCertificates.enabled=true")
	}
	steps := []Step{
		ChartInstall{Release: "kagent-crds", Namespace: cfg.Namespace, Chart: cloneChart(cfg.KagentCRDs)},
		ChartInstall{Release: "substrate-crds", Namespace: substrateNamespace, Chart: cloneChart(cfg.SubstrateCRDs)},
		SubstratePrerequisites{Namespace: cfg.Namespace},
		ChartInstall{Release: podCertificateRelease, Namespace: podCertificateNamespace, Chart: podcert},
	}
	if !cfg.SkipDatabaseSetup {
		steps = append(steps, database)
	}
	steps = append(steps,
		ChartInstall{Release: "substrate", Namespace: substrateNamespace, Chart: substrate},
		ChartInstall{Release: "kagent", Namespace: cfg.Namespace, Chart: kagent})
	return Plan{steps: steps}, nil
}

// Apply runs the plan in order and stops at the first failure. Progress messages
// contain operation names only, never chart values or credentials.
func (c *Client) Apply(ctx context.Context, plan Plan, progress func(string)) error {
	if len(plan.steps) == 0 {
		return fmt.Errorf("install plan is empty")
	}
	for _, step := range plan.steps {
		if err := ctx.Err(); err != nil {
			return err
		}
		if progress != nil {
			progress(step.Description())
		}
		switch s := step.(type) {
		case ChartInstall:
			out, err := c.runner.Run(ctx, s.Command())
			if err != nil {
				if s.Release == "kagent-crds" && strings.Contains(out+err.Error(), "exists and cannot be imported into the current release") {
					return fmt.Errorf("failed to install kagent-crds: Kagent CRDs from a previous installation exist outside Helm; delete them, then retry: %w: %s", err, strings.TrimSpace(out))
				}
				return fmt.Errorf("failed to install %s: %w: %s", s.Release, err, strings.TrimSpace(out))
			}
		case SubstratePrerequisites:
			if err := c.PrepareSubstrate(ctx, s.Namespace); err != nil {
				return fmt.Errorf("failed to prepare Substrate prerequisites: %w", err)
			}
		case Postgres:
			if err := c.PreparePostgres(ctx, s); err != nil {
				return err
			}
		}
	}
	return nil
}

func postgresClientValues(namespace, serviceAccount string, includeBundledPostgresClient bool) []string {
	var result []string
	if includeBundledPostgresClient {
		result = append(result,
			fmt.Sprintf("postgresClients[0]=%s/%s=%s", namespace, serviceAccount, kagentsetup.OwnerUser),
			fmt.Sprintf("postgresClients[1]=%s/%s=%s", namespace, serviceAccount, kagentsetup.ReadWriteUser))
	}
	return result
}

func substrateInstallValues(namespace, serviceAccount string) string {
	return fmt.Sprintf(`preview:
  - PerTemplateSnapshotLocation
ateApi:
  authorization:
    bootstrapOwners:
      - %s
      - "system:serviceaccount:ate-system:ate-client"
credentialProvider:
  namespacePolicies:
    - atespace: %s
      allowedNamespaces: [%s]
`, strconv.Quote("spiffe://cluster.local/ns/"+namespace+"/sa/"+serviceAccount), strconv.Quote(namespace), strconv.Quote(namespace))
}

func clonePostgres(p Postgres) Postgres {
	p.ExtraHBA = slices.Clone(p.ExtraHBA)
	p.ExtraSetup = slices.Clone(p.ExtraSetup)
	for i := range p.ExtraSetup {
		p.ExtraSetup[i].Args = slices.Clone(p.ExtraSetup[i].Args)
	}
	return p
}
