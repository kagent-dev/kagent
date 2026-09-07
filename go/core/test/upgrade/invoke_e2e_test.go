package upgrade

import (
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

const invokeE2ETest = "^TestAgentInstanceInteraction$"

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

// runInvokeE2E runs the invoke e2e slice from treeGoDir against the controller
// currently serving in the cluster. treeGoDir is the `go/` module dir of the
// matching-version tree (repo root for HEAD, the worktree for the prior
// release). It port-forwards the controller for KAGENT_API_URL — re-established per
// state, so it survives the controller being reinstalled between states — and
// relies on KAGENT_LOCAL_HOST (kind gateway IP, set by the make target) for the
// agent→host mock-LLM callback. label identifies the state in messages.
func runInvokeE2E(t *testing.T, env upgradeEnv, treeGoDir, label string) {
	t.Helper()

	requireInvokeEnvironment(t, treeGoDir, label)
	port, stop := startPortForward(t, env, controllerServiceName, controllerAPIPort)
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

func upgradeE2EEnv(t *testing.T, env upgradeEnv, port int) []string {
	t.Helper()

	kubeconfig := kubectl(t, env, time.Minute, "config", "view", "--raw", "--flatten", "--minify")
	kubeconfigPath := filepath.Join(t.TempDir(), "kubeconfig")
	require.NoError(t, os.WriteFile(kubeconfigPath, []byte(kubeconfig), 0o600))
	endpoint := fmt.Sprintf("http://127.0.0.1:%d", port)
	return append(os.Environ(),
		"KAGENT_E2E_API_URL="+endpoint,
		"KAGENT_GATEWAY_URL="+endpoint,
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
