package handlers_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gorilla/mux"
	"github.com/stretchr/testify/require"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/kagent-dev/kagent/go/api/adk"
	"github.com/kagent-dev/kagent/go/api/database"
	api "github.com/kagent-dev/kagent/go/api/httpapi"
	"github.com/kagent-dev/kagent/go/api/v1alpha2"
	agenttranslator "github.com/kagent-dev/kagent/go/core/internal/controller/translator/agent"
	"github.com/kagent-dev/kagent/go/core/internal/httpserver/auth"
	"github.com/kagent-dev/kagent/go/core/internal/httpserver/handlers"
	common "github.com/kagent-dev/kagent/go/core/internal/utils"
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

func createTestSandboxAgentCRD(name string, modelConfig *v1alpha2.ModelConfig, conditions []metav1.Condition) *v1alpha2.SandboxAgent {
	return &v1alpha2.SandboxAgent{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: "default",
		},
		Spec: v1alpha2.SandboxAgentSpec{
			AgentSpec: v1alpha2.AgentSpec{
				Type: v1alpha2.AgentType_Declarative,
				Declarative: &v1alpha2.DeclarativeAgentSpec{
					ModelConfig: modelConfig.Name,
				},
			},
		},
		Status: v1alpha2.AgentStatus{
			Conditions: conditions,
		},
	}
}

func setupTestHandler(t *testing.T, objects ...client.Object) (*handlers.AgentsHandler, string) {
	t.Helper()
	withRuntimeImageDigests(t)

	kubeClient := fake.NewClientBuilder().
		WithScheme(setupScheme()).
		WithObjects(objects...).
		Build()

	userID := "test-user"
	dbClient := setupTestDBClient(t)

	base := &handlers.Base{
		KubeClient: kubeClient,
		DefaultModelConfig: types.NamespacedName{
			Name:      "test-model-config",
			Namespace: "default",
		},
		DatabaseService: dbClient,
		Authorizer:      &auth.NoopAuthorizer{},
		ProxyURL:        "",
	}

	return handlers.NewAgentsHandler(base), userID
}

func withRuntimeImageDigests(t *testing.T) {
	t.Helper()
	originalPython := agenttranslator.PythonADKImageDigest
	originalGo := agenttranslator.GoADKImageDigest
	originalGoFull := agenttranslator.GoADKFullImageDigest
	agenttranslator.PythonADKImageDigest = "sha256:test-python-adk"
	agenttranslator.GoADKImageDigest = "sha256:test-go-adk"
	agenttranslator.GoADKFullImageDigest = "sha256:test-go-adk-full"
	t.Cleanup(func() {
		agenttranslator.PythonADKImageDigest = originalPython
		agenttranslator.GoADKImageDigest = originalGo
		agenttranslator.GoADKFullImageDigest = originalGoFull
	})
}

func createAgent(client database.Client, agent *v1alpha2.Agent) {
	dbAgent := &database.Agent{
		Config: &adk.AgentConfig{},
		ID:     common.GetObjectRef(agent),
	}
	client.StoreAgent(context.Background(), dbAgent) //nolint:errcheck
}

func TestHandleGetAgent(t *testing.T) {
	t.Run("gets team successfully", func(t *testing.T) {
		modelConfig := createTestModelConfig()
		team := createTestAgent("test-team", modelConfig)

		handler, _ := setupTestHandler(t, team, modelConfig)
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
		require.Equal(t, "test-team", response.Data.Agent.Metadata.Name)
		require.Equal(t, "default/test-model-config", response.Data.ModelConfigRef, w.Body.String())
		require.Equal(t, "gpt-4", response.Data.Model)
		require.Equal(t, v1alpha2.ModelProviderOpenAI, response.Data.ModelProvider)
		require.False(t, response.Data.DeploymentReady) // No status conditions, should be false
	})

	t.Run("gets agent with DeploymentReady=true, Accepted=true", func(t *testing.T) {
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

		handler, _ := setupTestHandler(t, agent, modelConfig)
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
		require.True(t, response.Data.Accepted)
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

		handler, _ := setupTestHandler(t, agent, modelConfig)
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

		handler, _ := setupTestHandler(t, agent, modelConfig)
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

	t.Run("returns 404 for SandboxAgent on GET /api/agents (use /api/sandboxagents)", func(t *testing.T) {
		modelConfig := createTestModelConfig()
		sa := createTestSandboxAgentCRD("sandbox-only", modelConfig, nil)

		handler, _ := setupTestHandler(t, sa, modelConfig)

		req := httptest.NewRequest("GET", "/api/agents/default/sandbox-only", nil)
		req = mux.SetURLVars(req, map[string]string{"namespace": "default", "name": "sandbox-only"})
		req = setUser(req, "test-user")
		w := httptest.NewRecorder()

		handler.HandleGetAgent(&testErrorResponseWriter{w}, req)

		require.Equal(t, http.StatusNotFound, w.Code, w.Body.String())
	})

	t.Run("returns 404 for missing agent", func(t *testing.T) {
		handler, _ := setupTestHandler(t)

		req := httptest.NewRequest("GET", "/api/agents/default/test-team", nil)
		req = mux.SetURLVars(req, map[string]string{"namespace": "default", "name": "test-team"})
		req = setUser(req, "test-user")
		w := httptest.NewRecorder()

		handler.HandleGetAgent(&testErrorResponseWriter{w}, req)

		require.Equal(t, http.StatusNotFound, w.Code, w.Body.String())
	})

	t.Run("returns 404 for AgentHarness on GET /api/agents (use /api/agentharnesses)", func(t *testing.T) {
		sb := &v1alpha2.AgentHarness{
			ObjectMeta: metav1.ObjectMeta{Name: "harness-only", Namespace: "default"},
			Spec:       v1alpha2.AgentHarnessSpec{Backend: v1alpha2.AgentHarnessBackendOpenClaw},
		}
		handler, _ := setupTestHandler(t, sb)

		req := httptest.NewRequest("GET", "/api/agents/default/harness-only", nil)
		req = mux.SetURLVars(req, map[string]string{"namespace": "default", "name": "harness-only"})
		req = setUser(req, "test-user")
		w := httptest.NewRecorder()

		handler.HandleGetAgent(&testErrorResponseWriter{w}, req)

		require.Equal(t, http.StatusNotFound, w.Code)
	})
}

func TestHandleGetSandboxAgent(t *testing.T) {
	t.Run("gets sandbox agent successfully", func(t *testing.T) {
		modelConfig := createTestModelConfig()
		conditions := []metav1.Condition{
			{Type: "Accepted", Status: "True", Reason: "AgentReconciled"},
			{Type: "Ready", Status: "True", Reason: "WorkloadReady"},
		}
		sa := createTestSandboxAgentCRD("sandbox-accepted", modelConfig, conditions)

		handler, _ := setupTestHandler(t, sa, modelConfig)

		req := httptest.NewRequest("GET", "/api/sandboxagents/default/sandbox-accepted", nil)
		req = mux.SetURLVars(req, map[string]string{"namespace": "default", "name": "sandbox-accepted"})
		req = setUser(req, "test-user")
		w := httptest.NewRecorder()

		handler.HandleGetSandboxAgent(&testErrorResponseWriter{w}, req)

		require.Equal(t, http.StatusOK, w.Code)

		var response api.StandardResponse[api.AgentResponse]
		err := json.Unmarshal(w.Body.Bytes(), &response)
		require.NoError(t, err)
		require.True(t, response.Data.Accepted)
		require.True(t, response.Data.DeploymentReady)
		require.Equal(t, v1alpha2.WorkloadModeSandbox, response.Data.WorkloadMode)
	})

	t.Run("same name as regular agent still returns sandbox resource", func(t *testing.T) {
		modelConfig := createTestModelConfig()
		agent := createTestAgent("shared-name", modelConfig)
		sa := createTestSandboxAgentCRD("shared-name", modelConfig, nil)
		handler, _ := setupTestHandler(t, agent, sa, modelConfig)

		req := httptest.NewRequest("GET", "/api/sandboxagents/default/shared-name", nil)
		req = mux.SetURLVars(req, map[string]string{"namespace": "default", "name": "shared-name"})
		req = setUser(req, "test-user")
		w := httptest.NewRecorder()

		handler.HandleGetSandboxAgent(&testErrorResponseWriter{w}, req)

		require.Equal(t, http.StatusOK, w.Code)
		var response api.StandardResponse[api.AgentResponse]
		err := json.Unmarshal(w.Body.Bytes(), &response)
		require.NoError(t, err)
		require.Equal(t, v1alpha2.WorkloadModeSandbox, response.Data.WorkloadMode)
	})
}

func TestHandleGetAgentHarness(t *testing.T) {
	t.Run("gets openshell AgentHarness", func(t *testing.T) {
		sb := &v1alpha2.AgentHarness{
			ObjectMeta: metav1.ObjectMeta{Name: "gh-get", Namespace: "default"},
			Spec:       v1alpha2.AgentHarnessSpec{Backend: v1alpha2.AgentHarnessBackendOpenClaw},
		}
		handler, _ := setupTestHandler(t, sb)

		req := httptest.NewRequest("GET", "/api/agentharnesses/default/gh-get", nil)
		req = mux.SetURLVars(req, map[string]string{"namespace": "default", "name": "gh-get"})
		req = setUser(req, "test-user")
		w := httptest.NewRecorder()

		handler.HandleGetAgentHarness(&testErrorResponseWriter{w}, req)

		require.Equal(t, http.StatusOK, w.Code)
		var response api.StandardResponse[api.AgentResponse]
		err := json.Unmarshal(w.Body.Bytes(), &response)
		require.NoError(t, err)
		require.Equal(t, "AgentHarness", response.Data.Agent.Kind)
		require.Equal(t, "gh-get", response.Data.Agent.Metadata.Name)
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
			{
				Type:   "Accepted",
				Status: "True",
				Reason: "AgentReconciled",
			},
		}
		readyAgent := createTestAgentWithStatus("ready-agent", modelConfig, readyConditions)

		// Agent with DeploymentReady=false
		notReadyAgent := createTestAgent("not-ready-agent", modelConfig)

		handler, _ := setupTestHandler(t, readyAgent, notReadyAgent, modelConfig)
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
		require.Equal(t, "not-ready-agent", response.Data[0].Agent.Metadata.Name)
		require.Equal(t, "default/test-model-config", response.Data[0].ModelConfigRef)
		require.Equal(t, "gpt-4", response.Data[0].Model)
		require.Equal(t, v1alpha2.ModelProviderOpenAI, response.Data[0].ModelProvider)
		require.Equal(t, false, response.Data[0].DeploymentReady)
		require.Equal(t, "ready-agent", response.Data[1].Agent.Metadata.Name)
		require.Equal(t, "default/test-model-config", response.Data[1].ModelConfigRef)
		require.Equal(t, "gpt-4", response.Data[1].Model)
		require.Equal(t, v1alpha2.ModelProviderOpenAI, response.Data[1].ModelProvider)
		require.Equal(t, true, response.Data[1].DeploymentReady)
	})

	t.Run("lists expected agent conditions", func(t *testing.T) {
		modelConfig := createTestModelConfig()

		// Agent with DeploymentReady=true
		readyConditions := []metav1.Condition{
			{
				Type:   "Ready",
				Status: "True",
				Reason: "DeploymentReady",
			},
			{
				Type:   "Accepted",
				Status: "True",
				Reason: "AgentReconciled",
			},
		}
		invalidConditions := []metav1.Condition{ // an agent's deployment can be ready although it's configuration is invalid
			{
				Type:   "Accepted",
				Status: "False",
				Reason: "AgentReconcileFailed",
			},
			{
				Type:   "Ready",
				Status: "True",
				Reason: "DeploymentReady",
			},
		}
		readyAgent := createTestAgentWithStatus("ready-agent", modelConfig, readyConditions)
		invalidAgent := createTestAgentWithStatus("invalid-agent", modelConfig, invalidConditions)

		handler, _ := setupTestHandler(t, readyAgent, invalidAgent, modelConfig)
		createAgent(handler.DatabaseService, readyAgent)
		createAgent(handler.DatabaseService, invalidAgent)

		req := httptest.NewRequest("GET", "/api/agents", nil)
		req = setUser(req, "test-user")

		w := httptest.NewRecorder()

		handler.HandleListAgents(&testErrorResponseWriter{w}, req)

		require.Equal(t, http.StatusOK, w.Code)

		// both agents are returned with their statuses
		var response api.StandardResponse[[]api.AgentResponse]
		err := json.Unmarshal(w.Body.Bytes(), &response)
		require.NoError(t, err)
		require.Len(t, response.Data, 2)
		require.Equal(t, "ready-agent", response.Data[1].Agent.Metadata.Name)
		require.Equal(t, true, response.Data[1].Accepted)
		require.Equal(t, true, response.Data[1].DeploymentReady)
		require.Equal(t, "invalid-agent", response.Data[0].Agent.Metadata.Name)
		require.Equal(t, false, response.Data[0].Accepted)
		require.Equal(t, true, response.Data[0].DeploymentReady)
	})

	t.Run("lists SandboxAgent CRD with Accepted and Ready from status", func(t *testing.T) {
		modelConfig := createTestModelConfig()
		conditions := []metav1.Condition{
			{Type: "Accepted", Status: "True", Reason: "Reconciled"},
			{Type: "Ready", Status: "True", Reason: "WorkloadReady"},
		}
		sa := createTestSandboxAgentCRD("mysandbox", modelConfig, conditions)
		handler, _ := setupTestHandler(t, sa, modelConfig)

		req := httptest.NewRequest("GET", "/api/agents", nil)
		req = setUser(req, "test-user")
		w := httptest.NewRecorder()

		handler.HandleListAgents(&testErrorResponseWriter{w}, req)

		require.Equal(t, http.StatusOK, w.Code)

		var response api.StandardResponse[[]api.AgentResponse]
		err := json.Unmarshal(w.Body.Bytes(), &response)
		require.NoError(t, err)
		require.Len(t, response.Data, 1)
		require.Equal(t, "mysandbox", response.Data[0].Agent.Metadata.Name)
		require.Equal(t, "SandboxAgent", response.Data[0].Agent.Kind)
		require.True(t, response.Data[0].Accepted)
		require.True(t, response.Data[0].DeploymentReady)
		require.Equal(t, v1alpha2.WorkloadModeSandbox, response.Data[0].WorkloadMode)
	})

	t.Run("includes openclaw AgentHarness CR in agent list", func(t *testing.T) {
		modelConfig := createTestModelConfig()
		agent := createTestAgent("list-agent", modelConfig)
		sb := &v1alpha2.AgentHarness{
			ObjectMeta: metav1.ObjectMeta{Name: "openclaw-1", Namespace: "default"},
			Spec: v1alpha2.AgentHarnessSpec{
				Backend:        v1alpha2.AgentHarnessBackendOpenClaw,
				Description:    "Workload VM for experiments",
				ModelConfigRef: "test-model-config",
			},
			Status: v1alpha2.AgentHarnessStatus{
				Conditions: []metav1.Condition{
					{Type: v1alpha2.AgentHarnessConditionTypeAccepted, Status: "True", Reason: "AgentHarnessAccepted"},
					{Type: v1alpha2.AgentHarnessConditionTypeReady, Status: "True", Reason: "SandboxReady"},
				},
				BackendRef: &v1alpha2.AgentHarnessStatusRef{Backend: v1alpha2.AgentHarnessBackendOpenClaw, ID: "default-openclaw-1"},
			},
		}
		handler, _ := setupTestHandler(t, agent, sb, modelConfig)
		createAgent(handler.DatabaseService, agent)

		req := httptest.NewRequest("GET", "/api/agents", nil)
		req = setUser(req, "test-user")
		w := httptest.NewRecorder()
		handler.HandleListAgents(&testErrorResponseWriter{w}, req)

		require.Equal(t, http.StatusOK, w.Code)
		var response api.StandardResponse[[]api.AgentResponse]
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &response))
		require.Len(t, response.Data, 2)

		var found bool
		for _, row := range response.Data {
			if row.OpenshellAgentHarness == nil {
				continue
			}
			found = true
			require.Equal(t, "default-openclaw-1", row.OpenshellAgentHarness.GatewaySandboxName)
			require.Equal(t, "AgentHarness", row.Agent.Kind)
			require.Equal(t, "openclaw-1", row.Agent.Metadata.Name)
			require.Equal(t, "Workload VM for experiments", row.Agent.Spec.Description)
			require.True(t, row.Accepted)
			require.True(t, row.DeploymentReady)
			require.Equal(t, v1alpha2.ModelProviderOpenAI, row.ModelProvider)
		}
		require.True(t, found)
	})

	t.Run("filters Agent and AgentHarness rows by namespace query parameter", func(t *testing.T) {
		modelConfig := createTestModelConfig()
		agentDefault := createTestAgent("agent-in-default", modelConfig)
		agentOther := &v1alpha2.Agent{
			ObjectMeta: metav1.ObjectMeta{Name: "agent-in-other", Namespace: "other"},
			Spec: v1alpha2.AgentSpec{
				Type: v1alpha2.AgentType_Declarative,
				Declarative: &v1alpha2.DeclarativeAgentSpec{
					ModelConfig: modelConfig.Name,
				},
			},
		}
		harnessDefault := &v1alpha2.AgentHarness{
			ObjectMeta: metav1.ObjectMeta{Name: "harness-default", Namespace: "default"},
			Spec: v1alpha2.AgentHarnessSpec{
				Backend:        v1alpha2.AgentHarnessBackendOpenClaw,
				ModelConfigRef: "test-model-config",
			},
		}
		harnessOther := &v1alpha2.AgentHarness{
			ObjectMeta: metav1.ObjectMeta{Name: "harness-other", Namespace: "other"},
			Spec: v1alpha2.AgentHarnessSpec{
				Backend:        v1alpha2.AgentHarnessBackendOpenClaw,
				ModelConfigRef: "test-model-config",
			},
		}
		unsupportedHarnessDefault := &v1alpha2.AgentHarness{
			ObjectMeta: metav1.ObjectMeta{Name: "unsupported-harness", Namespace: "default"},
			Spec: v1alpha2.AgentHarnessSpec{
				Backend:        v1alpha2.AgentHarnessBackendType("unsupported"),
				ModelConfigRef: "test-model-config",
			},
		}
		sandboxDefault := createTestSandboxAgentCRD("sandbox-in-default", modelConfig, nil)
		sandboxOther := createTestSandboxAgentCRD("sandbox-in-other", modelConfig, nil)
		sandboxOther.Namespace = "other"
		handler, _ := setupTestHandler(t, agentDefault, agentOther, harnessDefault, harnessOther, unsupportedHarnessDefault, sandboxDefault, sandboxOther, modelConfig)

		req := httptest.NewRequest("GET", "/api/agents?namespace=default", nil)
		req = setUser(req, "test-user")
		w := httptest.NewRecorder()

		handler.HandleListAgents(&testErrorResponseWriter{w}, req)

		require.Equal(t, http.StatusOK, w.Code)
		var response api.StandardResponse[[]api.AgentResponse]
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &response))
		require.Len(t, response.Data, 3)

		byName := make(map[string]api.AgentResponse, len(response.Data))
		for _, row := range response.Data {
			byName[row.Agent.Metadata.Name] = row
			require.Equal(t, "default", row.Agent.Metadata.Namespace)
		}
		require.Contains(t, byName, "agent-in-default")
		require.Contains(t, byName, "harness-default")
		require.Contains(t, byName, "sandbox-in-default")
		require.NotContains(t, byName, "agent-in-other")
		require.NotContains(t, byName, "harness-other")
		require.NotContains(t, byName, "sandbox-in-other")
		require.NotContains(t, byName, "unsupported-harness")
	})

	// Kubernetes namespace names must be DNS-1123 labels. Rejecting invalid input
	// before calling the Kubernetes client keeps the list path consistent with
	// other resource handlers and avoids surprising cross-namespace behavior.
	t.Run("returns 400 for invalid namespace query value", func(t *testing.T) {
		handler, _ := setupTestHandler(t)

		req := httptest.NewRequest("GET", "/api/agents?namespace=INVALID_NS!", nil)
		req = setUser(req, "test-user")
		w := httptest.NewRecorder()

		handler.HandleListAgents(&testErrorResponseWriter{w}, req)

		require.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("returns 400 for namespace query value with leading or trailing whitespace", func(t *testing.T) {
		handler, _ := setupTestHandler(t)

		req := httptest.NewRequest("GET", "/api/agents?namespace=%20default", nil)
		req = setUser(req, "test-user")
		w := httptest.NewRecorder()

		handler.HandleListAgents(&testErrorResponseWriter{w}, req)

		require.Equal(t, http.StatusBadRequest, w.Code)
		require.Contains(t, w.Body.String(), "must not contain leading or trailing whitespace")
	})
}

func TestHandleListSandboxAgents(t *testing.T) {
	t.Run("lists sandbox agents successfully", func(t *testing.T) {
		modelConfig := createTestModelConfig()
		conditions := []metav1.Condition{
			{Type: "Accepted", Status: "True", Reason: "Reconciled"},
			{Type: "Ready", Status: "True", Reason: "WorkloadReady"},
		}
		sa := createTestSandboxAgentCRD("mysandbox", modelConfig, conditions)
		agent := createTestAgent("myagent", modelConfig)
		handler, _ := setupTestHandler(t, sa, agent, modelConfig)

		req := httptest.NewRequest("GET", "/api/sandboxagents", nil)
		req = setUser(req, "test-user")
		w := httptest.NewRecorder()

		handler.HandleListSandboxAgents(&testErrorResponseWriter{w}, req)

		require.Equal(t, http.StatusOK, w.Code)

		var response api.StandardResponse[[]api.AgentResponse]
		err := json.Unmarshal(w.Body.Bytes(), &response)
		require.NoError(t, err)
		require.Len(t, response.Data, 1)
		require.Equal(t, "mysandbox", response.Data[0].Agent.Metadata.Name)
		require.True(t, response.Data[0].Accepted)
		require.True(t, response.Data[0].DeploymentReady)
		require.Equal(t, v1alpha2.WorkloadModeSandbox, response.Data[0].WorkloadMode)
	})

	t.Run("same name Agent and SandboxAgent both appear on agents list", func(t *testing.T) {
		modelConfig := createTestModelConfig()
		agent := createTestAgent("shared-name", modelConfig)
		sa := createTestSandboxAgentCRD("shared-name", modelConfig, nil)
		handler, _ := setupTestHandler(t, agent, sa, modelConfig)

		req := httptest.NewRequest("GET", "/api/agents", nil)
		req = setUser(req, "test-user")
		w := httptest.NewRecorder()
		handler.HandleListAgents(&testErrorResponseWriter{w}, req)

		require.Equal(t, http.StatusOK, w.Code)

		var resp api.StandardResponse[[]api.AgentResponse]
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
		require.Len(t, resp.Data, 2)
		var deploymentCount, sandboxCount int
		for _, row := range resp.Data {
			switch row.WorkloadMode {
			case v1alpha2.WorkloadModeDeployment:
				deploymentCount++
				require.Equal(t, "Agent", row.Agent.Kind)
			case v1alpha2.WorkloadModeSandbox:
				sandboxCount++
				require.Equal(t, "SandboxAgent", row.Agent.Kind)
			}
		}
		require.Equal(t, 1, deploymentCount)
		require.Equal(t, 1, sandboxCount)
	})
}

func TestHandleUpdateAgent(t *testing.T) {
	t.Run("updates agent successfully", func(t *testing.T) {
		oldModelConfig := &v1alpha2.ModelConfig{
			ObjectMeta: metav1.ObjectMeta{Name: "old-model-config", Namespace: "default"},
			Spec: v1alpha2.ModelConfigSpec{
				Model:    "gpt-4o-mini",
				Provider: v1alpha2.ModelProviderOpenAI,
			},
		}
		newModelConfig := &v1alpha2.ModelConfig{
			ObjectMeta: metav1.ObjectMeta{Name: "new-model-config", Namespace: "default"},
			Spec: v1alpha2.ModelConfigSpec{
				Model:    "gpt-4.1",
				Provider: v1alpha2.ModelProviderOpenAI,
			},
		}
		existingAgent := &v1alpha2.Agent{
			ObjectMeta: metav1.ObjectMeta{Name: "test-team", Namespace: "default"},
			Spec: v1alpha2.AgentSpec{
				Type: v1alpha2.AgentType_Declarative,
				Declarative: &v1alpha2.DeclarativeAgentSpec{
					ModelConfig:   "old-model-config",
					SystemMessage: "old system message",
				},
			},
		}

		handler, _ := setupTestHandler(t, existingAgent, oldModelConfig, newModelConfig)

		updatedAgent := &v1alpha2.Agent{
			ObjectMeta: metav1.ObjectMeta{Name: "test-team", Namespace: "default"},
			Spec: v1alpha2.AgentSpec{
				Type: v1alpha2.AgentType_Declarative,
				Declarative: &v1alpha2.DeclarativeAgentSpec{
					ModelConfig:   "new-model-config",
					SystemMessage: "new system message",
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

	t.Run("returns 400 for invalid updated agent configuration", func(t *testing.T) {
		modelConfig := &v1alpha2.ModelConfig{
			ObjectMeta: metav1.ObjectMeta{Name: "old-model-config", Namespace: "default"},
			Spec: v1alpha2.ModelConfigSpec{
				Model:    "gpt-4o-mini",
				Provider: v1alpha2.ModelProviderOpenAI,
			},
		}
		existingAgent := &v1alpha2.Agent{
			ObjectMeta: metav1.ObjectMeta{Name: "test-team", Namespace: "default"},
			Spec: v1alpha2.AgentSpec{
				Type: v1alpha2.AgentType_Declarative,
				Declarative: &v1alpha2.DeclarativeAgentSpec{
					ModelConfig:   modelConfig.Name,
					SystemMessage: "old system message",
				},
			},
		}

		handler, _ := setupTestHandler(t, existingAgent, modelConfig)

		updatedAgent := &v1alpha2.Agent{
			ObjectMeta: metav1.ObjectMeta{Name: "test-team", Namespace: "default"},
			Spec: v1alpha2.AgentSpec{
				Type: v1alpha2.AgentType_Declarative,
				Declarative: &v1alpha2.DeclarativeAgentSpec{
					ModelConfig:   "missing-model-config",
					SystemMessage: "updated system message",
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

		require.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("returns 404 for non-existent team", func(t *testing.T) {
		handler, _ := setupTestHandler(t)

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

		handler, _ := setupTestHandler(t, modelConfig)

		agent := &v1alpha2.Agent{
			ObjectMeta: metav1.ObjectMeta{Name: "test-team", Namespace: "default"},
			Spec: v1alpha2.AgentSpec{
				Type:        v1alpha2.AgentType_Declarative,
				Description: "Test team description",
				Declarative: &v1alpha2.DeclarativeAgentSpec{
					ModelConfig:   modelConfig.Name,
					SystemMessage: "You are an imaginary agent",
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
		require.Equal(t, "You are an imaginary agent", response.Data.Spec.Declarative.SystemMessage)
		require.Equal(t, "test-model-config", response.Data.Spec.Declarative.ModelConfig)
	})
}

func TestHandleDeleteTeam(t *testing.T) {
	t.Run("deletes team successfully", func(t *testing.T) {
		team := &v1alpha2.Agent{
			ObjectMeta: metav1.ObjectMeta{Name: "test-team", Namespace: "default"},
		}

		handler, _ := setupTestHandler(t, team)
		createAgent(handler.DatabaseService, team)

		req := httptest.NewRequest("DELETE", "/api/agents/default/test-team", nil)
		req = mux.SetURLVars(req, map[string]string{"namespace": "default", "name": "test-team"})
		req = setUser(req, "test-user")
		w := httptest.NewRecorder()

		handler.HandleDeleteAgent(&testErrorResponseWriter{w}, req)

		require.Equal(t, http.StatusOK, w.Code)
	})

	t.Run("returns 404 for non-existent team", func(t *testing.T) {
		handler, _ := setupTestHandler(t)

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

	t.Run("does not delete sandbox agent with same name", func(t *testing.T) {
		modelConfig := createTestModelConfig()
		agent := createTestAgent("shared-name", modelConfig)
		sa := createTestSandboxAgentCRD("shared-name", modelConfig, nil)
		handler, _ := setupTestHandler(t, agent, sa, modelConfig)

		req := httptest.NewRequest("DELETE", "/api/agents/default/shared-name", nil)
		req = mux.SetURLVars(req, map[string]string{"namespace": "default", "name": "shared-name"})
		req = setUser(req, "test-user")
		w := httptest.NewRecorder()

		handler.HandleDeleteAgent(&testErrorResponseWriter{w}, req)

		require.Equal(t, http.StatusOK, w.Code)

		var stillThere v1alpha2.SandboxAgent
		err := handler.KubeClient.Get(context.Background(), types.NamespacedName{Namespace: "default", Name: "shared-name"}, &stillThere)
		require.NoError(t, err)
	})

	t.Run("does not delete SandboxAgent via DELETE /api/agents (use /api/sandboxagents)", func(t *testing.T) {
		modelConfig := createTestModelConfig()
		sa := createTestSandboxAgentCRD("sandbox-only-delete", modelConfig, nil)
		handler, _ := setupTestHandler(t, sa, modelConfig)

		req := httptest.NewRequest("DELETE", "/api/agents/default/sandbox-only-delete", nil)
		req = mux.SetURLVars(req, map[string]string{"namespace": "default", "name": "sandbox-only-delete"})
		req = setUser(req, "test-user")
		w := httptest.NewRecorder()

		handler.HandleDeleteAgent(&testErrorResponseWriter{w}, req)

		require.Equal(t, http.StatusNotFound, w.Code)

		err := handler.KubeClient.Get(context.Background(), types.NamespacedName{Namespace: "default", Name: "sandbox-only-delete"}, sa)
		require.NoError(t, err)
	})

	t.Run("does not delete AgentHarness via DELETE /api/agents (use /api/agentharnesses)", func(t *testing.T) {
		sb := &v1alpha2.AgentHarness{
			ObjectMeta: metav1.ObjectMeta{Name: "sb-only", Namespace: "default"},
			Spec:       v1alpha2.AgentHarnessSpec{Backend: v1alpha2.AgentHarnessBackendOpenClaw},
		}
		handler, _ := setupTestHandler(t, sb)

		req := httptest.NewRequest("DELETE", "/api/agents/default/sb-only", nil)
		req = mux.SetURLVars(req, map[string]string{"namespace": "default", "name": "sb-only"})
		req = setUser(req, "test-user")
		w := httptest.NewRecorder()

		handler.HandleDeleteAgent(&testErrorResponseWriter{w}, req)

		require.Equal(t, http.StatusNotFound, w.Code)

		err := handler.KubeClient.Get(context.Background(), types.NamespacedName{Namespace: "default", Name: "sb-only"}, sb)
		require.NoError(t, err)
	})

	t.Run("does not delete AgentHarness when Agent with same name exists", func(t *testing.T) {
		modelConfig := createTestModelConfig()
		agent := createTestAgent("harness-shared", modelConfig)
		sb := &v1alpha2.AgentHarness{
			ObjectMeta: metav1.ObjectMeta{Name: "harness-shared", Namespace: "default"},
			Spec:       v1alpha2.AgentHarnessSpec{Backend: v1alpha2.AgentHarnessBackendOpenClaw},
		}
		handler, _ := setupTestHandler(t, agent, sb, modelConfig)
		createAgent(handler.DatabaseService, agent)

		req := httptest.NewRequest("DELETE", "/api/agents/default/harness-shared", nil)
		req = mux.SetURLVars(req, map[string]string{"namespace": "default", "name": "harness-shared"})
		req = setUser(req, "test-user")
		w := httptest.NewRecorder()

		handler.HandleDeleteAgent(&testErrorResponseWriter{w}, req)

		require.Equal(t, http.StatusOK, w.Code)

		err := handler.KubeClient.Get(context.Background(), types.NamespacedName{Namespace: "default", Name: "harness-shared"}, sb)
		require.NoError(t, err)
	})
}

func TestHandleDeleteAgentHarness(t *testing.T) {
	t.Run("deletes openshell AgentHarness", func(t *testing.T) {
		sb := &v1alpha2.AgentHarness{
			ObjectMeta: metav1.ObjectMeta{Name: "sb-only", Namespace: "default"},
			Spec:       v1alpha2.AgentHarnessSpec{Backend: v1alpha2.AgentHarnessBackendOpenClaw},
		}
		handler, _ := setupTestHandler(t, sb)

		req := httptest.NewRequest("DELETE", "/api/agentharnesses/default/sb-only", nil)
		req = mux.SetURLVars(req, map[string]string{"namespace": "default", "name": "sb-only"})
		req = setUser(req, "test-user")
		w := httptest.NewRecorder()

		handler.HandleDeleteAgentHarness(&testErrorResponseWriter{w}, req)

		require.Equal(t, http.StatusOK, w.Code)

		err := handler.KubeClient.Get(context.Background(), types.NamespacedName{Namespace: "default", Name: "sb-only"}, sb)
		require.Error(t, err)
		require.True(t, apierrors.IsNotFound(err))
	})
}

func TestHandleDeleteSandboxAgent(t *testing.T) {
	t.Run("deletes sandbox agent successfully", func(t *testing.T) {
		modelConfig := createTestModelConfig()
		sa := createTestSandboxAgentCRD("test-sandbox", modelConfig, nil)
		handler, _ := setupTestHandler(t, sa, modelConfig)

		req := httptest.NewRequest("DELETE", "/api/sandboxagents/default/test-sandbox", nil)
		req = mux.SetURLVars(req, map[string]string{"namespace": "default", "name": "test-sandbox"})
		req = setUser(req, "test-user")
		w := httptest.NewRecorder()

		handler.HandleDeleteSandboxAgent(&testErrorResponseWriter{w}, req)

		require.Equal(t, http.StatusOK, w.Code)
	})
}

func TestHandleCreateAgentHarness(t *testing.T) {
	t.Run("creates openclaw AgentHarness", func(t *testing.T) {
		modelConfig := createTestModelConfig()
		handler, _ := setupTestHandler(t, modelConfig)

		body := map[string]any{
			"apiVersion": "kagent.dev/v1alpha2",
			"kind":       "AgentHarness",
			"metadata": map[string]string{
				"name":      "my-openclaw",
				"namespace": "default",
			},
			"spec": map[string]any{
				"backend":        "openclaw",
				"description":    "test vm",
				"modelConfigRef": "test-model-config",
			},
		}
		raw, err := json.Marshal(body)
		require.NoError(t, err)

		req := httptest.NewRequest(http.MethodPost, "/api/agentharnesses", bytes.NewReader(raw))
		req.Header.Set("Content-Type", "application/json")
		req = setUser(req, "test-user")
		w := httptest.NewRecorder()

		handler.HandleCreateAgentHarness(&testErrorResponseWriter{w}, req)

		require.Equal(t, http.StatusCreated, w.Code, w.Body.String())

		var response api.StandardResponse[api.AgentResponse]
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &response))
		require.Equal(t, "AgentHarness", response.Data.Agent.Kind)
		require.Equal(t, "my-openclaw", response.Data.Agent.Metadata.Name)
		require.NotNil(t, response.Data.OpenshellAgentHarness)
		require.Equal(t, v1alpha2.AgentHarnessBackendOpenClaw, response.Data.OpenshellAgentHarness.Backend)

		var created v1alpha2.AgentHarness
		require.NoError(t, handler.KubeClient.Get(context.Background(), types.NamespacedName{Namespace: "default", Name: "my-openclaw"}, &created))
		require.Equal(t, v1alpha2.AgentHarnessBackendOpenClaw, created.Spec.Backend)
	})

	t.Run("creates hermes AgentHarness", func(t *testing.T) {
		modelConfig := createTestModelConfig()
		handler, _ := setupTestHandler(t, modelConfig)

		body := map[string]any{
			"apiVersion": "kagent.dev/v1alpha2",
			"kind":       "AgentHarness",
			"metadata": map[string]string{
				"name":      "my-hermes",
				"namespace": "default",
			},
			"spec": map[string]any{
				"backend":        "hermes",
				"description":    "hermes vm",
				"modelConfigRef": "test-model-config",
			},
		}
		raw, err := json.Marshal(body)
		require.NoError(t, err)

		req := httptest.NewRequest(http.MethodPost, "/api/agentharnesses", bytes.NewReader(raw))
		req.Header.Set("Content-Type", "application/json")
		req = setUser(req, "test-user")
		w := httptest.NewRecorder()

		handler.HandleCreateAgentHarness(&testErrorResponseWriter{w}, req)

		require.Equal(t, http.StatusCreated, w.Code, w.Body.String())

		var response api.StandardResponse[api.AgentResponse]
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &response))
		require.Equal(t, v1alpha2.AgentHarnessBackendHermes, response.Data.OpenshellAgentHarness.Backend)

		var created v1alpha2.AgentHarness
		require.NoError(t, handler.KubeClient.Get(context.Background(), types.NamespacedName{Namespace: "default", Name: "my-hermes"}, &created))
		require.Equal(t, v1alpha2.AgentHarnessBackendHermes, created.Spec.Backend)
	})
}
