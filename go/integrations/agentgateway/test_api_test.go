package agentgateway

import (
	"context"
	"log/slog"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	agw "github.com/agentgateway/agentgateway/controller/api/v1alpha1/agentgateway"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	kagentv1alpha3 "github.com/kagent-dev/kagent/go/api/v1alpha3"
	"github.com/kagent-dev/kagent/go/pkg/logging"
)

func moduleDir(t *testing.T, module string) string {
	t.Helper()
	output, err := exec.CommandContext(t.Context(), "go", "list", "-m", "-f", "{{.Dir}}", module).CombinedOutput()
	require.NoError(t, err, "%s", output)
	return strings.TrimSpace(string(output))
}

func testAPI(t *testing.T) (client.Client, *runtime.Scheme, func(Config)) {
	t.Helper()
	environment := &envtest.Environment{CRDDirectoryPaths: []string{
		filepath.Join(moduleDir(t, "github.com/kagent-dev/kagent/go"), "..", "helm", "kagent-crds", "templates"),
		filepath.Join(moduleDir(t, "github.com/agentgateway/agentgateway"), "controller", "install", "helm", "agentgateway-crds", "templates"),
		filepath.Join(moduleDir(t, "sigs.k8s.io/gateway-api"), "config", "crd", "standard"),
	}, ErrorIfCRDPathMissing: true}
	config, err := environment.Start()
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, environment.Stop()) })
	scheme := runtime.NewScheme()
	for _, add := range []func(*runtime.Scheme) error{clientgoscheme.AddToScheme, kagentv1alpha3.AddToScheme, agw.Install, gatewayv1.Install} {
		require.NoError(t, add(scheme))
	}
	api, err := client.New(config, client.Options{Scheme: scheme})
	require.NoError(t, err)
	require.NoError(t, api.Create(t.Context(), &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "team"}}))
	start := func(options Config) {
		ctrl.SetLogger(logging.AsLogr(slog.New(slog.DiscardHandler)))
		manager, err := ctrl.NewManager(config, ctrl.Options{
			Scheme: scheme, Cache: cache.Options{DefaultNamespaces: map[string]cache.Config{"team": {}}},
			Metrics: metricsserver.Options{BindAddress: "0"}, HealthProbeBindAddress: "0",
		})
		require.NoError(t, err)
		require.NoError(t, SetupWithManager(manager, options))
		ctx, cancel := context.WithCancel(t.Context())
		done := make(chan error, 1)
		go func() { done <- manager.Start(ctx) }()
		t.Cleanup(func() {
			cancel()
			select {
			case err := <-done:
				require.NoError(t, err)
			case <-time.After(10 * time.Second):
				t.Error("connection manager did not stop")
			}
		})
		require.True(t, manager.GetCache().WaitForCacheSync(ctx))
	}
	return api, scheme, start
}

func eventually(t *testing.T, condition func() bool) {
	t.Helper()
	require.Eventually(t, condition, 15*time.Second, 50*time.Millisecond)
}
