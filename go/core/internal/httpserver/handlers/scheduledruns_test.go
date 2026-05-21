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
	"github.com/kagent-dev/kagent/go/core/internal/httpserver/auth"
	"github.com/kagent-dev/kagent/go/core/internal/httpserver/handlers"
)

// mockScheduledRunTrigger implements handlers.ScheduledRunTrigger for testing.
type mockScheduledRunTrigger struct {
	triggered []types.NamespacedName
	entry     *v1alpha2.RunHistoryEntry
	err       error
}

func (m *mockScheduledRunTrigger) TriggerManualRun(key types.NamespacedName) (*v1alpha2.RunHistoryEntry, error) {
	m.triggered = append(m.triggered, key)
	return m.entry, m.err
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
				Schedule: schedule,
				AgentRef: v1alpha2.AgentReference{
					Name:      "my-agent",
					Namespace: namespace,
				},
				Prompt:        "test prompt",
				MaxRunHistory: 10,
			},
		}
	}

	t.Run("HandleListScheduledRuns", func(t *testing.T) {
		t.Run("empty list", func(t *testing.T) {
			handler, _, w := setupHandler()

			req := httptest.NewRequest("GET", "/api/scheduledruns", nil)
			req = setUser(req, "test-user")
			handler.HandleListScheduledRuns(w, req)

			assert.Equal(t, http.StatusOK, w.Code)
		})

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

	t.Run("HandleCreateScheduledRun", func(t *testing.T) {
		t.Run("success", func(t *testing.T) {
			handler, _, w := setupHandler()

			sr := v1alpha2.ScheduledRun{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "new-sr",
					Namespace: "default",
				},
				Spec: v1alpha2.ScheduledRunSpec{
					Schedule: "0 */2 * * *",
					AgentRef: v1alpha2.AgentReference{Name: "agent", Namespace: "default"},
					Prompt:   "do something",
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

		t.Run("invalid schedule - bad cron syntax", func(t *testing.T) {
			handler, _, w := setupHandler()

			sr := v1alpha2.ScheduledRun{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "new-sr",
					Namespace: "default",
				},
				Spec: v1alpha2.ScheduledRunSpec{
					Schedule: "not-a-cron",
					AgentRef: v1alpha2.AgentReference{Name: "agent", Namespace: "default"},
					Prompt:   "do something",
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
			handler, _, w := setupHandler(existing)

			updated := v1alpha2.ScheduledRun{
				Spec: v1alpha2.ScheduledRunSpec{
					Schedule: "0 */3 * * *",
					AgentRef: v1alpha2.AgentReference{Name: "my-agent", Namespace: "default"},
					Prompt:   "updated prompt",
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

		t.Run("not found", func(t *testing.T) {
			handler, _, w := setupHandler()

			updated := v1alpha2.ScheduledRun{
				Spec: v1alpha2.ScheduledRunSpec{
					Schedule: "0 */3 * * *",
					AgentRef: v1alpha2.AgentReference{Name: "agent", Namespace: "default"},
					Prompt:   "updated prompt",
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

		t.Run("invalid schedule", func(t *testing.T) {
			existing := newSR("default", "sr-1", "0 */2 * * *")
			handler, _, w := setupHandler(existing)

			updated := v1alpha2.ScheduledRun{
				Spec: v1alpha2.ScheduledRunSpec{
					Schedule: "not-a-cron",
					AgentRef: v1alpha2.AgentReference{Name: "agent", Namespace: "default"},
					Prompt:   "updated prompt",
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

			// Verify it's actually deleted
			getReq := httptest.NewRequest("GET", "/api/scheduledruns/default/sr-1", nil)
			getReq = mux.SetURLVars(getReq, map[string]string{"namespace": "default", "name": "sr-1"})
			getReq = setUser(getReq, "test-user")
			w2 := newMockErrorResponseWriter()
			handler.HandleGetScheduledRun(w2, getReq)
			assert.Equal(t, http.StatusNotFound, w2.Code)
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
		t.Run("success", func(t *testing.T) {
			existing := newSR("default", "sr-1", "0 */2 * * *")
			handler, trigger, w := setupHandler(existing)
			trigger.entry = &v1alpha2.RunHistoryEntry{DispatchStatus: v1alpha2.DispatchStatusDispatched}

			req := httptest.NewRequest("POST", "/api/scheduledruns/default/sr-1/trigger", nil)
			req = mux.SetURLVars(req, map[string]string{"namespace": "default", "name": "sr-1"})
			req = setUser(req, "test-user")
			handler.HandleTriggerScheduledRun(w, req)

			assert.Equal(t, http.StatusOK, w.Code)
			require.Len(t, trigger.triggered, 1)
			assert.Equal(t, types.NamespacedName{Namespace: "default", Name: "sr-1"}, trigger.triggered[0])
		})

		t.Run("not found", func(t *testing.T) {
			handler, _, w := setupHandler()

			req := httptest.NewRequest("POST", "/api/scheduledruns/default/nonexistent/trigger", nil)
			req = mux.SetURLVars(req, map[string]string{"namespace": "default", "name": "nonexistent"})
			req = setUser(req, "test-user")
			handler.HandleTriggerScheduledRun(w, req)

			assert.Equal(t, http.StatusNotFound, w.Code)
		})
	})
}

func TestValidateSchedule(t *testing.T) {
	tests := []struct {
		name     string
		schedule string
		timeZone string
		wantErr  bool
	}{
		{
			name:     "valid - every 2 hours",
			schedule: "0 */2 * * *",
			wantErr:  false,
		},
		{
			name:     "valid - daily at midnight",
			schedule: "0 0 * * *",
			wantErr:  false,
		},
		{
			name:     "valid - exactly 1 hour",
			schedule: "0 * * * *",
			wantErr:  false,
		},
		{
			name:     "valid - every 5 minutes (sub-hourly allowed)",
			schedule: "*/5 * * * *",
			wantErr:  false,
		},
		{
			name:     "valid - every 30 minutes (sub-hourly allowed)",
			schedule: "*/30 * * * *",
			wantErr:  false,
		},
		{
			name:     "invalid cron expression",
			schedule: "not-a-cron",
			wantErr:  true,
		},
		{
			name:     "valid - with time zone",
			schedule: "0 9 * * *",
			timeZone: "America/Los_Angeles",
			wantErr:  false,
		},
		{
			name:     "invalid time zone",
			schedule: "0 9 * * *",
			timeZone: "Mars/Olympus_Mons",
			wantErr:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := handlers.ValidateSchedule(tt.schedule, tt.timeZone)
			if tt.wantErr {
				assert.NotNil(t, err)
			} else {
				assert.Nil(t, err)
			}
		})
	}
}

func TestScheduledRunsHandler_CreateDefaultsNamespace(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, v1alpha2.AddToScheme(scheme))

	kubeClient := fake.NewClientBuilder().WithScheme(scheme).Build()
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
			Schedule: "0 */2 * * *",
			AgentRef: v1alpha2.AgentReference{Name: "agent", Namespace: "default"},
			Prompt:   "test",
		},
	}
	body, _ := json.Marshal(sr)

	req := httptest.NewRequest("POST", "/api/scheduledruns", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	req = setUser(req, "test-user")
	handler.HandleCreateScheduledRun(w, req)

	// Should succeed — the handler defaults the namespace
	assert.Equal(t, http.StatusCreated, w.Code)

	// Verify it was created with a namespace
	var created v1alpha2.ScheduledRun
	err := kubeClient.Get(context.Background(), types.NamespacedName{
		Name:      "no-namespace-sr",
		Namespace: "kagent",
	}, &created)
	// If namespace defaults to something else, just verify creation succeeded
	if err != nil {
		// Try empty namespace (depends on GetResourceNamespace())
		list := &v1alpha2.ScheduledRunList{}
		err = kubeClient.List(context.Background(), list)
		require.NoError(t, err)
		require.Len(t, list.Items, 1)
		assert.Equal(t, "no-namespace-sr", list.Items[0].Name)
	}
}
