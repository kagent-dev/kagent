package upgrade

import (
	"bufio"
	"context"
	"fmt"
	"os/exec"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	migrations "github.com/kagent-dev/kagent/go/core/pkg/migrations"
	"github.com/pressly/goose/v3"
	"github.com/stretchr/testify/require"
)

const postgresServiceName = "kagent-postgresql"

// pgTrackVersion returns the current applied migration version.
func pgTrackVersion(t *testing.T, env upgradeEnv, table string) int {
	t.Helper()

	raw := pgQuery(t, env, fmt.Sprintf(
		"SELECT CASE WHEN to_regclass('public.%s') IS NULL THEN 0 ELSE (SELECT COALESCE(MAX(version_id), 0) FROM public.%s WHERE is_applied) END",
		table, table))
	return parseInt(t, raw, table+" version")
}

// pgExecDB runs a statement against an arbitrary database on the bundled
// Postgres (pgExec always targets the kagent database). Used to create and drop
// the throwaway database that holds the clean-install reference schema.
func pgExecDB(t *testing.T, env upgradeEnv, database, query string) {
	t.Helper()

	pod := podNameForSelector(t, env, postgresSelector)
	kubectl(t, env, time.Minute,
		"exec", "-n", env.namespace, pod, "-c", postgresContainer, "--",
		"psql", "-v", "ON_ERROR_STOP=1", "-U", "kagent", "-d", database, "-tAc", query,
	)
}

// pgSchemaDump returns the normalized schema-only dump of a database, suitable
// for structural equality comparison between a migrated and a clean install.
func pgSchemaDump(t *testing.T, env upgradeEnv, database string) string {
	t.Helper()

	pod := podNameForSelector(t, env, postgresSelector)
	out := kubectl(t, env, 2*time.Minute,
		"exec", "-n", env.namespace, pod, "-c", postgresContainer, "--",
		"pg_dump", "--schema-only", "--no-owner", "--no-privileges",
		"-U", "kagent", "-d", database,
	)
	return normalizeSchemaDump(out)
}

// normalizeSchemaDump strips the non-structural noise from a pg_dump so two
// dumps of the same logical schema compare equal: comments, blank lines, the
// session-setup preamble (SET / SELECT pg_catalog.set_config), and psql
// meta-commands. The latter matters because pg_dump on PostgreSQL 17+ wraps the
// output in `\restrict`/`\unrestrict` lines carrying a per-invocation random
// token, so two dumps of an identical schema would otherwise never compare equal.
func normalizeSchemaDump(dump string) string {
	lines := strings.Split(dump, "\n")
	kept := make([]string, 0, len(lines))
	for _, line := range lines {
		trimmed := strings.TrimRight(line, " \t")
		if trimmed == "" {
			continue
		}
		if strings.HasPrefix(trimmed, "--") {
			continue
		}
		if strings.HasPrefix(trimmed, "SET ") {
			continue
		}
		if strings.HasPrefix(trimmed, "SELECT pg_catalog.set_config") {
			continue
		}
		// psql meta-commands (\restrict, \unrestrict, \connect): non-structural,
		// and the restrict tokens are randomized per dump.
		if strings.HasPrefix(trimmed, `\`) {
			continue
		}
		kept = append(kept, trimmed)
	}
	return strings.Join(kept, "\n")
}

// buildCleanInstallSchema provisions a throwaway database, applies the embedded
// migrations up to the latest version via the real RunUp code path, and
// returns its normalized schema. This is the "clean HEAD install" reference the
// design's round-trip gate compares an upgraded database against.
func buildCleanInstallSchema(t *testing.T, env upgradeEnv, dbName string, vectorEnabled bool) string {
	t.Helper()

	pgExecDB(t, env, "kagent", fmt.Sprintf("DROP DATABASE IF EXISTS %s WITH (FORCE)", dbName))
	pgExecDB(t, env, "kagent", "CREATE DATABASE "+dbName)
	// Best-effort drop with its own context: t.Context() is already canceled by
	// the time cleanups run, so a normal kubectl call here would always error.
	t.Cleanup(func() { dropDatabaseBestEffort(env, dbName) })

	applyEmbeddedMigrations(t, env, dbName, vectorEnabled)

	return pgSchemaDump(t, env, dbName)
}

func applyEmbeddedMigrations(t *testing.T, env upgradeEnv, database string, vectorEnabled bool) {
	t.Helper()

	localPort, stop := startPortForward(t, env, postgresServiceName, 5432)
	defer stop()

	url := fmt.Sprintf("postgres://kagent:kagent@127.0.0.1:%d/%s?sslmode=disable", localPort, database)
	require.NoError(t, migrations.RunUp(t.Context(), url, migrations.BuiltinSources(vectorEnabled)),
		"apply embedded migrations to database %s", database)
}

func migrateEmbeddedSourcesTo(t *testing.T, env upgradeEnv, targets map[string]int, vectorEnabled bool) {
	t.Helper()

	localPort, stop := startPortForward(t, env, postgresServiceName, 5432)
	defer stop()
	url := fmt.Sprintf("postgres://kagent:kagent@127.0.0.1:%d/kagent?sslmode=disable", localPort)

	for _, source := range slices.Backward(migrations.BuiltinSources(vectorEnabled)) {
		target, ok := targets[source.Name]
		require.True(t, ok, "missing rollback target for migration source %s", source.Name)
		err := migrations.WithProvider(t.Context(), url, source, func(provider *goose.Provider) error {
			_, err := provider.DownTo(t.Context(), int64(target))
			return err
		})
		require.NoError(t, err, "roll back %s migrations to version %d", source.Name, target)
	}
}

func scaleController(t *testing.T, env upgradeEnv, replicas int) {
	t.Helper()

	kubectl(t, env, 2*time.Minute,
		"scale", "deployment/kagent-controller",
		"-n", env.namespace,
		fmt.Sprintf("--replicas=%d", replicas),
	)
	if replicas == 0 {
		require.Eventually(t, func() bool {
			pods, err := podNamesForSelectorE(t, env, controllerSelector)
			return err == nil && len(pods) == 0
		}, 2*time.Minute, 2*time.Second, "controller pods did not terminate after scale to zero")
		return
	}
	kubectl(t, env, 3*time.Minute,
		"rollout", "status", "deployment/kagent-controller",
		"-n", env.namespace,
		"--timeout=3m",
	)
}

// dropDatabaseBestEffort removes a scratch database, ignoring all errors. It
// uses its own background context so it still runs during test teardown, after
// t.Context() has been canceled, and never fails the test.
func dropDatabaseBestEffort(env upgradeEnv, database string) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	podOut, err := exec.CommandContext(ctx, "kubectl",
		"--context", env.kubeContext, "get", "pods",
		"-n", env.namespace, "-l", postgresSelector,
		"-o", "jsonpath={.items[0].metadata.name}",
	).Output()
	if err != nil {
		return
	}
	pod := strings.TrimSpace(string(podOut))
	if pod == "" {
		return
	}
	_ = exec.CommandContext(ctx, "kubectl",
		"--context", env.kubeContext, "exec", "-n", env.namespace, pod, "-c", postgresContainer, "--",
		"psql", "-U", "kagent", "-d", "kagent", "-tAc",
		fmt.Sprintf("DROP DATABASE IF EXISTS %s WITH (FORCE)", database),
	).Run()
}

var forwardingPortRE = regexp.MustCompile(`Forwarding from 127\.0\.0\.1:(\d+)`)

// startPortForward opens a kubectl port-forward to a service and returns the
// chosen local port and a stop function.
func startPortForward(t *testing.T, env upgradeEnv, service string, remotePort int) (int, func()) {
	t.Helper()

	ctx, cancel := context.WithCancel(context.Background())
	cmd := exec.CommandContext(ctx, "kubectl",
		"--context", env.kubeContext,
		"port-forward",
		"-n", env.namespace,
		"svc/"+service,
		fmt.Sprintf(":%d", remotePort),
	)
	stdout, err := cmd.StdoutPipe()
	require.NoError(t, err, "pipe port-forward stdout")
	require.NoError(t, cmd.Start(), "start port-forward")

	stop := func() {
		cancel()
		_ = cmd.Wait()
	}

	portCh := make(chan int, 1)
	go func() {
		scanner := bufio.NewScanner(stdout)
		for scanner.Scan() {
			if match := forwardingPortRE.FindStringSubmatch(scanner.Text()); match != nil {
				p, convErr := strconv.Atoi(match[1])
				if convErr == nil {
					select {
					case portCh <- p:
					default:
					}
				}
			}
		}
	}()

	select {
	case port := <-portCh:
		return port, stop
	case <-time.After(15 * time.Second):
		stop()
		t.Fatalf("port-forward to svc/%s did not become ready", service)
		return 0, func() {}
	}
}
