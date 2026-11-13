package remotemcpserver

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"

	"github.com/kagent-dev/kagent/go/api/v1alpha3"
	"github.com/kagent-dev/kagent/go/core/pkg/consts"
)

func TestReferenceWatchesWithDiscoveryDisabled(t *testing.T) {
	environment := &envtest.Environment{CRDDirectoryPaths: []string{"../../../../../helm/kagent-crds/templates"}, ErrorIfCRDPathMissing: true}
	config, err := environment.Start()
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, environment.Stop()) })
	scheme := runtime.NewScheme()
	require.NoError(t, corev1.AddToScheme(scheme))
	require.NoError(t, v1alpha3.AddToScheme(scheme))
	manager, err := ctrl.NewManager(config, ctrl.Options{Scheme: scheme, Metrics: metricsserver.Options{BindAddress: "0"}, HealthProbeBindAddress: "0"})
	require.NoError(t, err)
	require.NoError(t, New(manager.GetClient(), &fakeDiscoverer{}, &fakeCatalog{}).SetupWithManager(manager))
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { done <- manager.Start(ctx) }()
	t.Cleanup(func() { cancel(); require.NoError(t, <-done) })
	kube, err := client.New(config, client.Options{Scheme: scheme})
	require.NoError(t, err)
	scenarios := []struct {
		name     string
		labels   map[string]string
		actorJWT *v1alpha3.ActorJWT
	}{
		{name: labelDiscoveryMode, labels: map[string]string{consts.DiscoveryLabel: consts.DiscoveryDisabled}},
		{name: "actor-jwt", actorJWT: &v1alpha3.ActorJWT{Audiences: []string{actorJWTAudience}}},
	}
	for _, scenario := range scenarios {
		t.Run("Given discovery disabled by "+scenario.name+", when a referenced Secret changes, assert that reference status follows its keys", func(t *testing.T) {
			require.NoError(t, kube.Create(ctx, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: scenario.name}}))
			server := testServer()
			server.ResourceVersion = ""
			server.Namespace = scenario.name
			server.Labels = scenario.labels
			server.Spec.ActorJWT = scenario.actorJWT
			server.Spec.HeadersFrom = []v1alpha3.ValueRef{{Name: referenceHeaderName, ValueFrom: &v1alpha3.ValueSource{Type: v1alpha3.SecretValueSource, Name: referenceSecretName, Key: referenceSecretKey}}}
			require.NoError(t, kube.Create(ctx, server))
			expect := func(reason string, status metav1.ConditionStatus) {
				t.Helper()
				require.Eventually(t, func() bool {
					updated := &v1alpha3.RemoteMCPServer{}
					if err := kube.Get(ctx, client.ObjectKeyFromObject(server), updated); err != nil {
						return false
					}
					condition := apimeta.FindStatusCondition(updated.Status.Conditions, v1alpha3.RemoteMCPServerConditionTypeResolvedRefs)
					accepted := apimeta.FindStatusCondition(updated.Status.Conditions, conditionAccepted)
					return condition != nil && condition.Status == status && condition.Reason == reason && condition.ObservedGeneration == server.Generation && accepted != nil && accepted.Status == metav1.ConditionTrue && accepted.Reason == discoveryDisabledReason
				}, 10*time.Second, 10*time.Millisecond)
			}
			expect(v1alpha3.RemoteMCPServerReasonSecretNotFound, metav1.ConditionFalse)
			secret := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Namespace: server.Namespace, Name: referenceSecretName}}
			require.NoError(t, kube.Create(ctx, secret))
			expect(v1alpha3.RemoteMCPServerReasonSecretKeyNotFound, metav1.ConditionFalse)
			secret.Data = map[string][]byte{referenceSecretKey: []byte("example")}
			require.NoError(t, kube.Update(ctx, secret))
			expect("Resolved", metav1.ConditionTrue)
			require.NoError(t, kube.Delete(ctx, secret))
			expect(v1alpha3.RemoteMCPServerReasonSecretNotFound, metav1.ConditionFalse)
		})
	}
}
