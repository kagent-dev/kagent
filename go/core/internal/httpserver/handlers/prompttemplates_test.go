package handlers_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gorilla/mux"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl_client "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	api "github.com/kagent-dev/kagent/go/api/httpapi"
	"github.com/kagent-dev/kagent/go/core/internal/httpserver/auth"
	"github.com/kagent-dev/kagent/go/core/internal/httpserver/handlers"
)

func TestPromptTemplatesHandler(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, corev1.AddToScheme(scheme))

	t.Run("HandleListPromptTemplates requires namespace", func(t *testing.T) {
		kubeClient := fake.NewClientBuilder().WithScheme(scheme).Build()
		base := &handlers.Base{KubeClient: kubeClient, Authorizer: &auth.NoopAuthorizer{}}
		h := handlers.NewPromptTemplatesHandler(base)
		w := newMockErrorResponseWriter()
		req := httptest.NewRequest(http.MethodGet, "/api/prompttemplates", nil)
		req = setUser(req, "u1")
		h.HandleListPromptTemplates(w, req)
		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("HandleListPromptTemplates lists all ConfigMaps in namespace", func(t *testing.T) {
		custom := &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: "ns1",
				Name:      "team-prompts",
			},
			Data: map[string]string{"rules": "be nice"},
		}
		builtin := &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{Namespace: "ns1", Name: "kagent-builtin-prompts"},
			Data:       map[string]string{"skills-usage": "skills"},
		}
		kubeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(custom, builtin).Build()
		base := &handlers.Base{KubeClient: kubeClient, Authorizer: &auth.NoopAuthorizer{}}
		h := handlers.NewPromptTemplatesHandler(base)
		w := newMockErrorResponseWriter()
		req := httptest.NewRequest(http.MethodGet, "/api/prompttemplates?namespace=ns1", nil)
		req = setUser(req, "u1")
		h.HandleListPromptTemplates(w, req)
		require.Equal(t, http.StatusOK, w.Code)
		var resp api.StandardResponse[[]api.PromptTemplateSummary]
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
		require.Len(t, resp.Data, 2)
	})

	t.Run("create then update", func(t *testing.T) {
		kubeClient := fake.NewClientBuilder().WithScheme(scheme).Build()
		base := &handlers.Base{KubeClient: kubeClient, Authorizer: &auth.NoopAuthorizer{}}
		h := handlers.NewPromptTemplatesHandler(base)

		body := api.CreatePromptTemplateRequest{
			Namespace: "ns1",
			Name:      "my-lib",
			Data:      map[string]string{"intro": "hello"},
		}
		b, err := json.Marshal(body)
		require.NoError(t, err)
		req := httptest.NewRequest(http.MethodPost, "/api/prompttemplates", bytes.NewReader(b))
		req.Header.Set("Content-Type", "application/json")
		req = setUser(req, "u1")
		w := newMockErrorResponseWriter()
		h.HandleCreatePromptTemplate(w, req)
		require.Equal(t, http.StatusCreated, w.Code)

		up := api.UpdatePromptTemplateRequest{Data: map[string]string{"intro": "updated"}}
		ub, err := json.Marshal(up)
		require.NoError(t, err)
		req2 := httptest.NewRequest(http.MethodPut, "/api/prompttemplates/ns1/my-lib", bytes.NewReader(ub))
		req2.Header.Set("Content-Type", "application/json")
		req2 = mux.SetURLVars(req2, map[string]string{"namespace": "ns1", "name": "my-lib"})
		req2 = setUser(req2, "u1")
		w2 := newMockErrorResponseWriter()
		h.HandleUpdatePromptTemplate(w2, req2)
		require.Equal(t, http.StatusOK, w2.Code)

		var cm corev1.ConfigMap
		require.NoError(t, kubeClient.Get(context.Background(), ctrl_client.ObjectKey{Namespace: "ns1", Name: "my-lib"}, &cm))
		assert.Equal(t, "updated", cm.Data["intro"])
	})

	t.Run("cannot update builtin", func(t *testing.T) {
		builtin := &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{Namespace: "ns1", Name: "kagent-builtin-prompts"},
			Data:       map[string]string{"k": "v"},
		}
		kubeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(builtin).Build()
		base := &handlers.Base{KubeClient: kubeClient, Authorizer: &auth.NoopAuthorizer{}}
		h := handlers.NewPromptTemplatesHandler(base)
		up := api.UpdatePromptTemplateRequest{Data: map[string]string{"k": "x"}}
		ub, err := json.Marshal(up)
		require.NoError(t, err)
		req := httptest.NewRequest(http.MethodPut, "/api/prompttemplates/ns1/kagent-builtin-prompts", bytes.NewReader(ub))
		req.Header.Set("Content-Type", "application/json")
		req = mux.SetURLVars(req, map[string]string{"namespace": "ns1", "name": "kagent-builtin-prompts"})
		req = setUser(req, "u1")
		w := newMockErrorResponseWriter()
		h.HandleUpdatePromptTemplate(w, req)
		assert.Equal(t, http.StatusForbidden, w.Code)
	})
}
