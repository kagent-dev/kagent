package upgrade

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

const (
	invokeE2ETest                = "^TestAgentInstanceInteraction$"
	acrossMigrationInvokeE2ETest = "^TestAgentInstanceInteractionAcrossMigration$"
)

func checkoutPreviousRelease(t *testing.T, env upgradeEnv) string {
	t.Helper()

	ref := env.upgradeFromVersion
	if !strings.HasPrefix(ref, "v") {
		ref = "v" + ref
	}
	worktree := filepath.Join(t.TempDir(), "previous-release")
	cmd := exec.CommandContext(t.Context(), "git", "worktree", "add", "--detach", worktree, ref)
	cmd.Dir = env.repoRoot
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, "check out previous release %s:\n%s", ref, string(out))
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()
		cmd := exec.CommandContext(ctx, "git", "worktree", "remove", "--force", worktree)
		cmd.Dir = env.repoRoot
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Logf("remove previous-release worktree: %v\n%s", err, string(out))
		}
	})
	return filepath.Join(worktree, "go")
}

func runInvokeE2E(t *testing.T, env upgradeEnv, treeGoDir, label string) {
	t.Helper()

	requireInvokeEnvironment(t, treeGoDir, label)
	port, stop := startPortForward(t, env, controllerServiceName, controllerGRPCPort)
	defer stop()

	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, "go", "test", "./core/test/e2e",
		"-run", invokeE2ETest, "-count=1", "-timeout=9m", "-v")
	cmd.Dir = treeGoDir
	cmd.Env = upgradeE2EEnv(t, env, port)

	out, err := cmd.CombinedOutput()
	require.NoErrorf(t, err, "[%s] invoke e2e slice (%s) failed:\n%s", label, treeGoDir, string(out))
	require.Contains(t, string(out), "--- PASS: TestAgentInstanceInteraction",
		"[%s] selected invoke test did not run:\n%s", label, string(out))
}

func runPreviousReleaseAcrossMigration(
	t *testing.T,
	env upgradeEnv,
	treeGoDir string,
	applyMigrations func(),
) {
	t.Helper()

	requireInvokeEnvironment(t, treeGoDir, "previous release")
	port, stop := startPortForward(t, env, controllerServiceName, controllerGRPCPort)
	defer stop()

	signalDir := t.TempDir()
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, "go", "test", "./core/test/e2e",
		"-run", acrossMigrationInvokeE2ETest, "-count=1", "-timeout=19m", "-v")
	cmd.Dir = treeGoDir
	cmd.Env = append(upgradeE2EEnv(t, env, port), "KAGENT_UPGRADE_SIGNAL_DIR="+signalDir)
	var output bytes.Buffer
	cmd.Stdout = &output
	cmd.Stderr = &output
	require.NoError(t, cmd.Start(), "start previous-release invoke e2e")
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()

	waitForUpgradeSignal(t, ctx, filepath.Join(signalDir, "ready"), done, &output)
	applyMigrations()
	require.NoError(t, os.WriteFile(filepath.Join(signalDir, "continue"), nil, 0o600))

	select {
	case err := <-done:
		require.NoErrorf(t, err, "previous-release invoke e2e failed:\n%s", output.String())
	case <-ctx.Done():
		t.Fatalf("previous-release invoke e2e did not finish: %v", ctx.Err())
	}
	require.Contains(t, output.String(), "--- PASS: TestAgentInstanceInteractionAcrossMigration",
		"selected previous-release invoke test did not run:\n%s", output.String())
}

func upgradeE2EEnv(t *testing.T, env upgradeEnv, port int) []string {
	t.Helper()

	kubeconfig := kubectl(t, env, time.Minute, "config", "view", "--raw", "--flatten", "--minify")
	kubeconfigPath := filepath.Join(t.TempDir(), "kubeconfig")
	require.NoError(t, os.WriteFile(kubeconfigPath, []byte(kubeconfig), 0o600))
	return append(os.Environ(),
		fmt.Sprintf("KAGENT_E2E_GRPC_TARGET=127.0.0.1:%d", port),
		"KUBECONFIG="+kubeconfigPath,
	)
}

func requireInvokeEnvironment(t *testing.T, treeGoDir, label string) {
	t.Helper()

	if os.Getenv("KAGENT_LOCAL_HOST") == "" {
		t.Skipf("[%s] KAGENT_LOCAL_HOST is not set; run via make run-upgrade-tests", label)
	}
	if _, err := os.Stat(filepath.Join(treeGoDir, "go.mod")); err != nil {
		t.Fatalf("[%s] test tree %q is not usable: %v", label, treeGoDir, err)
	}
}

func waitForUpgradeSignal(
	t *testing.T,
	ctx context.Context,
	path string,
	done <-chan error,
	output *bytes.Buffer,
) {
	t.Helper()

	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()
	for {
		if _, err := os.Stat(path); err == nil {
			return
		}
		select {
		case err := <-done:
			t.Fatalf("previous-release invoke e2e exited before its first invocation completed: %v\n%s",
				err, output.String())
		case <-ctx.Done():
			t.Fatalf("previous-release invoke e2e did not complete its first invocation: %v", ctx.Err())
		case <-ticker.C:
		}
	}
}
