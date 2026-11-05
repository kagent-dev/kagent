package upgrade

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"

	kagentenv "github.com/kagent-dev/kagent/go/core/pkg/env"
)

func TestRollingUpgradeCompatibility(t *testing.T) {
	if kagentenv.E2ERunRollingUpgradeTests.Get() != "true" {
		t.Skip("set KAGENT_E2E_RUN_ROLLING_UPGRADE_TESTS=true to run rolling upgrade tests")
	}

	env := loadUpgradeEnv(t)
	targetCoreVersion := latestCoreMigrationVersion(t)
	seed := fmt.Sprintf("%d", time.Now().UnixNano())
	baselineToolID := "rolling-baseline-tool-" + seed
	compatToolID := "rolling-compat-tool-" + seed
	toolServerName := "rolling-toolserver-" + seed
	groupKind := "upgrade.rolling/v1/Canary"

	t.Logf("rolling upgrade test: %s -> %s (registry=%s, kubeContext=%s)",
		env.upgradeFromVersion, env.version, env.dockerRegistry, env.kubeContext)

	waitForReadyPods(t, env, postgresSelector, 3*time.Minute)
	waitForPostgresSchema(t, env, 3*time.Minute)

	// Multiple previous-generation API replicas must all exit before checkpoint
	// recovery starts. Keep a terminating writer alive long enough that an unsafe
	// RollingUpdate also has a visible overlap window.
	kubectl(t, env, 2*time.Minute,
		"scale", "deployment/kagent-controller",
		"-n", env.namespace,
		"--replicas=2",
	)
	kubectl(t, env, time.Minute, "patch", "deployment/kagent-controller", "-n", env.namespace,
		"--type=strategic", "-p", `{"spec":{"template":{"spec":{"terminationGracePeriodSeconds":30,"containers":[{"name":"controller","lifecycle":{"preStop":{"sleep":{"seconds":5}}}}]}}}}`)
	kubectl(t, env, 3*time.Minute,
		"rollout", "status", "deployment/kagent-controller",
		"-n", env.namespace,
		"--timeout=3m",
	)
	oldPods := podNamesForSelector(t, env, controllerSelector)
	require.NotEmpty(t, oldPods, "expected old controller pods before rolling upgrade")

	// Preserve old-shape SQL read/write canaries across the upgrade. The
	// controller handoff itself is deliberately stop-before-start, not a claim
	// that old per-request checkpoint writers support mixed-version execution.
	pgExec(t, env, fmt.Sprintf(
		"INSERT INTO tool (id, server_name, group_kind, description) VALUES (%s, %s, %s, 'rolling baseline tool')",
		pgQuote(baselineToolID), pgQuote(toolServerName), pgQuote(groupKind)))

	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Minute)
	defer cancel()
	config, err := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(
		clientcmd.NewDefaultClientConfigLoadingRules(), &clientcmd.ConfigOverrides{CurrentContext: env.kubeContext}).ClientConfig()
	require.NoError(t, err)
	client, err := kubernetes.NewForConfig(config)
	require.NoError(t, err)
	pods, err := client.CoreV1().Pods(env.namespace).List(ctx, metav1.ListOptions{LabelSelector: controllerSelector})
	require.NoError(t, err)
	require.NotEmpty(t, pods.Items, "expected previous controller pod UIDs before upgrade")
	observer := newControllerHandoff(pods.Items)
	events, err := client.CoreV1().Pods(env.namespace).Watch(ctx, metav1.ListOptions{
		LabelSelector: controllerSelector, ResourceVersion: pods.ResourceVersion,
	})
	require.NoError(t, err)
	defer events.Stop()
	done := make(chan upgradeResult, 1)
	cmd := upgradeCommand(ctx, env)
	go func() {
		out, err := cmd.CombinedOutput()
		done <- upgradeResult{out: string(out), err: err}
	}()

	var result upgradeResult
	finished := false
	for !finished || !observer.sawReplacement {
		select {
		case event, ok := <-events.ResultChan():
			require.True(t, ok, "pod watch ended before the controller handoff was verified")
			require.NoError(t, observer.observe(event))
		case result = <-done:
			require.NoError(t, result.err, "upgrade failed:\n%s", result.out)
			finished = true
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		}
	}
	require.Empty(t, observer.old, "all previous writers must have exited")
	strategy := kubectl(t, env, time.Minute, "get", "deployment/kagent-controller", "-n", env.namespace,
		"-o", "jsonpath={.spec.strategy.type}")
	require.Equal(t, "Recreate", strings.TrimSpace(strategy))

	// These SQL canaries intentionally use the pre-upgrade column shape. They do
	// not prove every previous-release code path works, but they catch migrations
	// that break basic old read/write assumptions after the controller handoff.
	require.Equal(t, 1,
		pgQueryInt(t, env, fmt.Sprintf("SELECT count(*) FROM tool WHERE id = %s AND server_name = %s AND group_kind = %s",
			pgQuote(baselineToolID), pgQuote(toolServerName), pgQuote(groupKind))),
		"old-shape read failed after target schema was applied",
	)
	pgExec(t, env, fmt.Sprintf(
		"INSERT INTO tool (id, server_name, group_kind, description) VALUES (%s, %s, %s, 'rolling compatibility tool')",
		pgQuote(compatToolID), pgQuote(toolServerName), pgQuote(groupKind)))
	require.Equal(t, 1,
		pgQueryInt(t, env, fmt.Sprintf("SELECT count(*) FROM tool WHERE id = %s AND server_name = %s AND group_kind = %s",
			pgQuote(compatToolID), pgQuote(toolServerName), pgQuote(groupKind))),
		"old-shape tool write did not survive against target schema",
	)

	kubectl(t, env, 3*time.Minute,
		"rollout", "status", "deployment/kagent-controller",
		"-n", env.namespace,
		"--timeout=3m",
	)
	finalState := pgMigrationState(t, env)
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

type controllerHandoff struct {
	old            map[types.UID]bool
	sawReplacement bool
}

func newControllerHandoff(pods []corev1.Pod) *controllerHandoff {
	old := make(map[types.UID]bool, len(pods))
	for _, pod := range pods {
		if pod.Status.Phase != corev1.PodSucceeded && pod.Status.Phase != corev1.PodFailed {
			old[pod.UID] = true
		}
	}
	return &controllerHandoff{old: old}
}

// Terminating and unready old pods can still issue checkpoint writes. Only
// deletion or a terminal phase (all processes exited) releases the barrier,
// matching the Deployment controller's Recreate condition.
func (h *controllerHandoff) observe(event watch.Event) error {
	if event.Type == watch.Error {
		return fmt.Errorf("controller pod watch failed: %v", event.Object)
	}
	pod, ok := event.Object.(*corev1.Pod)
	if !ok {
		return fmt.Errorf("unexpected controller watch object %T", event.Object)
	}
	if h.old[pod.UID] {
		if event.Type == watch.Deleted || pod.Status.Phase == corev1.PodSucceeded || pod.Status.Phase == corev1.PodFailed {
			delete(h.old, pod.UID)
		}
		return nil
	}
	if event.Type == watch.Added || event.Type == watch.Modified {
		if len(h.old) != 0 {
			return fmt.Errorf("new controller %s started while %d previous writers still exist", pod.Name, len(h.old))
		}
		h.sawReplacement = true
	}
	return nil
}
