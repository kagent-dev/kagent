package chatgptlogin

import (
	"bytes"
	"errors"
	"io"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/kagent-dev/kagent/go/api/v1alpha3"
	"github.com/stretchr/testify/require"
	authorizationv1 "k8s.io/api/authorization/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	utilyaml "k8s.io/apimachinery/pkg/util/yaml"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

const (
	chartDir           = "../../../../../helm/kagent"
	releaseNamespace   = "kagent"
	refresherAccount   = "kagent-chatgpt-login-refresher"
	controllerAccount  = "kagent-controller"
	refresherTemplate  = "templates/rbac/chatgpt-login-refresher.yaml"
	vapAPIVersionCheck = "admissionregistration.k8s.io/v1/ValidatingAdmissionPolicy"
)

// renderRefresherRBAC renders the chart's refresher RBAC and admission policy
// for a release named kagent in the kagent namespace. The template and the
// chart's helpers are copied into a chart of their own, so the render needs
// none of the chart's dependencies.
func renderRefresherRBAC(t *testing.T) []*unstructured.Unstructured {
	t.Helper()
	helm, err := exec.LookPath("helm")
	if err != nil {
		if os.Getenv("CI") != "" {
			t.Fatal("helm is not on PATH")
		}
		t.Skip("helm is not on PATH")
	}
	chart := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(chart, "templates", "rbac"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(chart, "Chart.yaml"), []byte("apiVersion: v2\nname: kagent\nversion: 0.0.0\n"), 0o644))
	for _, file := range []string{"values.yaml", "templates/_helpers.tpl", refresherTemplate} {
		content, err := os.ReadFile(filepath.Join(chartDir, file))
		require.NoError(t, err)
		require.NoError(t, os.WriteFile(filepath.Join(chart, file), content, 0o644))
	}
	var stdout, stderr bytes.Buffer
	render := exec.Command(helm, "template", "kagent", chart, "--namespace", releaseNamespace, "--api-versions", vapAPIVersionCheck, "--show-only", refresherTemplate)
	render.Stdout, render.Stderr = &stdout, &stderr
	require.NoError(t, render.Run(), stderr.String())

	var objects []*unstructured.Unstructured
	decoder := utilyaml.NewYAMLOrJSONDecoder(&stdout, 4096)
	for {
		object := &unstructured.Unstructured{}
		if err := decoder.Decode(&object.Object); err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			require.NoError(t, err)
		}
		if len(object.Object) > 0 {
			objects = append(objects, object)
		}
	}
	return objects
}

// Against a real API server enforcing RBAC and admission policies, with the
// chart's refresher RBAC applied: the controller may impersonate the
// refresher's account and no other, and that account may update a Secret
// only while it is labelled as a ChatGPT login, and do nothing else. The
// refresher writing through it refreshes an opted-in login and leaves every
// other Secret as it is.
func TestRefresherRBACOnAnAPIServer(t *testing.T) {
	if os.Getenv("KUBEBUILDER_ASSETS") == "" {
		t.Skip("KUBEBUILDER_ASSETS is not set: run `make test` in go/, or export KUBEBUILDER_ASSETS=\"$(make -s envtest-path)\"")
	}
	objects := renderRefresherRBAC(t)
	testEnv := &envtest.Environment{CRDDirectoryPaths: []string{"../../../../api/config/crd/bases/api.kagent.dev_modelconfigs.yaml"}, ErrorIfCRDPathMissing: true}
	testEnv.ControlPlane.GetAPIServer().Configure().Set("authorization-mode", "RBAC")
	config, err := testEnv.Start()
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, testEnv.Stop()) })

	admin, err := client.New(config, client.Options{Scheme: scheme(t)})
	require.NoError(t, err)
	for _, namespace := range []string{releaseNamespace, "team"} {
		require.NoError(t, admin.Create(t.Context(), &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: namespace}}))
	}
	for _, object := range objects {
		require.NoError(t, admin.Create(t.Context(), object), "%s %s", object.GetKind(), object.GetName())
	}

	impersonated := rest.CopyConfig(config)
	impersonated.Impersonate = rest.ImpersonationConfig{UserName: "system:serviceaccount:" + releaseNamespace + ":" + refresherAccount}
	writer, err := client.New(impersonated, client.Options{Scheme: scheme(t)})
	require.NoError(t, err)

	now := time.Now()
	expiring := accessToken("access-1", now.Add(-time.Hour), now.Add(5*time.Minute))
	login := loginSecret(expiring)
	other := loginSecret(expiring)
	other.Name, other.Labels = "other", nil
	require.NoError(t, admin.Create(t.Context(), login))
	require.NoError(t, admin.Create(t.Context(), other))
	read := func(name string) *corev1.Secret {
		secret := &corev1.Secret{}
		require.NoError(t, admin.Get(t.Context(), types.NamespacedName{Namespace: "team", Name: name}, secret))
		return secret
	}

	t.Run("the controller may impersonate the refresher's account only", func(t *testing.T) {
		for account, allowed := range map[string]bool{refresherAccount: true, controllerAccount: false, "default": false} {
			review := &authorizationv1.SubjectAccessReview{Spec: authorizationv1.SubjectAccessReviewSpec{
				User: "system:serviceaccount:" + releaseNamespace + ":" + controllerAccount,
				ResourceAttributes: &authorizationv1.ResourceAttributes{
					Namespace: releaseNamespace, Verb: "impersonate", Resource: "serviceaccounts", Name: account,
				},
			}}
			require.NoError(t, admin.Create(t.Context(), review))
			require.Equal(t, allowed, review.Status.Allowed, account)
		}
	})

	t.Run("an unlabelled Secret is refused", func(t *testing.T) {
		// The policy comes into force asynchronously; the probes sent before
		// it may land. Once in force it refuses every update of an unlabelled
		// Secret by the refresher's account.
		require.Eventually(t, func() bool {
			secret := read("other")
			secret.Data["probe"] = []byte("x")
			return apierrors.IsForbidden(writer.Update(t.Context(), secret))
		}, 30*time.Second, 200*time.Millisecond)
		secret := read("other")
		secret.Data[v1alpha3.ChatGPTAccessTokenKey] = []byte("rewritten")
		err := writer.Update(t.Context(), secret)
		require.True(t, apierrors.IsForbidden(err), "%v", err)
		require.Contains(t, err.Error(), v1alpha3.ChatGPTLoginLabel+"=true")
		require.Equal(t, expiring, string(read("other").Data[v1alpha3.ChatGPTAccessTokenKey]))
	})

	t.Run("the label cannot be added or removed by the refresher", func(t *testing.T) {
		secret := read("other")
		secret.Labels = map[string]string{v1alpha3.ChatGPTLoginLabel: "true"}
		require.True(t, apierrors.IsForbidden(writer.Update(t.Context(), secret)))
		secret = read("login")
		secret.Labels = nil
		require.True(t, apierrors.IsForbidden(writer.Update(t.Context(), secret)))
	})

	t.Run("the account may only update Secrets", func(t *testing.T) {
		require.True(t, apierrors.IsForbidden(writer.Delete(t.Context(), read("login"))))
		require.True(t, apierrors.IsForbidden(writer.Patch(t.Context(), read("login"), client.RawPatch(types.MergePatchType, []byte(`{"data":{"probe":"eA=="}}`)))))
		created := loginSecret(expiring)
		created.Name = "created"
		require.True(t, apierrors.IsForbidden(writer.Create(t.Context(), created)))
		configMap := &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Namespace: "team", Name: "config"}}
		require.NoError(t, admin.Create(t.Context(), configMap))
		require.True(t, apierrors.IsForbidden(writer.Update(t.Context(), configMap)))
	})

	t.Run("the refresher refreshes the opted-in login and leaves the other", func(t *testing.T) {
		endpoint := &tokenEndpoint{valid: "refresh-1", lifetime: 10 * 24 * time.Hour, now: time.Now}
		server := httptest.NewServer(endpoint)
		t.Cleanup(server.Close)
		require.NoError(t, admin.Create(t.Context(), chatGPTModel("codex", "login")))
		require.NoError(t, admin.Create(t.Context(), chatGPTModel("codex-other", "other")))
		refresher := NewRefresher(admin, admin, writer, server.Client(), server.URL, ClientID)

		_, err := refresher.Reconcile(t.Context(), reconcile.Request{NamespacedName: types.NamespacedName{Namespace: "team", Name: "login"}})
		require.NoError(t, err)
		require.Equal(t, "refresh-2", string(read("login").Data[v1alpha3.ChatGPTRefreshTokenKey]))

		_, err = refresher.Reconcile(t.Context(), reconcile.Request{NamespacedName: types.NamespacedName{Namespace: "team", Name: "other"}})
		require.NoError(t, err)
		require.Equal(t, 1, endpoint.count())
		require.Equal(t, expiring, string(read("other").Data[v1alpha3.ChatGPTAccessTokenKey]))
	})
}
