package handlers

import (
	"context"
	"net/http"
	"strings"

	"github.com/go-logr/logr"
	"github.com/kagent-dev/kagent/go/controller/api/v1alpha1"
	"github.com/kagent-dev/kagent/go/controller/translator"
	"github.com/kagent-dev/kagent/go/internal/httpserver/errors"
	"github.com/kagent-dev/kagent/go/internal/utils"
	common "github.com/kagent-dev/kagent/go/internal/utils"
	"github.com/kagent-dev/kagent/go/pkg/client/api"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"
)

// AgentsHandler handles agent-related requests
type AgentsHandler struct {
	*Base
}

// NewAgentsHandler creates a new AgentsHandler
func NewAgentsHandler(base *Base) *AgentsHandler {
	return &AgentsHandler{Base: base}
}

// HandleListAgents handles GET /api/agents requests using database
func (h *AgentsHandler) HandleListAgents(w ErrorResponseWriter, r *http.Request) {
	log := ctrllog.FromContext(r.Context()).WithName("agents-handler").WithValues("operation", "list-db")

	agentList := &v1alpha1.AgentList{}
	if err := h.KubeClient.List(r.Context(), agentList); err != nil {
		w.RespondWithError(errors.NewInternalServerError("Failed to list Agents from Kubernetes", err))
		return
	}

	agentsWithID := make([]api.AgentResponse, 0)
	for _, agent := range agentList.Items {
		agentRef := common.GetObjectRef(&agent)
		log.V(1).Info("Processing Agent", "agentRef", agentRef)

		// dgAgent, err := h.DatabaseService.GetAgent(common.ConvertToPythonIdentifier(agentRef))
		// if err != nil {
		// 	w.RespondWithError(errors.NewNotFoundError("Agent not found", err))
		// 	return
		// }

		agentResponse, err := h.getAgentResponse(r.Context(), log, &agent)
		if err != nil {
			w.RespondWithError(err)
			return
		}

		agentsWithID = append(agentsWithID, agentResponse)
	}

	log.Info("Successfully listed agents", "count", len(agentsWithID))
	data := api.NewResponse(agentsWithID, "Successfully listed agents", false)
	RespondWithJSON(w, http.StatusOK, data)
}

func (h *AgentsHandler) getAgentResponse(ctx context.Context, log logr.Logger, agent *v1alpha1.Agent) (api.AgentResponse, error) {

	agentRef := common.GetObjectRef(agent)
	log.V(1).Info("Processing Agent", "agentRef", agentRef)

	// Get the ModelConfig for the team
	modelConfig := &v1alpha1.ModelConfig{}
	if err := common.GetObject(
		ctx,
		h.KubeClient,
		modelConfig,
		agent.Spec.ModelConfig,
		agent.Namespace,
	); err != nil {
		modelConfigRef := common.GetObjectRef(modelConfig)
		if k8serrors.IsNotFound(err) {
			log.V(1).Info("ModelConfig not found", "modelConfigRef", modelConfigRef)
		} else {
			log.Error(err, "Failed to get ModelConfig", "modelConfigRef", modelConfigRef)
		}
	}

	// Get the MemoryRefs for the team
	memoryRefs := make([]string, 0, len(agent.Spec.Memory))
	for _, memory := range agent.Spec.Memory {
		memoryRef, err := common.ParseRefString(memory, agent.Namespace)
		if err != nil {
			log.Error(err, "Failed to parse memory reference", "memoryRef", memory)
			continue
		}
		memoryRefs = append(memoryRefs, memoryRef.String())
	}

	// Get the tools for the team
	tools := make([]*v1alpha1.Tool, 0, len(agent.Spec.Tools))
	for _, tool := range agent.Spec.Tools {
		toolCopy := tool.DeepCopy()

		switch toolCopy.Type {
		case v1alpha1.ToolProviderType_Agent:
			if toolCopy.Agent == nil {
				log.Info("Agent tool has nil Agent field", "tool", toolCopy)
				continue
			}
			if err := updateRef(&toolCopy.Agent.Ref, agent.Namespace); err != nil {
				log.Error(err, "Failed to parse agent tool reference", "toolRef", toolCopy.Agent.Ref)
				continue
			}
			tools = append(tools, toolCopy)

		case v1alpha1.ToolProviderType_McpServer:
			if toolCopy.McpServer == nil {
				log.Info("McpServer tool has nil McpServer field", "tool", toolCopy)
				continue
			}
			if err := updateRef(&toolCopy.McpServer.ToolServer, agent.Namespace); err != nil {
				log.Error(err, "Failed to parse server tool reference", "toolRef", toolCopy.McpServer.ToolServer)
				continue
			}
			tools = append(tools, toolCopy)

		default:
			log.Info("Unknown tool type", "toolType", toolCopy.Type)
		}
	}

	return api.AgentResponse{
		ID:    common.ConvertToPythonIdentifier(agentRef),
		Agent: agent,
		// Config:         dbAgent.Config,
		ModelProvider:  modelConfig.Spec.Provider,
		Model:          modelConfig.Spec.Model,
		ModelConfigRef: common.GetObjectRef(modelConfig),
		MemoryRefs:     memoryRefs,
		Tools:          tools,
	}, nil
}

// HandleGetAgent handles GET /api/agents/{namespace}/{name} requests using database
func (h *AgentsHandler) HandleGetAgent(w ErrorResponseWriter, r *http.Request) {
	log := ctrllog.FromContext(r.Context()).WithName("agents-handler").WithValues("operation", "get-db")

	agentName, err := GetPathParam(r, "name")
	if err != nil {
		w.RespondWithError(errors.NewBadRequestError("Failed to get name from path", err))
		return
	}
	log = log.WithValues("agentName", agentName)

	agentNamespace, err := GetPathParam(r, "namespace")
	if err != nil {
		w.RespondWithError(errors.NewBadRequestError("Failed to get namespace from path", err))
		return
	}
	log = log.WithValues("agentNamespace", agentNamespace)

	agent := &v1alpha1.Agent{}
	if err := common.GetObject(
		r.Context(),
		h.KubeClient,
		agent,
		agentName,
		agentNamespace,
	); err != nil {
		w.RespondWithError(errors.NewNotFoundError("Agent not found", err))
		return
	}

	// log.V(1).Info("Getting agent from database")
	// dbAgent, err := h.DatabaseService.GetAgent(fmt.Sprintf("%s/%s", agentNamespace, agentName))
	// if err != nil {
	// 	w.RespondWithError(errors.NewNotFoundError("Agent not found", err))
	// 	return
	// }

	agentResponse, err := h.getAgentResponse(r.Context(), log, agent)
	if err != nil {
		w.RespondWithError(err)
		return
	}

	log.Info("Successfully retrieved agent")
	data := api.NewResponse(agentResponse, "Successfully retrieved agent", false)
	RespondWithJSON(w, http.StatusOK, data)
}

// HandleCreateAgent handles POST /api/agents requests using database
func (h *AgentsHandler) HandleCreateAgent(w ErrorResponseWriter, r *http.Request) {
	log := ctrllog.FromContext(r.Context()).WithName("agents-handler").WithValues("operation", "create-db")

	var req api.CreateAgentRequest
	if err := DecodeJSONBody(r, &req); err != nil {
		w.RespondWithError(errors.NewBadRequestError("Invalid request body", err))
		return
	}

	agentRef, err := common.ParseRefString(req.Ref, common.GetResourceNamespace())
	if err != nil {
		log.Error(err, "Failed to parse Ref")
		w.RespondWithError(errors.NewBadRequestError("Invalid Ref", err))
		return
	}
	if !strings.Contains(req.Ref, "/") {
		log.V(4).Info("Namespace not provided in request. Creating in controller installation namespace",
			"namespace", agentRef.Namespace)
	}

	log = log.WithValues(
		"agentNamespace", agentRef.Namespace,
		"agentName", agentRef.Name,
	)

	// Check if agent already exists
	log.V(1).Info("Checking if Agent already exists")
	existingAgent := &v1alpha1.Agent{}
	err = common.GetObject(
		r.Context(),
		h.KubeClient,
		existingAgent,
		agentRef.Name,
		agentRef.Namespace,
	)
	if err == nil {
		log.Info("Agent already exists")
		w.RespondWithError(errors.NewConflictError("Agent already exists", nil))
		return
	} else if !k8serrors.IsNotFound(err) {
		log.Error(err, "Failed to check if Agent exists")
		w.RespondWithError(errors.NewInternalServerError("Failed to check if Agent exists", err))
		return
	}

	// Create the v1alpha1.Agent from the request
	agentReq := &v1alpha1.Agent{
		ObjectMeta: metav1.ObjectMeta{
			Name:      agentRef.Name,
			Namespace: agentRef.Namespace,
		},
		Spec: v1alpha1.AgentSpec{
			Description:   req.Description,
			SystemMessage: req.SystemMessage,
			ModelConfig:   req.ModelConfig,
			Stream:        req.Stream,
			Tools:         req.Tools,
			Memory:        req.Memory,
			A2AConfig:     req.A2AConfig,
			Deployment:    req.Deployment,
		},
	}

	kubeClientWrapper := utils.NewKubeClientWrapper(h.KubeClient)
	kubeClientWrapper.AddInMemory(agentReq)

	apiTranslator := translator.NewAdkApiTranslator(
		kubeClientWrapper,
		h.DefaultModelConfig,
	)

	log.V(1).Info("Translating Agent to ADK format")
	_, err = apiTranslator.TranslateAgent(r.Context(), agentReq)
	if err != nil {
		w.RespondWithError(errors.NewInternalServerError("Failed to translate Agent to ADK format", err))
		return
	}

	// Agent is valid, we can store it
	log.V(1).Info("Creating Agent in Kubernetes")
	if err := h.KubeClient.Create(r.Context(), agentReq); err != nil {
		w.RespondWithError(errors.NewInternalServerError("Failed to create Agent in Kubernetes", err))
		return
	}

	log.Info("Successfully created agent", "agentRef", agentRef)
	data := api.NewResponse(agentReq, "Successfully created agent", false)
	RespondWithJSON(w, http.StatusCreated, data)
}

// HandleUpdateAgent handles PUT /api/agents/{namespace}/{name} requests using database
func (h *AgentsHandler) HandleUpdateAgent(w ErrorResponseWriter, r *http.Request) {
	log := ctrllog.FromContext(r.Context()).WithName("agents-handler").WithValues("operation", "update-db")

	namespace, err := GetPathParam(r, "namespace")
	if err != nil {
		log.Error(err, "Failed to get namespace from path")
		w.RespondWithError(errors.NewBadRequestError("Failed to get namespace from path", err))
		return
	}

	agentName, err := GetPathParam(r, "name")
	if err != nil {
		log.Error(err, "Failed to get name from path")
		w.RespondWithError(errors.NewBadRequestError("Failed to get name from path", err))
		return
	}

	var req api.UpdateAgentRequest
	if err := DecodeJSONBody(r, &req); err != nil {
		w.RespondWithError(errors.NewBadRequestError("Invalid request body", err))
		return
	}

	log = log.WithValues(
		"agentNamespace", namespace,
		"agentName", agentName,
	)

	log.V(1).Info("Getting existing Agent")
	existingAgent := &v1alpha1.Agent{}
	err = common.GetObject(
		r.Context(),
		h.KubeClient,
		existingAgent,
		agentName,
		namespace,
	)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			log.Info("Agent not found")
			w.RespondWithError(errors.NewNotFoundError("Agent not found", nil))
			return
		}
		log.Error(err, "Failed to get Agent")
		w.RespondWithError(errors.NewInternalServerError("Failed to get Agent", err))
		return
	}

	// Update fields from the request (only non-nil fields for partial updates)
	if req.Description != nil {
		existingAgent.Spec.Description = *req.Description
	}
	if req.SystemMessage != nil {
		existingAgent.Spec.SystemMessage = *req.SystemMessage
	}
	if req.ModelConfig != nil {
		existingAgent.Spec.ModelConfig = *req.ModelConfig
	}
	if req.Stream != nil {
		existingAgent.Spec.Stream = req.Stream
	}
	if req.Tools != nil {
		existingAgent.Spec.Tools = req.Tools
	}
	if req.Memory != nil {
		existingAgent.Spec.Memory = req.Memory
	}
	if req.A2AConfig != nil {
		existingAgent.Spec.A2AConfig = req.A2AConfig
	}
	if req.Deployment != nil {
		existingAgent.Spec.Deployment = req.Deployment
	}

	if err := h.KubeClient.Update(r.Context(), existingAgent); err != nil {
		w.RespondWithError(errors.NewInternalServerError("Failed to update Agent", err))
		return
	}

	// Get the agent response with all related information
	agentResponse, err := h.getAgentResponse(r.Context(), log, existingAgent)
	if err != nil {
		w.RespondWithError(err)
		return
	}

	log.Info("Successfully updated agent")
	data := api.NewResponse(agentResponse, "Successfully updated agent", false)
	RespondWithJSON(w, http.StatusOK, data)
}

// HandleDeleteAgent handles DELETE /api/agents/{namespace}/{name} requests using database
func (h *AgentsHandler) HandleDeleteAgent(w ErrorResponseWriter, r *http.Request) {
	log := ctrllog.FromContext(r.Context()).WithName("agents-handler").WithValues("operation", "delete-db")

	agentName, err := GetPathParam(r, "name")
	if err != nil {
		w.RespondWithError(errors.NewBadRequestError("Failed to get name from path", err))
		return
	}
	log = log.WithValues("agentName", agentName)

	agentNamespace, err := GetPathParam(r, "namespace")
	if err != nil {
		w.RespondWithError(errors.NewBadRequestError("Failed to get namespace from path", err))
		return
	}
	log = log.WithValues("agentNamespace", agentNamespace)

	log.V(1).Info("Getting Agent from Kubernetes")
	agent := &v1alpha1.Agent{}
	err = common.GetObject(
		r.Context(),
		h.KubeClient,
		agent,
		agentName,
		agentNamespace,
	)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			log.Info("Agent not found")
			w.RespondWithError(errors.NewNotFoundError("Agent not found", nil))
			return
		}
		log.Error(err, "Failed to get Agent")
		w.RespondWithError(errors.NewInternalServerError("Failed to get Agent", err))
		return
	}

	log.V(1).Info("Deleting Agent from Kubernetes")
	if err := h.KubeClient.Delete(r.Context(), agent); err != nil {
		w.RespondWithError(errors.NewInternalServerError("Failed to delete Agent", err))
		return
	}

	log.Info("Successfully deleted agent")
	data := api.NewResponse(struct{}{}, "Successfully deleted agent", false)
	RespondWithJSON(w, http.StatusOK, data)
}
