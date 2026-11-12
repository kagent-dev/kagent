package remotemcpserver

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	"github.com/kagent-dev/kagent/go/api/v1alpha3"
	"github.com/kagent-dev/kagent/go/core/pkg/consts"
)

func TestReferenceStatusAcrossDiscoveryModes(t *testing.T) {
	for _, mode := range []string{"discovery", "disabled", "actorJwt", "discovery failure"} {
		t.Run("Given a missing Secret key and "+mode+", when reconciled, assert that reference status recovers after repair", func(t *testing.T) {
			server := testServer()
			server.Spec.HeadersFrom = []v1alpha3.ValueRef{{Name: "Authorization", ValueFrom: &v1alpha3.ValueSource{Type: v1alpha3.SecretValueSource, Name: "credentials", Key: "key"}}}
			discoverer := &fakeDiscoverer{}
			switch mode {
			case "disabled":
				server.Labels = map[string]string{consts.DiscoveryLabel: consts.DiscoveryDisabled}
			case "actorJwt":
				server.Spec.ActorJWT = &v1alpha3.ActorJWT{}
			case "discovery failure":
				discoverer.err = errors.New("remote unavailable")
			}
			secret := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Namespace: server.Namespace, Name: "credentials"}}
			kube := testClient(t, server, secret)
			reconciler := New(kube, discoverer, &fakeCatalog{})
			request := ctrl.Request{NamespacedName: client.ObjectKeyFromObject(server)}
			_, _ = reconciler.Reconcile(t.Context(), request)
			updated := &v1alpha3.RemoteMCPServer{}
			require.NoError(t, kube.Get(t.Context(), request.NamespacedName, updated))
			condition := apimeta.FindStatusCondition(updated.Status.Conditions, v1alpha3.RemoteMCPServerConditionTypeResolvedRefs)
			require.NotNil(t, condition)
			require.Equal(t, metav1.ConditionFalse, condition.Status)
			require.Equal(t, v1alpha3.RemoteMCPServerReasonSecretKeyNotFound, condition.Reason)
			require.Equal(t, server.Generation, condition.ObservedGeneration)
			require.NotNil(t, apimeta.FindStatusCondition(updated.Status.Conditions, "Accepted"))
			secret.Data = map[string][]byte{"key": {}}
			require.NoError(t, kube.Update(t.Context(), secret))
			_, _ = reconciler.Reconcile(t.Context(), request)
			require.NoError(t, kube.Get(t.Context(), request.NamespacedName, updated))
			condition = apimeta.FindStatusCondition(updated.Status.Conditions, v1alpha3.RemoteMCPServerConditionTypeResolvedRefs)
			require.Equal(t, metav1.ConditionTrue, condition.Status)
			transition := condition.LastTransitionTime
			_, _ = reconciler.Reconcile(t.Context(), request)
			require.NoError(t, kube.Get(t.Context(), request.NamespacedName, updated))
			require.Equal(t, transition, apimeta.FindStatusCondition(updated.Status.Conditions, v1alpha3.RemoteMCPServerConditionTypeResolvedRefs).LastTransitionTime)
			if mode == "disabled" || mode == "actorJwt" {
				require.Zero(t, discoverer.calls)
			}
		})
	}
}

func TestReferenceStatusMissingObjects(t *testing.T) {
	for _, source := range []v1alpha3.ValueSourceType{v1alpha3.SecretValueSource, v1alpha3.ConfigMapValueSource} {
		t.Run("Given a missing "+string(source)+", when reconciled, assert that status identifies the unresolved object", func(t *testing.T) {
			server := testServer()
			server.Spec.HeadersFrom = []v1alpha3.ValueRef{{Name: "X-Key", ValueFrom: &v1alpha3.ValueSource{Type: source, Name: "missing", Key: "key"}}}
			kube := testClient(t, server)
			_, err := New(kube, &fakeDiscoverer{}, &fakeCatalog{}).Reconcile(t.Context(), ctrl.Request{NamespacedName: client.ObjectKeyFromObject(server)})
			require.NoError(t, err)
			updated := &v1alpha3.RemoteMCPServer{}
			require.NoError(t, kube.Get(t.Context(), client.ObjectKeyFromObject(server), updated))
			condition := apimeta.FindStatusCondition(updated.Status.Conditions, v1alpha3.RemoteMCPServerConditionTypeResolvedRefs)
			require.Equal(t, metav1.ConditionFalse, condition.Status)
			require.Contains(t, condition.Reason, "NotFound")
			require.Contains(t, condition.Message, "missing")
		})
	}
}

func TestReferenceReadFailureRetries(t *testing.T) {
	server := testServer()
	server.Spec.HeadersFrom = []v1alpha3.ValueRef{{Name: "X-Key", ValueFrom: &v1alpha3.ValueSource{Type: v1alpha3.SecretValueSource, Name: "credentials", Key: "key"}}}
	base := testClient(t, server)
	kube := fake.NewClientBuilder().WithScheme(base.Scheme()).WithObjects(server).WithStatusSubresource(server).WithInterceptorFuncs(interceptor.Funcs{
		Get: func(ctx context.Context, underlying client.WithWatch, key client.ObjectKey, object client.Object, options ...client.GetOption) error {
			if _, ok := object.(*corev1.Secret); ok {
				return errors.New("temporarily unavailable")
			}
			return underlying.Get(ctx, key, object, options...)
		},
	}).Build()
	discoverer := &fakeDiscoverer{}
	_, err := New(kube, discoverer, &fakeCatalog{}).Reconcile(t.Context(), ctrl.Request{NamespacedName: client.ObjectKeyFromObject(server)})
	require.ErrorContains(t, err, "temporarily unavailable")
	updated := &v1alpha3.RemoteMCPServer{}
	require.NoError(t, kube.Get(t.Context(), client.ObjectKeyFromObject(server), updated))
	require.Equal(t, metav1.ConditionUnknown, apimeta.FindStatusCondition(updated.Status.Conditions, v1alpha3.RemoteMCPServerConditionTypeResolvedRefs).Status)
	require.Zero(t, discoverer.calls)
}

func TestConfigMapReferencesRecoverWithoutDiscovery(t *testing.T) {
	server := testServer()
	server.Labels = map[string]string{consts.DiscoveryLabel: consts.DiscoveryDisabled}
	server.Spec.HeadersFrom = []v1alpha3.ValueRef{{Name: "X-Account", ValueFrom: &v1alpha3.ValueSource{Type: v1alpha3.ConfigMapValueSource, Name: "account", Key: "id"}}}
	configMap := &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Namespace: server.Namespace, Name: "account"}}
	kube := testClient(t, server, configMap)
	reconciler := New(kube, &fakeDiscoverer{}, &fakeCatalog{})
	request := ctrl.Request{NamespacedName: client.ObjectKeyFromObject(server)}
	_, err := reconciler.Reconcile(t.Context(), request)
	require.NoError(t, err)
	updated := &v1alpha3.RemoteMCPServer{}
	require.NoError(t, kube.Get(t.Context(), request.NamespacedName, updated))
	require.Equal(t, v1alpha3.RemoteMCPServerReasonConfigMapKeyNotFound, apimeta.FindStatusCondition(updated.Status.Conditions, v1alpha3.RemoteMCPServerConditionTypeResolvedRefs).Reason)
	configMap.Data = map[string]string{"id": ""}
	require.NoError(t, kube.Update(t.Context(), configMap))
	requests := reconciler.requestsForDependency(t.Context(), configMap)
	require.Contains(t, requests, request)
	_, err = reconciler.Reconcile(t.Context(), request)
	require.NoError(t, err)
	require.NoError(t, kube.Get(t.Context(), request.NamespacedName, updated))
	require.Equal(t, metav1.ConditionTrue, apimeta.FindStatusCondition(updated.Status.Conditions, v1alpha3.RemoteMCPServerConditionTypeResolvedRefs).Status)
	require.NoError(t, kube.Delete(t.Context(), configMap))
	_, err = reconciler.Reconcile(t.Context(), request)
	require.NoError(t, err)
	require.NoError(t, kube.Get(t.Context(), request.NamespacedName, updated))
	require.Equal(t, v1alpha3.RemoteMCPServerReasonConfigMapNotFound, apimeta.FindStatusCondition(updated.Status.Conditions, v1alpha3.RemoteMCPServerConditionTypeResolvedRefs).Reason)
}
