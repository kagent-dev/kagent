/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package modelproviderconfig

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	apiMeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	"github.com/kagent-dev/kagent/go/api/v1alpha3"
)

func request(config *v1alpha3.ModelProviderConfig) ctrl.Request {
	return ctrl.Request{NamespacedName: client.ObjectKeyFromObject(config)}
}

func readyCondition(t *testing.T, kube client.Client, config *v1alpha3.ModelProviderConfig) (*v1alpha3.ModelProviderConfig, *metav1.Condition) {
	t.Helper()
	got := &v1alpha3.ModelProviderConfig{}
	require.NoError(t, kube.Get(t.Context(), client.ObjectKeyFromObject(config), got))
	return got, apiMeta.FindStatusCondition(got.Status.Conditions, v1alpha3.ModelProviderConfigConditionTypeReady)
}

func TestReconcilePublishesDiscoveryStatus(t *testing.T) {
	t.Run("Given a reachable endpoint, when the config is reconciled, assert that Ready is true with the model count and a requeue", func(t *testing.T) {
		server, _ := serveModels(t, http.StatusOK, `{"data":[{"id":"a"},{"id":"b"}]}`)
		config := testConfig(server.URL, "")
		kube := testClient(t, config)

		result, err := New(kube, NewDiscoverer(kube)).Reconcile(t.Context(), request(config))

		require.NoError(t, err)
		require.Equal(t, 5*time.Minute, result.RequeueAfter)
		got, condition := readyCondition(t, kube, config)
		require.NotNil(t, condition)
		require.Equal(t, metav1.ConditionTrue, condition.Status)
		require.Equal(t, "DiscoverySucceeded", condition.Reason)
		require.Equal(t, "Discovered 2 models", condition.Message)
		require.Equal(t, 2, got.Status.ModelCount)
		require.Equal(t, int64(3), got.Status.ObservedGeneration)
	})

	t.Run("Given an endpoint that fails, when the config is reconciled, assert that Ready is false, the count resets and the error is returned", func(t *testing.T) {
		server, _ := serveModels(t, http.StatusBadGateway, "")
		config := testConfig(server.URL, "")
		config.Status.ModelCount = 7
		kube := testClient(t, config)

		_, err := New(kube, NewDiscoverer(kube)).Reconcile(t.Context(), request(config))

		require.ErrorContains(t, err, "502")
		got, condition := readyCondition(t, kube, config)
		require.NotNil(t, condition)
		require.Equal(t, metav1.ConditionFalse, condition.Status)
		require.Equal(t, "DiscoveryFailed", condition.Reason)
		require.Contains(t, condition.Message, "502")
		require.Zero(t, got.Status.ModelCount)
	})

	t.Run("Given an unchanged status, when the config is reconciled again, assert that no status write is issued", func(t *testing.T) {
		server, _ := serveModels(t, http.StatusOK, `{"data":[{"id":"a"}]}`)
		config := testConfig(server.URL, "")
		var patches int
		kube := interceptedClient(t, config, &patches)
		reconciler := New(kube, NewDiscoverer(kube))

		_, err := reconciler.Reconcile(t.Context(), request(config))
		require.NoError(t, err)
		_, err = reconciler.Reconcile(t.Context(), request(config))

		require.NoError(t, err)
		require.Equal(t, 1, patches)
	})

	t.Run("Given an endpoint that keeps failing, when the config is reconciled again, assert that no status write is issued", func(t *testing.T) {
		server, _ := serveModels(t, http.StatusBadGateway, "")
		config := testConfig(server.URL, "")
		var patches int
		kube := interceptedClient(t, config, &patches)
		reconciler := New(kube, NewDiscoverer(kube))

		_, err := reconciler.Reconcile(t.Context(), request(config))
		require.Error(t, err)
		_, err = reconciler.Reconcile(t.Context(), request(config))

		require.Error(t, err)
		require.Equal(t, 1, patches)
	})

	t.Run("Given a deleted config, when it is reconciled, assert that nothing happens", func(t *testing.T) {
		config := testConfig("http://unused.invalid", "")
		kube := testClient(t)

		result, err := New(kube, NewDiscoverer(kube)).Reconcile(t.Context(), request(config))

		require.NoError(t, err)
		require.Zero(t, result)
	})
}

func interceptedClient(t *testing.T, config *v1alpha3.ModelProviderConfig, patches *int) client.Client {
	t.Helper()
	base := testClient(t, config)
	return interceptor.NewClient(base.(client.WithWatch), interceptor.Funcs{
		SubResourcePatch: func(ctx context.Context, c client.Client, sub string, obj client.Object, patch client.Patch, opts ...client.SubResourcePatchOption) error {
			*patches++
			return c.SubResource(sub).Patch(ctx, obj, patch, opts...)
		},
	})
}

func TestRequestsForSecret(t *testing.T) {
	t.Run("Given configs in two namespaces, when a Secret changes, assert that only configs in its namespace referencing it are enqueued", func(t *testing.T) {
		matching := testConfig("https://a.example", "discovery")
		otherSecret := testConfig("https://b.example", "other")
		otherSecret.Name = "other-secret"
		noAuth := testConfig("https://c.example", "")
		noAuth.Name = "no-auth"
		otherNamespace := testConfig("https://d.example", "discovery")
		otherNamespace.Namespace = "elsewhere"
		kube := testClient(t, matching, otherSecret, noAuth, otherNamespace)

		got := New(kube, NewDiscoverer(kube)).requestsForSecret(t.Context(), testSecret("x"))

		require.Len(t, got, 1)
		require.Equal(t, types.NamespacedName{Namespace: "team", Name: "litellm"}, got[0].NamespacedName)
	})
}
