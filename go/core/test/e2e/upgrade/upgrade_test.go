package upgrade

import (
	"bytes"
	"context"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
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
	dirty   bool
}

func TestUpgrade(t *testing.T) {
	if os.Getenv("RUN_UPGRADE_TESTS") != "true" {
		t.Skip("set RUN_UPGRADE_TESTS=true to run upgrade tests")
	}

	env := loadUpgradeEnv(t)
	seed := fmt.Sprintf("%d", time.Now().UnixNano())
	seedAgentID := "upgrade-seed-agent-" + seed
	seedUserID := "upgrade-seed-user-" + seed
	seedSessionID := "upgrade-seed-session-" + seed
	seedEventID := "upgrade-seed-event-" + seed
	seedTaskID := "upgrade-seed-task-" + seed
	seedPushID := "upgrade-seed-push-" + seed
	seedToolID := "upgrade-seed-tool-" + seed
	seedToolServerName := "upgrade-seed-toolserver-" + seed
	seedGroupKind := "upgrade.seed/v1/Canary"
	seedCanaryCounts := map[string]int{}
	// The controller image embeds the migration files. Comparing the DB
	// state to this version proves the upgraded pod actually applied the
	// migration set shipped in the target build.
	targetCoreVersion := latestCoreMigrationVersion(t)

	t.Logf("upgrade test: %s -> %s (registry=%s, kubeContext=%s)",
		env.upgradeFromVersion, env.version, env.dockerRegistry, env.kubeContext)

	var pgBaselineState postgresMigrationState
	var baselineVectorVersion int
	// cleanTargetSchema is the previous release's freshly-installed schema. The
	// rollback round-trip below asserts the reversed database matches it exactly.
	var cleanTargetSchema string
	// cleanHeadSchema is an independent clean install of the current build's
	// migrations. The post-upgrade database must match it exactly.
	var cleanHeadSchema string

	if !t.Run("seed baseline data before upgrade", func(t *testing.T) {
		waitForReadyPods(t, env, postgresSelector, 3*time.Minute)
		waitForPostgresAgentTable(t, env, 3*time.Minute)

		// A dirty baseline means a previous migration failed; continuing would
		// make any post-upgrade failure ambiguous rather than a regression signal.
		pgBaselineState = pgMigrationState(t, env)
		require.False(t, pgBaselineState.dirty, "baseline Postgres migrations are dirty")
		baselineVectorVersion = pgTrackVersion(t, env, "vector_schema_migrations")
		t.Logf("baseline Postgres schema_migrations version: %d dirty=%t vector=%d (target=%d)",
			pgBaselineState.version, pgBaselineState.dirty, baselineVectorVersion, targetCoreVersion)

		// Capture the clean target schema before any seeding or upgrade. The
		// freshly-installed previous release is, by definition, a clean target
		// install, so its schema is the reversal reference. schema-only dumps
		// exclude row data, so seeding afterward does not perturb it.
		cleanTargetSchema = pgSchemaDump(t, env, "kagent")

		// Seed a small cross-section of stable tables. These rows are not
		// meant to validate every future migration's semantics; they are canaries
		// for accidental table drops, destructive rewrites, and key/index changes
		// that lose existing customer data during an upgrade.
		pgExec(t, env, fmt.Sprintf("INSERT INTO agent (id, type) VALUES (%s, 'Deployment')", pgQuote(seedAgentID)))
		pgExec(t, env, fmt.Sprintf("INSERT INTO session (id, user_id, name, agent_id, source) VALUES (%s, %s, 'upgrade canary session', %s, 'upgrade-test')",
			pgQuote(seedSessionID), pgQuote(seedUserID), pgQuote(seedAgentID)))
		pgExec(t, env, fmt.Sprintf("INSERT INTO event (id, user_id, session_id, data) VALUES (%s, %s, %s, '{}')",
			pgQuote(seedEventID), pgQuote(seedUserID), pgQuote(seedSessionID)))
		pgExec(t, env, fmt.Sprintf("INSERT INTO task (id, session_id, data) VALUES (%s, %s, '{}')",
			pgQuote(seedTaskID), pgQuote(seedSessionID)))
		pgExec(t, env, fmt.Sprintf("INSERT INTO push_notification (id, task_id, data) VALUES (%s, %s, '{}')",
			pgQuote(seedPushID), pgQuote(seedTaskID)))
		pgExec(t, env, fmt.Sprintf("INSERT INTO feedback (user_id, feedback_text) VALUES (%s, 'pre-upgrade feedback')", pgQuote(seedUserID)))
		pgExec(t, env, fmt.Sprintf("INSERT INTO tool (id, server_name, group_kind, description) VALUES (%s, %s, %s, 'upgrade canary tool')",
			pgQuote(seedToolID), pgQuote(seedToolServerName), pgQuote(seedGroupKind)))
		pgExec(t, env, fmt.Sprintf("INSERT INTO toolserver (name, group_kind, description) VALUES (%s, %s, 'upgrade canary toolserver')",
			pgQuote(seedToolServerName), pgQuote(seedGroupKind)))

		seedAgents := pgQueryInt(t, env, fmt.Sprintf("SELECT count(*) FROM agent WHERE id = %s", pgQuote(seedAgentID)))
		require.GreaterOrEqual(t, seedAgents, 1, "expected seeded agent row")
		t.Logf("seeded agent rows: %d", seedAgents)

		seedCanaryCounts = map[string]int{
			"agent":             pgQueryInt(t, env, fmt.Sprintf("SELECT count(*) FROM agent WHERE id = %s", pgQuote(seedAgentID))),
			"session":           pgQueryInt(t, env, fmt.Sprintf("SELECT count(*) FROM session WHERE id = %s AND user_id = %s", pgQuote(seedSessionID), pgQuote(seedUserID))),
			"event":             pgQueryInt(t, env, fmt.Sprintf("SELECT count(*) FROM event WHERE id = %s AND user_id = %s", pgQuote(seedEventID), pgQuote(seedUserID))),
			"task":              pgQueryInt(t, env, fmt.Sprintf("SELECT count(*) FROM task WHERE id = %s", pgQuote(seedTaskID))),
			"push_notification": pgQueryInt(t, env, fmt.Sprintf("SELECT count(*) FROM push_notification WHERE id = %s", pgQuote(seedPushID))),
			"feedback":          pgQueryInt(t, env, fmt.Sprintf("SELECT count(*) FROM feedback WHERE user_id = %s", pgQuote(seedUserID))),
			"tool":              pgQueryInt(t, env, fmt.Sprintf("SELECT count(*) FROM tool WHERE id = %s AND server_name = %s AND group_kind = %s", pgQuote(seedToolID), pgQuote(seedToolServerName), pgQuote(seedGroupKind))),
			"toolserver":        pgQueryInt(t, env, fmt.Sprintf("SELECT count(*) FROM toolserver WHERE name = %s AND group_kind = %s", pgQuote(seedToolServerName), pgQuote(seedGroupKind))),
		}
		for table, count := range seedCanaryCounts {
			require.GreaterOrEqual(t, count, 1, "expected seeded %s canary row", table)
		}
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
		// These checks are migration plumbing checks: the version cannot regress,
		// the migration table must be clean, and the upgraded controller must
		// have reached the latest core migration embedded in this test build.
		pgPostState := pgMigrationState(t, env)
		require.False(t, pgPostState.dirty, "post-upgrade Postgres migrations are dirty")
		require.GreaterOrEqual(t, pgPostState.version, pgBaselineState.version,
			"Postgres migration version regressed")
		require.Equal(t, targetCoreVersion, pgPostState.version,
			"Postgres migrations did not reach the target embedded migration version")
		t.Logf("Postgres schema_migrations version: %d -> %d dirty=%t",
			pgBaselineState.version, pgPostState.version, pgPostState.dirty)

		// Keep the schema invariant intentionally broad and cheap: core
		// tables should still exist before we ask more specific questions about
		// the seeded rows below.
		requirePostgresTablesExist(t, env,
			"agent",
			"session",
			"event",
			"task",
			"push_notification",
			"feedback",
			"tool",
			"toolserver",
		)

		postAgents := pgQueryInt(t, env,
			fmt.Sprintf("SELECT count(*) FROM agent WHERE id = %s AND workload_type = 'deployment'", pgQuote(seedAgentID)))
		require.GreaterOrEqual(t, postAgents, 1,
			"seeded agent row missing or not backfilled to workload_type='deployment' after upgrade")

		postFeedback := pgQueryInt(t, env,
			fmt.Sprintf("SELECT count(*) FROM feedback WHERE user_id = %s", pgQuote(seedUserID)))
		require.GreaterOrEqual(t, postFeedback, 1, "seeded feedback row did not survive the upgrade migrations")

		postCanaryCounts := map[string]int{
			"agent":             pgQueryInt(t, env, fmt.Sprintf("SELECT count(*) FROM agent WHERE id = %s", pgQuote(seedAgentID))),
			"session":           pgQueryInt(t, env, fmt.Sprintf("SELECT count(*) FROM session WHERE id = %s AND user_id = %s", pgQuote(seedSessionID), pgQuote(seedUserID))),
			"event":             pgQueryInt(t, env, fmt.Sprintf("SELECT count(*) FROM event WHERE id = %s AND user_id = %s", pgQuote(seedEventID), pgQuote(seedUserID))),
			"task":              pgQueryInt(t, env, fmt.Sprintf("SELECT count(*) FROM task WHERE id = %s", pgQuote(seedTaskID))),
			"push_notification": pgQueryInt(t, env, fmt.Sprintf("SELECT count(*) FROM push_notification WHERE id = %s", pgQuote(seedPushID))),
			"feedback":          postFeedback,
			"tool":              pgQueryInt(t, env, fmt.Sprintf("SELECT count(*) FROM tool WHERE id = %s AND server_name = %s AND group_kind = %s", pgQuote(seedToolID), pgQuote(seedToolServerName), pgQuote(seedGroupKind))),
			"toolserver":        pgQueryInt(t, env, fmt.Sprintf("SELECT count(*) FROM toolserver WHERE name = %s AND group_kind = %s", pgQuote(seedToolServerName), pgQuote(seedGroupKind))),
		}
		for table, before := range seedCanaryCounts {
			// The generic canaries only assert non-regression. Future migrations
			// that intentionally transform data should still add targeted
			// assertions for their expected post-upgrade shape.
			require.GreaterOrEqual(t, postCanaryCounts[table], before,
				"%s canary row count decreased across upgrade", table)
		}
	})

	vectorEnabled := baselineVectorVersion > 0

	if !t.Run("verify upgraded schema matches a clean install", func(t *testing.T) {
		// Build an independent clean install of the current build's migrations
		// and require the upgraded database to be structurally identical. This
		// catches upgrade paths that leave residue a fresh install would not.
		cleanHeadSchema = buildCleanInstallSchema(t, env, "clean_head_"+seed, vectorEnabled)
		upgradedSchema := pgSchemaDump(t, env, "kagent")

		if cleanHeadSchema == cleanTargetSchema {
			t.Log("no schema change between target and HEAD; the round-trip is a structural no-op until a new migration lands")
		}
		require.Equal(t, cleanHeadSchema, upgradedSchema,
			"upgraded schema diverged from a clean install of the current build")
	}) {
		return
	}

	t.Run("reverse schema to target", func(t *testing.T) {
		// Scale the controller to zero so no booting pod re-applies migrations
		// while we reverse the schema (the design's scale-to-zero recipe).
		scaleController(t, env, 0)

		localPort, stop := startPortForward(t, env, postgresServiceName, 5432)
		defer stop()
		dbURL := fmt.Sprintf("postgres://kagent:kagent@127.0.0.1:%d/kagent?sslmode=disable", localPort)

		// Reverse each track to the target release's version, in reverse
		// registration order (vector before core). This stands in for
		// `kagent db migrate goto --release <target>` until that CLI exists; it
		// exercises every down file between HEAD and the target.
		targets := map[string]int{
			"core":   pgBaselineState.version,
			"vector": baselineVectorVersion,
		}
		for _, track := range slices.Backward(migrationTracks) {
			migrateTrackTo(t, dbURL, track, targets[track.name])
		}

		// Migration bookkeeping is back at the target versions and clean.
		reverted := pgMigrationState(t, env)
		require.False(t, reverted.dirty, "reverted Postgres migrations are dirty")
		require.Equal(t, pgBaselineState.version, reverted.version, "core track not reversed to target version")
		require.Equal(t, baselineVectorVersion, pgTrackVersion(t, env, "vector_schema_migrations"),
			"vector track not reversed to target version")

		// Schema matches a clean target install, and the seeded rows survived
		// the down migrations.
		require.Equal(t, cleanTargetSchema, pgSchemaDump(t, env, "kagent"),
			"reversed schema diverged from a clean target install")
		require.GreaterOrEqual(t, pgQueryInt(t, env, fmt.Sprintf("SELECT count(*) FROM agent WHERE id = %s", pgQuote(seedAgentID))), 1,
			"seeded agent row did not survive the rollback")
		require.GreaterOrEqual(t, pgQueryInt(t, env, fmt.Sprintf("SELECT count(*) FROM feedback WHERE user_id = %s", pgQuote(seedUserID))), 1,
			"seeded feedback row did not survive the rollback")
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

func waitForPostgresAgentTable(t *testing.T, env upgradeEnv, timeout time.Duration) {
	t.Helper()

	require.Eventually(t, func() bool {
		out, err := pgQueryE(t, env, "SELECT to_regclass('public.agent') IS NOT NULL")
		return err == nil && out == "t"
	}, timeout, 5*time.Second, "agent table did not appear in the baseline Postgres schema")
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
	raw, err := pgQueryE(t, env, "SELECT CASE WHEN to_regclass('public.schema_migrations') IS NULL THEN '0,false' ELSE (SELECT concat(COALESCE(MAX(version), 0), ',', COALESCE(bool_or(dirty), false)) FROM public.schema_migrations) END")
	if err != nil {
		return postgresMigrationState{}, err
	}
	parts := strings.Split(raw, ",")
	if len(parts) != 2 {
		return postgresMigrationState{}, fmt.Errorf("parse schema_migrations state %q", raw)
	}
	version, err := strconv.Atoi(strings.TrimSpace(parts[0]))
	if err != nil {
		return postgresMigrationState{}, fmt.Errorf("parse schema_migrations version %q: %w", parts[0], err)
	}
	var dirty bool
	switch strings.TrimSpace(parts[1]) {
	case "t", "true":
		dirty = true
	case "f", "false":
		dirty = false
	default:
		return postgresMigrationState{}, fmt.Errorf("parse schema_migrations dirty %q", parts[1])
	}
	return postgresMigrationState{version: version, dirty: dirty}, nil
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
func podNameForSelectorE(t *testing.T, env upgradeEnv, selector string) (string, error) {
	out, err := kubectlOutput(t, env, time.Minute,
		"get", "pods",
		"-n", env.namespace,
		"-l", selector,
		"-o", "jsonpath={.items[0].metadata.name}",
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
		if entry.IsDir() || !strings.HasSuffix(name, ".up.sql") {
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
