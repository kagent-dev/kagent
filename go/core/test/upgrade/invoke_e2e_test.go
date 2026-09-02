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

// invokeE2ETest deploys and invokes one agent through the real controller.
const invokeE2ETest = "^TestE2EInvokeInlineAgent$"

// runInvokeE2E runs the invoke test against the current controller.
func runInvokeE2E(t *testing.T, env upgradeEnv, label string) {
	t.Helper()

	if os.Getenv("KAGENT_LOCAL_HOST") == "" {
		t.Skipf("[%s] KAGENT_LOCAL_HOST is not set; run via `make run-upgrade-tests` to exercise the invoke e2e slice", label)
	}
	treeGoDir := filepath.Join(env.repoRoot, "go")
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
