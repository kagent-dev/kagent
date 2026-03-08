package handlers_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/types"

	database_fake "github.com/kagent-dev/kagent/go/core/internal/database/fake"
	"github.com/kagent-dev/kagent/go/core/internal/httpserver/auth"
	"github.com/kagent-dev/kagent/go/core/internal/httpserver/handlers"
)

// makeVector returns a float32 slice of length n filled with the given value.
// Used to produce valid 768-dimensional test vectors.
func makeVector(n int, val float32) []float32 {
	v := make([]float32, n)
	for i := range v {
		v[i] = val
	}
	return v
}

func TestMemoryHandler(t *testing.T) {
	setupHandler := func() (*handlers.MemoryHandler, *mockErrorResponseWriter) {
		base := &handlers.Base{
			DefaultModelConfig: types.NamespacedName{Namespace: "default", Name: "default"},
			DatabaseService:    database_fake.NewClient(),
			Authorizer:         &auth.NoopAuthorizer{},
		}
		handler := handlers.NewMemoryHandler(base)
		responseRecorder := newMockErrorResponseWriter()
		return handler, responseRecorder
	}

	t.Run("AddSession", func(t *testing.T) {
		t.Run("Success", func(t *testing.T) {
			handler, responseRecorder := setupHandler()

			reqBody := handlers.AddSessionMemoryRequest{
				AgentName: "test-agent",
				UserID:    "user123",
				Content:   "This is a test conversation",
				Vector:    makeVector(768, 0.1),
				Metadata:  json.RawMessage(`{"session_id": "session-abc"}`),
			}

			jsonBody, _ := json.Marshal(reqBody)
			req := httptest.NewRequest("POST", "/api/memories/sessions", bytes.NewBuffer(jsonBody))
			req = setUser(req, "test-user")
			req.Header.Set("Content-Type", "application/json")

			handler.AddSession(responseRecorder, req)

			assert.Equal(t, http.StatusCreated, responseRecorder.Code)
			var response map[string]string
			require.NoError(t, json.Unmarshal(responseRecorder.Body.Bytes(), &response))
			assert.Contains(t, response, "id")
		})

		t.Run("MissingFields", func(t *testing.T) {
			handler, responseRecorder := setupHandler()

			reqBody := handlers.AddSessionMemoryRequest{UserID: "user123", Vector: makeVector(768, 0.1)}
			jsonBody, _ := json.Marshal(reqBody)
			req := httptest.NewRequest("POST", "/api/memories/sessions", bytes.NewBuffer(jsonBody))
			req = setUser(req, "test-user")

			handler.AddSession(responseRecorder, req)

			assert.Equal(t, http.StatusBadRequest, responseRecorder.Code)
		})

		t.Run("WrongVectorDimension", func(t *testing.T) {
			handler, responseRecorder := setupHandler()

			reqBody := handlers.AddSessionMemoryRequest{
				AgentName: "test-agent",
				UserID:    "user123",
				Vector:    makeVector(16, 0.1), // not 768
			}
			jsonBody, _ := json.Marshal(reqBody)
			req := httptest.NewRequest("POST", "/api/memories/sessions", bytes.NewBuffer(jsonBody))
			req = setUser(req, "test-user")

			handler.AddSession(responseRecorder, req)

			assert.Equal(t, http.StatusBadRequest, responseRecorder.Code)
		})
	})

	t.Run("AddSessionBatch", func(t *testing.T) {
		t.Run("Success", func(t *testing.T) {
			handler, responseRecorder := setupHandler()

			reqBody := handlers.AddSessionMemoryBatchRequest{
				Items: []handlers.AddSessionMemoryRequest{
					{AgentName: "test-agent", UserID: "user123", Content: "First item", Vector: makeVector(768, 0.1)},
					{AgentName: "test-agent", UserID: "user123", Content: "Second item", Vector: makeVector(768, 0.2)},
				},
			}

			jsonBody, _ := json.Marshal(reqBody)
			req := httptest.NewRequest("POST", "/api/memories/sessions/batch", bytes.NewBuffer(jsonBody))
			req = setUser(req, "test-user")

			handler.AddSessionBatch(responseRecorder, req)

			assert.Equal(t, http.StatusCreated, responseRecorder.Code)
			var response map[string]int
			require.NoError(t, json.Unmarshal(responseRecorder.Body.Bytes(), &response))
			assert.Equal(t, 2, response["count"])
		})

		t.Run("EmptyBatch", func(t *testing.T) {
			handler, responseRecorder := setupHandler()

			reqBody := handlers.AddSessionMemoryBatchRequest{Items: []handlers.AddSessionMemoryRequest{}}
			jsonBody, _ := json.Marshal(reqBody)
			req := httptest.NewRequest("POST", "/api/memories/sessions/batch", bytes.NewBuffer(jsonBody))
			req = setUser(req, "test-user")

			handler.AddSessionBatch(responseRecorder, req)

			assert.Equal(t, http.StatusBadRequest, responseRecorder.Code)
		})

		t.Run("BatchTooLarge", func(t *testing.T) {
			handler, responseRecorder := setupHandler()

			items := make([]handlers.AddSessionMemoryRequest, 51)
			for i := range items {
				items[i] = handlers.AddSessionMemoryRequest{AgentName: "test-agent", UserID: "user123", Vector: makeVector(768, 0.1)}
			}
			jsonBody, _ := json.Marshal(handlers.AddSessionMemoryBatchRequest{Items: items})
			req := httptest.NewRequest("POST", "/api/memories/sessions/batch", bytes.NewBuffer(jsonBody))
			req = setUser(req, "test-user")

			handler.AddSessionBatch(responseRecorder, req)

			assert.Equal(t, http.StatusBadRequest, responseRecorder.Code)
		})
	})

	t.Run("Search", func(t *testing.T) {
		t.Run("Success", func(t *testing.T) {
			handler, responseRecorder := setupHandler()

			reqBody := handlers.SearchSessionMemoryRequest{
				AgentName: "test-agent",
				UserID:    "user123",
				Vector:    makeVector(768, 0.1),
				Limit:     5,
			}

			jsonBody, _ := json.Marshal(reqBody)
			req := httptest.NewRequest("POST", "/api/memories/search", bytes.NewBuffer(jsonBody))
			req = setUser(req, "test-user")

			handler.Search(responseRecorder, req)

			assert.Equal(t, http.StatusOK, responseRecorder.Code)
			var response []handlers.SearchSessionMemoryResponse
			require.NoError(t, json.Unmarshal(responseRecorder.Body.Bytes(), &response))
		})

		t.Run("MissingFields", func(t *testing.T) {
			handler, responseRecorder := setupHandler()

			reqBody := handlers.SearchSessionMemoryRequest{AgentName: "test-agent", Vector: makeVector(768, 0.1)}
			jsonBody, _ := json.Marshal(reqBody)
			req := httptest.NewRequest("POST", "/api/memories/search", bytes.NewBuffer(jsonBody))
			req = setUser(req, "test-user")

			handler.Search(responseRecorder, req)

			assert.Equal(t, http.StatusBadRequest, responseRecorder.Code)
		})
	})

	t.Run("List", func(t *testing.T) {
		t.Run("Success", func(t *testing.T) {
			handler, responseRecorder := setupHandler()

			req := httptest.NewRequest("GET", "/api/memories?agent_name=test-agent&user_id=user123", nil)
			req = setUser(req, "test-user")

			handler.List(responseRecorder, req)

			assert.Equal(t, http.StatusOK, responseRecorder.Code)
			var response []handlers.ListMemoryResponse
			require.NoError(t, json.Unmarshal(responseRecorder.Body.Bytes(), &response))
		})

		t.Run("MissingFields", func(t *testing.T) {
			handler, responseRecorder := setupHandler()

			req := httptest.NewRequest("GET", "/api/memories?agent_name=test-agent", nil)
			req = setUser(req, "test-user")

			handler.List(responseRecorder, req)

			assert.Equal(t, http.StatusBadRequest, responseRecorder.Code)
		})
	})

	t.Run("Delete", func(t *testing.T) {
		t.Run("Success", func(t *testing.T) {
			handler, responseRecorder := setupHandler()

			req := httptest.NewRequest("DELETE", "/api/memories?agent_name=test-agent&user_id=user123", nil)
			req = setUser(req, "test-user")

			handler.Delete(responseRecorder, req)

			assert.Equal(t, http.StatusOK, responseRecorder.Code)
			var response map[string]string
			require.NoError(t, json.Unmarshal(responseRecorder.Body.Bytes(), &response))
			assert.Equal(t, "deleted", response["status"])
		})

		t.Run("MissingFields", func(t *testing.T) {
			handler, responseRecorder := setupHandler()

			req := httptest.NewRequest("DELETE", "/api/memories?agent_name=test-agent", nil)
			req = setUser(req, "test-user")

			handler.Delete(responseRecorder, req)

			assert.Equal(t, http.StatusBadRequest, responseRecorder.Code)
		})
	})
}
