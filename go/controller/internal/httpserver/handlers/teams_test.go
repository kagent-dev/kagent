package handlers

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gorilla/mux"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/kagent-dev/kagent/go/autogen/api"
	autogen_client "github.com/kagent-dev/kagent/go/autogen/client"
	autogen_fake "github.com/kagent-dev/kagent/go/autogen/client/fake"
	"github.com/kagent-dev/kagent/go/controller/api/v1alpha1"
	common "github.com/kagent-dev/kagent/go/controller/internal/utils"
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

func setupTestHandler(objects ...client.Object) (*TeamsHandler, string) {
	kubeClient := fake.NewClientBuilder().
		WithScheme(setupScheme()).
		WithObjects(objects...).
		Build()

	userID := common.GetGlobalUserID()
	autogenClient := autogen_fake.NewInMemoryAutogenClient()

	base := &Base{
		KubeClient:    kubeClient,
		AutogenClient: autogenClient,
		DefaultModelConfig: types.NamespacedName{
			Name:      "test-model-config",
			Namespace: "default",
		},
	}

	return NewTeamsHandler(base), userID
}

func createAutogenTeam(client *autogen_fake.InMemoryAutogenClient, userID string, agent *v1alpha1.Agent) {
	autogenTeam := &autogen_client.Team{
		BaseObject: autogen_client.BaseObject{
			Id:     1,
			UserID: userID,
		},
		Component: &api.Component{
			Label: common.GetObjectRef(agent),
		},
	}
	client.CreateTeam(autogenTeam)
}

func TestHandleGetTeam(t *testing.T) {
	t.Run("gets team successfully", func(t *testing.T) {
		modelConfig := createTestModelConfig()
		team := createTestAgent("test-team", modelConfig)

		handler, userID := setupTestHandler(team, modelConfig)
		createAutogenTeam(handler.Base.AutogenClient.(*autogen_fake.InMemoryAutogenClient), userID, team)

		req := httptest.NewRequest("GET", fmt.Sprintf("/api/teams/1?user_id=%s", userID), nil)
		req = mux.SetURLVars(req, map[string]string{"teamID": "1"})
		w := httptest.NewRecorder()

		handler.HandleGetTeam(&testErrorResponseWriter{w}, req)

		assert.Equal(t, http.StatusOK, w.Code)

		var response TeamResponse
		err := json.Unmarshal(w.Body.Bytes(), &response)
		require.NoError(t, err)
		assert.Equal(t, 1, response.Id)
		assert.Equal(t, "test-team", response.Agent.Name)
	})

	t.Run("returns 400 for missing user ID", func(t *testing.T) {
		handler, _ := setupTestHandler()

		req := httptest.NewRequest("GET", "/api/teams/1", nil)
		req = mux.SetURLVars(req, map[string]string{"teamID": "1"})
		w := httptest.NewRecorder()

		handler.HandleGetTeam(&testErrorResponseWriter{w}, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
	})
}

func TestHandleUpdateTeam(t *testing.T) {
	t.Run("updates team successfully", func(t *testing.T) {
		existingTeam := &v1alpha1.Agent{
			ObjectMeta: metav1.ObjectMeta{Name: "test-team", Namespace: "default"},
			Spec:       v1alpha1.AgentSpec{ModelConfig: "default/old-model-config"},
		}

		handler, _ := setupTestHandler(existingTeam)

		updatedTeam := &v1alpha1.Agent{
			ObjectMeta: metav1.ObjectMeta{Name: "test-team", Namespace: "default"},
			Spec:       v1alpha1.AgentSpec{ModelConfig: "kagent/new-model-config"},
		}

		body, _ := json.Marshal(updatedTeam)
		req := httptest.NewRequest("PUT", "/api/teams", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		handler.HandleUpdateTeam(&testErrorResponseWriter{w}, req)

		assert.Equal(t, http.StatusOK, w.Code)

		var response v1alpha1.Agent
		err := json.Unmarshal(w.Body.Bytes(), &response)
		require.NoError(t, err)
		assert.Equal(t, "kagent/new-model-config", response.Spec.ModelConfig)
	})

	t.Run("returns 404 for non-existent team", func(t *testing.T) {
		handler, _ := setupTestHandler()

		team := &v1alpha1.Agent{
			ObjectMeta: metav1.ObjectMeta{Name: "non-existent", Namespace: "default"},
		}

		body, _ := json.Marshal(team)
		req := httptest.NewRequest("PUT", "/api/teams", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		handler.HandleUpdateTeam(&testErrorResponseWriter{w}, req)

		assert.Equal(t, http.StatusNotFound, w.Code)
	})
}

func TestHandleListTeams(t *testing.T) {
	t.Run("lists teams successfully", func(t *testing.T) {
		modelConfig := createTestModelConfig()
		team := createTestAgent("test-team", modelConfig)

		handler, userID := setupTestHandler(team, modelConfig)
		createAutogenTeam(handler.Base.AutogenClient.(*autogen_fake.InMemoryAutogenClient), userID, team)

		req := httptest.NewRequest("GET", fmt.Sprintf("/api/teams?user_id=%s", userID), nil)
		w := httptest.NewRecorder()

		handler.HandleListTeams(&testErrorResponseWriter{w}, req)

		assert.Equal(t, http.StatusOK, w.Code)

		var response []TeamResponse
		err := json.Unmarshal(w.Body.Bytes(), &response)
		require.NoError(t, err)
		assert.Len(t, response, 1)
		assert.Equal(t, "test-team", response[0].Agent.Name)
	})

	t.Run("returns 400 for missing user ID", func(t *testing.T) {
		handler, _ := setupTestHandler()

		req := httptest.NewRequest("GET", "/api/teams", nil)
		w := httptest.NewRecorder()

		handler.HandleListTeams(&testErrorResponseWriter{w}, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
	})
}

func TestHandleCreateTeam(t *testing.T) {
	t.Run("creates team successfully", func(t *testing.T) {
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

		team := &v1alpha1.Agent{
			ObjectMeta: metav1.ObjectMeta{Name: "test-team", Namespace: "default"},
			Spec: v1alpha1.AgentSpec{
				ModelConfig:   common.GetObjectRef(modelConfig),
				SystemMessage: "You are an imagenary agent",
				Description:   "Test team description",
			},
		}

		body, _ := json.Marshal(team)
		req := httptest.NewRequest("POST", "/api/teams", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		handler.HandleCreateTeam(&testErrorResponseWriter{w}, req)

		assert.Equal(t, http.StatusCreated, w.Code)

		var response v1alpha1.Agent
		err := json.Unmarshal(w.Body.Bytes(), &response)
		require.NoError(t, err)
		assert.Equal(t, "test-team", response.Name)
	})
}

func TestHandleDeleteTeam(t *testing.T) {
	t.Run("deletes team successfully", func(t *testing.T) {
		team := &v1alpha1.Agent{
			ObjectMeta: metav1.ObjectMeta{Name: "test-team", Namespace: "default"},
		}

		handler, _ := setupTestHandler(team)

		req := httptest.NewRequest("DELETE", "/api/teams/default/test-team", nil)
		req = mux.SetURLVars(req, map[string]string{
			"namespace": "default",
			"teamName":  "test-team",
		})
		w := httptest.NewRecorder()

		handler.HandleDeleteTeam(&testErrorResponseWriter{w}, req)

		assert.Equal(t, http.StatusNoContent, w.Code)
	})

	t.Run("returns 404 for non-existent team", func(t *testing.T) {
		handler, _ := setupTestHandler()

		req := httptest.NewRequest("DELETE", "/api/teams/default/non-existent", nil)
		req = mux.SetURLVars(req, map[string]string{
			"namespace": "default",
			"teamName":  "non-existent",
		})
		w := httptest.NewRecorder()

		handler.HandleDeleteTeam(&testErrorResponseWriter{w}, req)

		assert.Equal(t, http.StatusNotFound, w.Code)
	})
}
