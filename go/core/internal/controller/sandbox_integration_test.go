package controller

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	ateapipb "go.ate.dev/substrate/api/ateapi/v1"
	atev1 "go.ate.dev/substrate/api/infra/k8s/ate/v1"
	"istio.io/istio/pkg/kube/krt"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/envtest"

	kagentclient "github.com/kagent-dev/kagent/go/api/clientset/versioned"
	kagentv1alpha3 "github.com/kagent-dev/kagent/go/api/v1alpha3"
	"github.com/kagent-dev/kagent/go/core/internal/controller/apiclient"
	"github.com/kagent-dev/kagent/go/core/internal/substrate"
)

// Exercise the real registered SandboxTemplate informer, API resource versions,
// status writes, and restart cleanup. Only the external DB/Substrate work is fake.
func TestSandboxKRTInformerQueueAndRestartCleanup(t *testing.T) {
	testEnv := &envtest.Environment{CRDDirectoryPaths: []string{"../../../../helm/kagent-crds/templates"}, ErrorIfCRDPathMissing: true}
	config, err := testEnv.Start()
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, testEnv.Stop()) })
	kube, err := apiclient.New(config)
	require.NoError(t, err)
	for _, namespace := range []string{"team-a", "team-b"} {
		_, err = kube.Kube().CoreV1().Namespaces().Create(t.Context(), &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: namespace}}, metav1.CreateOptions{})
		require.NoError(t, err)
	}
	client, err := kagentclient.NewForConfig(config)
	require.NoError(t, err)
	template := sandboxTestTemplate()
	template.UID = ""
	template.Generation = 0
	template.Spec.EgressPolicyRefs = []corev1.LocalObjectReference{{Name: "tickets"}}
	template, err = client.ApiV1alpha3().SandboxTemplates(template.Namespace).Create(t.Context(), template, metav1.CreateOptions{})
	require.NoError(t, err)
	unwatched := template.DeepCopy()
	unwatched.Namespace = "team-b"
	unwatched.ResourceVersion = ""
	unwatched.UID = ""
	_, err = client.ApiV1alpha3().SandboxTemplates(unwatched.Namespace).Create(t.Context(), unwatched, metav1.CreateOptions{})
	require.NoError(t, err)
	store := &sandboxTestStore{}
	actors := &sandboxTestActors{store: store, templates: map[string]*ateapipb.ActorTemplate{}, autoReady: true}
	pool := &atev1.WorkerPool{ObjectMeta: metav1.ObjectMeta{Namespace: template.Namespace, Name: "default"}}
	start := func() (func(), krt.StaticCollection[*atev1.WorkerPool]) {
		ctx, cancel := context.WithCancel(t.Context())
		runtimeClient, err := apiclient.New(config)
		require.NoError(t, err)
		options := krt.NewOptionsBuilder(ctx.Done(), "sandbox-integration", nil)
		pools := krt.NewStaticCollection(nil, []*atev1.WorkerPool{pool}, options.WithName("WorkerPools")...)
		runtime := &Runtime{Client: runtimeClient, Options: options, Collections: Collections{
			EgressPolicies:   typedCollection[*kagentv1alpha3.EgressPolicy](runtimeClient, []string{"team-a"}, "EgressPolicies", options),
			Secrets:          typedCollection[*corev1.Secret](runtimeClient, []string{"team-a"}, "Secrets", options),
			SandboxTemplates: typedCollection[*kagentv1alpha3.SandboxTemplate](runtimeClient, []string{"team-a"}, "SandboxTemplates", options), WorkerPools: pools,
		}}
		reconciler, err := NewSandboxReconciler(config, runtime, store, actors, substrate.SandboxPolicy{GuestImage: "guest@sha256:" + strings.Repeat("b", 64), CPU: "1", Memory: "1Gi"})
		require.NoError(t, err)
		results := make(chan error, 2)
		go func() { results <- runtime.Start(ctx) }()
		go func() { results <- reconciler.Start(ctx) }()
		stop := sync.OnceFunc(func() {
			cancel()
			for range 2 {
				select {
				case err := <-results:
					require.NoError(t, err)
				case <-time.After(5 * time.Second):
					t.Fatal("sandbox KRT runtime did not stop")
				}
			}
		})
		t.Cleanup(stop)
		return stop, pools
	}
	stop, pools := start()
	ready := func() bool {
		current, err := client.ApiV1alpha3().SandboxTemplates(template.Namespace).Get(t.Context(), template.Name, metav1.GetOptions{})
		if err != nil {
			return false
		}
		return apimeta.IsStatusConditionTrue(current.Status.Conditions, "Ready")
	}
	waitUnresolved := func(reason, message string) {
		t.Helper()
		require.Eventually(t, func() bool {
			current, err := client.ApiV1alpha3().SandboxTemplates(template.Namespace).Get(t.Context(), template.Name, metav1.GetOptions{})
			if err != nil {
				return false
			}
			condition := apimeta.FindStatusCondition(current.Status.Conditions, kagentv1alpha3.SandboxTemplateConditionEgressPoliciesApplied)
			return condition != nil && condition.Status == metav1.ConditionFalse && condition.Reason == reason && strings.Contains(condition.Message, message)
		}, 10*time.Second, 10*time.Millisecond)
	}
	waitUnresolved(kagentv1alpha3.EgressPoliciesReasonNotFound, "tickets")
	require.Eventually(t, ready, 10*time.Second, 10*time.Millisecond)
	grant := &kagentv1alpha3.EgressPolicy{ObjectMeta: metav1.ObjectMeta{Namespace: template.Namespace, Name: "tickets"}, Spec: kagentv1alpha3.EgressPolicySpec{
		Destinations: []kagentv1alpha3.EgressDestination{{Origin: "https://tickets.example.com", Headers: []kagentv1alpha3.Header{{Name: "Authorization", Prefix: new("Bearer "), ValueFrom: kagentv1alpha3.HeaderValueSource{SecretKeyRef: new(corev1.SecretKeySelector{LocalObjectReference: corev1.LocalObjectReference{Name: "ticket-auth"}, Key: "token"})}}}}},
	}}
	grant, err = client.ApiV1alpha3().EgressPolicies(template.Namespace).Create(t.Context(), grant, metav1.CreateOptions{})
	require.NoError(t, err)
	waitUnresolved(kagentv1alpha3.EgressPoliciesReasonInvalidEgressPolicy, "Secret")
	secret, err := kube.Kube().CoreV1().Secrets(template.Namespace).Create(t.Context(), &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Namespace: template.Namespace, Name: "ticket-auth"}, Data: map[string][]byte{"token": []byte("original-token")}}, metav1.CreateOptions{})
	require.NoError(t, err)
	require.Eventually(t, ready, 10*time.Second, 10*time.Millisecond)
	current, err := client.ApiV1alpha3().SandboxTemplates(template.Namespace).Get(t.Context(), template.Name, metav1.GetOptions{})
	require.NoError(t, err)
	require.Contains(t, current.Finalizers, sandboxPreparationFinalizer)
	store.mu.Lock()
	revision := store.revision.Revision
	require.True(t, store.ready)
	require.NotContains(t, string(store.revision.SourceSnapshot), "original-token")
	store.mu.Unlock()
	other, err := client.ApiV1alpha3().SandboxTemplates(unwatched.Namespace).Get(t.Context(), unwatched.Name, metav1.GetOptions{})
	require.NoError(t, err)
	require.Empty(t, other.Finalizers, "namespace filters must apply to the new informer")
	require.Empty(t, other.Status.Conditions)

	// A missing key revokes its policy without changing runtime readiness.
	// Restoring it admits grants without serializing either value.
	secret.Data = nil
	secret, err = kube.Kube().CoreV1().Secrets(template.Namespace).Update(t.Context(), secret, metav1.UpdateOptions{})
	require.NoError(t, err)
	waitUnresolved(kagentv1alpha3.EgressPoliciesReasonInvalidEgressPolicy, "missing key")
	require.True(t, ready())
	secret.Data = map[string][]byte{"token": []byte("rotated-token")}
	_, err = kube.Kube().CoreV1().Secrets(template.Namespace).Update(t.Context(), secret, metav1.UpdateOptions{})
	require.NoError(t, err)
	require.Eventually(t, ready, 10*time.Second, 10*time.Millisecond)
	store.mu.Lock()
	require.Equal(t, revision, store.revision.Revision)
	store.mu.Unlock()
	grant.Spec.Destinations = append(grant.Spec.Destinations, kagentv1alpha3.EgressDestination{Origin: "https://registry.npmjs.org"})
	grant, err = client.ApiV1alpha3().EgressPolicies(template.Namespace).Update(t.Context(), grant, metav1.UpdateOptions{})
	require.NoError(t, err)
	require.NoError(t, client.ApiV1alpha3().EgressPolicies(template.Namespace).Delete(t.Context(), grant.Name, metav1.DeleteOptions{}))
	waitUnresolved(kagentv1alpha3.EgressPoliciesReasonNotFound, "tickets")
	require.Eventually(t, ready, 10*time.Second, 10*time.Millisecond)
	grant.ResourceVersion, grant.UID = "", ""
	_, err = client.ApiV1alpha3().EgressPolicies(template.Namespace).Create(t.Context(), grant, metav1.CreateOptions{})
	require.NoError(t, err)
	require.Eventually(t, ready, 10*time.Second, 10*time.Millisecond)

	store.mu.Lock()
	require.Equal(t, revision, store.revision.Revision, "policy changes do not reprepare the runtime")
	store.mu.Unlock()

	// A dependency update alone must change the persisted revision.
	changedPool := pool.DeepCopy()
	changedPool.Spec.SandboxClasses = []atev1.WorkerPoolSandboxClass{{Name: atev1.SandboxClassMicroVM}}
	pools.UpdateObject(changedPool)
	require.Eventually(t, func() bool {
		store.mu.Lock()
		defer store.mu.Unlock()
		return store.ready && store.revision.Revision != revision
	}, 10*time.Second, 10*time.Millisecond)
	pools.DeleteObject("team-a/default")
	require.Eventually(t, func() bool {
		current, err := client.ApiV1alpha3().SandboxTemplates(template.Namespace).Get(t.Context(), template.Name, metav1.GetOptions{})
		if err != nil {
			return false
		}
		condition := apimeta.FindStatusCondition(current.Status.Conditions, "Ready")
		return condition != nil && condition.Status == metav1.ConditionFalse && condition.Reason == "WorkerPoolNotFound"
	}, 10*time.Second, 10*time.Millisecond)
	pools.UpdateObject(changedPool)
	require.Eventually(t, ready, 10*time.Second, 10*time.Millisecond)
	stop()

	// Delete while the controller is offline. A failed DB cleanup must retain
	// the finalizer until a restarted controller can durably retire preparation.
	require.NoError(t, client.ApiV1alpha3().SandboxTemplates(template.Namespace).Delete(t.Context(), template.Name, metav1.DeleteOptions{}))
	current, err = client.ApiV1alpha3().SandboxTemplates(template.Namespace).Get(t.Context(), template.Name, metav1.GetOptions{})
	require.NoError(t, err)
	require.False(t, current.DeletionTimestamp.IsZero())
	store.mu.Lock()
	store.retireErr = errors.New("database unavailable")
	store.mu.Unlock()
	stop, _ = start()
	require.Eventually(t, func() bool {
		store.mu.Lock()
		defer store.mu.Unlock()
		return store.retireAttempts > 0 && !store.retired
	}, 5*time.Second, 10*time.Millisecond)
	current, err = client.ApiV1alpha3().SandboxTemplates(template.Namespace).Get(t.Context(), template.Name, metav1.GetOptions{})
	require.NoError(t, err)
	require.Contains(t, current.Finalizers, sandboxPreparationFinalizer)
	// The queue must retry retirement when the database recovers.
	store.mu.Lock()
	store.retireErr = nil
	store.mu.Unlock()
	require.Eventually(t, func() bool {
		_, err := client.ApiV1alpha3().SandboxTemplates(template.Namespace).Get(t.Context(), template.Name, metav1.GetOptions{})
		return apierrors.IsNotFound(err)
	}, 10*time.Second, 10*time.Millisecond)
	stop()
	store.mu.Lock()
	require.True(t, store.retired)
	store.mu.Unlock()
}
