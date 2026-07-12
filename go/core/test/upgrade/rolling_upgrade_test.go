package upgrade

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestRollingUpgradeCompatibility(t *testing.T) {
	if os.Getenv("RUN_ROLLING_UPGRADE_TESTS") != "true" {
		t.Skip("set RUN_ROLLING_UPGRADE_TESTS=true to run rolling upgrade tests")
	}

	env := loadUpgradeEnv(t)
	targetCoreVersion := latestCoreMigrationVersion(t)
	seed := fmt.Sprintf("%d", time.Now().UnixNano())
	baselineAgentID := "rolling-baseline-agent-" + seed
	compatAgentID := "rolling-compat-agent-" + seed
	compatUserID := "rolling-compat-user-" + seed

	t.Logf("rolling upgrade test: %s -> %s (registry=%s, kubeContext=%s)",
		env.upgradeFromVersion, env.version, env.dockerRegistry, env.kubeContext)

	waitForReadyPods(t, env, postgresSelector, 3*time.Minute)
	waitForPostgresAgentTable(t, env, 3*time.Minute)

	// Run even when there is no migration delta: a rolling upgrade rolls the new
	// image regardless of migrations, so the deploy can still break for
	// non-schema reasons (a crashing new image, readiness, old pods against
	// new-code-created resources). When the target build does add migrations, the
	// same flow additionally exercises the old-code/new-schema window below.
	baselineState := pgMigrationState(t, env)
	require.False(t, baselineState.dirty, "baseline Postgres migrations are dirty")

	// Keep multiple old controller pods around during the rollout. With a single
	// replica the old-code/new-schema window can be too small to observe reliably.
	kubectl(t, env, 2*time.Minute,
		"scale", "deployment/kagent-controller",
		"-n", env.namespace,
		"--replicas=2",
	)
	kubectl(t, env, 3*time.Minute,
		"rollout", "status", "deployment/kagent-controller",
		"-n", env.namespace,
		"--timeout=3m",
	)
	oldPods := podNamesForSelector(t, env, controllerSelector)
	require.NotEmpty(t, oldPods, "expected old controller pods before rolling upgrade")

	// Seed with the baseline schema before the target controller applies new
	// migrations. The compatibility canary below verifies this row is still
	// readable while old pods are alive against the target schema.
	pgExec(t, env, fmt.Sprintf("INSERT INTO agent (id, type) VALUES (%s, 'Deployment')", pgQuote(baselineAgentID)))

	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Minute)
	defer cancel()
	done := make(chan upgradeResult, 1)
	cmd := helmUpgradeCommand(ctx, env)
	go func() {
		out, err := cmd.CombinedOutput()
		done <- upgradeResult{out: string(out), err: err}
	}()

	// Wait until the target schema has landed while at least one previous-release
	// controller pod is still ready. That is the rolling deploy hazard: old code
	// can keep serving briefly after a new pod has applied migrations. Surface a
	// helm upgrade failure immediately instead of letting it look like a
	// schema-observation timeout.
	var helmErr error
	var helmOut string
	require.Eventually(t, func() bool {
		select {
		case r := <-done:
			if r.err != nil {
				helmErr, helmOut = r.err, r.out
				return true
			}
			// Helm finished successfully before we caught the window; put the
			// result back so the final read below still sees it, then keep polling.
			done <- r
		default:
		}
		state, err := pgMigrationStateE(t, env)
		if err != nil {
			return false
		}
		return state.version == targetCoreVersion &&
			!state.dirty &&
			anyPodsReady(t, env, oldPods)
	}, 6*time.Minute, 500*time.Millisecond, "target schema was not observed while old controller pods were still ready")
	require.NoError(t, helmErr, "helm upgrade failed before target schema was observed:\n%s", helmOut)

	// These SQL canaries intentionally use the pre-upgrade column shape. They do
	// not prove every previous-release code path works, but they catch migrations
	// that break basic old read/write assumptions during a rolling deployment.
	require.Equal(t, 1,
		pgQueryInt(t, env, fmt.Sprintf("SELECT count(*) FROM agent WHERE id = %s AND type = 'Deployment'", pgQuote(baselineAgentID))),
		"old-shape read failed after target schema was applied",
	)
	pgExec(t, env, fmt.Sprintf("INSERT INTO agent (id, type) VALUES (%s, 'Deployment')", pgQuote(compatAgentID)))
	pgExec(t, env, fmt.Sprintf("INSERT INTO feedback (user_id, feedback_text) VALUES (%s, 'rolling compatibility feedback')", pgQuote(compatUserID)))
	require.Equal(t, 1,
		pgQueryInt(t, env, fmt.Sprintf("SELECT count(*) FROM agent WHERE id = %s", pgQuote(compatAgentID))),
		"old-shape agent write did not survive against target schema",
	)
	require.Equal(t, 1,
		pgQueryInt(t, env, fmt.Sprintf("SELECT count(*) FROM feedback WHERE user_id = %s", pgQuote(compatUserID))),
		"old-shape feedback write did not survive against target schema",
	)

	result := <-done
	require.NoError(t, result.err, "helm upgrade failed:\n%s", result.out)

	kubectl(t, env, 3*time.Minute,
		"rollout", "status", "deployment/kagent-controller",
		"-n", env.namespace,
		"--timeout=3m",
	)
	finalState := pgMigrationState(t, env)
	require.False(t, finalState.dirty, "post-rollout Postgres migrations are dirty")
	require.Equal(t, targetCoreVersion, finalState.version, "final migration version")
}

type upgradeResult struct {
	out string
	err error
}

func podNamesForSelector(t *testing.T, env upgradeEnv, selector string) []string {
	t.Helper()

	pods, err := podNamesForSelectorE(t, env, selector)
	require.NoError(t, err)
	return pods
}

// podNamesForSelectorE is the error-returning core of podNamesForSelector, for
// use inside require.Eventually conditions (see pgQueryE).
func podNamesForSelectorE(t *testing.T, env upgradeEnv, selector string) ([]string, error) {
	out, err := kubectlOutput(t, env, time.Minute,
		"get", "pods",
		"-n", env.namespace,
		"-l", selector,
		"-o", "jsonpath={range .items[*]}{.metadata.name}{\"\\n\"}{end}",
	)
	if err != nil {
		return nil, err
	}
	lines := strings.Split(strings.TrimSpace(out), "\n")
	pods := make([]string, 0, len(lines))
	for _, line := range lines {
		pod := strings.TrimSpace(line)
		if pod != "" {
			pods = append(pods, pod)
		}
	}
	return pods, nil
}

func anyPodsReady(t *testing.T, env upgradeEnv, pods []string) bool {
	t.Helper()

	for _, pod := range pods {
		out, err := kubectlOutput(t, env, 10*time.Second,
			"get", "pod", pod,
			"-n", env.namespace,
			"-o", fmt.Sprintf("jsonpath={.status.containerStatuses[?(@.name==%q)].ready}", controllerContainer),
		)
		if err == nil && strings.TrimSpace(out) == "true" {
			return true
		}
	}
	return false
}
