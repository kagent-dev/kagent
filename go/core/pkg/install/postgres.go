package install

import (
	"context"
	_ "embed"
	"fmt"
	"strings"

	"github.com/google/go-containerregistry/pkg/name"
	substratesetup "go.ate.dev/substrate/pkg/postgressetup"
	"k8s.io/apimachinery/pkg/util/validation"

	kagentsetup "github.com/kagent-dev/kagent/go/core/pkg/postgressetup"
)

const substrateNamespace = "ate-system"

//go:embed bundled_postgres.yaml
var bundledPostgresYAML string

// SQLScript supplies SQL and psql variables, without starting a transaction.
type SQLScript struct {
	SQL  string
	Args []string
}

// Postgres describes the bundled database. ExtraSetup runs after core and
// Substrate setup, in the same transaction. ExtraHBA admits additional application logins.
type Postgres struct {
	Namespace  string
	Image      string
	ExtraHBA   []string
	ExtraSetup []SQLScript
}

var _ Step = Postgres{}

func (Postgres) installStep() {}

func (Postgres) Description() string { return "Preparing bundled PostgreSQL" }

// Manifest renders the database objects without contacting the cluster.
func (p Postgres) Manifest() (string, error) {
	if problems := validation.IsDNS1123Label(p.Namespace); len(problems) != 0 {
		return "", fmt.Errorf("invalid namespace %q: %s", p.Namespace, strings.Join(problems, ", "))
	}
	if _, err := name.ParseReference(p.Image); err != nil {
		return "", fmt.Errorf("invalid PostgreSQL image %q: %w", p.Image, err)
	}
	const hba = "hostssl kagent " + kagentsetup.OwnerUser + "," + kagentsetup.ReadWriteUser + ",substrate_owner_user,substrate_readwrite_user all cert"
	manifest := bundledPostgresYAML
	if len(p.ExtraHBA) > 0 {
		if !strings.Contains(manifest, "    "+hba+"\n") {
			return "", fmt.Errorf("bundled PostgreSQL manifest no longer contains the application pg_hba.conf line")
		}
		manifest = strings.Replace(manifest, hba+"\n", hba+"\n    "+strings.Join(p.ExtraHBA, "\n    ")+"\n", 1)
	}
	return strings.NewReplacer("${NAMESPACE}", p.Namespace,
		"${SUBSTRATE_NAMESPACE}", substrateNamespace, "${POSTGRES_IMAGE}", p.Image).Replace(manifest), nil
}

// SetupScript returns core, Substrate and additional application setup in one transaction.
func (p Postgres) SetupScript() SQLScript {
	var sql strings.Builder
	sql.WriteString("BEGIN;\n" + kagentsetup.SQL() + "\n" + substratesetup.SQL() + "\n")
	args := append(kagentsetup.DefaultConfig().PSQLArgs(), substratesetup.DefaultConfig().PSQLArgs()...)
	for _, script := range p.ExtraSetup {
		sql.WriteString(script.SQL + "\n")
		args = append(args, script.Args...)
	}
	sql.WriteString("COMMIT;\n")
	return SQLScript{SQL: sql.String(), Args: args}
}

// PreparePostgres deploys the database, waits for readiness and prepares identities.
func (c *Client) PreparePostgres(ctx context.Context, p Postgres) error {
	manifest, err := p.Manifest()
	if err != nil {
		return err
	}
	if _, err := c.kubectl(ctx, manifest, "apply", "--server-side", "--field-manager=kagent-cli", "-f", "-"); err != nil {
		return fmt.Errorf("failed to deploy bundled PostgreSQL: %w", err)
	}
	if _, err := c.kubectl(ctx, "", "-n", p.Namespace, "rollout", "status", "deployment/kagent-postgresql", "--timeout=5m"); err != nil {
		return fmt.Errorf("failed to wait for bundled PostgreSQL: %w", err)
	}
	return c.RunSQL(ctx, p.Namespace, "kagent", p.SetupScript())
}

// RunSQL executes a setup script through the database pod's local superuser socket.
// Embedding applications can prepare additional databases without CLI wrappers.
func (c *Client) RunSQL(ctx context.Context, namespace, database string, script SQLScript) error {
	args := []string{"-n", namespace, "exec", "-i", "deployment/kagent-postgresql", "-c", "postgresql", "--",
		"psql", "--no-psqlrc", "--set=ON_ERROR_STOP=1", "--username", "postgres", "--dbname", database}
	if _, err := c.runner.Run(ctx, Command{Program: "kubectl", Args: append(args, script.Args...), Stdin: script.SQL}); err != nil {
		return fmt.Errorf("failed to prepare PostgreSQL database %s: %w", database, err)
	}
	return nil
}

func (c *Client) bundledPostgresExists(ctx context.Context, namespace string) (bool, error) {
	out, err := c.kubectl(ctx, "", "-n", namespace, "get", "deployment/kagent-postgresql", "--ignore-not-found", "-o", "name")
	if err != nil {
		return false, fmt.Errorf("failed to check for bundled PostgreSQL: %w", err)
	}
	return strings.TrimSpace(out) != "", nil
}

func (c *Client) deleteBundledPostgres(ctx context.Context, namespace, image string) error {
	manifest, err := (Postgres{Namespace: namespace, Image: image}).Manifest()
	if err != nil {
		return err
	}
	if _, err := c.kubectl(ctx, manifest, "delete", "--ignore-not-found", "-f", "-"); err != nil {
		return fmt.Errorf("failed to delete bundled PostgreSQL: %w", err)
	}
	return nil
}
