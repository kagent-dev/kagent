package handlers_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gorilla/mux"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/kagent-dev/kagent/go/controller/api/v1alpha1"
	autogen_api "github.com/kagent-dev/kagent/go/internal/autogen/api"
	autogen_fake "github.com/kagent-dev/kagent/go/internal/autogen/client/fake"
	"github.com/kagent-dev/kagent/go/internal/database"
	database_fake "github.com/kagent-dev/kagent/go/internal/database/fake"
	"github.com/kagent-dev/kagent/go/internal/httpserver/handlers"
	common "github.com/kagent-dev/kagent/go/internal/utils"
	"github.com/kagent-dev/kagent/go/pkg/client/api"
)

// Test fixtures and helper functions
func createTestModelConfig() *v1alpha1.ModelConfig {
	return &v1alpha1.ModelConfig{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-model-config",
			Namespace: "default",
		},
		Spec: v1alpha1.ModelConfigSpec{
			Provider: v1alpha1.OpenAI,
			Model:    "gpt-4",
		},
	}
}

func createTestAgent(name string, modelConfig *v1alpha1.ModelConfig) *v1alpha1.Agent {
	return &v1alpha1.Agent{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: "default",
		},
		Spec: v1alpha1.AgentSpec{
			ModelConfig: common.GetObjectRef(modelConfig),
		},
	}
}

func setupTestHandler(objects ...client.Object) (*handlers.AgentsHandler, string) {
	kubeClient := fake.NewClientBuilder().
		WithScheme(setupScheme()).
		WithObjects(objects...).
		Build()

	userID := common.GetGlobalUserID()
	autogenClient := autogen_fake.NewInMemoryAutogenClient()
	dbClient := database_fake.NewClient()

	base := &handlers.Base{
		KubeClient:    kubeClient,
		AutogenClient: autogenClient,
		DefaultModelConfig: types.NamespacedName{
			Name:      "test-model-config",
			Namespace: "default",
		},
		DatabaseService: dbClient,
	}

	return handlers.NewAgentsHandler(base), userID
}

func createAutogenTeam(client database.Client, agent *v1alpha1.Agent) {
	autogenTeam := &database.Agent{
		Component: autogen_api.Component{
			Label: common.GetObjectRef(agent),
		},
		Name: common.GetObjectRef(agent),
	}
	client.CreateAgent(autogenTeam)
}

func TestHandleGetAgent(t *testing.T) {
	t.Run("gets team successfully", func(t *testing.T) {
		modelConfig := createTestModelConfig()
		team := createTestAgent("test-team", modelConfig)

		handler, _ := setupTestHandler(team, modelConfig)
		createAutogenTeam(handler.Base.DatabaseService, team)

		req := httptest.NewRequest("GET", "/api/agents/default/test-team", nil)
		req = mux.SetURLVars(req, map[string]string{"namespace": "default", "name": "test-team"})
		w := httptest.NewRecorder()

		handler.HandleGetAgent(&testErrorResponseWriter{w}, req)

		require.Equal(t, http.StatusOK, w.Code)

		var response api.StandardResponse[api.AgentResponse]
		err := json.Unmarshal(w.Body.Bytes(), &response)
		require.NoError(t, err)
		require.Equal(t, "test-team", response.Data.Agent.Name)
		require.Equal(t, "default/test-model-config", response.Data.ModelConfigRef)
		require.Equal(t, "gpt-4", response.Data.Model)
		require.Equal(t, v1alpha1.OpenAI, response.Data.ModelProvider)
	})

	t.Run("returns 404 for missing agent", func(t *testing.T) {
		handler, _ := setupTestHandler()

		req := httptest.NewRequest("GET", "/api/agents/default/test-team", nil)
		req = mux.SetURLVars(req, map[string]string{"namespace": "default", "name": "test-team"})
		w := httptest.NewRecorder()

		handler.HandleGetAgent(&testErrorResponseWriter{w}, req)

		require.Equal(t, http.StatusNotFound, w.Code, w.Body.String())
	})
}

func TestHandleListTeams(t *testing.T) {
	t.Run("lists teams successfully", func(t *testing.T) {
		modelConfig := createTestModelConfig()
		team := createTestAgent("test-team", modelConfig)

		handler, _ := setupTestHandler(team, modelConfig)
		createAutogenTeam(handler.Base.DatabaseService, team)

		req := httptest.NewRequest("GET", "/api/agents", nil)
		w := httptest.NewRecorder()

		handler.HandleListAgents(&testErrorResponseWriter{w}, req)

		require.Equal(t, http.StatusOK, w.Code)

		var response api.StandardResponse[[]api.AgentResponse]
		err := json.Unmarshal(w.Body.Bytes(), &response)
		require.NoError(t, err)
		require.Len(t, response.Data, 1)
		require.Equal(t, "test-team", response.Data[0].Agent.Name)
		require.Equal(t, "default/test-model-config", response.Data[0].ModelConfigRef)
		require.Equal(t, "gpt-4", response.Data[0].Model)
		require.Equal(t, v1alpha1.OpenAI, response.Data[0].ModelProvider)
	})
}

func TestHandleUpdateAgent(t *testing.T) {
	t.Run("updates agent successfully", func(t *testing.T) {
		existingAgent := &v1alpha1.Agent{
			ObjectMeta: metav1.ObjectMeta{Name: "test-team", Namespace: "default"},
			Spec:       v1alpha1.AgentSpec{ModelConfig: "default/old-model-config"},
		}

		handler, _ := setupTestHandler(existingAgent)

		updatedAgent := &v1alpha1.Agent{
			ObjectMeta: metav1.ObjectMeta{Name: "test-team", Namespace: "default"},
			Spec:       v1alpha1.AgentSpec{ModelConfig: "kagent/new-model-config"},
		}

		body, _ := json.Marshal(updatedAgent)
		req := httptest.NewRequest("PUT", "/api/agents/default/test-team", bytes.NewBuffer(body))
		req = mux.SetURLVars(req, map[string]string{"namespace": "default", "name": "test-team"})
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		handler.HandleUpdateAgent(&testErrorResponseWriter{w}, req)

		require.Equal(t, http.StatusOK, w.Code)

		var response api.StandardResponse[v1alpha1.Agent]
		err := json.Unmarshal(w.Body.Bytes(), &response)
		require.NoError(t, err)
		require.Equal(t, "kagent/new-model-config", response.Data.Spec.ModelConfig)
	})

	t.Run("returns 404 for non-existent team", func(t *testing.T) {
		handler, _ := setupTestHandler()

		agent := &v1alpha1.Agent{
			ObjectMeta: metav1.ObjectMeta{Name: "non-existent", Namespace: "default"},
		}

		body, _ := json.Marshal(agent)
		req := httptest.NewRequest("PUT", "/api/agents/default/non-existent", bytes.NewBuffer(body))
		req = mux.SetURLVars(req, map[string]string{"namespace": "default", "name": "non-existent"})
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		handler.HandleUpdateAgent(&testErrorResponseWriter{w}, req)

		require.Equal(t, http.StatusNotFound, w.Code)
	})
}

func TestHandleCreateAgent(t *testing.T) {
	t.Run("creates agent successfully", func(t *testing.T) {
		modelConfig := &v1alpha1.ModelConfig{
			ObjectMeta: metav1.ObjectMeta{Name: "test-model-config", Namespace: "default"},
			Spec: v1alpha1.ModelConfigSpec{
				Model:    "test",
				Provider: "Ollama",
				Ollama:   &v1alpha1.OllamaConfig{Host: "http://test-host"},
				ModelInfo: &v1alpha1.ModelInfo{
					JSONOutput:       true,
					StructuredOutput: true,
				},
			},
		}

		handler, _ := setupTestHandler(modelConfig)

		agent := &v1alpha1.Agent{
			ObjectMeta: metav1.ObjectMeta{Name: "test-team", Namespace: "default"},
			Spec: v1alpha1.AgentSpec{
				ModelConfig:   common.GetObjectRef(modelConfig),
				SystemMessage: "You are an imagenary agent",
				Description:   "Test team description",
			},
		}

		body, _ := json.Marshal(agent)
		req := httptest.NewRequest("POST", "/api/agents", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		handler.HandleCreateAgent(&testErrorResponseWriter{w}, req)

		require.Equal(t, http.StatusCreated, w.Code)

		var response api.StandardResponse[v1alpha1.Agent]
		err := json.Unmarshal(w.Body.Bytes(), &response)
		require.NoError(t, err)
		require.Equal(t, "test-team", response.Data.Name)
		require.Equal(t, "default", response.Data.Namespace)
		require.Equal(t, "You are an imagenary agent", response.Data.Spec.SystemMessage)
		require.Equal(t, "default/test-model-config", response.Data.Spec.ModelConfig)
	})
}

func TestHandleDeleteTeam(t *testing.T) {
	t.Run("deletes team successfully", func(t *testing.T) {
		team := &v1alpha1.Agent{
			ObjectMeta: metav1.ObjectMeta{Name: "test-team", Namespace: "default"},
		}

		handler, _ := setupTestHandler(team)
		createAutogenTeam(handler.Base.DatabaseService, team)

		req := httptest.NewRequest("DELETE", "/api/agents/default/test-team", nil)
		req = mux.SetURLVars(req, map[string]string{"namespace": "default", "name": "test-team"})
		w := httptest.NewRecorder()

		handler.HandleDeleteAgent(&testErrorResponseWriter{w}, req)

		require.Equal(t, http.StatusOK, w.Code)
	})

	t.Run("returns 404 for non-existent team", func(t *testing.T) {
		handler, _ := setupTestHandler()

		req := httptest.NewRequest("DELETE", "/api/teams/default/non-existent", nil)
		req = mux.SetURLVars(req, map[string]string{
			"namespace": "default",
			"name":      "non-existent",
		})
		w := httptest.NewRecorder()

		handler.HandleDeleteAgent(&testErrorResponseWriter{w}, req)

		require.Equal(t, http.StatusNotFound, w.Code)
	})
}
