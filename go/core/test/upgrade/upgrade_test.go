// Package upgrade holds the database upgrade compatibility tests.
package upgrade

import (
	"bytes"
	"context"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	migrations "github.com/kagent-dev/kagent/go/core/pkg/migrations"
	"github.com/stretchr/testify/require"
)

const (
	postgresSelector   = "app.kubernetes.io/name=kagent,app.kubernetes.io/component=database"
	controllerSelector = "app.kubernetes.io/name=kagent,app.kubernetes.io/component=controller"

	postgresContainer   = "postgresql"
	controllerContainer = "controller"

	controllerServiceName = "kagent-controller"
	controllerAPIPort     = 8083
)

type upgradeEnv struct {
	repoRoot           string
	upgradeFromVersion string
	version            string
	dockerRegistry     string
	kindClusterName    string
	namespace          string
	kubeContext        string
	openAIAPIKey       string
}

type postgresMigrationState struct {
	version int
}

func TestUpgrade(t *testing.T) {
	if os.Getenv("RUN_UPGRADE_TESTS") != "true" {
		t.Skip("set RUN_UPGRADE_TESTS=true to run upgrade tests")
	}

	env := loadUpgradeEnv(t)
	seed := fmt.Sprintf("%d", time.Now().UnixNano())
	seedToolID := "upgrade-seed-tool-" + seed
	seedToolServerName := "upgrade-seed-toolserver-" + seed
	seedGroupKind := "upgrade.seed/v1/Canary"
	seedCanaryCounts := map[string]int{}
	seedCanaryQueries := map[string]string{
		"tool":       fmt.Sprintf("SELECT count(*) FROM tool WHERE id = %s AND server_name = %s AND group_kind = %s", pgQuote(seedToolID), pgQuote(seedToolServerName), pgQuote(seedGroupKind)),
		"toolserver": fmt.Sprintf("SELECT count(*) FROM toolserver WHERE name = %s AND group_kind = %s", pgQuote(seedToolServerName), pgQuote(seedGroupKind)),
	}
	// The controller image embeds the migration files. Comparing the DB
	// state to this version proves the upgraded pod actually applied the
	// migration set shipped in the target build.
	targetCoreVersion := latestCoreMigrationVersion(t)

	t.Logf("upgrade test: %s -> %s (registry=%s, kubeContext=%s)",
		env.upgradeFromVersion, env.version, env.dockerRegistry, env.kubeContext)
	waitForReadyPods(t, env, postgresSelector, 3*time.Minute)
	waitForPostgresSchema(t, env, 3*time.Minute)
	if !hasGooseMigrationTable(t, env) {
		t.Skip("the baseline release does not use Goose")
	}

	var pgBaselineState postgresMigrationState
	var baselineVectorVersion int
	var cleanPreviousSchema string

	if !t.Run("seed baseline data before upgrade", func(t *testing.T) {
		pgBaselineState = pgMigrationState(t, env)
		baselineVectorVersion = pgTrackVersion(t, env, "vector_schema_migrations")
		cleanPreviousSchema = pgSchemaDump(t, env, "kagent")
		t.Logf("baseline Postgres schema_migrations version: %d vector=%d (target=%d)",
			pgBaselineState.version, baselineVectorVersion, targetCoreVersion)

		// Seed a small cross-section of stable tables. These rows are not
		// meant to validate every future migration's semantics; they are canaries
		// for accidental table drops, destructive rewrites, and key/index changes
		// that lose existing customer data during an upgrade.
		pgExec(t, env, fmt.Sprintf("INSERT INTO tool (id, server_name, group_kind, description) VALUES (%s, %s, %s, 'upgrade canary tool')",
			pgQuote(seedToolID), pgQuote(seedToolServerName), pgQuote(seedGroupKind)))
		pgExec(t, env, fmt.Sprintf("INSERT INTO toolserver (name, group_kind, description) VALUES (%s, %s, 'upgrade canary toolserver')",
			pgQuote(seedToolServerName), pgQuote(seedGroupKind)))

		for table, query := range seedCanaryQueries {
			count := pgQueryInt(t, env, query)
			require.GreaterOrEqual(t, count, 1, "expected seeded %s canary row", table)
			seedCanaryCounts[table] = count
		}
	}) {
		return
	}

	previousGoDir := checkoutPreviousRelease(t, env)
	vectorEnabled := baselineVectorVersion > 0
	if !t.Run("apply target migrations", func(t *testing.T) {
		applyEmbeddedMigrations(t, env, "kagent", vectorEnabled)
		pgPostState := pgMigrationState(t, env)
		require.Equal(t, targetCoreVersion, pgPostState.version,
			"Postgres migrations did not reach the target embedded migration version")
	}) {
		return
	}

	if !t.Run("previous release serves against target migrations", func(t *testing.T) {
		runInvokeE2E(t, env, previousGoDir, "target migrations")
	}) {
		return
	}

	if !t.Run("previous release restarts against migrated schema", func(t *testing.T) {
		kubectl(t, env, time.Minute,
			"rollout", "restart", "deployment/kagent-controller",
			"-n", env.namespace,
		)
		kubectl(t, env, 3*time.Minute,
			"rollout", "status", "deployment/kagent-controller",
			"-n", env.namespace,
			"--timeout=3m",
		)
		pod := newestPodNameForSelector(t, env, controllerSelector)
		restarts := podContainerRestartCount(t, env, pod, controllerContainer)
		require.Zero(t, restarts,
			"previous-release controller pod %s crash-looped against the migrated schema", pod)
	}) {
		return
	}

	if !t.Run("upgrade with helm", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(t.Context(), 10*time.Minute)
		defer cancel()

		cmd := helmUpgradeCommand(ctx, env)
		out, err := cmd.CombinedOutput()
		require.NoError(t, err, "helm upgrade to current build failed:\n%s", string(out))
	}) {
		return
	}

	if !t.Run("verify controller rollout", func(t *testing.T) {
		kubectl(t, env, 3*time.Minute,
			"rollout", "status", "deployment/kagent-controller",
			"-n", env.namespace,
			"--timeout=3m",
		)

		// Wait for Postgres to be fully ready before rolling out a fresh controller pod.
		// The controller can crash on startup if Postgres isn't accepting connections yet
		// (e.g. due to a concurrent Postgres restart during the helm upgrade), which
		// would leave a non-zero restart count on the upgraded pod.
		waitForReadyPods(t, env, postgresSelector, 2*time.Minute)

		// Restart the controller now that Postgres is confirmed healthy, then verify
		// the fresh pod starts without any crashes.
		kubectl(t, env, time.Minute,
			"rollout", "restart", "deployment/kagent-controller",
			"-n", env.namespace,
		)
		kubectl(t, env, 3*time.Minute,
			"rollout", "status", "deployment/kagent-controller",
			"-n", env.namespace,
			"--timeout=3m",
		)

		pod := newestPodNameForSelector(t, env, controllerSelector)
		restarts := podContainerRestartCount(t, env, pod, controllerContainer)
		require.Zero(t, restarts, "kagent-controller pod %s restarted after upgrade", pod)
		t.Logf("kagent-controller %s restarts=%d", pod, restarts)
	}) {
		return
	}

	t.Run("verify seeded data survived migrations", func(t *testing.T) {
		pgPostState := pgMigrationState(t, env)
		require.GreaterOrEqual(t, pgPostState.version, pgBaselineState.version,
			"Postgres migration version regressed")
		require.Equal(t, targetCoreVersion, pgPostState.version,
			"Postgres migrations did not reach the target embedded migration version")
		t.Logf("Postgres migration version: %d -> %d",
			pgBaselineState.version, pgPostState.version)

		// Keep the schema invariant intentionally broad and cheap: core
		// tables should still exist before we ask more specific questions about
		// the seeded rows below.
		requirePostgresTablesExist(t, env,
			"tool",
			"toolserver",
		)

		for table, before := range seedCanaryCounts {
			// The generic canaries only assert non-regression. Future migrations
			// that intentionally transform data should still add targeted
			// assertions for their expected post-upgrade shape.
			require.GreaterOrEqual(t, pgQueryInt(t, env, seedCanaryQueries[table]), before,
				"%s canary row count decreased across upgrade", table)
		}
	})

	if !t.Run("verify upgraded schema matches a clean install", func(t *testing.T) {
		// Build an independent clean install of the current build's migrations
		// and require the upgraded database to be structurally identical. This
		// catches upgrade paths that leave residue a fresh install would not.
		cleanHeadSchema := buildCleanInstallSchema(t, env, "clean_head_"+seed, vectorEnabled)
		upgradedSchema := pgSchemaDump(t, env, "kagent")
		require.Equal(t, cleanHeadSchema, upgradedSchema,
			"upgraded schema diverged from a clean install of the current build")
	}) {
		return
	}

	if !t.Run("post-upgrade invoke (HEAD)", func(t *testing.T) {
		// The HEAD controller is serving on the migrated schema. Exercise the
		// current code's real query paths against it (deploy + invoke an agent),
		// not just the psql-level checks above.
		runInvokeE2E(t, env, filepath.Join(env.repoRoot, "go"), "post-upgrade")
	}) {
		return
	}

	if !t.Run("roll back application to previous release", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(t.Context(), 10*time.Minute)
		defer cancel()

		out, err := installPreviousReleaseCommand(ctx, env).CombinedOutput()
		require.NoError(t, err, "rolling back to the previous release failed:\n%s", string(out))
		kubectl(t, env, 3*time.Minute,
			"rollout", "status", "deployment/kagent-controller",
			"-n", env.namespace,
			"--timeout=3m",
		)
		pod := newestPodNameForSelector(t, env, controllerSelector)
		require.Zero(t, podContainerRestartCount(t, env, pod, controllerContainer),
			"previous-release controller pod %s crash-looped against the target schema", pod)
	}) {
		return
	}

	if !t.Run("previous release invokes against target schema", func(t *testing.T) {
		runInvokeE2E(t, env, previousGoDir, "application rollback")
	}) {
		return
	}

	if !t.Run("reverse schema to previous release", func(t *testing.T) {
		scaleController(t, env, 0)
		migrateEmbeddedSourcesTo(t, env, map[string]int{
			"core":   pgBaselineState.version,
			"vector": baselineVectorVersion,
		}, vectorEnabled)

		require.Equal(t, pgBaselineState.version, pgMigrationState(t, env).version,
			"core migrations did not return to the previous-release version")
		require.Equal(t, baselineVectorVersion, pgTrackVersion(t, env, "vector_schema_migrations"),
			"vector migrations did not return to the previous-release version")
		require.Equal(t, cleanPreviousSchema, pgSchemaDump(t, env, "kagent"),
			"reversed schema diverged from a clean previous-release install")
		for table, before := range seedCanaryCounts {
			require.GreaterOrEqual(t, pgQueryInt(t, env, seedCanaryQueries[table]), before,
				"%s row count decreased during schema rollback", table)
		}
	}) {
		return
	}

	t.Run("previous release invokes after schema rollback", func(t *testing.T) {
		scaleController(t, env, 1)
		runInvokeE2E(t, env, previousGoDir, "schema rollback")
	})
}

// helmUpgradeCommand returns the command that upgrades the cluster from the
// previously-installed release to the current local build. It reuses the repo's
// helm-install-provider target, which packages the local charts and runs
// `helm upgrade --install` of kagent-crds and kagent against the locally-built
// images (registry=DOCKER_REGISTRY, tag=VERSION).
func helmUpgradeCommand(ctx context.Context, env upgradeEnv) *exec.Cmd {
	cmd := exec.CommandContext(ctx, "make", "-C", env.repoRoot, "helm-install-provider")
	cmd.Dir = env.repoRoot
	cmd.Env = append(os.Environ(),
		"VERSION="+env.version,
		"DOCKER_REGISTRY="+env.dockerRegistry,
		"KIND_CLUSTER_NAME="+env.kindClusterName,
		"OPENAI_API_KEY="+env.openAIAPIKey,
		"KAGENT_DEFAULT_MODEL_PROVIDER=openAI",
	)
	return cmd
}

func installPreviousReleaseCommand(ctx context.Context, env upgradeEnv) *exec.Cmd {
	cmd := exec.CommandContext(ctx, "make", "-C", env.repoRoot, "install-previous-release")
	cmd.Dir = env.repoRoot
	cmd.Env = append(os.Environ(),
		"UPGRADE_FROM_VERSION="+env.upgradeFromVersion,
		"KIND_CLUSTER_NAME="+env.kindClusterName,
		"OPENAI_API_KEY="+env.openAIAPIKey,
	)
	return cmd
}

func loadUpgradeEnv(t *testing.T) upgradeEnv {
	t.Helper()

	// Resolve the repo root. Prefer an explicit REPO_ROOT (set by the make
	// targets), otherwise ask git: this is location-independent, so moving this
	// file cannot silently point make at the wrong root, and git is already a hard
	// dependency of this flow (the make targets derive versions from git tags).
	repoRoot := os.Getenv("REPO_ROOT")
	if repoRoot == "" {
		out, err := exec.CommandContext(t.Context(), "git", "rev-parse", "--show-toplevel").Output()
		require.NoError(t, err, "resolve repo root via `git rev-parse --show-toplevel`; set REPO_ROOT to override")
		repoRoot = strings.TrimSpace(string(out))
	}

	// Fail clearly here rather than letting `make -C <repoRoot> ...` fail with a
	// confusing "no rule to make target" if the root resolved wrong.
	_, err := os.Stat(filepath.Join(repoRoot, "Makefile"))
	require.NoError(t, err, "resolved repo root %q has no Makefile; set REPO_ROOT", repoRoot)

	clusterName := envOrDefault("KIND_CLUSTER_NAME", "kagent")
	return upgradeEnv{
		repoRoot:           repoRoot,
		upgradeFromVersion: requireEnv(t, "UPGRADE_FROM_VERSION"),
		version:            requireEnv(t, "VERSION"),
		dockerRegistry:     envOrDefault("DOCKER_REGISTRY", "localhost:5001"),
		kindClusterName:    clusterName,
		namespace:          envOrDefault("NAMESPACE", "kagent"),
		kubeContext:        envOrDefault("KUBE_CONTEXT", "kind-"+clusterName),
		openAIAPIKey:       envOrDefault("OPENAI_API_KEY", "fake"),
	}
}

func requireEnv(t *testing.T, key string) string {
	t.Helper()

	val := os.Getenv(key)
	require.NotEmpty(t, val, "%s must be set", key)
	return val
}

func envOrDefault(key, fallback string) string {
	if val := os.Getenv(key); val != "" {
		return val
	}
	return fallback
}

func waitForReadyPods(t *testing.T, env upgradeEnv, selector string, timeout time.Duration) {
	t.Helper()

	kubectl(t, env, timeout,
		"wait", "--for=condition=Ready", "pod",
		"-l", selector,
		"-n", env.namespace,
		fmt.Sprintf("--timeout=%s", timeout),
	)
}

func waitForPostgresSchema(t *testing.T, env upgradeEnv, timeout time.Duration) {
	t.Helper()

	require.Eventually(t, func() bool {
		out, err := pgQueryE(t, env, "SELECT to_regclass('public.tool') IS NOT NULL")
		return err == nil && out == "t"
	}, timeout, 5*time.Second, "baseline Postgres schema did not appear")
}

func pgExec(t *testing.T, env upgradeEnv, query string) {
	t.Helper()
	_ = pgQuery(t, env, query)
}

func pgQueryInt(t *testing.T, env upgradeEnv, query string) int {
	t.Helper()
	return parseInt(t, pgQuery(t, env, query), query)
}

func pgQuery(t *testing.T, env upgradeEnv, query string) string {
	t.Helper()

	out, err := pgQueryE(t, env, query)
	require.NoError(t, err, "psql query failed: %s", query)
	return out
}

// pgQueryE is the error-returning core of pgQuery. Condition functions passed to
// require.Eventually must use this form (returning false on error) rather than
// pgQuery: testify runs the condition in a separate goroutine, where require's
// t.FailNow would be dropped silently, turning a transient kubectl/psql failure
// into a misleading poll timeout instead of a clear failure.
func pgQueryE(t *testing.T, env upgradeEnv, query string) (string, error) {
	pod, err := podNameForSelectorE(t, env, postgresSelector)
	if err != nil {
		return "", err
	}
	out, err := kubectlOutput(t, env, time.Minute,
		"exec", "-n", env.namespace, pod, "-c", postgresContainer, "--",
		"psql", "-v", "ON_ERROR_STOP=1", "-U", "kagent", "-d", "kagent", "-tAc", query,
	)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(out), nil
}

func pgMigrationState(t *testing.T, env upgradeEnv) postgresMigrationState {
	t.Helper()

	state, err := pgMigrationStateE(t, env)
	require.NoError(t, err)
	return state
}

// pgMigrationStateE is the error-returning core of pgMigrationState, for use
// inside require.Eventually conditions (see pgQueryE).
func pgMigrationStateE(t *testing.T, env upgradeEnv) (postgresMigrationState, error) {
	raw, err := pgQueryE(t, env, "SELECT CASE WHEN to_regclass('public.schema_migrations') IS NULL THEN 0 ELSE (SELECT COALESCE(MAX(version_id), 0) FROM public.schema_migrations WHERE is_applied) END")
	if err != nil {
		return postgresMigrationState{}, err
	}
	version, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil {
		return postgresMigrationState{}, fmt.Errorf("parse Goose migration version %q: %w", raw, err)
	}
	return postgresMigrationState{version: version}, nil
}

func hasGooseMigrationTable(t *testing.T, env upgradeEnv) bool {
	t.Helper()
	return pgQuery(t, env, "SELECT EXISTS(SELECT 1 FROM information_schema.columns WHERE table_schema = 'public' AND table_name = 'schema_migrations' AND column_name = 'version_id')") == "t"
}

func requirePostgresTablesExist(t *testing.T, env upgradeEnv, tables ...string) {
	t.Helper()

	for _, table := range tables {
		exists := pgQuery(t, env, fmt.Sprintf("SELECT to_regclass(%s) IS NOT NULL", pgQuote("public."+table)))
		require.Equal(t, "t", exists, "expected public.%s to exist after upgrade", table)
	}
}

func podNameForSelector(t *testing.T, env upgradeEnv, selector string) string {
	t.Helper()

	pod, err := podNameForSelectorE(t, env, selector)
	require.NoError(t, err)
	return pod
}

// podNameForSelectorE is the error-returning core of podNameForSelector, for use
// inside require.Eventually conditions (see pgQueryE).
//
// Newest pod: an upgrade that changes the bundled Postgres pod spec (e.g. an
// image tag bump) recreates the pod, and the old Terminating pod can still
// match the selector for its whole grace period. Sorting by creation time and
// taking the last entry always targets the replacement pod.
func podNameForSelectorE(t *testing.T, env upgradeEnv, selector string) (string, error) {
	out, err := kubectlOutput(t, env, time.Minute,
		"get", "pods",
		"-n", env.namespace,
		"-l", selector,
		"--sort-by=.metadata.creationTimestamp",
		"-o", "jsonpath={.items[-1].metadata.name}",
	)
	if err != nil {
		return "", err
	}
	pod := strings.TrimSpace(out)
	if pod == "" {
		return "", fmt.Errorf("no pod matched selector %q in namespace %s", selector, env.namespace)
	}
	return pod, nil
}

func newestPodNameForSelector(t *testing.T, env upgradeEnv, selector string) string {
	t.Helper()

	out := kubectl(t, env, time.Minute,
		"get", "pods",
		"-n", env.namespace,
		"-l", selector,
		"--sort-by=.metadata.creationTimestamp",
		"-o", "jsonpath={.items[-1].metadata.name}",
	)
	pod := strings.TrimSpace(out)
	require.NotEmpty(t, pod, "no pod matched selector %q in namespace %s", selector, env.namespace)
	return pod
}

func podContainerRestartCount(t *testing.T, env upgradeEnv, pod, container string) int {
	t.Helper()

	out := kubectl(t, env, time.Minute,
		"get", "pod", pod,
		"-n", env.namespace,
		"-o", fmt.Sprintf("jsonpath={.status.containerStatuses[?(@.name==%q)].restartCount}", container),
	)
	if strings.TrimSpace(out) == "" {
		return 0
	}
	return parseInt(t, out, "container restart count")
}

func kubectl(t *testing.T, env upgradeEnv, timeout time.Duration, args ...string) string {
	t.Helper()

	out, err := kubectlOutput(t, env, timeout, args...)
	require.NoError(t, err, "kubectl %s failed:\n%s", strings.Join(append([]string{"--context", env.kubeContext}, args...), " "), out)
	return out
}

func kubectlOutput(t *testing.T, env upgradeEnv, timeout time.Duration, args ...string) (string, error) {
	t.Helper()

	ctx, cancel := context.WithTimeout(t.Context(), timeout)
	defer cancel()

	fullArgs := append([]string{"--context", env.kubeContext}, args...)
	cmd := exec.CommandContext(ctx, "kubectl", fullArgs...)
	cmd.Dir = env.repoRoot

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	if err != nil {
		return stdout.String(), fmt.Errorf("kubectl %s: %w\nstderr: %s", strings.Join(fullArgs, " "), err, stderr.String())
	}
	return stdout.String(), nil
}

func parseInt(t *testing.T, raw, description string) int {
	t.Helper()

	n, err := strconv.Atoi(strings.TrimSpace(raw))
	require.NoError(t, err, "parse integer from %s output %q", description, raw)
	return n
}

func latestCoreMigrationVersion(t *testing.T) int {
	t.Helper()

	entries, err := fs.ReadDir(migrations.FS, "core")
	require.NoError(t, err, "read embedded core migrations")

	maxVersion := 0
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".sql") {
			continue
		}
		versionPart, _, ok := strings.Cut(name, "_")
		require.True(t, ok, "migration file %q should start with a version prefix", name)
		version := parseInt(t, versionPart, name)
		if version > maxVersion {
			maxVersion = version
		}
	}
	require.NotZero(t, maxVersion, "expected at least one embedded core migration")
	return maxVersion
}

func pgQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "''") + "'"
}
