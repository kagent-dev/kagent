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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/kagent-dev/kagent/go/api/v1alpha2"
	database_fake "github.com/kagent-dev/kagent/go/internal/database/fake"
	"github.com/kagent-dev/kagent/go/internal/httpserver/auth"
	"github.com/kagent-dev/kagent/go/internal/httpserver/handlers"
	"github.com/kagent-dev/kagent/go/pkg/client/api"
)

func setupCronJobHandler() (*handlers.AgentCronJobsHandler, *fake.ClientBuilder, *runtime.Scheme) {
	scheme := runtime.NewScheme()
	_ = v1alpha2.AddToScheme(scheme)

	builder := fake.NewClientBuilder().WithScheme(scheme)
	return nil, builder, scheme // handler created per test after objects added
}

func newCronJobHandler(builder *fake.ClientBuilder) *handlers.AgentCronJobsHandler {
	kubeClient := builder.Build()
	dbClient := database_fake.NewClient()
	base := &handlers.Base{
		KubeClient:         kubeClient,
		DefaultModelConfig: types.NamespacedName{Namespace: "default", Name: "default"},
		DatabaseService:    dbClient,
		Authorizer:         &auth.NoopAuthorizer{},
	}
	return handlers.NewAgentCronJobsHandler(base)
}

func TestAgentCronJobsHandler(t *testing.T) {
	t.Run("HandleListCronJobs", func(t *testing.T) {
		t.Run("EmptyList", func(t *testing.T) {
			scheme := runtime.NewScheme()
			_ = v1alpha2.AddToScheme(scheme)
			builder := fake.NewClientBuilder().WithScheme(scheme)
			handler := newCronJobHandler(builder)
			rr := newMockErrorResponseWriter()

			req := httptest.NewRequest("GET", "/api/cronjobs", nil)
			req = setUser(req, "test-user")
			handler.HandleListCronJobs(rr, req)

			require.Equal(t, http.StatusOK, rr.Code)

			var resp api.StandardResponse[[]v1alpha2.AgentCronJob]
			err := json.Unmarshal(rr.Body.Bytes(), &resp)
			require.NoError(t, err)
			assert.Len(t, resp.Data, 0)
			assert.False(t, resp.Error)
		})

		t.Run("WithItems", func(t *testing.T) {
			scheme := runtime.NewScheme()
			_ = v1alpha2.AddToScheme(scheme)

			cj1 := &v1alpha2.AgentCronJob{
				ObjectMeta: metav1.ObjectMeta{Name: "cj1", Namespace: "default"},
				Spec:       v1alpha2.AgentCronJobSpec{Schedule: "*/5 * * * *", Prompt: "check health", AgentRef: "my-agent"},
			}
			cj2 := &v1alpha2.AgentCronJob{
				ObjectMeta: metav1.ObjectMeta{Name: "cj2", Namespace: "default"},
				Spec:       v1alpha2.AgentCronJobSpec{Schedule: "0 9 * * *", Prompt: "daily report", AgentRef: "report-agent"},
			}

			builder := fake.NewClientBuilder().WithScheme(scheme).WithObjects(cj1, cj2)
			handler := newCronJobHandler(builder)
			rr := newMockErrorResponseWriter()

			req := httptest.NewRequest("GET", "/api/cronjobs", nil)
			req = setUser(req, "test-user")
			handler.HandleListCronJobs(rr, req)

			require.Equal(t, http.StatusOK, rr.Code)

			var resp api.StandardResponse[[]v1alpha2.AgentCronJob]
			err := json.Unmarshal(rr.Body.Bytes(), &resp)
			require.NoError(t, err)
			assert.Len(t, resp.Data, 2)
		})
	})

	t.Run("HandleGetCronJob", func(t *testing.T) {
		t.Run("Success", func(t *testing.T) {
			scheme := runtime.NewScheme()
			_ = v1alpha2.AddToScheme(scheme)

			cj := &v1alpha2.AgentCronJob{
				ObjectMeta: metav1.ObjectMeta{Name: "my-cj", Namespace: "default"},
				Spec:       v1alpha2.AgentCronJobSpec{Schedule: "*/5 * * * *", Prompt: "check", AgentRef: "agent1"},
			}

			builder := fake.NewClientBuilder().WithScheme(scheme).WithObjects(cj)
			handler := newCronJobHandler(builder)
			rr := newMockErrorResponseWriter()

			req := httptest.NewRequest("GET", "/api/cronjobs/default/my-cj", nil)
			req = setUser(req, "test-user")

			router := mux.NewRouter()
			router.HandleFunc("/api/cronjobs/{namespace}/{name}", func(w http.ResponseWriter, r *http.Request) {
				handler.HandleGetCronJob(rr, r)
			}).Methods("GET")
			router.ServeHTTP(rr, req)

			require.Equal(t, http.StatusOK, rr.Code)

			var resp api.StandardResponse[v1alpha2.AgentCronJob]
			err := json.Unmarshal(rr.Body.Bytes(), &resp)
			require.NoError(t, err)
			assert.Equal(t, "my-cj", resp.Data.Name)
			assert.Equal(t, "*/5 * * * *", resp.Data.Spec.Schedule)
		})

		t.Run("NotFound", func(t *testing.T) {
			scheme := runtime.NewScheme()
			_ = v1alpha2.AddToScheme(scheme)

			builder := fake.NewClientBuilder().WithScheme(scheme)
			handler := newCronJobHandler(builder)
			rr := newMockErrorResponseWriter()

			req := httptest.NewRequest("GET", "/api/cronjobs/default/nonexistent", nil)
			req = setUser(req, "test-user")

			router := mux.NewRouter()
			router.HandleFunc("/api/cronjobs/{namespace}/{name}", func(w http.ResponseWriter, r *http.Request) {
				handler.HandleGetCronJob(rr, r)
			}).Methods("GET")
			router.ServeHTTP(rr, req)

			require.Equal(t, http.StatusNotFound, rr.Code)
			require.NotNil(t, rr.errorReceived)
		})
	})

	t.Run("HandleCreateCronJob", func(t *testing.T) {
		t.Run("Success", func(t *testing.T) {
			scheme := runtime.NewScheme()
			_ = v1alpha2.AddToScheme(scheme)

			builder := fake.NewClientBuilder().WithScheme(scheme)
			handler := newCronJobHandler(builder)
			rr := newMockErrorResponseWriter()

			cronJob := v1alpha2.AgentCronJob{
				ObjectMeta: metav1.ObjectMeta{Name: "new-cj", Namespace: "default"},
				Spec:       v1alpha2.AgentCronJobSpec{Schedule: "0 */2 * * *", Prompt: "do stuff", AgentRef: "my-agent"},
			}
			body, _ := json.Marshal(cronJob)

			req := httptest.NewRequest("POST", "/api/cronjobs", bytes.NewBuffer(body))
			req.Header.Set("Content-Type", "application/json")
			req = setUser(req, "test-user")
			handler.HandleCreateCronJob(rr, req)

			require.Equal(t, http.StatusCreated, rr.Code)

			var resp api.StandardResponse[v1alpha2.AgentCronJob]
			err := json.Unmarshal(rr.Body.Bytes(), &resp)
			require.NoError(t, err)
			assert.Equal(t, "new-cj", resp.Data.Name)
			assert.Equal(t, "default", resp.Data.Namespace)
		})

		t.Run("MissingName", func(t *testing.T) {
			scheme := runtime.NewScheme()
			_ = v1alpha2.AddToScheme(scheme)

			builder := fake.NewClientBuilder().WithScheme(scheme)
			handler := newCronJobHandler(builder)
			rr := newMockErrorResponseWriter()

			cronJob := v1alpha2.AgentCronJob{
				ObjectMeta: metav1.ObjectMeta{Namespace: "default"},
				Spec:       v1alpha2.AgentCronJobSpec{Schedule: "0 * * * *", Prompt: "do stuff", AgentRef: "agent"},
			}
			body, _ := json.Marshal(cronJob)

			req := httptest.NewRequest("POST", "/api/cronjobs", bytes.NewBuffer(body))
			req.Header.Set("Content-Type", "application/json")
			req = setUser(req, "test-user")
			handler.HandleCreateCronJob(rr, req)

			require.Equal(t, http.StatusBadRequest, rr.Code)
			require.NotNil(t, rr.errorReceived)
		})

		t.Run("MissingRequiredFields", func(t *testing.T) {
			scheme := runtime.NewScheme()
			_ = v1alpha2.AddToScheme(scheme)

			builder := fake.NewClientBuilder().WithScheme(scheme)
			handler := newCronJobHandler(builder)
			rr := newMockErrorResponseWriter()

			cronJob := v1alpha2.AgentCronJob{
				ObjectMeta: metav1.ObjectMeta{Name: "cj", Namespace: "default"},
				Spec:       v1alpha2.AgentCronJobSpec{Schedule: "0 * * * *"},
			}
			body, _ := json.Marshal(cronJob)

			req := httptest.NewRequest("POST", "/api/cronjobs", bytes.NewBuffer(body))
			req.Header.Set("Content-Type", "application/json")
			req = setUser(req, "test-user")
			handler.HandleCreateCronJob(rr, req)

			require.Equal(t, http.StatusBadRequest, rr.Code)
		})

		t.Run("AlreadyExists", func(t *testing.T) {
			scheme := runtime.NewScheme()
			_ = v1alpha2.AddToScheme(scheme)

			existing := &v1alpha2.AgentCronJob{
				ObjectMeta: metav1.ObjectMeta{Name: "existing-cj", Namespace: "default"},
				Spec:       v1alpha2.AgentCronJobSpec{Schedule: "0 * * * *", Prompt: "old", AgentRef: "agent"},
			}

			builder := fake.NewClientBuilder().WithScheme(scheme).WithObjects(existing)
			handler := newCronJobHandler(builder)
			rr := newMockErrorResponseWriter()

			cronJob := v1alpha2.AgentCronJob{
				ObjectMeta: metav1.ObjectMeta{Name: "existing-cj", Namespace: "default"},
				Spec:       v1alpha2.AgentCronJobSpec{Schedule: "0 * * * *", Prompt: "new", AgentRef: "agent"},
			}
			body, _ := json.Marshal(cronJob)

			req := httptest.NewRequest("POST", "/api/cronjobs", bytes.NewBuffer(body))
			req.Header.Set("Content-Type", "application/json")
			req = setUser(req, "test-user")
			handler.HandleCreateCronJob(rr, req)

			require.Equal(t, http.StatusConflict, rr.Code)
			require.NotNil(t, rr.errorReceived)
		})

		t.Run("InvalidJSON", func(t *testing.T) {
			scheme := runtime.NewScheme()
			_ = v1alpha2.AddToScheme(scheme)

			builder := fake.NewClientBuilder().WithScheme(scheme)
			handler := newCronJobHandler(builder)
			rr := newMockErrorResponseWriter()

			req := httptest.NewRequest("POST", "/api/cronjobs", bytes.NewBufferString("not json"))
			req.Header.Set("Content-Type", "application/json")
			req = setUser(req, "test-user")
			handler.HandleCreateCronJob(rr, req)

			require.Equal(t, http.StatusBadRequest, rr.Code)
		})
	})

	t.Run("HandleUpdateCronJob", func(t *testing.T) {
		t.Run("Success", func(t *testing.T) {
			scheme := runtime.NewScheme()
			_ = v1alpha2.AddToScheme(scheme)

			existing := &v1alpha2.AgentCronJob{
				ObjectMeta: metav1.ObjectMeta{Name: "update-cj", Namespace: "default"},
				Spec:       v1alpha2.AgentCronJobSpec{Schedule: "0 * * * *", Prompt: "old prompt", AgentRef: "agent"},
			}

			builder := fake.NewClientBuilder().WithScheme(scheme).WithObjects(existing)
			handler := newCronJobHandler(builder)
			rr := newMockErrorResponseWriter()

			update := v1alpha2.AgentCronJob{
				Spec: v1alpha2.AgentCronJobSpec{Schedule: "*/10 * * * *", Prompt: "new prompt", AgentRef: "agent"},
			}
			body, _ := json.Marshal(update)

			req := httptest.NewRequest("PUT", "/api/cronjobs/default/update-cj", bytes.NewBuffer(body))
			req.Header.Set("Content-Type", "application/json")
			req = setUser(req, "test-user")

			router := mux.NewRouter()
			router.HandleFunc("/api/cronjobs/{namespace}/{name}", func(w http.ResponseWriter, r *http.Request) {
				handler.HandleUpdateCronJob(rr, r)
			}).Methods("PUT")
			router.ServeHTTP(rr, req)

			require.Equal(t, http.StatusOK, rr.Code)

			var resp api.StandardResponse[v1alpha2.AgentCronJob]
			err := json.Unmarshal(rr.Body.Bytes(), &resp)
			require.NoError(t, err)
			assert.Equal(t, "*/10 * * * *", resp.Data.Spec.Schedule)
			assert.Equal(t, "new prompt", resp.Data.Spec.Prompt)
		})

		t.Run("NotFound", func(t *testing.T) {
			scheme := runtime.NewScheme()
			_ = v1alpha2.AddToScheme(scheme)

			builder := fake.NewClientBuilder().WithScheme(scheme)
			handler := newCronJobHandler(builder)
			rr := newMockErrorResponseWriter()

			update := v1alpha2.AgentCronJob{
				Spec: v1alpha2.AgentCronJobSpec{Schedule: "0 * * * *", Prompt: "prompt", AgentRef: "agent"},
			}
			body, _ := json.Marshal(update)

			req := httptest.NewRequest("PUT", "/api/cronjobs/default/nonexistent", bytes.NewBuffer(body))
			req.Header.Set("Content-Type", "application/json")
			req = setUser(req, "test-user")

			router := mux.NewRouter()
			router.HandleFunc("/api/cronjobs/{namespace}/{name}", func(w http.ResponseWriter, r *http.Request) {
				handler.HandleUpdateCronJob(rr, r)
			}).Methods("PUT")
			router.ServeHTTP(rr, req)

			require.Equal(t, http.StatusNotFound, rr.Code)
		})
	})

	t.Run("HandleDeleteCronJob", func(t *testing.T) {
		t.Run("Success", func(t *testing.T) {
			scheme := runtime.NewScheme()
			_ = v1alpha2.AddToScheme(scheme)

			cj := &v1alpha2.AgentCronJob{
				ObjectMeta: metav1.ObjectMeta{Name: "delete-cj", Namespace: "default"},
				Spec:       v1alpha2.AgentCronJobSpec{Schedule: "0 * * * *", Prompt: "check", AgentRef: "agent"},
			}

			builder := fake.NewClientBuilder().WithScheme(scheme).WithObjects(cj)
			handler := newCronJobHandler(builder)
			rr := newMockErrorResponseWriter()

			req := httptest.NewRequest("DELETE", "/api/cronjobs/default/delete-cj", nil)
			req = setUser(req, "test-user")

			router := mux.NewRouter()
			router.HandleFunc("/api/cronjobs/{namespace}/{name}", func(w http.ResponseWriter, r *http.Request) {
				handler.HandleDeleteCronJob(rr, r)
			}).Methods("DELETE")
			router.ServeHTTP(rr, req)

			require.Equal(t, http.StatusOK, rr.Code)

			// Verify it's actually deleted
			kubeClient := fake.NewClientBuilder().WithScheme(scheme).Build()
			deleted := &v1alpha2.AgentCronJob{}
			err := kubeClient.Get(context.Background(), types.NamespacedName{Name: "delete-cj", Namespace: "default"}, deleted)
			require.Error(t, err)
		})

		t.Run("NotFound", func(t *testing.T) {
			scheme := runtime.NewScheme()
			_ = v1alpha2.AddToScheme(scheme)

			builder := fake.NewClientBuilder().WithScheme(scheme)
			handler := newCronJobHandler(builder)
			rr := newMockErrorResponseWriter()

			req := httptest.NewRequest("DELETE", "/api/cronjobs/default/nonexistent", nil)
			req = setUser(req, "test-user")

			router := mux.NewRouter()
			router.HandleFunc("/api/cronjobs/{namespace}/{name}", func(w http.ResponseWriter, r *http.Request) {
				handler.HandleDeleteCronJob(rr, r)
			}).Methods("DELETE")
			router.ServeHTTP(rr, req)

			require.Equal(t, http.StatusNotFound, rr.Code)
			require.NotNil(t, rr.errorReceived)
		})

		t.Run("MissingPathParams", func(t *testing.T) {
			scheme := runtime.NewScheme()
			_ = v1alpha2.AddToScheme(scheme)

			builder := fake.NewClientBuilder().WithScheme(scheme)
			handler := newCronJobHandler(builder)
			rr := newMockErrorResponseWriter()

			req := httptest.NewRequest("DELETE", "/api/cronjobs/", nil)
			req = setUser(req, "test-user")
			handler.HandleDeleteCronJob(rr, req)

			require.Equal(t, http.StatusBadRequest, rr.Code)
		})
	})
}
