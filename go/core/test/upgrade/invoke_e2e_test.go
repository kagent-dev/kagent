package upgrade

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// invokeE2ETest is the representative slice of the e2e suite we run at each
// upgrade state: it deploys a declarative agent (default runtime), invokes it
// through the real controller against an in-process mock LLM, and asserts — i.e.
// it exercises kagent's actual query paths, not raw SQL. Run it from the tree
// whose version matches the serving controller so the API/CRD shapes line up.
const invokeE2ETest = "^TestE2EInvokeInlineAgent$"

// prevTreeGoDir returns the `go/` directory of the previous release's checkout
// (a git worktree at its tag, created by the run-upgrade-tests make target and
// passed via PREV_E2E_DIR), or "" when it isn't available.
func prevTreeGoDir() string {
	dir := os.Getenv("PREV_E2E_DIR")
	if dir == "" {
		return ""
	}
	return filepath.Join(dir, "go")
}

// runInvokeE2E runs the invoke e2e slice from treeGoDir against the controller
// currently serving in the cluster. treeGoDir is the `go/` module dir of the
// matching-version tree (repo root for HEAD, the worktree for the prior
// release). It port-forwards the controller for KAGENT_URL — re-established per
// state, so it survives the controller being reinstalled between states — and
// relies on KAGENT_LOCAL_HOST (kind gateway IP, set by the make target) for the
// agent→host mock-LLM callback. label identifies the state in messages.
//
// It self-skips when the harness isn't set up (no KAGENT_LOCAL_HOST) or the
// test tree isn't available, so a bare `go test -run TestUpgrade` still runs the
// DB round-trip without needing a worktree.
func runInvokeE2E(t *testing.T, env upgradeEnv, treeGoDir, label string) {
	t.Helper()

	if os.Getenv("KAGENT_LOCAL_HOST") == "" {
		t.Skipf("[%s] KAGENT_LOCAL_HOST is not set; run via `make run-upgrade-tests` to exercise the invoke e2e slice", label)
	}
	if treeGoDir == "" {
		t.Skipf("[%s] no matching-version test tree available (PREV_E2E_DIR unset)", label)
	}
	if _, err := os.Stat(filepath.Join(treeGoDir, "go.mod")); err != nil {
		t.Skipf("[%s] test tree %q is not usable: %v", label, treeGoDir, err)
	}

	port, stop := startPortForward(t, env, controllerServiceName, controllerAPIPort)
	defer stop()

	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Minute)
	defer cancel()

	cmd := exec.CommandContext(ctx, "go", "test", "./core/test/e2e",
		"-run", invokeE2ETest, "-count=1", "-v")
	cmd.Dir = treeGoDir
	// KAGENT_LOCAL_HOST is inherited from the environment (set by the make
	// target); KAGENT_URL points the e2e client at the port-forwarded controller.
	cmd.Env = append(os.Environ(), fmt.Sprintf("KAGENT_URL=http://127.0.0.1:%d", port))

	out, err := cmd.CombinedOutput()
	require.NoErrorf(t, err, "[%s] invoke e2e slice (%s) failed:\n%s", label, treeGoDir, string(out))
	t.Logf("[%s] invoke e2e slice passed", label)
}
