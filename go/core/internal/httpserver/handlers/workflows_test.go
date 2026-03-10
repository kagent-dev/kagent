package handlers_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gorilla/mux"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl_client "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	api "github.com/kagent-dev/kagent/go/api/httpapi"
	"github.com/kagent-dev/kagent/go/api/v1alpha2"
	"github.com/kagent-dev/kagent/go/core/internal/httpserver/auth"
	authimpl "github.com/kagent-dev/kagent/go/core/internal/httpserver/auth"
	"github.com/kagent-dev/kagent/go/core/internal/httpserver/handlers"
	kagentauth "github.com/kagent-dev/kagent/go/core/pkg/auth"
)

func setupWorkflowsHandler(objs ...ctrl_client.Object) (*handlers.WorkflowsHandler, ctrl_client.Client, *mockErrorResponseWriter) {
	scheme := runtime.NewScheme()
	_ = v1alpha2.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)

	kubeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(objs...).
		WithStatusSubresource(&v1alpha2.WorkflowTemplate{}, &v1alpha2.WorkflowRun{}).
		Build()
	base := &handlers.Base{
		KubeClient:         kubeClient,
		DefaultModelConfig: types.NamespacedName{Namespace: "default", Name: "default"},
		Authorizer:         &auth.NoopAuthorizer{},
	}
	handler := handlers.NewWorkflowsHandler(base)
	recorder := newMockErrorResponseWriter()
	return handler, kubeClient, recorder
}

func workflowSetUser(req *http.Request, userID string) *http.Request {
	ctx := kagentauth.AuthSessionTo(req.Context(), &authimpl.SimpleSession{
		P: kagentauth.Principal{
			User: kagentauth.User{
				ID: userID,
			},
		},
	})
	return req.WithContext(ctx)
}

func withVars(r *http.Request, vars map[string]string) *http.Request {
	return mux.SetURLVars(r, vars)
}

func TestWorkflowsHandler_ListTemplates(t *testing.T) {
	t.Run("empty list", func(t *testing.T) {
		handler, _, recorder := setupWorkflowsHandler()

		req := httptest.NewRequest("GET", "/api/workflow-templates", nil)
		req = workflowSetUser(req, "test-user")
		handler.HandleListWorkflowTemplates(recorder, req)

		require.Equal(t, http.StatusOK, recorder.Code)
		var resp api.StandardResponse[[]v1alpha2.WorkflowTemplate]
		require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &resp))
		require.Len(t, resp.Data, 0)
	})

	t.Run("returns templates", func(t *testing.T) {
		tmpl := &v1alpha2.WorkflowTemplate{
			ObjectMeta: metav1.ObjectMeta{Name: "build-test", Namespace: "default"},
			Spec: v1alpha2.WorkflowTemplateSpec{
				Steps: []v1alpha2.StepSpec{
					{Name: "step-a", Type: v1alpha2.StepTypeAction, Action: "noop"},
				},
			},
		}
		handler, _, recorder := setupWorkflowsHandler(tmpl)

		req := httptest.NewRequest("GET", "/api/workflow-templates", nil)
		req = workflowSetUser(req, "test-user")
		handler.HandleListWorkflowTemplates(recorder, req)

		require.Equal(t, http.StatusOK, recorder.Code)
		var resp api.StandardResponse[[]v1alpha2.WorkflowTemplate]
		require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &resp))
		require.Len(t, resp.Data, 1)
		require.Equal(t, "build-test", resp.Data[0].Name)
	})
}

func TestWorkflowsHandler_GetTemplate(t *testing.T) {
	t.Run("found", func(t *testing.T) {
		tmpl := &v1alpha2.WorkflowTemplate{
			ObjectMeta: metav1.ObjectMeta{Name: "my-tmpl", Namespace: "default"},
			Spec: v1alpha2.WorkflowTemplateSpec{
				Steps: []v1alpha2.StepSpec{
					{Name: "step-a", Type: v1alpha2.StepTypeAction, Action: "noop"},
				},
			},
		}
		handler, _, recorder := setupWorkflowsHandler(tmpl)

		req := httptest.NewRequest("GET", "/api/workflow-templates/default/my-tmpl", nil)
		req = workflowSetUser(req, "test-user")
		req = withVars(req, map[string]string{"namespace": "default", "name": "my-tmpl"})
		handler.HandleGetWorkflowTemplate(recorder, req)

		require.Equal(t, http.StatusOK, recorder.Code)
		var resp api.StandardResponse[v1alpha2.WorkflowTemplate]
		require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &resp))
		require.Equal(t, "my-tmpl", resp.Data.Name)
	})

	t.Run("not found", func(t *testing.T) {
		handler, _, recorder := setupWorkflowsHandler()

		req := httptest.NewRequest("GET", "/api/workflow-templates/default/missing", nil)
		req = workflowSetUser(req, "test-user")
		req = withVars(req, map[string]string{"namespace": "default", "name": "missing"})
		handler.HandleGetWorkflowTemplate(recorder, req)

		require.Equal(t, http.StatusNotFound, recorder.Code)
	})
}

func TestWorkflowsHandler_CreateRun(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		handler, _, recorder := setupWorkflowsHandler()

		body, _ := json.Marshal(api.CreateWorkflowRunRequest{
			Name:                "run-1",
			Namespace:           "default",
			WorkflowTemplateRef: "my-template",
			Params:              []v1alpha2.Param{{Name: "env", Value: "prod"}},
		})
		req := httptest.NewRequest("POST", "/api/workflow-runs", bytes.NewReader(body))
		req = workflowSetUser(req, "test-user")
		handler.HandleCreateWorkflowRun(recorder, req)

		require.Equal(t, http.StatusCreated, recorder.Code)
		var resp api.StandardResponse[v1alpha2.WorkflowRun]
		require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &resp))
		require.Equal(t, "run-1", resp.Data.Name)
		require.Equal(t, "my-template", resp.Data.Spec.WorkflowTemplateRef)
		require.Len(t, resp.Data.Spec.Params, 1)
	})

	t.Run("missing name", func(t *testing.T) {
		handler, _, recorder := setupWorkflowsHandler()

		body, _ := json.Marshal(api.CreateWorkflowRunRequest{
			WorkflowTemplateRef: "my-template",
		})
		req := httptest.NewRequest("POST", "/api/workflow-runs", bytes.NewReader(body))
		req = workflowSetUser(req, "test-user")
		handler.HandleCreateWorkflowRun(recorder, req)

		require.Equal(t, http.StatusBadRequest, recorder.Code)
	})

	t.Run("missing templateRef", func(t *testing.T) {
		handler, _, recorder := setupWorkflowsHandler()

		body, _ := json.Marshal(api.CreateWorkflowRunRequest{
			Name:      "run-1",
			Namespace: "default",
		})
		req := httptest.NewRequest("POST", "/api/workflow-runs", bytes.NewReader(body))
		req = workflowSetUser(req, "test-user")
		handler.HandleCreateWorkflowRun(recorder, req)

		require.Equal(t, http.StatusBadRequest, recorder.Code)
	})

	t.Run("conflict", func(t *testing.T) {
		existing := &v1alpha2.WorkflowRun{
			ObjectMeta: metav1.ObjectMeta{Name: "run-1", Namespace: "default"},
			Spec: v1alpha2.WorkflowRunSpec{
				WorkflowTemplateRef: "my-template",
			},
		}
		handler, _, recorder := setupWorkflowsHandler(existing)

		body, _ := json.Marshal(api.CreateWorkflowRunRequest{
			Name:                "run-1",
			Namespace:           "default",
			WorkflowTemplateRef: "my-template",
		})
		req := httptest.NewRequest("POST", "/api/workflow-runs", bytes.NewReader(body))
		req = workflowSetUser(req, "test-user")
		handler.HandleCreateWorkflowRun(recorder, req)

		require.Equal(t, http.StatusConflict, recorder.Code)
	})
}

func TestWorkflowsHandler_ListRuns(t *testing.T) {
	t.Run("returns runs", func(t *testing.T) {
		run := &v1alpha2.WorkflowRun{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "run-1",
				Namespace: "default",
				Labels:    map[string]string{"kagent.dev/workflow-template": "my-tmpl"},
			},
			Spec: v1alpha2.WorkflowRunSpec{
				WorkflowTemplateRef: "my-tmpl",
			},
		}
		handler, _, recorder := setupWorkflowsHandler(run)

		req := httptest.NewRequest("GET", "/api/workflow-runs", nil)
		req = workflowSetUser(req, "test-user")
		handler.HandleListWorkflowRuns(recorder, req)

		require.Equal(t, http.StatusOK, recorder.Code)
		var resp api.StandardResponse[[]v1alpha2.WorkflowRun]
		require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &resp))
		require.Len(t, resp.Data, 1)
	})

	t.Run("filter by status", func(t *testing.T) {
		run1 := &v1alpha2.WorkflowRun{
			ObjectMeta: metav1.ObjectMeta{Name: "run-1", Namespace: "default"},
			Spec:       v1alpha2.WorkflowRunSpec{WorkflowTemplateRef: "t"},
			Status:     v1alpha2.WorkflowRunStatus{Phase: "Running"},
		}
		run2 := &v1alpha2.WorkflowRun{
			ObjectMeta: metav1.ObjectMeta{Name: "run-2", Namespace: "default"},
			Spec:       v1alpha2.WorkflowRunSpec{WorkflowTemplateRef: "t"},
			Status:     v1alpha2.WorkflowRunStatus{Phase: "Succeeded"},
		}
		handler, _, recorder := setupWorkflowsHandler(run1, run2)

		req := httptest.NewRequest("GET", "/api/workflow-runs?status=Running", nil)
		req = workflowSetUser(req, "test-user")
		handler.HandleListWorkflowRuns(recorder, req)

		require.Equal(t, http.StatusOK, recorder.Code)
		var resp api.StandardResponse[[]v1alpha2.WorkflowRun]
		require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &resp))
		require.Len(t, resp.Data, 1)
		require.Equal(t, "run-1", resp.Data[0].Name)
	})
}

func TestWorkflowsHandler_GetRun(t *testing.T) {
	t.Run("found with step statuses", func(t *testing.T) {
		run := &v1alpha2.WorkflowRun{
			ObjectMeta: metav1.ObjectMeta{Name: "run-1", Namespace: "default"},
			Spec:       v1alpha2.WorkflowRunSpec{WorkflowTemplateRef: "t"},
			Status: v1alpha2.WorkflowRunStatus{
				Phase: "Running",
				Steps: []v1alpha2.StepStatus{
					{Name: "step-a", Phase: v1alpha2.StepPhaseSucceeded},
					{Name: "step-b", Phase: v1alpha2.StepPhaseRunning},
				},
			},
		}
		handler, _, recorder := setupWorkflowsHandler(run)

		req := httptest.NewRequest("GET", "/api/workflow-runs/default/run-1", nil)
		req = workflowSetUser(req, "test-user")
		req = withVars(req, map[string]string{"namespace": "default", "name": "run-1"})
		handler.HandleGetWorkflowRun(recorder, req)

		require.Equal(t, http.StatusOK, recorder.Code)
		var resp api.StandardResponse[v1alpha2.WorkflowRun]
		require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &resp))
		require.Len(t, resp.Data.Status.Steps, 2)
	})

	t.Run("not found", func(t *testing.T) {
		handler, _, recorder := setupWorkflowsHandler()

		req := httptest.NewRequest("GET", "/api/workflow-runs/default/missing", nil)
		req = workflowSetUser(req, "test-user")
		req = withVars(req, map[string]string{"namespace": "default", "name": "missing"})
		handler.HandleGetWorkflowRun(recorder, req)

		require.Equal(t, http.StatusNotFound, recorder.Code)
	})
}

func TestWorkflowsHandler_DeleteRun(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		run := &v1alpha2.WorkflowRun{
			ObjectMeta: metav1.ObjectMeta{Name: "run-1", Namespace: "default"},
			Spec:       v1alpha2.WorkflowRunSpec{WorkflowTemplateRef: "t"},
		}
		handler, _, recorder := setupWorkflowsHandler(run)

		req := httptest.NewRequest("DELETE", "/api/workflow-runs/default/run-1", nil)
		req = workflowSetUser(req, "test-user")
		req = withVars(req, map[string]string{"namespace": "default", "name": "run-1"})
		handler.HandleDeleteWorkflowRun(recorder, req)

		require.Equal(t, http.StatusOK, recorder.Code)
	})

	t.Run("not found", func(t *testing.T) {
		handler, _, recorder := setupWorkflowsHandler()

		req := httptest.NewRequest("DELETE", "/api/workflow-runs/default/missing", nil)
		req = workflowSetUser(req, "test-user")
		req = withVars(req, map[string]string{"namespace": "default", "name": "missing"})
		handler.HandleDeleteWorkflowRun(recorder, req)

		require.Equal(t, http.StatusNotFound, recorder.Code)
	})
}
