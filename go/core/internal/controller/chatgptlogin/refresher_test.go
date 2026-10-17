package chatgptlogin

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/kagent-dev/kagent/go/api/v1alpha3"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

var epoch = time.Unix(1_800_000_000, 0)

// accessToken is an unsigned JWT with the given issue and expiry times.
func accessToken(name string, iat, exp time.Time) string {
	encode := func(v any) string {
		raw, _ := json.Marshal(v)
		return base64.RawURLEncoding.EncodeToString(raw)
	}
	return encode(map[string]string{"alg": "none"}) + "." + encode(map[string]any{"iat": iat.Unix(), "exp": exp.Unix(), "jti": name}) + ".sig"
}

func TestRefreshAt(t *testing.T) {
	for _, test := range []struct {
		name   string
		token  string
		want   time.Time
		wantOK bool
	}{
		{"long-lived token is refreshed the lead before expiry", accessToken("a", epoch, epoch.Add(10*24*time.Hour)), epoch.Add(10*24*time.Hour - maxLead), true},
		{"short-lived token is refreshed a quarter of its life before expiry", accessToken("a", epoch, epoch.Add(time.Hour)), epoch.Add(45 * time.Minute), true},
		{"opaque token has no readable expiry", "opaque", time.Time{}, false},
		{"undecodable payload", "a.!!!.c", time.Time{}, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			got, ok := RefreshAt([]byte(test.token))
			require.Equal(t, test.wantOK, ok)
			require.True(t, test.want.Equal(got), "RefreshAt = %v, want %v", got, test.want)
		})
	}
}

// tokenEndpoint is a stand-in for the OpenAI token endpoint. Like the real
// one, it accepts each refresh token once and rotates it.
type tokenEndpoint struct {
	mu       sync.Mutex
	valid    string
	calls    int
	status   int
	lifetime time.Duration
	now      func() time.Time
}

func (e *tokenEndpoint) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.calls++
	if err := r.ParseForm(); err != nil || r.PostForm.Get("grant_type") != "refresh_token" || r.PostForm.Get("client_id") != ClientID || r.Header.Get("Content-Type") != "application/x-www-form-urlencoded" {
		http.Error(w, `{"error":"invalid_request"}`, http.StatusBadRequest)
		return
	}
	if e.status != 0 {
		http.Error(w, `{"error":"refresh_token_reused"}`, e.status)
		return
	}
	if r.PostForm.Get("refresh_token") != e.valid {
		http.Error(w, `{"error":"refresh_token_reused"}`, http.StatusUnauthorized)
		return
	}
	e.valid = fmt.Sprintf("refresh-%d", e.calls+1)
	now := e.now()
	_ = json.NewEncoder(w).Encode(map[string]string{
		"access_token": accessToken(fmt.Sprintf("access-%d", e.calls+1), now, now.Add(e.lifetime)), "refresh_token": e.valid, "id_token": "id",
	})
}

func (e *tokenEndpoint) count() int {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.calls
}

func scheme(t *testing.T) *runtime.Scheme {
	t.Helper()
	s := runtime.NewScheme()
	require.NoError(t, clientgoscheme.AddToScheme(s))
	require.NoError(t, v1alpha3.AddToScheme(s))
	return s
}

func chatGPTModel(name, secret string) *v1alpha3.ModelConfig {
	return &v1alpha3.ModelConfig{
		ObjectMeta: metav1.ObjectMeta{Namespace: "team", Name: name},
		Spec: v1alpha3.ModelConfigSpec{
			Provider: v1alpha3.ModelProviderOpenAI, Model: "gpt-5.1-codex", APIKeySecret: secret,
			OpenAI: &v1alpha3.OpenAIConfig{CredentialType: v1alpha3.OpenAICredentialTypeChatGPTOAuth},
		},
	}
}

func loginSecret(access string) *corev1.Secret {
	return &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Namespace: "team", Name: "login", Labels: map[string]string{v1alpha3.ChatGPTLoginLabel: "true"}}, Data: map[string][]byte{
		v1alpha3.ChatGPTAccessTokenKey: []byte(access), v1alpha3.ChatGPTRefreshTokenKey: []byte("refresh-1"), v1alpha3.ChatGPTAccountIDKey: []byte("account"),
	}}
}

func TestReconcile(t *testing.T) {
	expiring := accessToken("access-1", epoch.Add(-10*24*time.Hour), epoch.Add(10*time.Minute))
	fresh := accessToken("access-1", epoch, epoch.Add(10*24*time.Hour))
	login := types.NamespacedName{Namespace: "team", Name: "login"}
	setup := func(t *testing.T, access string, objects ...client.Object) (*Refresher, client.Client, *tokenEndpoint, *time.Time) {
		now := epoch
		endpoint := &tokenEndpoint{valid: "refresh-1", lifetime: 10 * 24 * time.Hour, now: func() time.Time { return now }}
		server := httptest.NewServer(endpoint)
		t.Cleanup(server.Close)
		kube := fake.NewClientBuilder().WithScheme(scheme(t)).WithObjects(append(objects, loginSecret(access))...).Build()
		refresher := NewRefresher(kube, kube, kube, server.Client(), server.URL, ClientID)
		refresher.now = func() time.Time { return now }
		return refresher, kube, endpoint, &now
	}
	readLogin := func(t *testing.T, kube client.Client) *corev1.Secret {
		secret := &corev1.Secret{}
		require.NoError(t, kube.Get(t.Context(), login, secret))
		return secret
	}

	t.Run("a token not yet due is left alone", func(t *testing.T) {
		refresher, kube, endpoint, _ := setup(t, fresh, chatGPTModel("codex", "login"))
		result, err := refresher.Reconcile(t.Context(), reconcile.Request{NamespacedName: login})
		require.NoError(t, err)
		require.Equal(t, 10*24*time.Hour-maxLead, result.RequeueAfter)
		require.Zero(t, endpoint.count())
		require.Equal(t, fresh, string(readLogin(t, kube).Data[v1alpha3.ChatGPTAccessTokenKey]))
	})

	t.Run("a due token is refreshed once and the rotated refresh token stored", func(t *testing.T) {
		// Two ModelConfigs on one login share one refresh.
		refresher, kube, endpoint, now := setup(t, expiring, chatGPTModel("codex", "login"), chatGPTModel("codex-mini", "login"))
		result, err := refresher.Reconcile(t.Context(), reconcile.Request{NamespacedName: login})
		require.NoError(t, err)
		require.Equal(t, 1, endpoint.count())
		secret := readLogin(t, kube)
		require.Equal(t, "refresh-2", string(secret.Data[v1alpha3.ChatGPTRefreshTokenKey]))
		require.Equal(t, "account", string(secret.Data[v1alpha3.ChatGPTAccountIDKey]))
		due, ok := RefreshAt(secret.Data[v1alpha3.ChatGPTAccessTokenKey])
		require.True(t, ok)
		require.Equal(t, due.Sub(*now), result.RequeueAfter)
		// Every event for either ModelConfig before the next due time is a
		// no-op: one refresh per expiry.
		for range 3 {
			_, err = refresher.Reconcile(t.Context(), reconcile.Request{NamespacedName: login})
			require.NoError(t, err)
		}
		require.Equal(t, 1, endpoint.count())
		// At the next due time it refreshes again, with the rotated token.
		*now = due
		_, err = refresher.Reconcile(t.Context(), reconcile.Request{NamespacedName: login})
		require.NoError(t, err)
		require.Equal(t, 2, endpoint.count())
		require.Equal(t, "refresh-3", string(readLogin(t, kube).Data[v1alpha3.ChatGPTRefreshTokenKey]))
	})

	t.Run("a Secret no ChatGPT login names is never refreshed", func(t *testing.T) {
		apiKey := chatGPTModel("api", "login")
		apiKey.Spec.OpenAI.CredentialType = v1alpha3.OpenAICredentialTypeAPIKey
		refresher, _, endpoint, _ := setup(t, expiring, apiKey, chatGPTModel("elsewhere", "other-login"))
		result, err := refresher.Reconcile(t.Context(), reconcile.Request{NamespacedName: login})
		require.NoError(t, err)
		require.Zero(t, result)
		require.Zero(t, endpoint.count())
	})

	t.Run("a Secret that has not opted in is never written", func(t *testing.T) {
		refresher, kube, endpoint, _ := setup(t, expiring, chatGPTModel("codex", "login"))
		secret := readLogin(t, kube)
		secret.Labels = nil
		require.NoError(t, kube.Update(t.Context(), secret))
		result, err := refresher.Reconcile(t.Context(), reconcile.Request{NamespacedName: login})
		require.NoError(t, err)
		require.Zero(t, result)
		require.Zero(t, endpoint.count())
		require.Equal(t, expiring, string(readLogin(t, kube).Data[v1alpha3.ChatGPTAccessTokenKey]))
	})

	t.Run("a refused refresh keeps the login and fails the reconcile", func(t *testing.T) {
		refresher, kube, endpoint, _ := setup(t, expiring, chatGPTModel("codex", "login"))
		endpoint.status = http.StatusUnauthorized
		_, err := refresher.Reconcile(t.Context(), reconcile.Request{NamespacedName: login})
		require.ErrorContains(t, err, "refresh_token_reused")
		require.Equal(t, expiring, string(readLogin(t, kube).Data[v1alpha3.ChatGPTAccessTokenKey]))
		require.Equal(t, "refresh-1", string(readLogin(t, kube).Data[v1alpha3.ChatGPTRefreshTokenKey]))
	})

	t.Run("a token without a readable expiry is never refreshed on a guess", func(t *testing.T) {
		refresher, _, endpoint, _ := setup(t, "opaque", chatGPTModel("codex", "login"))
		_, err := refresher.Reconcile(t.Context(), reconcile.Request{NamespacedName: login})
		require.ErrorContains(t, err, "not a JWT with an expiry")
		require.Zero(t, endpoint.count())
	})
}

// Against a real API server and a running manager: a ChatGPT-login
// ModelConfig whose access token is about to expire gets its Secret
// refreshed exactly once, and later ModelConfig events do not refresh again.
func TestRefresherOnAManager(t *testing.T) {
	if os.Getenv("KUBEBUILDER_ASSETS") == "" {
		t.Skip("KUBEBUILDER_ASSETS is not set: run `make test` in go/, or export KUBEBUILDER_ASSETS=\"$(make -s envtest-path)\"")
	}
	testEnv := &envtest.Environment{CRDDirectoryPaths: []string{"../../../../api/config/crd/bases/api.kagent.dev_modelconfigs.yaml"}, ErrorIfCRDPathMissing: true}
	config, err := testEnv.Start()
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, testEnv.Stop()) })

	endpoint := &tokenEndpoint{valid: "refresh-1", lifetime: 10 * 24 * time.Hour, now: time.Now}
	server := httptest.NewServer(endpoint)
	t.Cleanup(server.Close)

	mgr, err := ctrl.NewManager(config, ctrl.Options{Scheme: scheme(t), Metrics: metricsserver.Options{BindAddress: "0"}})
	require.NoError(t, err)
	require.NoError(t, NewRefresher(mgr.GetClient(), mgr.GetAPIReader(), mgr.GetClient(), server.Client(), server.URL, ClientID).SetupWithManager(mgr))
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { done <- mgr.Start(ctx) }()
	t.Cleanup(func() {
		cancel()
		require.NoError(t, <-done)
	})

	kube, err := client.New(config, client.Options{Scheme: scheme(t)})
	require.NoError(t, err)
	require.NoError(t, kube.Create(t.Context(), &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "team"}}))
	now := time.Now()
	require.NoError(t, kube.Create(t.Context(), loginSecret(accessToken("access-1", now.Add(-time.Hour), now.Add(5*time.Minute)))))
	model := chatGPTModel("codex", "login")
	require.NoError(t, kube.Create(t.Context(), model))

	login := types.NamespacedName{Namespace: "team", Name: "login"}
	require.Eventually(t, func() bool {
		secret := &corev1.Secret{}
		return kube.Get(t.Context(), login, secret) == nil && string(secret.Data[v1alpha3.ChatGPTRefreshTokenKey]) == "refresh-2"
	}, 30*time.Second, 100*time.Millisecond)

	// A ModelConfig change enqueues the login again; the refreshed token is
	// not due, so the endpoint is not called a second time.
	model.Labels = map[string]string{"touched": "true"}
	require.NoError(t, kube.Update(t.Context(), model))
	require.Never(t, func() bool { return endpoint.count() > 1 }, 3*time.Second, 100*time.Millisecond)
	require.Equal(t, 1, endpoint.count())
}
