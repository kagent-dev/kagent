package upgrade

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/utils/ptr"

	kagentenv "github.com/kagent-dev/kagent/go/core/pkg/env"
	"github.com/kagent-dev/kagent/go/core/pkg/postgres"
)

// Exercise the process-drain boundary in an isolated namespace. This models
// legacy process lifetime; TestUpgrade uses the same drain with real old binaries.
func TestUpgradeWaitsForTerminatingCheckpointWriters(t *testing.T) {
	if kagentenv.E2ERunUpgradeTests.Get() != "true" {
		t.Skip("requires a disposable upgrade cluster")
	}
	env := loadUpgradeEnv(t)
	env.namespace = "checkpoint-upgrade-" + uuid.NewString()
	kubectl(t, env, time.Minute, "create", "namespace", env.namespace)
	t.Cleanup(func() {
		_, err := kubectlOutputContext(context.WithoutCancel(t.Context()), env, time.Minute,
			"delete", "namespace", env.namespace, "--wait=false")
		require.NoError(t, err)
	})
	labels := map[string]string{"app.kubernetes.io/name": "kagent", "app.kubernetes.io/component": "controller"}
	image := kagentenv.E2ERuntimeImage.Get()
	if image == "" {
		image = "busybox:1.36"
	}
	deployment := &appsv1.Deployment{
		TypeMeta:   metav1.TypeMeta{APIVersion: "apps/v1", Kind: "Deployment"},
		ObjectMeta: metav1.ObjectMeta{Name: "kagent-controller", Namespace: env.namespace},
		Spec: appsv1.DeploymentSpec{Replicas: ptr.To[int32](2), Selector: &metav1.LabelSelector{MatchLabels: labels},
			Template: corev1.PodTemplateSpec{ObjectMeta: metav1.ObjectMeta{Labels: labels}, Spec: corev1.PodSpec{
				TerminationGracePeriodSeconds: ptr.To[int64](1),
				Containers: []corev1.Container{{Name: controllerContainer, Image: image,
					Command: []string{"/bin/sh", "-c", "trap 'exit 0' TERM; while :; do sleep 1; done"}}},
			}}},
	}
	data, err := json.Marshal(deployment)
	require.NoError(t, err)
	manifest := filepath.Join(t.TempDir(), "legacy-writers.json")
	require.NoError(t, os.WriteFile(manifest, data, 0o600))
	kubectl(t, env, time.Minute, "apply", "-f", manifest)
	kubectl(t, env, 2*time.Minute, "rollout", "status", "deployment/kagent-controller", "-n", env.namespace, "--timeout=2m")
	pods := podNamesForSelector(t, env, controllerSelector)
	require.Len(t, pods, 2)
	held := pods[0]
	// Hold the Pod object after its process becomes unready. A scale request
	// or readiness transition alone must never authorize starting new writers.
	kubectl(t, env, time.Minute, "patch", "pod", held, "-n", env.namespace,
		"--type=merge", "-p", `{"metadata":{"finalizers":["checkpoint.kagent.dev/upgrade-test"]}}`)
	released := false
	t.Cleanup(func() {
		if !released {
			_, _ = kubectlOutputContext(context.WithoutCancel(t.Context()), env, time.Minute, "patch", "pod", held, "-n", env.namespace,
				"--type=merge", "-p", `{"metadata":{"finalizers":[]}}`)
		}
	})
	drained := make(chan error, 1)
	go func() { drained <- drainLegacyCheckpointWriters(t, env) }()
	require.Eventually(t, func() bool {
		out, err := kubectlOutput(t, env, 10*time.Second, "get", "pod", held, "-n", env.namespace,
			"-o", `jsonpath={.metadata.deletionTimestamp}{"|"}{.status.containerStatuses[?(@.name=="controller")].ready}`)
		deleting, readiness, ok := strings.Cut(out, "|")
		return err == nil && ok && deleting != "" && readiness == "false"
	}, time.Minute, 100*time.Millisecond)
	select {
	case err := <-drained:
		t.Fatalf("upgrade boundary passed a terminating legacy writer: %v", err)
	case <-time.After(200 * time.Millisecond):
	}
	kubectl(t, env, time.Minute, "patch", "pod", held, "-n", env.namespace,
		"--type=merge", "-p", `{"metadata":{"finalizers":[]}}`)
	released = true
	select {
	case err := <-drained:
		require.NoError(t, err)
	case <-time.After(time.Minute):
		t.Fatal("upgrade boundary did not open after all legacy writers exited")
	}
	require.Empty(t, podNamesForSelector(t, env, controllerSelector))
	scaleController(t, env, 1)
	newPods := podNamesForSelector(t, env, controllerSelector)
	require.Len(t, newPods, 1)
	require.NotContains(t, pods, newPods[0], "new writers must start only after old Pods are gone")
}

func drainLegacyCheckpointWriters(t *testing.T, env upgradeEnv) error {
	t.Helper()
	if _, err := kubectlOutput(t, env, time.Minute, "scale", "deployment/kagent-controller",
		"-n", env.namespace, "--replicas=0"); err != nil {
		return fmt.Errorf("stop legacy checkpoint writers: %w", err)
	}
	if _, err := kubectlOutput(t, env, 2*time.Minute, "rollout", "status", "deployment/kagent-controller",
		"-n", env.namespace, "--timeout=2m"); err != nil {
		return fmt.Errorf("observe legacy controller scale-down: %w", err)
	}
	return wait.PollUntilContextTimeout(t.Context(), 500*time.Millisecond, 2*time.Minute, true, func(context.Context) (bool, error) {
		pods, err := podNamesForSelectorE(t, env, controllerSelector)
		return err == nil && len(pods) == 0, err
	})
}

func requireLegacyCheckpointRollbackReady(t *testing.T, env upgradeEnv) {
	t.Helper()
	port, stop := startPortForward(t, env, postgresServiceName, 5432)
	defer stop()
	config, err := postgres.PoolConfig(postgresURL(t, env, port), postgresRole)
	require.NoError(t, err)
	config.ConnConfig.RuntimeParams["search_path"] = kagentSchema
	pool, err := pgxpool.NewWithConfig(t.Context(), config)
	require.NoError(t, err)
	defer pool.Close()
	require.NoError(t, checkLegacyCheckpointRollback(t.Context(), pool))
}

// Legacy deletion requires a completed URI and cannot collect retained partial
// tags. Keep this guard shared with the PostgreSQL rollback regression.
func checkLegacyCheckpointRollback(ctx context.Context, pool *pgxpool.Pool) error {
	var blockers int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM session_checkpoint
		WHERE state IN ('CREATING', 'DELETING') OR (state = 'FAILED' AND tag_uid <> '')`).Scan(&blockers); err != nil {
		return fmt.Errorf("check checkpoint rollback compatibility: %w", err)
	}
	if blockers != 0 {
		return fmt.Errorf("finish pending operations and delete retained failed checkpoints before restoring a legacy writer: %d incompatible checkpoints", blockers)
	}
	return nil
}
