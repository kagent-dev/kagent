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

	api "github.com/kagent-dev/kagent/go/api/httpapi"
	"github.com/kagent-dev/kagent/go/api/v1alpha2"
	"github.com/kagent-dev/kagent/go/core/internal/httpserver/auth"
	"github.com/kagent-dev/kagent/go/core/internal/httpserver/handlers"
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

			modelConfig1 := &v1alpha2.ModelConfig{
				ObjectMeta: metav1.ObjectMeta{Name: "test-config-1", Namespace: "default"},
				Spec: v1alpha2.ModelConfigSpec{
					Model:           "gpt-4",
					Provider:        v1alpha2.ModelProviderOpenAI,
					APIKeySecret:    "test-secret",
					APIKeySecretKey: "OPENAI_API_KEY",
					OpenAI:          &v1alpha2.OpenAIConfig{BaseURL: "https://api.openai.com/v1", Temperature: "0.7", MaxTokens: 1000},
				},
			}

			err := kubeClient.Create(context.Background(), modelConfig1)
			require.NoError(t, err)

			req := httptest.NewRequest("GET", "/api/modelconfigs/", nil)
			req = setUser(req, "test-user")
			handler.HandleListModelConfigs(responseRecorder, req)

			assert.Equal(t, http.StatusOK, responseRecorder.Code)

			var configs api.StandardResponse[[]api.ModelConfigResource]
			err = json.Unmarshal(responseRecorder.Body.Bytes(), &configs)
			require.NoError(t, err)
			assert.Len(t, configs.Data, 1)

			config := configs.Data[0]
			assert.Equal(t, "default/test-config-1", config.Ref)
			assert.Equal(t, v1alpha2.ModelProviderOpenAI, config.Spec.Provider)
			assert.Equal(t, "gpt-4", config.Spec.Model)
			assert.Equal(t, "test-secret", config.Spec.APIKeySecret)
			assert.Equal(t, "OPENAI_API_KEY", config.Spec.APIKeySecretKey)
			assert.NotNil(t, config.Spec.OpenAI)
		})

		t.Run("EmptyList", func(t *testing.T) {
			handler, _, responseRecorder := setupHandler()

			req := httptest.NewRequest("GET", "/api/modelconfigs/", nil)
			req = setUser(req, "test-user")
			handler.HandleListModelConfigs(responseRecorder, req)

			assert.Equal(t, http.StatusOK, responseRecorder.Code)

			var configs api.StandardResponse[[]api.ModelConfigResource]
			err := json.Unmarshal(responseRecorder.Body.Bytes(), &configs)
			require.NoError(t, err)
			assert.Len(t, configs.Data, 0)
		})
	})

	t.Run("HandleCreateModelConfig", func(t *testing.T) {
		t.Run("Success_OpenAI", func(t *testing.T) {
			handler, _, responseRecorder := setupHandler()

			reqBody := api.CreateModelConfigRequest{
				Ref:    "default/test-config",
				APIKey: "test-api-key",
				Spec: v1alpha2.ModelConfigSpec{
					Model:    "gpt-4",
					Provider: v1alpha2.ModelProviderOpenAI,
					OpenAI:   &v1alpha2.OpenAIConfig{BaseURL: "https://api.openai.com/v1", Temperature: "0.7", MaxTokens: 1000},
				},
			}

			jsonBody, _ := json.Marshal(reqBody)
			req := httptest.NewRequest("POST", "/api/modelconfigs/", bytes.NewBuffer(jsonBody))
			req = setUser(req, "test-user")
			req.Header.Set("Content-Type", "application/json")

			handler.HandleCreateModelConfig(responseRecorder, req)

			assert.Equal(t, http.StatusCreated, responseRecorder.Code)

			var config api.StandardResponse[api.ModelConfigResource]
			err := json.Unmarshal(responseRecorder.Body.Bytes(), &config)
			require.NoError(t, err)
			assert.Equal(t, "default/test-config", config.Data.Ref)
			assert.Equal(t, v1alpha2.ModelProviderOpenAI, config.Data.Spec.Provider)
			assert.Equal(t, "gpt-4", config.Data.Spec.Model)
			// Secret ref should be auto-set from inline apiKey
			assert.Equal(t, "test-config", config.Data.Spec.APIKeySecret)
		})

		t.Run("Success_Anthropic", func(t *testing.T) {
			handler, _, responseRecorder := setupHandler()

			reqBody := api.CreateModelConfigRequest{
				Ref:    "default/test-anthropic",
				APIKey: "test-api-key",
				Spec: v1alpha2.ModelConfigSpec{
					Model:     "claude-3-sonnet",
					Provider:  v1alpha2.ModelProviderAnthropic,
					Anthropic: &v1alpha2.AnthropicConfig{BaseURL: "https://api.anthropic.com", Temperature: "0.5", MaxTokens: 2000},
				},
			}

			jsonBody, _ := json.Marshal(reqBody)
			req := httptest.NewRequest("POST", "/api/modelconfigs/", bytes.NewBuffer(jsonBody))
			req = setUser(req, "test-user")
			req.Header.Set("Content-Type", "application/json")

			handler.HandleCreateModelConfig(responseRecorder, req)

			assert.Equal(t, http.StatusCreated, responseRecorder.Code)

			var config api.StandardResponse[api.ModelConfigResource]
			err := json.Unmarshal(responseRecorder.Body.Bytes(), &config)
			require.NoError(t, err)
			assert.Equal(t, v1alpha2.ModelProviderAnthropic, config.Data.Spec.Provider)
		})

		t.Run("Success_Ollama_NoAPIKey", func(t *testing.T) {
			handler, _, responseRecorder := setupHandler()

			reqBody := api.CreateModelConfigRequest{
				Ref: "default/test-ollama",
				Spec: v1alpha2.ModelConfigSpec{
					Model:    "llama2",
					Provider: v1alpha2.ModelProviderOllama,
					Ollama:   &v1alpha2.OllamaConfig{Host: "http://localhost:11434", Options: map[string]string{"temperature": "0.8"}},
				},
			}

			jsonBody, _ := json.Marshal(reqBody)
			req := httptest.NewRequest("POST", "/api/modelconfigs/", bytes.NewBuffer(jsonBody))
			req = setUser(req, "test-user")
			req.Header.Set("Content-Type", "application/json")

			handler.HandleCreateModelConfig(responseRecorder, req)

			assert.Equal(t, http.StatusCreated, responseRecorder.Code)

			var config api.StandardResponse[api.ModelConfigResource]
			err := json.Unmarshal(responseRecorder.Body.Bytes(), &config)
			require.NoError(t, err)
			assert.Equal(t, v1alpha2.ModelProviderOllama, config.Data.Spec.Provider)
			assert.Empty(t, config.Data.Spec.APIKeySecret)
		})

		t.Run("Success_AzureOpenAI", func(t *testing.T) {
			handler, _, responseRecorder := setupHandler()

			reqBody := api.CreateModelConfigRequest{
				Ref:    "default/test-azure",
				APIKey: "test-api-key",
				Spec: v1alpha2.ModelConfigSpec{
					Model:    "gpt-4",
					Provider: v1alpha2.ModelProviderAzureOpenAI,
					AzureOpenAI: &v1alpha2.AzureOpenAIConfig{
						Endpoint:   "https://myresource.openai.azure.com/",
						APIVersion: "2023-05-15",
					},
				},
			}

			jsonBody, _ := json.Marshal(reqBody)
			req := httptest.NewRequest("POST", "/api/modelconfigs/", bytes.NewBuffer(jsonBody))
			req = setUser(req, "test-user")
			req.Header.Set("Content-Type", "application/json")

			handler.HandleCreateModelConfig(responseRecorder, req)

			assert.Equal(t, http.StatusCreated, responseRecorder.Code, responseRecorder.Body.String())

			var config api.StandardResponse[api.ModelConfigResource]
			err := json.Unmarshal(responseRecorder.Body.Bytes(), &config)
			require.NoError(t, err)
			assert.Equal(t, v1alpha2.ModelProviderAzureOpenAI, config.Data.Spec.Provider)
		})

		t.Run("Success_Bedrock", func(t *testing.T) {
			handler, _, responseRecorder := setupHandler()

			reqBody := api.CreateModelConfigRequest{
				Ref: "default/test-bedrock",
				Spec: v1alpha2.ModelConfigSpec{
					Model:    "anthropic.claude-3-sonnet-20240229-v1:0",
					Provider: v1alpha2.ModelProviderBedrock,
					Bedrock:  &v1alpha2.BedrockConfig{Region: "us-east-1"},
				},
			}

			jsonBody, _ := json.Marshal(reqBody)
			req := httptest.NewRequest("POST", "/api/modelconfigs/", bytes.NewBuffer(jsonBody))
			req = setUser(req, "test-user")
			req.Header.Set("Content-Type", "application/json")

			handler.HandleCreateModelConfig(responseRecorder, req)

			assert.Equal(t, http.StatusCreated, responseRecorder.Code, responseRecorder.Body.String())

			var config api.StandardResponse[api.ModelConfigResource]
			err := json.Unmarshal(responseRecorder.Body.Bytes(), &config)
			require.NoError(t, err)
			assert.Equal(t, v1alpha2.ModelProviderBedrock, config.Data.Spec.Provider)
			require.NotNil(t, config.Data.Spec.Bedrock)
			assert.Equal(t, "us-east-1", config.Data.Spec.Bedrock.Region)
		})

		t.Run("Success_ExistingSecret", func(t *testing.T) {
			handler, _, responseRecorder := setupHandler()

			reqBody := api.CreateModelConfigRequest{
				Ref: "default/test-existing-secret",
				Spec: v1alpha2.ModelConfigSpec{
					Model:           "gpt-4",
					Provider:        v1alpha2.ModelProviderOpenAI,
					APIKeySecret:    "my-existing-secret",
					APIKeySecretKey: "OPENAI_API_KEY",
				},
			}

			jsonBody, _ := json.Marshal(reqBody)
			req := httptest.NewRequest("POST", "/api/modelconfigs/", bytes.NewBuffer(jsonBody))
			req = setUser(req, "test-user")
			req.Header.Set("Content-Type", "application/json")

			handler.HandleCreateModelConfig(responseRecorder, req)

			assert.Equal(t, http.StatusCreated, responseRecorder.Code, responseRecorder.Body.String())

			var config api.StandardResponse[api.ModelConfigResource]
			err := json.Unmarshal(responseRecorder.Body.Bytes(), &config)
			require.NoError(t, err)
			assert.Equal(t, "my-existing-secret", config.Data.Spec.APIKeySecret)
			assert.Equal(t, "OPENAI_API_KEY", config.Data.Spec.APIKeySecretKey)
		})

		t.Run("Success_CompanionSecrets", func(t *testing.T) {
			handler, kubeClient, responseRecorder := setupHandler()

			reqBody := api.CreateModelConfigRequest{
				Ref: "default/test-companion-secrets",
				Secrets: []api.SecretMaterial{
					{Name: "provider-credentials", Key: "credentials.json", Value: `{"token":"secret"}`, Type: "Opaque"},
					{Name: "provider-ca", Key: "ca.crt", Value: "-----BEGIN CERTIFICATE-----\n...\n-----END CERTIFICATE-----\n"},
				},
				Spec: v1alpha2.ModelConfigSpec{
					Model:           "gpt-4",
					Provider:        v1alpha2.ModelProviderOpenAI,
					APIKeySecret:    "provider-credentials",
					APIKeySecretKey: "credentials.json",
				},
			}

			jsonBody, _ := json.Marshal(reqBody)
			req := httptest.NewRequest("POST", "/api/modelconfigs/", bytes.NewBuffer(jsonBody))
			req = setUser(req, "test-user")
			req.Header.Set("Content-Type", "application/json")

			handler.HandleCreateModelConfig(responseRecorder, req)

			assert.Equal(t, http.StatusCreated, responseRecorder.Code, responseRecorder.Body.String())

			credentialsSecret := &corev1.Secret{}
			err := kubeClient.Get(context.Background(), ctrl_client.ObjectKey{Namespace: "default", Name: "provider-credentials"}, credentialsSecret)
			require.NoError(t, err)
			assert.Equal(t, corev1.SecretTypeOpaque, credentialsSecret.Type)
			assert.Equal(t, `{"token":"secret"}`, string(credentialsSecret.Data["credentials.json"]))

			caSecret := &corev1.Secret{}
			err = kubeClient.Get(context.Background(), ctrl_client.ObjectKey{Namespace: "default", Name: "provider-ca"}, caSecret)
			require.NoError(t, err)
			assert.Equal(t, "-----BEGIN CERTIFICATE-----\n...\n-----END CERTIFICATE-----\n", string(caSecret.Data["ca.crt"]))
		})

		t.Run("InvalidCompanionSecretName_Returns400", func(t *testing.T) {
			handler, _, responseRecorder := setupHandler()

			reqBody := api.CreateModelConfigRequest{
				Ref: "default/test-invalid-secret",
				Secrets: []api.SecretMaterial{
					{Name: "Invalid_Name", Key: "sa.json", Value: "{}"},
				},
				Spec: v1alpha2.ModelConfigSpec{Model: "gpt-4", Provider: v1alpha2.ModelProviderOpenAI},
			}

			jsonBody, _ := json.Marshal(reqBody)
			req := httptest.NewRequest("POST", "/api/modelconfigs/", bytes.NewBuffer(jsonBody))
			req = setUser(req, "test-user")
			req.Header.Set("Content-Type", "application/json")

			handler.HandleCreateModelConfig(responseRecorder, req)

			assert.Equal(t, http.StatusBadRequest, responseRecorder.Code)
			assert.NotNil(t, responseRecorder.errorReceived)
		})

		t.Run("APIKeySecret_MissingKey_Returns400", func(t *testing.T) {
			handler, _, responseRecorder := setupHandler()

			reqBody := api.CreateModelConfigRequest{
				Ref: "default/test-missing-key",
				Spec: v1alpha2.ModelConfigSpec{
					Model:        "gpt-4",
					Provider:     v1alpha2.ModelProviderOpenAI,
					APIKeySecret: "my-existing-secret",
					// APIKeySecretKey intentionally omitted
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

		t.Run("Bedrock_APIKeySecret_NoKey_OK", func(t *testing.T) {
			handler, _, responseRecorder := setupHandler()

			reqBody := api.CreateModelConfigRequest{
				Ref: "default/test-bedrock-nokey",
				Spec: v1alpha2.ModelConfigSpec{
					Model:        "anthropic.claude-3-sonnet-20240229-v1:0",
					Provider:     v1alpha2.ModelProviderBedrock,
					APIKeySecret: "bedrock-creds",
					Bedrock:      &v1alpha2.BedrockConfig{Region: "us-west-2"},
				},
			}

			jsonBody, _ := json.Marshal(reqBody)
			req := httptest.NewRequest("POST", "/api/modelconfigs/", bytes.NewBuffer(jsonBody))
			req = setUser(req, "test-user")
			req.Header.Set("Content-Type", "application/json")

			handler.HandleCreateModelConfig(responseRecorder, req)

			assert.Equal(t, http.StatusCreated, responseRecorder.Code, responseRecorder.Body.String())

			var config api.StandardResponse[api.ModelConfigResource]
			err := json.Unmarshal(responseRecorder.Body.Bytes(), &config)
			require.NoError(t, err)
			assert.Equal(t, "bedrock-creds", config.Data.Spec.APIKeySecret)
			assert.Empty(t, config.Data.Spec.APIKeySecretKey)
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
				Ref:  "invalid/ref/with/too/many/slashes",
				Spec: v1alpha2.ModelConfigSpec{Model: "gpt-4", Provider: v1alpha2.ModelProviderOpenAI},
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

			existingConfig := &v1alpha2.ModelConfig{
				ObjectMeta: metav1.ObjectMeta{Name: "test-config", Namespace: "default"},
				Spec:       v1alpha2.ModelConfigSpec{Model: "gpt-4", Provider: v1alpha2.ModelProviderOpenAI},
			}
			err := kubeClient.Create(context.Background(), existingConfig)
			require.NoError(t, err)

			reqBody := api.CreateModelConfigRequest{
				Ref:  "default/test-config",
				Spec: v1alpha2.ModelConfigSpec{Model: "gpt-4", Provider: v1alpha2.ModelProviderOpenAI},
			}

			jsonBody, _ := json.Marshal(reqBody)
			req := httptest.NewRequest("POST", "/api/modelconfigs/", bytes.NewBuffer(jsonBody))
			req = setUser(req, "test-user")
			req.Header.Set("Content-Type", "application/json")

			handler.HandleCreateModelConfig(responseRecorder, req)

			assert.Equal(t, http.StatusConflict, responseRecorder.Code)
			assert.NotNil(t, responseRecorder.errorReceived)
		})
	})

	t.Run("HandleGetModelConfig", func(t *testing.T) {
		t.Run("Success", func(t *testing.T) {
			handler, kubeClient, responseRecorder := setupHandler()

			config := &v1alpha2.ModelConfig{
				ObjectMeta: metav1.ObjectMeta{Name: "test-config", Namespace: "default"},
				Spec: v1alpha2.ModelConfigSpec{
					Model:           "gpt-4",
					Provider:        v1alpha2.ModelProviderOpenAI,
					APIKeySecret:    "test-secret",
					APIKeySecretKey: "OPENAI_API_KEY",
					OpenAI:          &v1alpha2.OpenAIConfig{BaseURL: "https://api.openai.com/v1", Temperature: "0.7"},
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

			var configResponse api.StandardResponse[api.ModelConfigResource]
			err = json.Unmarshal(responseRecorder.Body.Bytes(), &configResponse)
			require.NoError(t, err)
			assert.Equal(t, "default/test-config", configResponse.Data.Ref)
			assert.Equal(t, v1alpha2.ModelProviderOpenAI, configResponse.Data.Spec.Provider)
			assert.Equal(t, "gpt-4", configResponse.Data.Spec.Model)
			assert.Equal(t, "test-secret", configResponse.Data.Spec.APIKeySecret)
			assert.NotNil(t, configResponse.Data.Spec.OpenAI)
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

			config := &v1alpha2.ModelConfig{
				ObjectMeta: metav1.ObjectMeta{Name: "test-config", Namespace: "default"},
				Spec: v1alpha2.ModelConfigSpec{
					Model:    "gpt-3.5-turbo",
					Provider: v1alpha2.ModelProviderOpenAI,
					OpenAI:   &v1alpha2.OpenAIConfig{BaseURL: "https://api.openai.com/v1", Temperature: "0.5"},
				},
			}

			err := kubeClient.Create(context.Background(), config)
			require.NoError(t, err)

			apiKey := "new-api-key"
			reqBody := api.UpdateModelConfigRequest{
				APIKey: &apiKey,
				Spec: v1alpha2.ModelConfigSpec{
					Model:    "gpt-4",
					Provider: v1alpha2.ModelProviderOpenAI,
					OpenAI:   &v1alpha2.OpenAIConfig{BaseURL: "https://api.openai.com/v1", Temperature: "0.7", MaxTokens: 2000},
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

			var updatedConfig api.StandardResponse[api.ModelConfigResource]
			err = json.Unmarshal(responseRecorder.Body.Bytes(), &updatedConfig)
			require.NoError(t, err)
			assert.Equal(t, "gpt-4", updatedConfig.Data.Spec.Model)
			require.NotNil(t, updatedConfig.Data.Spec.OpenAI)
			assert.Equal(t, "0.7", updatedConfig.Data.Spec.OpenAI.Temperature)
		})

		t.Run("Success_CompanionSecrets", func(t *testing.T) {
			handler, kubeClient, responseRecorder := setupHandler()

			config := &v1alpha2.ModelConfig{
				ObjectMeta: metav1.ObjectMeta{Name: "test-config", Namespace: "default"},
				Spec:       v1alpha2.ModelConfigSpec{Model: "gpt-3.5-turbo", Provider: v1alpha2.ModelProviderOpenAI},
			}
			err := kubeClient.Create(context.Background(), config)
			require.NoError(t, err)

			existingSecret := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{Name: "provider-credentials", Namespace: "default"},
				Type:       corev1.SecretTypeOpaque,
				Data: map[string][]byte{
					"keep": []byte("preserved"),
				},
			}
			err = kubeClient.Create(context.Background(), existingSecret)
			require.NoError(t, err)

			reqBody := api.UpdateModelConfigRequest{
				Secrets: []api.SecretMaterial{
					{Name: "provider-credentials", Key: "credentials.json", Value: `{"token":"updated"}`},
				},
				Spec: v1alpha2.ModelConfigSpec{
					Model:           "gpt-4",
					Provider:        v1alpha2.ModelProviderOpenAI,
					APIKeySecret:    "provider-credentials",
					APIKeySecretKey: "credentials.json",
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

			updatedSecret := &corev1.Secret{}
			err = kubeClient.Get(context.Background(), ctrl_client.ObjectKey{Namespace: "default", Name: "provider-credentials"}, updatedSecret)
			require.NoError(t, err)
			assert.Equal(t, corev1.SecretTypeOpaque, updatedSecret.Type)
			assert.Equal(t, `{"token":"updated"}`, string(updatedSecret.Data["credentials.json"]))
			assert.Equal(t, "preserved", string(updatedSecret.Data["keep"]))
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
				Spec: v1alpha2.ModelConfigSpec{
					Model:    "gpt-4",
					Provider: v1alpha2.ModelProviderOpenAI,
					OpenAI:   &v1alpha2.OpenAIConfig{Temperature: "0.7"},
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

			config := &v1alpha2.ModelConfig{
				ObjectMeta: metav1.ObjectMeta{Name: "test-config", Namespace: "default"},
				Spec:       v1alpha2.ModelConfigSpec{Model: "gpt-4", Provider: v1alpha2.ModelProviderOpenAI},
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
