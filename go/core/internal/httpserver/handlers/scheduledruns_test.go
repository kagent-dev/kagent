package handlers_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gorilla/mux"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/validation/field"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	"github.com/kagent-dev/kagent/go/api/database"
	"github.com/kagent-dev/kagent/go/api/v1alpha2"
	"github.com/kagent-dev/kagent/go/core/internal/httpserver/auth"
	"github.com/kagent-dev/kagent/go/core/internal/httpserver/handlers"
	"github.com/kagent-dev/kagent/go/core/internal/scheduledrun"
	"github.com/kagent-dev/kagent/go/core/internal/utils"
)

// mockScheduledRunTrigger implements handlers.ScheduledRunTrigger for testing.
type mockScheduledRunTrigger struct {
	triggered []types.NamespacedName
	execution *v1alpha2.ScheduledRunExecution
	err       error
}

type mockScheduledRunExecutionDatabase struct {
	database.Client
	executions      []database.ScheduledRunExecutionRecord
	namespace       string
	name            string
	scheduledRunUID string
	queryOptions    database.ScheduledRunExecutionQueryOptions
}

func (m *mockScheduledRunExecutionDatabase) ListScheduledRunExecutions(_ context.Context, namespace, name, scheduledRunUID string, options database.ScheduledRunExecutionQueryOptions) ([]database.ScheduledRunExecutionRecord, error) {
	m.namespace = namespace
	m.name = name
	m.scheduledRunUID = scheduledRunUID
	m.queryOptions = options
	return m.executions, nil
}

func (m *mockScheduledRunTrigger) TriggerManualExecution(_ context.Context, key types.NamespacedName) (*v1alpha2.ScheduledRunExecution, error) {
	m.triggered = append(m.triggered, key)
	return m.execution, m.err
}

func scheduledRunTargetRef(kind, name string) corev1.TypedLocalObjectReference {
	if kind == "" {
		kind = scheduledrun.TargetKindAgent
	}
	apiGroup := scheduledrun.TargetAPIGroup
	return corev1.TypedLocalObjectReference{APIGroup: &apiGroup, Kind: kind, Name: name}
}

func invalidScheduledRunError(name string) error {
	return apierrors.NewInvalid(
		schema.GroupKind{Group: "kagent.dev", Kind: "ScheduledRun"},
		name,
		field.ErrorList{
			field.Invalid(field.NewPath("spec", "recentExecutionsLimit"), 101, "must be between 1 and 100"),
		},
	)
}

func TestScheduledRunsHandler(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, v1alpha2.AddToScheme(scheme))

	setupHandler := func(objects ...runtime.Object) (*handlers.ScheduledRunsHandler, *mockScheduledRunTrigger, *mockErrorResponseWriter) {
		clientBuilder := fake.NewClientBuilder().
			WithScheme(scheme).
			WithStatusSubresource(&v1alpha2.ScheduledRun{})
		for _, obj := range objects {
			clientBuilder = clientBuilder.WithRuntimeObjects(obj)
		}
		kubeClient := clientBuilder.Build()

		trigger := &mockScheduledRunTrigger{}
		base := &handlers.Base{
			KubeClient: kubeClient,
			Authorizer: &auth.NoopAuthorizer{},
		}
		handler := handlers.NewScheduledRunsHandler(base, trigger)
		responseRecorder := newMockErrorResponseWriter()
		return handler, trigger, responseRecorder
	}

	newSR := func(namespace, name, schedule string) *v1alpha2.ScheduledRun {
		return &v1alpha2.ScheduledRun{
			ObjectMeta: metav1.ObjectMeta{
				Name:      name,
				Namespace: namespace,
			},
			Spec: v1alpha2.ScheduledRunSpec{
				Schedule:              schedule,
				TargetRef:             scheduledRunTargetRef("", "my-agent"),
				Prompt:                "test prompt",
				RecentExecutionsLimit: new(int32(10)),
			},
		}
	}
	newAgent := func(namespace, name string) *v1alpha2.Agent {
		return &v1alpha2.Agent{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace}}
	}
	newSandboxAgent := func(namespace, name string) *v1alpha2.SandboxAgent {
		return &v1alpha2.SandboxAgent{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace}}
	}

	t.Run("HandleListScheduledRuns", func(t *testing.T) {
		t.Run("list with items", func(t *testing.T) {
			sr := newSR("default", "sr-1", "0 */2 * * *")
			handler, _, w := setupHandler(sr)

			req := httptest.NewRequest("GET", "/api/scheduledruns", nil)
			req = setUser(req, "test-user")
			handler.HandleListScheduledRuns(w, req)

			assert.Equal(t, http.StatusOK, w.Code)
			assert.Contains(t, w.Body.String(), "sr-1")
		})
	})

	t.Run("HandleGetScheduledRun", func(t *testing.T) {
		t.Run("success", func(t *testing.T) {
			sr := newSR("default", "sr-1", "0 */2 * * *")
			handler, _, w := setupHandler(sr)

			req := httptest.NewRequest("GET", "/api/scheduledruns/default/sr-1", nil)
			req = mux.SetURLVars(req, map[string]string{"namespace": "default", "name": "sr-1"})
			req = setUser(req, "test-user")
			handler.HandleGetScheduledRun(w, req)

			assert.Equal(t, http.StatusOK, w.Code)
			assert.Contains(t, w.Body.String(), "sr-1")
		})

		t.Run("not found", func(t *testing.T) {
			handler, _, w := setupHandler()

			req := httptest.NewRequest("GET", "/api/scheduledruns/default/nonexistent", nil)
			req = mux.SetURLVars(req, map[string]string{"namespace": "default", "name": "nonexistent"})
			req = setUser(req, "test-user")
			handler.HandleGetScheduledRun(w, req)

			assert.Equal(t, http.StatusNotFound, w.Code)
		})
	})

	t.Run("HandleListScheduledRunExecutions", func(t *testing.T) {
		before := time.Date(2026, 7, 22, 9, 30, 0, 123, time.UTC)
		sr := newSR("default", "sr-1", "0 */2 * * *")
		sr.UID = types.UID("sr-1-uid")
		db := &mockScheduledRunExecutionDatabase{executions: []database.ScheduledRunExecutionRecord{{
			ID:                    "execution-1",
			ScheduledRunNamespace: "default",
			ScheduledRunName:      "sr-1",
			Trigger:               v1alpha2.ScheduledRunExecutionTrigger_Scheduled,
			Status:                v1alpha2.ScheduledRunExecutionStatus_Succeeded,
		}}}
		base := &handlers.Base{
			KubeClient:      fake.NewClientBuilder().WithScheme(scheme).WithRuntimeObjects(sr).Build(),
			DatabaseService: db,
			Authorizer:      &auth.NoopAuthorizer{},
		}
		handler := handlers.NewScheduledRunsHandler(base, &mockScheduledRunTrigger{})
		w := newMockErrorResponseWriter()
		req := httptest.NewRequest("GET", "/api/scheduledruns/default/sr-1/executions?limit=25&before="+before.Format(time.RFC3339Nano)+"&beforeID=execution-cursor", nil)
		req = mux.SetURLVars(req, map[string]string{"namespace": "default", "name": "sr-1"})
		req = setUser(req, "test-user")

		handler.HandleListScheduledRunExecutions(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		assert.Contains(t, w.Body.String(), "execution-1")
		assert.Equal(t, "default", db.namespace)
		assert.Equal(t, "sr-1", db.name)
		assert.Equal(t, "sr-1-uid", db.scheduledRunUID)
		assert.Equal(t, 25, db.queryOptions.Limit)
		assert.Equal(t, before, db.queryOptions.Before)
		assert.Equal(t, "execution-cursor", db.queryOptions.BeforeID)
	})

	t.Run("HandleCreateScheduledRun", func(t *testing.T) {
		t.Run("success", func(t *testing.T) {
			handler, _, w := setupHandler(newAgent("default", "agent"))

			sr := v1alpha2.ScheduledRun{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "new-sr",
					Namespace: "default",
				},
				Spec: v1alpha2.ScheduledRunSpec{
					Schedule:  "0 */2 * * *",
					TargetRef: scheduledRunTargetRef("", "agent"),
					Prompt:    "do something",
				},
			}
			body, _ := json.Marshal(sr)

			req := httptest.NewRequest("POST", "/api/scheduledruns", bytes.NewBuffer(body))
			req.Header.Set("Content-Type", "application/json")
			req = setUser(req, "test-user")
			handler.HandleCreateScheduledRun(w, req)

			assert.Equal(t, http.StatusCreated, w.Code)
			assert.Contains(t, w.Body.String(), "new-sr")
		})

		t.Run("success with sandbox agent target", func(t *testing.T) {
			handler, _, w := setupHandler(newSandboxAgent("default", "sandbox-agent"))

			sr := v1alpha2.ScheduledRun{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "new-sandbox-sr",
					Namespace: "default",
				},
				Spec: v1alpha2.ScheduledRunSpec{
					Schedule:  "0 */2 * * *",
					TargetRef: scheduledRunTargetRef(scheduledrun.TargetKindSandboxAgent, "sandbox-agent"),
					Prompt:    "do something",
				},
			}
			body, _ := json.Marshal(sr)

			req := httptest.NewRequest("POST", "/api/scheduledruns", bytes.NewBuffer(body))
			req.Header.Set("Content-Type", "application/json")
			req = setUser(req, "test-user")
			handler.HandleCreateScheduledRun(w, req)

			assert.Equal(t, http.StatusCreated, w.Code)
			assert.Contains(t, w.Body.String(), "new-sandbox-sr")
		})

		t.Run("preserves explicit allowSessionInteraction true", func(t *testing.T) {
			handler, _, w := setupHandler(newAgent("default", "agent"))

			sr := v1alpha2.ScheduledRun{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "interactive-sr",
					Namespace: "default",
				},
				Spec: v1alpha2.ScheduledRunSpec{
					Schedule:                "0 */2 * * *",
					TargetRef:               scheduledRunTargetRef("", "agent"),
					Prompt:                  "do something",
					AllowSessionInteraction: new(true),
				},
			}
			body, err := json.Marshal(sr)
			require.NoError(t, err)
			assert.Contains(t, string(body), `"allowSessionInteraction":true`)

			req := httptest.NewRequest("POST", "/api/scheduledruns", bytes.NewBuffer(body))
			req.Header.Set("Content-Type", "application/json")
			req = setUser(req, "test-user")
			handler.HandleCreateScheduledRun(w, req)

			require.Equal(t, http.StatusCreated, w.Code)
			got := &v1alpha2.ScheduledRun{}
			require.NoError(t, handler.KubeClient.Get(context.Background(), types.NamespacedName{Namespace: "default", Name: "interactive-sr"}, got))
			require.NotNil(t, got.Spec.AllowSessionInteraction)
			assert.True(t, *got.Spec.AllowSessionInteraction)
		})

		t.Run("defers missing target validation to the controller", func(t *testing.T) {
			handler, _, w := setupHandler()

			sr := v1alpha2.ScheduledRun{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "new-sr",
					Namespace: "default",
				},
				Spec: v1alpha2.ScheduledRunSpec{
					Schedule:  "0 */2 * * *",
					TargetRef: scheduledRunTargetRef("", "missing"),
					Prompt:    "do something",
				},
			}
			body, _ := json.Marshal(sr)

			req := httptest.NewRequest("POST", "/api/scheduledruns", bytes.NewBuffer(body))
			req.Header.Set("Content-Type", "application/json")
			req = setUser(req, "test-user")
			handler.HandleCreateScheduledRun(w, req)

			assert.Equal(t, http.StatusCreated, w.Code)
		})

		t.Run("defers invalid schedule validation to the controller", func(t *testing.T) {
			handler, _, w := setupHandler()

			sr := v1alpha2.ScheduledRun{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "new-sr",
					Namespace: "default",
				},
				Spec: v1alpha2.ScheduledRunSpec{
					Schedule:  "not-a-cron",
					TargetRef: scheduledRunTargetRef("", "agent"),
					Prompt:    "do something",
				},
			}
			body, _ := json.Marshal(sr)

			req := httptest.NewRequest("POST", "/api/scheduledruns", bytes.NewBuffer(body))
			req.Header.Set("Content-Type", "application/json")
			req = setUser(req, "test-user")
			handler.HandleCreateScheduledRun(w, req)

			assert.Equal(t, http.StatusCreated, w.Code)
		})

		t.Run("maps apiserver invalid to bad request", func(t *testing.T) {
			kubeClient := fake.NewClientBuilder().
				WithScheme(scheme).
				WithRuntimeObjects(newAgent("default", "agent")).
				WithInterceptorFuncs(interceptor.Funcs{
					Create: func(ctx context.Context, c ctrlclient.WithWatch, obj ctrlclient.Object, opts ...ctrlclient.CreateOption) error {
						if _, ok := obj.(*v1alpha2.ScheduledRun); ok {
							return invalidScheduledRunError(obj.GetName())
						}
						return c.Create(ctx, obj, opts...)
					},
				}).
				Build()
			handler := handlers.NewScheduledRunsHandler(&handlers.Base{
				KubeClient: kubeClient,
				Authorizer: &auth.NoopAuthorizer{},
			}, &mockScheduledRunTrigger{})
			w := newMockErrorResponseWriter()

			sr := v1alpha2.ScheduledRun{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "new-sr",
					Namespace: "default",
				},
				Spec: v1alpha2.ScheduledRunSpec{
					Schedule:              "0 */2 * * *",
					TargetRef:             scheduledRunTargetRef("", "agent"),
					Prompt:                "do something",
					RecentExecutionsLimit: new(int32(20)),
				},
			}
			body, _ := json.Marshal(sr)

			req := httptest.NewRequest("POST", "/api/scheduledruns", bytes.NewBuffer(body))
			req.Header.Set("Content-Type", "application/json")
			req = setUser(req, "test-user")
			handler.HandleCreateScheduledRun(w, req)

			assert.Equal(t, http.StatusBadRequest, w.Code)
		})

		t.Run("invalid body", func(t *testing.T) {
			handler, _, w := setupHandler()

			req := httptest.NewRequest("POST", "/api/scheduledruns", bytes.NewBufferString("{invalid"))
			req.Header.Set("Content-Type", "application/json")
			req = setUser(req, "test-user")
			handler.HandleCreateScheduledRun(w, req)

			assert.Equal(t, http.StatusBadRequest, w.Code)
		})
	})

	t.Run("HandleUpdateScheduledRun", func(t *testing.T) {
		t.Run("success", func(t *testing.T) {
			existing := newSR("default", "sr-1", "0 */2 * * *")
			existing.Spec.RecentExecutionsLimit = new(int32(42))
			handler, _, w := setupHandler(existing, newAgent("default", "my-agent"))

			updated := v1alpha2.ScheduledRun{
				Spec: v1alpha2.ScheduledRunSpec{
					Schedule:  "0 */3 * * *",
					TargetRef: scheduledRunTargetRef("", "my-agent"),
					Prompt:    "updated prompt",
				},
			}
			body, _ := json.Marshal(updated)

			req := httptest.NewRequest("PUT", "/api/scheduledruns/default/sr-1", bytes.NewBuffer(body))
			req = mux.SetURLVars(req, map[string]string{"namespace": "default", "name": "sr-1"})
			req.Header.Set("Content-Type", "application/json")
			req = setUser(req, "test-user")
			handler.HandleUpdateScheduledRun(w, req)

			assert.Equal(t, http.StatusOK, w.Code)
			got := &v1alpha2.ScheduledRun{}
			require.NoError(t, handler.KubeClient.Get(context.Background(), types.NamespacedName{Namespace: "default", Name: "sr-1"}, got))
			require.NotNil(t, got.Spec.RecentExecutionsLimit)
			assert.Equal(t, int32(42), *got.Spec.RecentExecutionsLimit)
		})

		t.Run("updates explicit status history limit", func(t *testing.T) {
			existing := newSR("default", "sr-1", "0 */2 * * *")
			existing.Spec.RecentExecutionsLimit = new(int32(42))
			existing.Labels = map[string]string{"existing": "true"}
			handler, _, w := setupHandler(existing, newAgent("default", "my-agent"))

			updated := v1alpha2.ScheduledRun{
				ObjectMeta: metav1.ObjectMeta{
					Labels:      map[string]string{"updated": "true"},
					Annotations: map[string]string{"example.com/source": "api"},
				},
				Spec: v1alpha2.ScheduledRunSpec{
					Schedule:              "0 */3 * * *",
					TargetRef:             scheduledRunTargetRef("", "my-agent"),
					Prompt:                "updated prompt",
					RecentExecutionsLimit: new(int32(20)),
				},
			}
			body, _ := json.Marshal(updated)

			req := httptest.NewRequest("PUT", "/api/scheduledruns/default/sr-1", bytes.NewBuffer(body))
			req = mux.SetURLVars(req, map[string]string{"namespace": "default", "name": "sr-1"})
			req.Header.Set("Content-Type", "application/json")
			req = setUser(req, "test-user")
			handler.HandleUpdateScheduledRun(w, req)

			assert.Equal(t, http.StatusOK, w.Code)
			got := &v1alpha2.ScheduledRun{}
			require.NoError(t, handler.KubeClient.Get(context.Background(), types.NamespacedName{Namespace: "default", Name: "sr-1"}, got))
			require.NotNil(t, got.Spec.RecentExecutionsLimit)
			assert.Equal(t, int32(20), *got.Spec.RecentExecutionsLimit)
			assert.Equal(t, "true", got.Labels["updated"])
			assert.Equal(t, "api", got.Annotations["example.com/source"])
		})

		t.Run("enables session interaction", func(t *testing.T) {
			existing := newSR("default", "sr-1", "0 */2 * * *")
			existing.Spec.AllowSessionInteraction = new(false)
			handler, _, w := setupHandler(existing, newAgent("default", "my-agent"))

			updated := v1alpha2.ScheduledRun{
				Spec: v1alpha2.ScheduledRunSpec{
					Schedule:                "0 */3 * * *",
					TargetRef:               scheduledRunTargetRef("", "my-agent"),
					Prompt:                  "updated prompt",
					AllowSessionInteraction: new(true),
				},
			}
			body, err := json.Marshal(updated)
			require.NoError(t, err)
			assert.Contains(t, string(body), `"allowSessionInteraction":true`)

			req := httptest.NewRequest("PUT", "/api/scheduledruns/default/sr-1", bytes.NewBuffer(body))
			req = mux.SetURLVars(req, map[string]string{"namespace": "default", "name": "sr-1"})
			req.Header.Set("Content-Type", "application/json")
			req = setUser(req, "test-user")
			handler.HandleUpdateScheduledRun(w, req)

			require.Equal(t, http.StatusOK, w.Code)
			got := &v1alpha2.ScheduledRun{}
			require.NoError(t, handler.KubeClient.Get(context.Background(), types.NamespacedName{Namespace: "default", Name: "sr-1"}, got))
			require.NotNil(t, got.Spec.AllowSessionInteraction)
			assert.True(t, *got.Spec.AllowSessionInteraction)
		})

		t.Run("not found", func(t *testing.T) {
			handler, _, w := setupHandler()

			updated := v1alpha2.ScheduledRun{
				Spec: v1alpha2.ScheduledRunSpec{
					Schedule:  "0 */3 * * *",
					TargetRef: scheduledRunTargetRef("", "agent"),
					Prompt:    "updated prompt",
				},
			}
			body, _ := json.Marshal(updated)

			req := httptest.NewRequest("PUT", "/api/scheduledruns/default/nonexistent", bytes.NewBuffer(body))
			req = mux.SetURLVars(req, map[string]string{"namespace": "default", "name": "nonexistent"})
			req.Header.Set("Content-Type", "application/json")
			req = setUser(req, "test-user")
			handler.HandleUpdateScheduledRun(w, req)

			assert.Equal(t, http.StatusNotFound, w.Code)
		})

		t.Run("defers invalid schedule validation to the controller", func(t *testing.T) {
			existing := newSR("default", "sr-1", "0 */2 * * *")
			handler, _, w := setupHandler(existing)

			updated := v1alpha2.ScheduledRun{
				Spec: v1alpha2.ScheduledRunSpec{
					Schedule:  "not-a-cron",
					TargetRef: scheduledRunTargetRef("", "agent"),
					Prompt:    "updated prompt",
				},
			}
			body, _ := json.Marshal(updated)

			req := httptest.NewRequest("PUT", "/api/scheduledruns/default/sr-1", bytes.NewBuffer(body))
			req = mux.SetURLVars(req, map[string]string{"namespace": "default", "name": "sr-1"})
			req.Header.Set("Content-Type", "application/json")
			req = setUser(req, "test-user")
			handler.HandleUpdateScheduledRun(w, req)

			assert.Equal(t, http.StatusOK, w.Code)
		})

		t.Run("maps apiserver invalid to bad request", func(t *testing.T) {
			existing := newSR("default", "sr-1", "0 */2 * * *")
			agent := newAgent("default", "my-agent")
			kubeClient := fake.NewClientBuilder().
				WithScheme(scheme).
				WithStatusSubresource(&v1alpha2.ScheduledRun{}).
				WithRuntimeObjects(existing, agent).
				WithInterceptorFuncs(interceptor.Funcs{
					Patch: func(ctx context.Context, c ctrlclient.WithWatch, obj ctrlclient.Object, patch ctrlclient.Patch, opts ...ctrlclient.PatchOption) error {
						if _, ok := obj.(*v1alpha2.ScheduledRun); ok {
							return invalidScheduledRunError(obj.GetName())
						}
						return c.Patch(ctx, obj, patch, opts...)
					},
				}).
				Build()
			handler := handlers.NewScheduledRunsHandler(&handlers.Base{
				KubeClient: kubeClient,
				Authorizer: &auth.NoopAuthorizer{},
			}, &mockScheduledRunTrigger{})
			w := newMockErrorResponseWriter()

			updated := v1alpha2.ScheduledRun{
				Spec: v1alpha2.ScheduledRunSpec{
					Schedule:              "0 */3 * * *",
					TargetRef:             scheduledRunTargetRef("", "my-agent"),
					Prompt:                "updated prompt",
					RecentExecutionsLimit: new(int32(20)),
				},
			}
			body, _ := json.Marshal(updated)

			req := httptest.NewRequest("PUT", "/api/scheduledruns/default/sr-1", bytes.NewBuffer(body))
			req = mux.SetURLVars(req, map[string]string{"namespace": "default", "name": "sr-1"})
			req.Header.Set("Content-Type", "application/json")
			req = setUser(req, "test-user")
			handler.HandleUpdateScheduledRun(w, req)

			assert.Equal(t, http.StatusBadRequest, w.Code)
		})
	})

	t.Run("HandleDeleteScheduledRun", func(t *testing.T) {
		t.Run("success", func(t *testing.T) {
			existing := newSR("default", "sr-1", "0 */2 * * *")
			handler, _, w := setupHandler(existing)

			req := httptest.NewRequest("DELETE", "/api/scheduledruns/default/sr-1", nil)
			req = mux.SetURLVars(req, map[string]string{"namespace": "default", "name": "sr-1"})
			req = setUser(req, "test-user")
			handler.HandleDeleteScheduledRun(w, req)

			assert.Equal(t, http.StatusOK, w.Code)

			err := handler.KubeClient.Get(context.Background(), types.NamespacedName{Namespace: "default", Name: "sr-1"}, &v1alpha2.ScheduledRun{})
			require.Error(t, err)
			assert.True(t, apierrors.IsNotFound(err))
		})

		t.Run("not found", func(t *testing.T) {
			handler, _, w := setupHandler()

			req := httptest.NewRequest("DELETE", "/api/scheduledruns/default/nonexistent", nil)
			req = mux.SetURLVars(req, map[string]string{"namespace": "default", "name": "nonexistent"})
			req = setUser(req, "test-user")
			handler.HandleDeleteScheduledRun(w, req)

			assert.Equal(t, http.StatusNotFound, w.Code)
		})
	})

	t.Run("HandleTriggerScheduledRun", func(t *testing.T) {
		t.Run("suspended", func(t *testing.T) {
			existing := newSR("default", "sr-1", "0 */2 * * *")
			existing.Spec.Suspended = new(true)
			handler, trigger, w := setupHandler(existing, newAgent("default", "my-agent"))
			trigger.execution = &v1alpha2.ScheduledRunExecution{ID: "execution-id", Status: v1alpha2.ScheduledRunExecutionStatus_InProgress}

			req := httptest.NewRequest("POST", "/api/scheduledruns/default/sr-1/trigger", nil)
			req = mux.SetURLVars(req, map[string]string{"namespace": "default", "name": "sr-1"})
			req = setUser(req, "test-user")
			handler.HandleTriggerScheduledRun(w, req)

			assert.Equal(t, http.StatusOK, w.Code)
			require.Len(t, trigger.triggered, 1)
			assert.Equal(t, types.NamespacedName{Namespace: "default", Name: "sr-1"}, trigger.triggered[0])
		})

		t.Run("not found", func(t *testing.T) {
			handler, trigger, w := setupHandler()
			trigger.err = apierrors.NewNotFound(
				schema.GroupResource{Group: "kagent.dev", Resource: "scheduledruns"},
				"nonexistent",
			)

			req := httptest.NewRequest("POST", "/api/scheduledruns/default/nonexistent/trigger", nil)
			req = mux.SetURLVars(req, map[string]string{"namespace": "default", "name": "nonexistent"})
			req = setUser(req, "test-user")
			handler.HandleTriggerScheduledRun(w, req)

			assert.Equal(t, http.StatusNotFound, w.Code)
		})

		t.Run("scheduler not active", func(t *testing.T) {
			existing := newSR("default", "sr-1", "0 */2 * * *")
			handler, trigger, w := setupHandler(existing)
			trigger.err = scheduledrun.ErrSchedulerNotActive

			req := httptest.NewRequest("POST", "/api/scheduledruns/default/sr-1/trigger", nil)
			req = mux.SetURLVars(req, map[string]string{"namespace": "default", "name": "sr-1"})
			req = setUser(req, "test-user")
			handler.HandleTriggerScheduledRun(w, req)

			assert.Equal(t, http.StatusServiceUnavailable, w.Code)
		})
	})
}

func TestScheduledRunsHandler_DefaultsNamespace(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, v1alpha2.AddToScheme(scheme))
	namespace := utils.GetResourceNamespace()

	kubeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithRuntimeObjects(&v1alpha2.Agent{ObjectMeta: metav1.ObjectMeta{Name: "agent", Namespace: namespace}}).
		Build()
	trigger := &mockScheduledRunTrigger{}
	base := &handlers.Base{
		KubeClient: kubeClient,
		Authorizer: &auth.NoopAuthorizer{},
	}
	handler := handlers.NewScheduledRunsHandler(base, trigger)
	w := newMockErrorResponseWriter()

	sr := v1alpha2.ScheduledRun{
		ObjectMeta: metav1.ObjectMeta{
			Name: "no-namespace-sr",
		},
		Spec: v1alpha2.ScheduledRunSpec{
			Schedule:  "0 */2 * * *",
			TargetRef: scheduledRunTargetRef("", "agent"),
			Prompt:    "test",
		},
	}
	body, _ := json.Marshal(sr)

	req := httptest.NewRequest("POST", "/api/scheduledruns", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	req = setUser(req, "test-user")
	handler.HandleCreateScheduledRun(w, req)

	assert.Equal(t, http.StatusCreated, w.Code)

	var created v1alpha2.ScheduledRun
	require.NoError(t, kubeClient.Get(context.Background(), types.NamespacedName{
		Name:      "no-namespace-sr",
		Namespace: namespace,
	}, &created))
}
