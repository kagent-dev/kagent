package controller

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	atev1alpha1 "github.com/agent-substrate/substrate/pkg/api/v1alpha1"
	"github.com/agent-substrate/substrate/pkg/proto/ateapipb"
	kagentclient "github.com/kagent-dev/kagent/go/api/clientset/versioned"
	kagentv1alpha3 "github.com/kagent-dev/kagent/go/api/v1alpha3"
	"github.com/kagent-dev/kagent/go/core/internal/controller/apiclient"
	"github.com/kagent-dev/kagent/go/core/internal/substrate"
	"github.com/stretchr/testify/require"
	"istio.io/istio/pkg/kube/krt"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
)

// Exercise the real registered SandboxTemplate informer, API resource versions,
// status writes, and restart cleanup. Only the external DB/Substrate work is fake.
func TestSandboxKRTInformerQueueAndRestartCleanup(t *testing.T) {
	testEnv := &envtest.Environment{CRDDirectoryPaths: []string{"../../../api/config/crd/bases/api.kagent.dev_sandboxtemplates.yaml"}, ErrorIfCRDPathMissing: true}
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
	pool := &atev1alpha1.WorkerPool{ObjectMeta: metav1.ObjectMeta{Namespace: template.Namespace, Name: "default"}}
	start := func() (func(), krt.StaticCollection[*atev1alpha1.WorkerPool]) {
		ctx, cancel := context.WithCancel(t.Context())
		runtimeClient, err := apiclient.New(config)
		require.NoError(t, err)
		options := krt.NewOptionsBuilder(ctx.Done(), "sandbox-integration", nil)
		pools := krt.NewStaticCollection(nil, []*atev1alpha1.WorkerPool{pool}, options.WithName("WorkerPools")...)
		runtime := &Runtime{Client: runtimeClient, Options: options, Collections: Collections{
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
	require.Eventually(t, ready, 10*time.Second, 10*time.Millisecond)
	current, err := client.ApiV1alpha3().SandboxTemplates(template.Namespace).Get(t.Context(), template.Name, metav1.GetOptions{})
	require.NoError(t, err)
	require.Contains(t, current.Finalizers, sandboxPreparationFinalizer)
	store.mu.Lock()
	revision := store.revision.Revision
	require.True(t, store.ready)
	store.mu.Unlock()
	other, err := client.ApiV1alpha3().SandboxTemplates(unwatched.Namespace).Get(t.Context(), unwatched.Name, metav1.GetOptions{})
	require.NoError(t, err)
	require.Empty(t, other.Finalizers, "namespace filters must apply to the new informer")
	require.Empty(t, other.Status.Conditions)

	// A dependency update alone must change the persisted revision.
	changedPool := pool.DeepCopy()
	changedPool.Spec.SandboxClass = atev1alpha1.SandboxClassMicroVM
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
