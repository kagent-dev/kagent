package upgrade

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	kubeyaml "k8s.io/apimachinery/pkg/util/yaml"
	"sigs.k8s.io/yaml"

	configv1 "github.com/kagent-dev/kagent/go/api/gen/kagent/config/v1"
	kagentenv "github.com/kagent-dev/kagent/go/core/pkg/env"
)

const invokeE2ETest = "^TestSessionInteraction$"

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
// release). It port-forwards the controller for KAGENT_E2E_API_URL — re-established per
// state, so it survives the controller being reinstalled between states — and
// relies on KAGENT_E2E_LOCAL_HOST (kind gateway IP, set by the make target) for the
// agent→host mock-LLM callback. label identifies the state in messages.
func runInvokeE2E(t *testing.T, env upgradeEnv, treeGoDir, label string) {
	t.Helper()

	requireInvokeEnvironment(t, treeGoDir, label)
	installInvokeHarnesses(t, env, treeGoDir)
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
	require.Contains(t, string(out), "--- PASS: TestSessionInteraction",
		"[%s] selected invoke test did not run:\n%s", label, string(out))
}

// Reapply the matching release's fixtures at each invocation stage so upgrades
// and application rollbacks exercise that installation's runtime images too.
func installInvokeHarnesses(t *testing.T, env upgradeEnv, treeGoDir string) {
	t.Helper()

	manifest, err := os.ReadFile(filepath.Join(treeGoDir, "core/test/e2e/manifests/lifecycle.yaml.tmpl"))
	require.NoError(t, err)
	controllerYAML := kubectl(t, env, time.Minute, "get", "configmap", controllerServiceName,
		"-n", env.namespace, "-o", `jsonpath={.data.controller\.yaml}`)
	var config struct {
		HarnessImages *configv1.HarnessImagesConfig `json:"harnessImages"`
	}
	require.NoError(t, yaml.Unmarshal([]byte(controllerYAML), &config))
	fixtures, err := invokeHarnesses(manifest, config.HarnessImages)
	require.NoError(t, err)
	path := filepath.Join(t.TempDir(), "harnesses.json")
	require.NoError(t, os.WriteFile(path, fixtures, 0o600))
	kubectl(t, env, time.Minute, "apply", "-f", path)
}

func invokeHarnesses(manifest []byte, images *configv1.HarnessImagesConfig) ([]byte, error) {
	if images.GetKagent() == "" || images.GetClaude() == "" || images.GetCodex() == "" {
		return nil, fmt.Errorf("installed controller must provide kagent, claude, and codex runtime images")
	}
	rendered := strings.NewReplacer(
		"${KAGENT_E2E_RUNTIME_IMAGE}", images.GetKagent(),
		"${KAGENT_E2E_CLAUDE_IMAGE}", images.GetClaude(),
		"${KAGENT_E2E_CODEX_IMAGE}", images.GetCodex(),
	).Replace(string(manifest))
	decoder := kubeyaml.NewYAMLOrJSONDecoder(bytes.NewBufferString(rendered), 4096)
	fixtures := metav1.List{TypeMeta: metav1.TypeMeta{APIVersion: "v1", Kind: "List"}}
	for {
		var resource runtime.RawExtension
		if err := decoder.Decode(&resource); err != nil {
			if err == io.EOF {
				break
			}
			return nil, fmt.Errorf("decode lifecycle fixture: %w", err)
		}
		var metadata metav1.PartialObjectMetadata
		if err := json.Unmarshal(resource.Raw, &metadata); err != nil {
			return nil, fmt.Errorf("decode lifecycle fixture metadata: %w", err)
		}
		if metadata.Kind != "Harness" {
			continue
		}
		switch metadata.Name {
		case "kagent", "codex-e2e", "claude-e2e", "byo-adk-e2e":
			fixtures.Items = append(fixtures.Items, resource)
		}
	}
	return json.Marshal(fixtures)
}

func upgradeE2EEnv(t *testing.T, env upgradeEnv, port int) []string {
	t.Helper()

	kubeconfig := kubectl(t, env, time.Minute, "config", "view", "--raw", "--flatten", "--minify")
	kubeconfigPath := filepath.Join(t.TempDir(), "kubeconfig")
	require.NoError(t, os.WriteFile(kubeconfigPath, []byte(kubeconfig), 0o600))
	endpoint := fmt.Sprintf("http://127.0.0.1:%d", port)
	return append(os.Environ(),
		kagentenv.E2EAPIURL.Name()+"="+endpoint,
		kagentenv.Kubeconfig.Name()+"="+kubeconfigPath,
	)
}

func requireInvokeEnvironment(t *testing.T, treeGoDir, label string) {
	t.Helper()

	if kagentenv.KagentLocalHost.Get() == "" {
		t.Skipf("[%s] KAGENT_E2E_LOCAL_HOST is not set; run via make run-upgrade-tests", label)
	}
	if _, err := os.Stat(filepath.Join(treeGoDir, "go.mod")); err != nil {
		t.Fatalf("[%s] test tree %q is not usable: %v", label, treeGoDir, err)
	}
}
