package agent

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/kagent-dev/kagent/go/api/v1alpha2"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	k8sruntime "k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestAdkApiTranslator_IsInternalK8sURL(t *testing.T) {
	scheme := k8sruntime.NewScheme()
	require.NoError(t, corev1.AddToScheme(scheme))
	require.NoError(t, v1alpha2.AddToScheme(scheme))

	namespace := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{Name: "kagent"},
	}

	kubeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(namespace).Build()
	translatorImpl := NewAdkApiTranslatorWithWatchedNamespaces(kubeClient, nil, types.NamespacedName{Name: "default-model"}, nil, "", nil).WithClusterDomain("cluster.local").(*adkApiTranslator)

	require.True(t, translatorImpl.isInternalK8sURL(context.Background(), "http://grafana.kagent.svc.cluster.local:3000/api", "kagent"), "should recognize fully qualified service DNS as internal")
	require.True(t, translatorImpl.isInternalK8sURL(context.Background(), "http://grafana.kagent.svc:3000/api", "kagent"), "should recognize service.namespace.svc shorthand as internal")
	require.True(t, translatorImpl.isInternalK8sURL(context.Background(), "http://grafana.kagent:3000/api", "kagent"), "should recognize service.namespace shorthand as internal")

	require.False(t, translatorImpl.isInternalK8sURL(context.Background(), "http://grafana.kagent.svc.cluster.local.evil.com:3000/api", "kagent"), "should reject external domains that only contain the cluster suffix")
	require.False(t, translatorImpl.isInternalK8sURL(context.Background(), "http://example.com:8080", "kagent"), "should reject normal external URLs")

	translatorImpl.WithClusterDomain("custom.internal")
	require.True(t, translatorImpl.isInternalK8sURL(context.Background(), "http://grafana.kagent.svc.custom.internal:3000/api", "kagent"), "should honor custom cluster-domain values")
}
