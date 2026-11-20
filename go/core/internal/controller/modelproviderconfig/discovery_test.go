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
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/kagent-dev/kagent/go/api/v1alpha3"
)

func TestParseModelIDs(t *testing.T) {
	scenarios := []struct {
		name    string
		body    string
		want    []string
		wantErr string
	}{
		{"Given duplicate, empty and unordered ids, when the list is parsed, assert that ids are sorted, unique and non-empty",
			`{"data":[{"id":"b"},{"id":""},{"id":"a"},{"id":"b"}]}`, []string{"a", "b"}, ""},
		{"Given an empty data array, when the list is parsed, assert that no models are returned",
			`{"data":[]}`, []string{}, ""},
		{"Given malformed JSON, when the list is parsed, assert that parsing fails",
			`{"data":`, nil, "parse models response"},
		{"Given a body without data, when the list is parsed, assert that parsing fails",
			`{"object":"list"}`, nil, "no data array"},
	}
	for _, scenario := range scenarios {
		t.Run(scenario.name, func(t *testing.T) {
			got, err := parseModelIDs([]byte(scenario.body))
			if scenario.wantErr != "" {
				require.ErrorContains(t, err, scenario.wantErr)
				return
			}
			require.NoError(t, err)
			require.Equal(t, scenario.want, got)
		})
	}
}

type recordedRequest struct {
	path, authorization, accept string
}

func serveModels(t *testing.T, status int, body string) (*httptest.Server, *recordedRequest) {
	t.Helper()
	seen := &recordedRequest{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		*seen = recordedRequest{path: r.URL.Path, authorization: r.Header.Get("Authorization"), accept: r.Header.Get("Accept")}
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(server.Close)
	return server, seen
}

func testConfig(endpoint string, secretName string) *v1alpha3.ModelProviderConfig {
	config := &v1alpha3.ModelProviderConfig{
		ObjectMeta: metav1.ObjectMeta{Namespace: "team", Name: "litellm", Generation: 3},
		Spec:       v1alpha3.ModelProviderConfigSpec{Type: v1alpha3.ModelProviderOpenAI, Endpoint: endpoint},
	}
	if secretName != "" {
		config.Spec.DiscoveryAuth = &v1alpha3.ModelDiscoveryAuth{SecretRef: corev1.SecretKeySelector{
			LocalObjectReference: corev1.LocalObjectReference{Name: secretName}, Key: "token",
		}}
	}
	return config
}

func testSecret(value string) *corev1.Secret {
	return &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Namespace: "team", Name: "discovery"},
		Data:       map[string][]byte{"token": []byte(value)},
	}
}

func testClient(t *testing.T, objects ...client.Object) client.Client {
	t.Helper()
	scheme := runtime.NewScheme()
	require.NoError(t, corev1.AddToScheme(scheme))
	require.NoError(t, v1alpha3.AddToScheme(scheme))
	return fake.NewClientBuilder().WithScheme(scheme).WithObjects(objects...).
		WithStatusSubresource(&v1alpha3.ModelProviderConfig{}).Build()
}

func TestDiscover(t *testing.T) {
	const body = `{"data":[{"id":"gpt-4o"},{"id":"claude"}]}`

	t.Run("Given a Secret token, when discovery runs, assert that models are listed from the models path with a Bearer header", func(t *testing.T) {
		server, seen := serveModels(t, http.StatusOK, body)
		discoverer := NewDiscoverer(testClient(t, testSecret("s3cret\n")))

		got, err := discoverer.discover(t.Context(), testConfig(server.URL+"/v1", "discovery"))

		require.NoError(t, err)
		require.Equal(t, []string{"claude", "gpt-4o"}, got)
		require.Equal(t, recordedRequest{path: "/v1/models", authorization: "Bearer s3cret", accept: "application/json"}, *seen)
	})

	t.Run("Given an endpoint with a trailing slash, when discovery runs, assert that the path has no double slash", func(t *testing.T) {
		server, seen := serveModels(t, http.StatusOK, body)

		_, err := NewDiscoverer(testClient(t)).discover(t.Context(), testConfig(server.URL+"/v1/", ""))

		require.NoError(t, err)
		require.Equal(t, "/v1/models", seen.path)
	})

	t.Run("Given no discoveryAuth, when discovery runs, assert that no Authorization header is sent", func(t *testing.T) {
		server, seen := serveModels(t, http.StatusOK, body)

		_, err := NewDiscoverer(testClient(t, testSecret("s3cret"))).discover(t.Context(), testConfig(server.URL, ""))

		require.NoError(t, err)
		require.Empty(t, seen.authorization)
	})

	t.Run("Given a non-2xx response, when discovery runs, assert that the error names the status but not the token or body", func(t *testing.T) {
		server, _ := serveModels(t, http.StatusUnauthorized, "echo s3cret")

		_, err := NewDiscoverer(testClient(t, testSecret("s3cret"))).discover(t.Context(), testConfig(server.URL, "discovery"))

		require.ErrorContains(t, err, "401")
		require.NotContains(t, err.Error(), "s3cret")
	})

	t.Run("Given an endpoint that redirects, when discovery runs, assert that the redirect is not followed and the token stays with the endpoint", func(t *testing.T) {
		target, followed := serveModels(t, http.StatusOK, body)
		redirecting := httptest.NewServer(http.RedirectHandler(target.URL+"/models", http.StatusFound))
		t.Cleanup(redirecting.Close)

		_, err := NewDiscoverer(testClient(t, testSecret("s3cret"))).discover(t.Context(), testConfig(redirecting.URL, "discovery"))

		require.ErrorContains(t, err, "302")
		require.Empty(t, followed.path)
	})

	t.Run("Given a missing Secret, when discovery runs, assert that it fails before any request", func(t *testing.T) {
		server, seen := serveModels(t, http.StatusOK, body)

		_, err := NewDiscoverer(testClient(t)).discover(t.Context(), testConfig(server.URL, "discovery"))

		require.ErrorContains(t, err, "discovery")
		require.Empty(t, seen.path)
	})

	t.Run("Given an empty Secret value, when discovery runs, assert that it fails", func(t *testing.T) {
		server, _ := serveModels(t, http.StatusOK, body)

		_, err := NewDiscoverer(testClient(t, testSecret(" "))).discover(t.Context(), testConfig(server.URL, "discovery"))

		require.ErrorContains(t, err, "no value")
	})

	t.Run("Given a body over the size cap, when discovery runs, assert that it fails", func(t *testing.T) {
		huge := `{"data":[{"id":"` + strings.Repeat("a", maxResponseBytes) + `"}]}`
		server, _ := serveModels(t, http.StatusOK, huge)

		_, err := NewDiscoverer(testClient(t)).discover(t.Context(), testConfig(server.URL, ""))

		require.ErrorContains(t, err, "exceeds")
	})
}

func TestRefreshModelProviderConfigModels(t *testing.T) {
	t.Run("Given a stored config, when a refresh is requested by name, assert that its models are returned", func(t *testing.T) {
		server, _ := serveModels(t, http.StatusOK, `{"data":[{"id":"m1"}]}`)
		config := testConfig(server.URL, "")

		got, err := NewDiscoverer(testClient(t, config)).RefreshModelProviderConfigModels(t.Context(), "team", "litellm")

		require.NoError(t, err)
		require.Equal(t, []string{"m1"}, got)
	})
}
