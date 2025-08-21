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

	"github.com/kagent-dev/kagent/go/api/v1alpha2"
	"github.com/kagent-dev/kagent/go/internal/adk"
	"github.com/kagent-dev/kagent/go/internal/database"
	database_fake "github.com/kagent-dev/kagent/go/internal/database/fake"
	"github.com/kagent-dev/kagent/go/internal/httpserver/auth"
	"github.com/kagent-dev/kagent/go/internal/httpserver/handlers"
	common "github.com/kagent-dev/kagent/go/internal/utils"
	"github.com/kagent-dev/kagent/go/pkg/client/api"
)

// Test fixtures and helper functions
func createTestModelConfig() *v1alpha2.ModelConfig {
	return &v1alpha2.ModelConfig{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-model-config",
			Namespace: "default",
		},
		Spec: v1alpha2.ModelConfigSpec{
			Provider: v1alpha2.ModelProviderOpenAI,
			Model:    "gpt-4",
		},
	}
}

func createTestAgent(name string, modelConfig *v1alpha2.ModelConfig) *v1alpha2.Agent {
	return &v1alpha2.Agent{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: "default",
		},
		Spec: v1alpha2.AgentSpec{
			Type: v1alpha2.AgentType_Declarative,
			Declarative: &v1alpha2.DeclarativeAgentSpec{
				ModelConfig: modelConfig.Name,
			},
		},
	}
}

func createTestAgentWithStatus(name string, modelConfig *v1alpha2.ModelConfig, conditions []metav1.Condition) *v1alpha2.Agent {
	agent := createTestAgent(name, modelConfig)
	agent.Status = v1alpha2.AgentStatus{
		Conditions: conditions,
	}
	return agent
}

func setupTestHandler(objects ...client.Object) (*handlers.AgentsHandler, string) {
	kubeClient := fake.NewClientBuilder().
		WithScheme(setupScheme()).
		WithObjects(objects...).
		Build()

	userID := "test-user"
	dbClient := database_fake.NewClient()

	base := &handlers.Base{
		KubeClient: kubeClient,
		DefaultModelConfig: types.NamespacedName{
			Name:      "test-model-config",
			Namespace: "default",
		},
		DatabaseService: dbClient,
		Authorizer:      &auth.NoopAuthorizer{},
	}

	return handlers.NewAgentsHandler(base), userID
}

func createAgent(client database.Client, agent *v1alpha2.Agent) {
	dbAgent := &database.Agent{
		Config: &adk.AgentConfig{},
		ID:     common.GetObjectRef(agent),
	}
	client.StoreAgent(dbAgent) //nolint:errcheck
}

func TestHandleGetAgent(t *testing.T) {
	t.Run("gets team successfully", func(t *testing.T) {
		modelConfig := createTestModelConfig()
		team := createTestAgent("test-team", modelConfig)

		handler, _ := setupTestHandler(team, modelConfig)
		createAgent(handler.DatabaseService, team)

		req := httptest.NewRequest("GET", "/api/agents/default/test-team", nil)
		req = mux.SetURLVars(req, map[string]string{"namespace": "default", "name": "test-team"})
		req = setUser(req, "test-user")
		w := httptest.NewRecorder()

		handler.HandleGetAgent(&testErrorResponseWriter{w}, req)

		require.Equal(t, http.StatusOK, w.Code)

		var response api.StandardResponse[api.AgentResponse]
		err := json.Unmarshal(w.Body.Bytes(), &response)
		require.NoError(t, err)
		require.Equal(t, "test-team", response.Data.Agent.Name)
		require.Equal(t, "default/test-model-config", response.Data.ModelConfigRef, w.Body.String())
		require.Equal(t, "gpt-4", response.Data.Model)
		require.Equal(t, v1alpha2.ModelProviderOpenAI, response.Data.ModelProvider)
		require.False(t, response.Data.DeploymentReady) // No status conditions, should be false
	})

	t.Run("gets agent with DeploymentReady=true", func(t *testing.T) {
		modelConfig := createTestModelConfig()
		conditions := []metav1.Condition{
			{
				Type:   "Accepted",
				Status: "True",
				Reason: "AgentReconciled",
			},
			{
				Type:   "Ready",
				Status: "True",
				Reason: "DeploymentReady",
			},
		}
		agent := createTestAgentWithStatus("test-agent-ready", modelConfig, conditions)

		handler, _ := setupTestHandler(agent, modelConfig)
		createAgent(handler.DatabaseService, agent)

		req := httptest.NewRequest("GET", "/api/agents/default/test-agent-ready", nil)
		req = mux.SetURLVars(req, map[string]string{"namespace": "default", "name": "test-agent-ready"})
		req = setUser(req, "test-user")
		w := httptest.NewRecorder()

		handler.HandleGetAgent(&testErrorResponseWriter{w}, req)

		require.Equal(t, http.StatusOK, w.Code)

		var response api.StandardResponse[api.AgentResponse]
		err := json.Unmarshal(w.Body.Bytes(), &response)
		require.NoError(t, err)
		require.True(t, response.Data.DeploymentReady)
	})

	t.Run("gets agent with DeploymentReady=false when Ready status is False", func(t *testing.T) {
		modelConfig := createTestModelConfig()
		conditions := []metav1.Condition{
			{
				Type:   "Ready",
				Status: "False", // Status is False
				Reason: "DeploymentReady",
			},
		}
		agent := createTestAgentWithStatus("test-agent-not-ready", modelConfig, conditions)

		handler, _ := setupTestHandler(agent, modelConfig)
		createAgent(handler.DatabaseService, agent)

		req := httptest.NewRequest("GET", "/api/agents/default/test-agent-not-ready", nil)
		req = mux.SetURLVars(req, map[string]string{"namespace": "default", "name": "test-agent-not-ready"})
		req = setUser(req, "test-user")
		w := httptest.NewRecorder()

		handler.HandleGetAgent(&testErrorResponseWriter{w}, req)

		require.Equal(t, http.StatusOK, w.Code)

		var response api.StandardResponse[api.AgentResponse]
		err := json.Unmarshal(w.Body.Bytes(), &response)
		require.NoError(t, err)
		require.False(t, response.Data.DeploymentReady)
	})

	t.Run("gets agent with DeploymentReady=false when reason is not DeploymentReady", func(t *testing.T) {
		modelConfig := createTestModelConfig()
		conditions := []metav1.Condition{
			{
				Type:   "Ready",
				Status: "True",
				Reason: "DifferentReason", // Different reason
			},
		}
		agent := createTestAgentWithStatus("test-agent-different-reason", modelConfig, conditions)

		handler, _ := setupTestHandler(agent, modelConfig)
		createAgent(handler.DatabaseService, agent)

		req := httptest.NewRequest("GET", "/api/agents/default/test-agent-different-reason", nil)
		req = mux.SetURLVars(req, map[string]string{"namespace": "default", "name": "test-agent-different-reason"})
		req = setUser(req, "test-user")
		w := httptest.NewRecorder()

		handler.HandleGetAgent(&testErrorResponseWriter{w}, req)

		require.Equal(t, http.StatusOK, w.Code)

		var response api.StandardResponse[api.AgentResponse]
		err := json.Unmarshal(w.Body.Bytes(), &response)
		require.NoError(t, err)
		require.False(t, response.Data.DeploymentReady)
	})

	t.Run("returns 404 for missing agent", func(t *testing.T) {
		handler, _ := setupTestHandler()

		req := httptest.NewRequest("GET", "/api/agents/default/test-team", nil)
		req = mux.SetURLVars(req, map[string]string{"namespace": "default", "name": "test-team"})
		req = setUser(req, "test-user")
		w := httptest.NewRecorder()

		handler.HandleGetAgent(&testErrorResponseWriter{w}, req)

		require.Equal(t, http.StatusNotFound, w.Code, w.Body.String())
	})
}

func TestHandleListAgents(t *testing.T) {
	t.Run("lists agents successfully", func(t *testing.T) {
		modelConfig := createTestModelConfig()

		// Agent with DeploymentReady=true
		readyConditions := []metav1.Condition{
			{
				Type:   "Ready",
				Status: "True",
				Reason: "DeploymentReady",
			},
		}
		readyAgent := createTestAgentWithStatus("ready-agent", modelConfig, readyConditions)

		// Agent with DeploymentReady=false
		notReadyAgent := createTestAgent("not-ready-agent", modelConfig)

		handler, _ := setupTestHandler(readyAgent, notReadyAgent, modelConfig)
		createAgent(handler.DatabaseService, readyAgent)
		createAgent(handler.DatabaseService, notReadyAgent)

		req := httptest.NewRequest("GET", "/api/agents", nil)
		req = setUser(req, "test-user")

		w := httptest.NewRecorder()

		handler.HandleListAgents(&testErrorResponseWriter{w}, req)

		require.Equal(t, http.StatusOK, w.Code)

		var response api.StandardResponse[[]api.AgentResponse]
		err := json.Unmarshal(w.Body.Bytes(), &response)
		require.NoError(t, err)
		require.Len(t, response.Data, 2)
		require.Equal(t, "not-ready-agent", response.Data[0].Agent.Name)
		require.Equal(t, "default/test-model-config", response.Data[0].ModelConfigRef)
		require.Equal(t, "gpt-4", response.Data[0].Model)
		require.Equal(t, v1alpha2.ModelProviderOpenAI, response.Data[0].ModelProvider)
		require.Equal(t, false, response.Data[0].DeploymentReady)
		require.Equal(t, "ready-agent", response.Data[1].Agent.Name)
		require.Equal(t, "default/test-model-config", response.Data[1].ModelConfigRef)
		require.Equal(t, "gpt-4", response.Data[1].Model)
		require.Equal(t, v1alpha2.ModelProviderOpenAI, response.Data[1].ModelProvider)
		require.Equal(t, true, response.Data[1].DeploymentReady)
	})
}

func TestHandleUpdateAgent(t *testing.T) {
	t.Run("updates agent successfully", func(t *testing.T) {
		existingAgent := &v1alpha2.Agent{
			ObjectMeta: metav1.ObjectMeta{Name: "test-team", Namespace: "default"},
			Spec: v1alpha2.AgentSpec{
				Type: v1alpha2.AgentType_Declarative,
				Declarative: &v1alpha2.DeclarativeAgentSpec{
					ModelConfig: "old-model-config",
				},
			},
		}

		handler, _ := setupTestHandler(existingAgent)

		updatedAgent := &v1alpha2.Agent{
			ObjectMeta: metav1.ObjectMeta{Name: "test-team", Namespace: "default"},
			Spec: v1alpha2.AgentSpec{
				Type: v1alpha2.AgentType_Declarative,
				Declarative: &v1alpha2.DeclarativeAgentSpec{
					ModelConfig: "new-model-config",
				},
			},
		}

		body, _ := json.Marshal(updatedAgent)
		req := httptest.NewRequest("PUT", "/api/agents/default/test-team", bytes.NewBuffer(body))
		req = mux.SetURLVars(req, map[string]string{"namespace": "default", "name": "test-team"})
		req.Header.Set("Content-Type", "application/json")
		req = setUser(req, "test-user")
		w := httptest.NewRecorder()

		handler.HandleUpdateAgent(&testErrorResponseWriter{w}, req)

		require.Equal(t, http.StatusOK, w.Code)

		var response api.StandardResponse[v1alpha2.Agent]
		err := json.Unmarshal(w.Body.Bytes(), &response)
		require.NoError(t, err)
		require.Equal(t, "new-model-config", response.Data.Spec.Declarative.ModelConfig)
	})

	t.Run("returns 404 for non-existent team", func(t *testing.T) {
		handler, _ := setupTestHandler()

		agent := &v1alpha2.Agent{
			ObjectMeta: metav1.ObjectMeta{Name: "non-existent", Namespace: "default"},
		}

		body, _ := json.Marshal(agent)
		req := httptest.NewRequest("PUT", "/api/agents/default/non-existent", bytes.NewBuffer(body))
		req = mux.SetURLVars(req, map[string]string{"namespace": "default", "name": "non-existent"})
		req.Header.Set("Content-Type", "application/json")
		req = setUser(req, "test-user")
		w := httptest.NewRecorder()

		handler.HandleUpdateAgent(&testErrorResponseWriter{w}, req)

		require.Equal(t, http.StatusNotFound, w.Code)
	})
}

func TestHandleCreateAgent(t *testing.T) {
	t.Run("creates agent successfully", func(t *testing.T) {
		modelConfig := &v1alpha2.ModelConfig{
			ObjectMeta: metav1.ObjectMeta{Name: "test-model-config", Namespace: "default"},
			Spec: v1alpha2.ModelConfigSpec{
				Model:    "test",
				Provider: "Ollama",
				Ollama:   &v1alpha2.OllamaConfig{Host: "http://test-host"},
			},
		}

		handler, _ := setupTestHandler(modelConfig)

		agent := &v1alpha2.Agent{
			ObjectMeta: metav1.ObjectMeta{Name: "test-team", Namespace: "default"},
			Spec: v1alpha2.AgentSpec{
				Type:        v1alpha2.AgentType_Declarative,
				Description: "Test team description",
				Declarative: &v1alpha2.DeclarativeAgentSpec{
					ModelConfig:   modelConfig.Name,
					SystemMessage: "You are an imagenary agent",
				},
			},
		}

		body, _ := json.Marshal(agent)
		req := httptest.NewRequest("POST", "/api/agents", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")
		req = setUser(req, "test-user")
		w := httptest.NewRecorder()

		handler.HandleCreateAgent(&testErrorResponseWriter{w}, req)

		require.Equal(t, http.StatusCreated, w.Code)

		var response api.StandardResponse[v1alpha2.Agent]
		err := json.Unmarshal(w.Body.Bytes(), &response)
		require.NoError(t, err)
		require.Equal(t, "test-team", response.Data.Name)
		require.Equal(t, "default", response.Data.Namespace)
		require.Equal(t, "You are an imagenary agent", response.Data.Spec.Declarative.SystemMessage)
		require.Equal(t, "test-model-config", response.Data.Spec.Declarative.ModelConfig)
	})
}

func TestHandleDeleteTeam(t *testing.T) {
	t.Run("deletes team successfully", func(t *testing.T) {
		team := &v1alpha2.Agent{
			ObjectMeta: metav1.ObjectMeta{Name: "test-team", Namespace: "default"},
		}

		handler, _ := setupTestHandler(team)
		createAgent(handler.DatabaseService, team)

		req := httptest.NewRequest("DELETE", "/api/agents/default/test-team", nil)
		req = mux.SetURLVars(req, map[string]string{"namespace": "default", "name": "test-team"})
		req = setUser(req, "test-user")
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
		req = setUser(req, "test-user")
		w := httptest.NewRecorder()

		handler.HandleDeleteAgent(&testErrorResponseWriter{w}, req)

		require.Equal(t, http.StatusNotFound, w.Code)
	})
}
