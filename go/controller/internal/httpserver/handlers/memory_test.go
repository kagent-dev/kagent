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
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/kagent-dev/kagent/go/controller/api/v1alpha1"
	"github.com/kagent-dev/kagent/go/controller/internal/httpserver/handlers"
)

func TestMemoryHandler(t *testing.T) {
	scheme := runtime.NewScheme()

	err := v1alpha1.AddToScheme(scheme)
	require.NoError(t, err)
	err = corev1.AddToScheme(scheme)
	require.NoError(t, err)

	setupHandler := func() (*handlers.MemoryHandler, client.Client, *mockErrorResponseWriter) {
		kubeClient := fake.NewClientBuilder().WithScheme(scheme).Build()
		base := &handlers.Base{
			KubeClient:         kubeClient,
			DefaultModelConfig: types.NamespacedName{Namespace: "default", Name: "default"},
		}
		handler := handlers.NewMemoryHandler(base)
		responseRecorder := newMockErrorResponseWriter()
		return handler, kubeClient, responseRecorder
	}

	t.Run("HandleListMemories", func(t *testing.T) {
		t.Run("Success", func(t *testing.T) {
			handler, kubeClient, responseRecorder := setupHandler()

			// Create test memories
			memory1 := &v1alpha1.Memory{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-memory-1",
					Namespace: "default",
				},
				Spec: v1alpha1.MemorySpec{
					Provider:        v1alpha1.Pinecone,
					APIKeySecretRef: "test-secret",
					APIKeySecretKey: "PINECONE_API_KEY",
					Pinecone: &v1alpha1.PineconeConfig{
						IndexHost:      "test-index.pinecone.io",
						TopK:           10,
						Namespace:      "test-ns",
						RecordFields:   []string{"field1", "field2"},
						ScoreThreshold: "0.8",
					},
				},
			}

			err := kubeClient.Create(context.Background(), memory1)
			require.NoError(t, err)

			req := httptest.NewRequest("GET", "/api/memories/", nil)
			handler.HandleListMemories(responseRecorder, req)

			assert.Equal(t, http.StatusOK, responseRecorder.Code)

			var memories []handlers.MemoryResponse
			err = json.Unmarshal(responseRecorder.Body.Bytes(), &memories)
			require.NoError(t, err)
			assert.Len(t, memories, 1)

			// Verify memory response
			memory := memories[0]
			assert.Equal(t, "default/test-memory-1", memory.Ref)
			assert.Equal(t, "Pinecone", memory.ProviderName)
			assert.Equal(t, "test-secret", memory.APIKeySecretRef)
			assert.Equal(t, "PINECONE_API_KEY", memory.APIKeySecretKey)
		})

		t.Run("EmptyList", func(t *testing.T) {
			handler, _, responseRecorder := setupHandler()

			req := httptest.NewRequest("GET", "/api/memories/", nil)
			handler.HandleListMemories(responseRecorder, req)

			assert.Equal(t, http.StatusOK, responseRecorder.Code)

			var memories []handlers.MemoryResponse
			err := json.Unmarshal(responseRecorder.Body.Bytes(), &memories)
			require.NoError(t, err)
			assert.Len(t, memories, 0)
		})
	})

	t.Run("HandleCreateMemory", func(t *testing.T) {
		t.Run("Success", func(t *testing.T) {
			handler, _, responseRecorder := setupHandler()

			reqBody := handlers.CreateMemoryRequest{
				Ref:      "default/test-memory",
				Provider: handlers.Provider{Type: "Pinecone"},
				APIKey:   "dGVzdC1hcGkta2V5Cg==",
				PineconeParams: &v1alpha1.PineconeConfig{
					IndexHost:      "test-index.pinecone.io",
					TopK:           10,
					Namespace:      "test-ns",
					RecordFields:   []string{"field1", "field2"},
					ScoreThreshold: "0.8",
				},
			}

			jsonBody, _ := json.Marshal(reqBody)
			req := httptest.NewRequest("POST", "/api/memories/", bytes.NewBuffer(jsonBody))
			req.Header.Set("Content-Type", "application/json")

			handler.HandleCreateMemory(responseRecorder, req)

			assert.Equal(t, http.StatusCreated, responseRecorder.Code)

			var memory v1alpha1.Memory
			err := json.Unmarshal(responseRecorder.Body.Bytes(), &memory)
			require.NoError(t, err)
			assert.Equal(t, "test-memory", memory.Name)
			assert.Equal(t, "default", memory.Namespace)
			assert.Equal(t, v1alpha1.Pinecone, memory.Spec.Provider)
		})

		t.Run("InvalidJSON", func(t *testing.T) {
			handler, _, responseRecorder := setupHandler()

			req := httptest.NewRequest("POST", "/api/memories/", bytes.NewBufferString("invalid json"))
			req.Header.Set("Content-Type", "application/json")

			handler.HandleCreateMemory(responseRecorder, req)

			assert.Equal(t, http.StatusBadRequest, responseRecorder.Code)
			assert.NotNil(t, responseRecorder.errorReceived)
		})

		t.Run("InvalidRef", func(t *testing.T) {
			handler, _, responseRecorder := setupHandler()

			reqBody := handlers.CreateMemoryRequest{
				Ref:      "invalid/ref/with/too/many/slashes",
				Provider: handlers.Provider{Type: "Pinecone"},
				APIKey:   "test-api-key",
			}

			jsonBody, _ := json.Marshal(reqBody)
			req := httptest.NewRequest("POST", "/api/memories/", bytes.NewBuffer(jsonBody))
			req.Header.Set("Content-Type", "application/json")

			handler.HandleCreateMemory(responseRecorder, req)

			assert.Equal(t, http.StatusBadRequest, responseRecorder.Code)
			assert.NotNil(t, responseRecorder.errorReceived)
		})

		t.Run("MemoryAlreadyExists", func(t *testing.T) {
			handler, kubeClient, responseRecorder := setupHandler()

			// Create existing memory
			existingMemory := &v1alpha1.Memory{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-memory",
					Namespace: "default",
				},
				Spec: v1alpha1.MemorySpec{
					Provider: v1alpha1.Pinecone,
				},
			}
			err := kubeClient.Create(context.Background(), existingMemory)
			require.NoError(t, err)

			reqBody := handlers.CreateMemoryRequest{
				Ref:      "default/test-memory",
				Provider: handlers.Provider{Type: "Pinecone"},
				APIKey:   "test-api-key",
			}

			jsonBody, _ := json.Marshal(reqBody)
			req := httptest.NewRequest("POST", "/api/memories/", bytes.NewBuffer(jsonBody))
			req.Header.Set("Content-Type", "application/json")

			handler.HandleCreateMemory(responseRecorder, req)

			assert.Equal(t, http.StatusConflict, responseRecorder.Code)
			assert.NotNil(t, responseRecorder.errorReceived)
		})
	})

	t.Run("HandleGetMemory", func(t *testing.T) {
		t.Run("Success", func(t *testing.T) {
			handler, kubeClient, responseRecorder := setupHandler()

			// Create test memory
			memory := &v1alpha1.Memory{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-memory",
					Namespace: "default",
				},
				Spec: v1alpha1.MemorySpec{
					Provider:        v1alpha1.Pinecone,
					APIKeySecretRef: "test-secret",
					APIKeySecretKey: "PINECONE_API_KEY",
					Pinecone: &v1alpha1.PineconeConfig{
						IndexHost: "test-index.pinecone.io",
						TopK:      10,
					},
				},
			}

			err := kubeClient.Create(context.Background(), memory)
			require.NoError(t, err)

			req := httptest.NewRequest("GET", "/api/memories/default/test-memory", nil)

			router := mux.NewRouter()
			router.HandleFunc("/api/memories/{namespace}/{memoryName}", func(w http.ResponseWriter, r *http.Request) {
				handler.HandleGetMemory(responseRecorder, r)
			}).Methods("GET")

			router.ServeHTTP(responseRecorder, req)

			assert.Equal(t, http.StatusOK, responseRecorder.Code)

			var memoryResponse handlers.MemoryResponse
			err = json.Unmarshal(responseRecorder.Body.Bytes(), &memoryResponse)
			require.NoError(t, err)
			assert.Equal(t, "default/test-memory", memoryResponse.Ref)
			assert.Equal(t, "Pinecone", memoryResponse.ProviderName)
		})

		t.Run("NotFound", func(t *testing.T) {
			handler, _, responseRecorder := setupHandler()

			req := httptest.NewRequest("GET", "/api/memories/default/nonexistent", nil)

			router := mux.NewRouter()
			router.HandleFunc("/api/memories/{namespace}/{memoryName}", func(w http.ResponseWriter, r *http.Request) {
				handler.HandleGetMemory(responseRecorder, r)
			}).Methods("GET")

			router.ServeHTTP(responseRecorder, req)

			assert.Equal(t, http.StatusNotFound, responseRecorder.Code)
			assert.NotNil(t, responseRecorder.errorReceived)
		})
	})

	t.Run("HandleUpdateMemory", func(t *testing.T) {
		t.Run("Success", func(t *testing.T) {
			handler, kubeClient, responseRecorder := setupHandler()

			// Create existing memory
			memory := &v1alpha1.Memory{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-memory",
					Namespace: "default",
				},
				Spec: v1alpha1.MemorySpec{
					Provider: v1alpha1.Pinecone,
					Pinecone: &v1alpha1.PineconeConfig{
						IndexHost: "old-index.pinecone.io",
						TopK:      5,
					},
				},
			}

			err := kubeClient.Create(context.Background(), memory)
			require.NoError(t, err)

			reqBody := handlers.UpdateMemoryRequest{
				PineconeParams: &v1alpha1.PineconeConfig{
					IndexHost: "new-index.pinecone.io",
					TopK:      15,
				},
			}

			jsonBody, _ := json.Marshal(reqBody)
			req := httptest.NewRequest("PUT", "/api/memories/default/test-memory", bytes.NewBuffer(jsonBody))
			req.Header.Set("Content-Type", "application/json")

			router := mux.NewRouter()
			router.HandleFunc("/api/memories/{namespace}/{memoryName}", func(w http.ResponseWriter, r *http.Request) {
				handler.HandleUpdateMemory(responseRecorder, r)
			}).Methods("PUT")

			router.ServeHTTP(responseRecorder, req)

			assert.Equal(t, http.StatusOK, responseRecorder.Code)

			var updatedMemory v1alpha1.Memory
			err = json.Unmarshal(responseRecorder.Body.Bytes(), &updatedMemory)
			require.NoError(t, err)
			assert.Equal(t, "new-index.pinecone.io", updatedMemory.Spec.Pinecone.IndexHost)
			assert.Equal(t, 15, updatedMemory.Spec.Pinecone.TopK)
		})

		t.Run("InvalidJSON", func(t *testing.T) {
			handler, _, responseRecorder := setupHandler()

			req := httptest.NewRequest("PUT", "/api/memories/default/test-memory", bytes.NewBufferString("invalid json"))
			req.Header.Set("Content-Type", "application/json")

			router := mux.NewRouter()
			router.HandleFunc("/api/memories/{namespace}/{memoryName}", func(w http.ResponseWriter, r *http.Request) {
				handler.HandleUpdateMemory(responseRecorder, r)
			}).Methods("PUT")

			router.ServeHTTP(responseRecorder, req)

			assert.Equal(t, http.StatusBadRequest, responseRecorder.Code)
			assert.NotNil(t, responseRecorder.errorReceived)
		})

		t.Run("MemoryNotFound", func(t *testing.T) {
			handler, _, responseRecorder := setupHandler()

			reqBody := handlers.UpdateMemoryRequest{
				PineconeParams: &v1alpha1.PineconeConfig{
					IndexHost: "new-index.pinecone.io",
				},
			}

			jsonBody, _ := json.Marshal(reqBody)
			req := httptest.NewRequest("PUT", "/api/memories/default/nonexistent", bytes.NewBuffer(jsonBody))
			req.Header.Set("Content-Type", "application/json")

			router := mux.NewRouter()
			router.HandleFunc("/api/memories/{namespace}/{memoryName}", func(w http.ResponseWriter, r *http.Request) {
				handler.HandleUpdateMemory(responseRecorder, r)
			}).Methods("PUT")

			router.ServeHTTP(responseRecorder, req)

			assert.Equal(t, http.StatusInternalServerError, responseRecorder.Code)
			assert.NotNil(t, responseRecorder.errorReceived)
		})
	})

	t.Run("HandleDeleteMemory", func(t *testing.T) {
		t.Run("Success", func(t *testing.T) {
			handler, kubeClient, responseRecorder := setupHandler()

			// Create memory to delete
			memory := &v1alpha1.Memory{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-memory",
					Namespace: "default",
				},
				Spec: v1alpha1.MemorySpec{
					Provider: v1alpha1.Pinecone,
				},
			}

			err := kubeClient.Create(context.Background(), memory)
			require.NoError(t, err)

			req := httptest.NewRequest("DELETE", "/api/memories/default/test-memory", nil)

			router := mux.NewRouter()
			router.HandleFunc("/api/memories/{namespace}/{memoryName}", func(w http.ResponseWriter, r *http.Request) {
				handler.HandleDeleteMemory(responseRecorder, r)
			}).Methods("DELETE")

			router.ServeHTTP(responseRecorder, req)

			assert.Equal(t, http.StatusOK, responseRecorder.Code)

			var response map[string]string
			err = json.Unmarshal(responseRecorder.Body.Bytes(), &response)
			require.NoError(t, err)
			assert.Equal(t, "Memory deleted successfully", response["message"])
		})

		t.Run("NotFound", func(t *testing.T) {
			handler, _, responseRecorder := setupHandler()

			req := httptest.NewRequest("DELETE", "/api/memories/default/nonexistent", nil)

			router := mux.NewRouter()
			router.HandleFunc("/api/memories/{namespace}/{memoryName}", func(w http.ResponseWriter, r *http.Request) {
				handler.HandleDeleteMemory(responseRecorder, r)
			}).Methods("DELETE")

			router.ServeHTTP(responseRecorder, req)

			assert.Equal(t, http.StatusNotFound, responseRecorder.Code)
			assert.NotNil(t, responseRecorder.errorReceived)
		})
	})
}
