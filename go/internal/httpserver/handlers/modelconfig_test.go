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
	ctrl_client "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/kagent-dev/kagent/go/api/v1alpha2"
	"github.com/kagent-dev/kagent/go/internal/httpserver/auth"
	"github.com/kagent-dev/kagent/go/internal/httpserver/handlers"
	"github.com/kagent-dev/kagent/go/pkg/client/api"
)

func TestModelConfigHandler(t *testing.T) {
	scheme := runtime.NewScheme()

	err := v1alpha2.AddToScheme(scheme)
	require.NoError(t, err)
	err = corev1.AddToScheme(scheme)
	require.NoError(t, err)

	setupHandler := func() (*handlers.ModelConfigHandler, ctrl_client.Client, *mockErrorResponseWriter) {
		kubeClient := fake.NewClientBuilder().WithScheme(scheme).Build()
		base := &handlers.Base{
			KubeClient:         kubeClient,
			DefaultModelConfig: types.NamespacedName{Namespace: "default", Name: "default"},
			Authorizer:         &auth.NoopAuthorizer{},
		}
		handler := handlers.NewModelConfigHandler(base)
		responseRecorder := newMockErrorResponseWriter()
		return handler, kubeClient, responseRecorder
	}

	t.Run("HandleListModelConfigs", func(t *testing.T) {
		t.Run("Success", func(t *testing.T) {
			handler, kubeClient, responseRecorder := setupHandler()

			// Create test model configs
			modelConfig1 := &v1alpha2.ModelConfig{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-config-1",
					Namespace: "default",
				},
				Spec: v1alpha2.ModelConfigSpec{
					Model:           "gpt-4",
					Provider:        v1alpha2.ModelProviderOpenAI,
					APIKeySecret:    "test-secret",
					APIKeySecretKey: "OPENAI_API_KEY",
					OpenAI: &v1alpha2.OpenAIConfig{
						BaseURL:     "https://api.openai.com/v1",
						Temperature: "0.7",
						MaxTokens:   1000,
					},
				},
			}

			err := kubeClient.Create(context.Background(), modelConfig1)
			require.NoError(t, err)

			req := httptest.NewRequest("GET", "/api/modelconfigs/", nil)
			req = setUser(req, "test-user")
			handler.HandleListModelConfigs(responseRecorder, req)

			assert.Equal(t, http.StatusOK, responseRecorder.Code)

			var configs api.StandardResponse[[]api.ModelConfigResponse]
			err = json.Unmarshal(responseRecorder.Body.Bytes(), &configs)
			require.NoError(t, err)
			assert.Len(t, configs.Data, 1)

			// Verify model config response
			config := configs.Data[0]
			assert.Equal(t, "default/test-config-1", config.Ref)
			assert.Equal(t, "OpenAI", config.ProviderName)
			assert.Equal(t, "gpt-4", config.Model)
			assert.Equal(t, "test-secret", config.APIKeySecret)
			assert.Equal(t, "OPENAI_API_KEY", config.APIKeySecretKey)
		})

		t.Run("EmptyList", func(t *testing.T) {
			handler, _, responseRecorder := setupHandler()

			req := httptest.NewRequest("GET", "/api/modelconfigs/", nil)
			req = setUser(req, "test-user")
			handler.HandleListModelConfigs(responseRecorder, req)

			assert.Equal(t, http.StatusOK, responseRecorder.Code)

			var configs api.StandardResponse[[]api.ModelConfigResponse]
			err := json.Unmarshal(responseRecorder.Body.Bytes(), &configs)
			require.NoError(t, err)
			assert.Len(t, configs.Data, 0)
		})
	})

	t.Run("HandleCreateModelConfig", func(t *testing.T) {
		t.Run("Success_OpenAI", func(t *testing.T) {
			handler, _, responseRecorder := setupHandler()

			reqBody := api.CreateModelConfigRequest{
				Ref:      "default/test-config",
				Provider: api.Provider{Type: "OpenAI"},
				Model:    "gpt-4",
				APIKey:   "test-api-key",
				OpenAIParams: &v1alpha2.OpenAIConfig{
					BaseURL:     "https://api.openai.com/v1",
					Temperature: "0.7",
					MaxTokens:   1000,
				},
			}

			jsonBody, _ := json.Marshal(reqBody)
			req := httptest.NewRequest("POST", "/api/modelconfigs/", bytes.NewBuffer(jsonBody))
			req = setUser(req, "test-user")
			req.Header.Set("Content-Type", "application/json")

			handler.HandleCreateModelConfig(responseRecorder, req)

			assert.Equal(t, http.StatusCreated, responseRecorder.Code)

			var config api.StandardResponse[v1alpha2.ModelConfig]
			err := json.Unmarshal(responseRecorder.Body.Bytes(), &config)
			require.NoError(t, err)
			assert.Equal(t, "test-config", config.Data.Name)
			assert.Equal(t, "default", config.Data.Namespace)
			assert.Equal(t, v1alpha2.ModelProviderOpenAI, config.Data.Spec.Provider)
			assert.Equal(t, "gpt-4", config.Data.Spec.Model)
		})

		t.Run("Success_Anthropic", func(t *testing.T) {
			handler, _, responseRecorder := setupHandler()

			reqBody := api.CreateModelConfigRequest{
				Ref:      "default/test-anthropic",
				Provider: api.Provider{Type: "Anthropic"},
				Model:    "claude-3-sonnet",
				APIKey:   "test-api-key",
				AnthropicParams: &v1alpha2.AnthropicConfig{
					BaseURL:     "https://api.anthropic.com",
					Temperature: "0.5",
					MaxTokens:   2000,
				},
			}

			jsonBody, _ := json.Marshal(reqBody)
			req := httptest.NewRequest("POST", "/api/modelconfigs/", bytes.NewBuffer(jsonBody))
			req = setUser(req, "test-user")
			req.Header.Set("Content-Type", "application/json")

			handler.HandleCreateModelConfig(responseRecorder, req)

			assert.Equal(t, http.StatusCreated, responseRecorder.Code)

			var config api.StandardResponse[v1alpha2.ModelConfig]
			err := json.Unmarshal(responseRecorder.Body.Bytes(), &config)
			require.NoError(t, err)
			assert.Equal(t, v1alpha2.ModelProviderAnthropic, config.Data.Spec.Provider)
		})

		t.Run("Success_Ollama_NoAPIKey", func(t *testing.T) {
			handler, _, responseRecorder := setupHandler()

			reqBody := api.CreateModelConfigRequest{
				Ref:      "default/test-ollama",
				Provider: api.Provider{Type: "Ollama"},
				Model:    "llama2",
				OllamaParams: &v1alpha2.OllamaConfig{
					Host: "http://localhost:11434",
					Options: map[string]string{
						"temperature": "0.8",
					},
				},
			}

			jsonBody, _ := json.Marshal(reqBody)
			req := httptest.NewRequest("POST", "/api/modelconfigs/", bytes.NewBuffer(jsonBody))
			req = setUser(req, "test-user")
			req.Header.Set("Content-Type", "application/json")

			handler.HandleCreateModelConfig(responseRecorder, req)

			assert.Equal(t, http.StatusCreated, responseRecorder.Code)

			var config api.StandardResponse[v1alpha2.ModelConfig]
			err := json.Unmarshal(responseRecorder.Body.Bytes(), &config)
			require.NoError(t, err)
			assert.Equal(t, v1alpha2.ModelProviderOllama, config.Data.Spec.Provider)
			assert.Empty(t, config.Data.Spec.APIKeySecret)
		})

		t.Run("Success_AzureOpenAI", func(t *testing.T) {
			handler, _, responseRecorder := setupHandler()

			reqBody := api.CreateModelConfigRequest{
				Ref:      "default/test-azure",
				Provider: api.Provider{Type: "AzureOpenAI"},
				Model:    "gpt-4",
				APIKey:   "test-api-key",
				AzureParams: &v1alpha2.AzureOpenAIConfig{
					Endpoint:   "https://myresource.openai.azure.com/",
					APIVersion: "2023-05-15",
				},
			}

			jsonBody, _ := json.Marshal(reqBody)
			req := httptest.NewRequest("POST", "/api/modelconfigs/", bytes.NewBuffer(jsonBody))
			req = setUser(req, "test-user")
			req.Header.Set("Content-Type", "application/json")

			handler.HandleCreateModelConfig(responseRecorder, req)

			assert.Equal(t, http.StatusCreated, responseRecorder.Code, responseRecorder.Body.String())

			var config api.StandardResponse[v1alpha2.ModelConfig]
			err := json.Unmarshal(responseRecorder.Body.Bytes(), &config)
			require.NoError(t, err)
			assert.Equal(t, v1alpha2.ModelProviderAzureOpenAI, config.Data.Spec.Provider)
		})

		t.Run("InvalidJSON", func(t *testing.T) {
			handler, _, responseRecorder := setupHandler()

			req := httptest.NewRequest("POST", "/api/modelconfigs/", bytes.NewBufferString("invalid json"))
			req = setUser(req, "test-user")
			req.Header.Set("Content-Type", "application/json")

			handler.HandleCreateModelConfig(responseRecorder, req)

			assert.Equal(t, http.StatusBadRequest, responseRecorder.Code)
			assert.NotNil(t, responseRecorder.errorReceived)
		})

		t.Run("InvalidRef", func(t *testing.T) {
			handler, _, responseRecorder := setupHandler()

			reqBody := api.CreateModelConfigRequest{
				Ref:      "invalid/ref/with/too/many/slashes",
				Provider: api.Provider{Type: "OpenAI"},
				Model:    "gpt-4",
				APIKey:   "test-api-key",
			}

			jsonBody, _ := json.Marshal(reqBody)
			req := httptest.NewRequest("POST", "/api/modelconfigs/", bytes.NewBuffer(jsonBody))
			req = setUser(req, "test-user")
			req.Header.Set("Content-Type", "application/json")

			handler.HandleCreateModelConfig(responseRecorder, req)

			assert.Equal(t, http.StatusBadRequest, responseRecorder.Code)
			assert.NotNil(t, responseRecorder.errorReceived)
		})

		t.Run("ModelConfigAlreadyExists", func(t *testing.T) {
			handler, kubeClient, responseRecorder := setupHandler()

			// Create existing model config
			existingConfig := &v1alpha2.ModelConfig{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-config",
					Namespace: "default",
				},
				Spec: v1alpha2.ModelConfigSpec{
					Model:    "gpt-4",
					Provider: v1alpha2.ModelProviderOpenAI,
				},
			}
			err := kubeClient.Create(context.Background(), existingConfig)
			require.NoError(t, err)

			reqBody := api.CreateModelConfigRequest{
				Ref:      "default/test-config",
				Provider: api.Provider{Type: "OpenAI"},
				Model:    "gpt-4",
				APIKey:   "test-api-key",
			}

			jsonBody, _ := json.Marshal(reqBody)
			req := httptest.NewRequest("POST", "/api/modelconfigs/", bytes.NewBuffer(jsonBody))
			req = setUser(req, "test-user")
			req.Header.Set("Content-Type", "application/json")

			handler.HandleCreateModelConfig(responseRecorder, req)

			assert.Equal(t, http.StatusConflict, responseRecorder.Code)
			assert.NotNil(t, responseRecorder.errorReceived)
		})

		t.Run("AzureOpenAI_MissingRequiredParams", func(t *testing.T) {
			handler, _, responseRecorder := setupHandler()

			reqBody := api.CreateModelConfigRequest{
				Ref:         "default/test-azure",
				Provider:    api.Provider{Type: "AzureOpenAI"},
				Model:       "gpt-4",
				APIKey:      "test-api-key",
				AzureParams: &v1alpha2.AzureOpenAIConfig{
					// Missing required Endpoint and APIVersion
				},
			}

			jsonBody, _ := json.Marshal(reqBody)
			req := httptest.NewRequest("POST", "/api/modelconfigs/", bytes.NewBuffer(jsonBody))
			req = setUser(req, "test-user")
			req.Header.Set("Content-Type", "application/json")

			handler.HandleCreateModelConfig(responseRecorder, req)

			assert.Equal(t, http.StatusBadRequest, responseRecorder.Code)
			assert.NotNil(t, responseRecorder.errorReceived)
		})

		t.Run("UnsupportedProvider", func(t *testing.T) {
			handler, _, responseRecorder := setupHandler()

			reqBody := api.CreateModelConfigRequest{
				Ref:      "default/test-config",
				Provider: api.Provider{Type: "UnsupportedProvider"},
				Model:    "some-model",
				APIKey:   "test-api-key",
			}

			jsonBody, _ := json.Marshal(reqBody)
			req := httptest.NewRequest("POST", "/api/modelconfigs/", bytes.NewBuffer(jsonBody))
			req = setUser(req, "test-user")
			req.Header.Set("Content-Type", "application/json")

			handler.HandleCreateModelConfig(responseRecorder, req)

			assert.Equal(t, http.StatusBadRequest, responseRecorder.Code)
			assert.NotNil(t, responseRecorder.errorReceived)
		})
	})

	t.Run("HandleGetModelConfig", func(t *testing.T) {
		t.Run("Success", func(t *testing.T) {
			handler, kubeClient, responseRecorder := setupHandler()

			// Create test model config
			config := &v1alpha2.ModelConfig{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-config",
					Namespace: "default",
				},
				Spec: v1alpha2.ModelConfigSpec{
					Model:           "gpt-4",
					Provider:        v1alpha2.ModelProviderOpenAI,
					APIKeySecret:    "test-secret",
					APIKeySecretKey: "OPENAI_API_KEY",
					OpenAI: &v1alpha2.OpenAIConfig{
						BaseURL:     "https://api.openai.com/v1",
						Temperature: "0.7",
					},
				},
			}

			err := kubeClient.Create(context.Background(), config)
			require.NoError(t, err)

			req := httptest.NewRequest("GET", "/api/modelconfigs/default/test-config", nil)
			req = setUser(req, "test-user")

			router := mux.NewRouter()
			router.HandleFunc("/api/modelconfigs/{namespace}/{name}", func(w http.ResponseWriter, r *http.Request) {
				handler.HandleGetModelConfig(responseRecorder, r)
			}).Methods("GET")

			router.ServeHTTP(responseRecorder, req)

			assert.Equal(t, http.StatusOK, responseRecorder.Code, responseRecorder.Body.String())

			var configResponse api.StandardResponse[api.ModelConfigResponse]
			err = json.Unmarshal(responseRecorder.Body.Bytes(), &configResponse)
			require.NoError(t, err)
			assert.Equal(t, "default/test-config", configResponse.Data.Ref)
			assert.Equal(t, "OpenAI", configResponse.Data.ProviderName)
			assert.Equal(t, "gpt-4", configResponse.Data.Model)
			assert.Equal(t, "test-secret", configResponse.Data.APIKeySecret)
			assert.Equal(t, "OPENAI_API_KEY", configResponse.Data.APIKeySecretKey)
			assert.NotEmpty(t, configResponse.Data.ModelParams)
		})

		t.Run("NotFound", func(t *testing.T) {
			handler, _, responseRecorder := setupHandler()

			req := httptest.NewRequest("GET", "/api/modelconfigs/default/nonexistent", nil)
			req = setUser(req, "test-user")

			router := mux.NewRouter()
			router.HandleFunc("/api/modelconfigs/{namespace}/{name}", func(w http.ResponseWriter, r *http.Request) {
				handler.HandleGetModelConfig(responseRecorder, r)
			}).Methods("GET")

			router.ServeHTTP(responseRecorder, req)

			assert.Equal(t, http.StatusNotFound, responseRecorder.Code, responseRecorder.Body.String())
			assert.NotNil(t, responseRecorder.errorReceived)
		})
	})

	t.Run("HandleUpdateModelConfig", func(t *testing.T) {
		t.Run("Success", func(t *testing.T) {
			handler, kubeClient, responseRecorder := setupHandler()

			// Create existing model config
			config := &v1alpha2.ModelConfig{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-config",
					Namespace: "default",
				},
				Spec: v1alpha2.ModelConfigSpec{
					Model:    "gpt-3.5-turbo",
					Provider: v1alpha2.ModelProviderOpenAI,
					OpenAI: &v1alpha2.OpenAIConfig{
						BaseURL:     "https://api.openai.com/v1",
						Temperature: "0.5",
					},
				},
			}

			err := kubeClient.Create(context.Background(), config)
			require.NoError(t, err)

			apiKey := "new-api-key"
			reqBody := api.UpdateModelConfigRequest{
				Provider: api.Provider{Type: "OpenAI"},
				Model:    "gpt-4",
				APIKey:   &apiKey,
				OpenAIParams: &v1alpha2.OpenAIConfig{
					BaseURL:     "https://api.openai.com/v1",
					Temperature: "0.7",
					MaxTokens:   2000,
				},
			}

			jsonBody, _ := json.Marshal(reqBody)
			req := httptest.NewRequest("PUT", "/api/modelconfigs/default/test-config", bytes.NewBuffer(jsonBody))
			req = setUser(req, "test-user")
			req.Header.Set("Content-Type", "application/json")

			router := mux.NewRouter()
			router.HandleFunc("/api/modelconfigs/{namespace}/{name}", func(w http.ResponseWriter, r *http.Request) {
				handler.HandleUpdateModelConfig(responseRecorder, r)
			}).Methods("PUT")

			router.ServeHTTP(responseRecorder, req)

			assert.Equal(t, http.StatusOK, responseRecorder.Code, responseRecorder.Body.String())

			var updatedConfig api.StandardResponse[api.ModelConfigResponse]
			err = json.Unmarshal(responseRecorder.Body.Bytes(), &updatedConfig)
			require.NoError(t, err)
			assert.Equal(t, "gpt-4", updatedConfig.Data.Model)
			assert.Contains(t, updatedConfig.Data.ModelParams, "temperature")
		})

		t.Run("InvalidJSON", func(t *testing.T) {
			handler, _, responseRecorder := setupHandler()

			req := httptest.NewRequest("PUT", "/api/modelconfigs/default/test-config", bytes.NewBufferString("invalid json"))
			req = setUser(req, "test-user")
			req.Header.Set("Content-Type", "application/json")

			router := mux.NewRouter()
			router.HandleFunc("/api/modelconfigs/{namespace}/{configName}", func(w http.ResponseWriter, r *http.Request) {
				handler.HandleUpdateModelConfig(responseRecorder, r)
			}).Methods("PUT")

			router.ServeHTTP(responseRecorder, req)

			assert.Equal(t, http.StatusBadRequest, responseRecorder.Code)
			assert.NotNil(t, responseRecorder.errorReceived)
		})

		t.Run("ModelConfigNotFound", func(t *testing.T) {
			handler, _, responseRecorder := setupHandler()

			reqBody := api.UpdateModelConfigRequest{
				Provider: api.Provider{Type: "OpenAI"},
				Model:    "gpt-4",
				OpenAIParams: &v1alpha2.OpenAIConfig{
					Temperature: "0.7",
				},
			}

			jsonBody, _ := json.Marshal(reqBody)
			req := httptest.NewRequest("PUT", "/api/modelconfigs/default/nonexistent", bytes.NewBuffer(jsonBody))
			req = setUser(req, "test-user")
			req.Header.Set("Content-Type", "application/json")

			router := mux.NewRouter()
			router.HandleFunc("/api/modelconfigs/{namespace}/{name}", func(w http.ResponseWriter, r *http.Request) {
				handler.HandleUpdateModelConfig(responseRecorder, r)
			}).Methods("PUT")

			router.ServeHTTP(responseRecorder, req)

			assert.Equal(t, http.StatusNotFound, responseRecorder.Code, responseRecorder.Body.String())
			assert.NotNil(t, responseRecorder.errorReceived)
		})
	})

	t.Run("HandleDeleteModelConfig", func(t *testing.T) {
		t.Run("Success", func(t *testing.T) {
			handler, kubeClient, responseRecorder := setupHandler()

			// Create config to delete
			config := &v1alpha2.ModelConfig{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-config",
					Namespace: "default",
				},
				Spec: v1alpha2.ModelConfigSpec{
					Model:    "gpt-4",
					Provider: v1alpha2.ModelProviderOpenAI,
				},
			}

			err := kubeClient.Create(context.Background(), config)
			require.NoError(t, err)

			req := httptest.NewRequest("DELETE", "/api/modelconfigs/default/test-config", nil)
			req = setUser(req, "test-user")

			router := mux.NewRouter()
			router.HandleFunc("/api/modelconfigs/{namespace}/{name}", func(w http.ResponseWriter, r *http.Request) {
				handler.HandleDeleteModelConfig(responseRecorder, r)
			}).Methods("DELETE")

			router.ServeHTTP(responseRecorder, req)

			assert.Equal(t, http.StatusOK, responseRecorder.Code)
		})

		t.Run("NotFound", func(t *testing.T) {
			handler, _, responseRecorder := setupHandler()

			req := httptest.NewRequest("DELETE", "/api/modelconfigs/default/nonexistent", nil)
			req = setUser(req, "test-user")

			router := mux.NewRouter()
			router.HandleFunc("/api/modelconfigs/{namespace}/{name}", func(w http.ResponseWriter, r *http.Request) {
				handler.HandleDeleteModelConfig(responseRecorder, r)
			}).Methods("DELETE")

			router.ServeHTTP(responseRecorder, req)

			assert.Equal(t, http.StatusNotFound, responseRecorder.Code)
			assert.NotNil(t, responseRecorder.errorReceived)
		})
	})
}
