package handlers

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/go-logr/logr"
	"github.com/kagent-dev/kagent/go/controller/api/v1alpha1"
	"github.com/kagent-dev/kagent/go/controller/translator"
	autogen_client "github.com/kagent-dev/kagent/go/internal/autogen/client"
	"github.com/kagent-dev/kagent/go/internal/database"
	"github.com/kagent-dev/kagent/go/internal/httpserver/errors"
	"github.com/kagent-dev/kagent/go/internal/utils"
	common "github.com/kagent-dev/kagent/go/internal/utils"
	"github.com/kagent-dev/kagent/go/pkg/client/api"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
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
		w.RespondWithError(errors.NewInternalServerError("Failed to list Teams from Kubernetes", err))
		return
	}

	agentsWithID := make([]api.AgentResponse, 0)
	for _, team := range agentList.Items {
		teamRef := common.GetObjectRef(&team)
		log.V(1).Info("Processing Team", "teamRef", teamRef)

		agent, err := h.DatabaseService.GetAgent(teamRef)
		if err != nil {
			w.RespondWithError(errors.NewNotFoundError("Agent not found", err))
			return
		}

		agentResponse, err := h.getAgentResponse(r.Context(), log, &team, agent)
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

func (h *AgentsHandler) getAgentResponse(ctx context.Context, log logr.Logger, agent *v1alpha1.Agent, dbAgent *database.Agent) (api.AgentResponse, error) {

	agentRef := common.GetObjectRef(agent)
	log.V(1).Info("Processing Team", "teamRef", agentRef)

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
		ID:             dbAgent.ID,
		Agent:          agent,
		Component:      &dbAgent.Component,
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

	log.V(1).Info("Getting agent from database")
	dbAgent, err := h.DatabaseService.GetAgent(fmt.Sprintf("%s/%s", agentNamespace, agentName))
	if err != nil {
		w.RespondWithError(errors.NewNotFoundError("Agent not found", err))
		return
	}

	agentResponse, err := h.getAgentResponse(r.Context(), log, agent, dbAgent)
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

	var agentReq v1alpha1.Agent
	if err := DecodeJSONBody(r, &agentReq); err != nil {
		w.RespondWithError(errors.NewBadRequestError("Invalid request body", err))
		return
	}
	if agentReq.Namespace == "" {
		agentReq.Namespace = common.GetResourceNamespace()
		log.V(4).Info("Namespace not provided in request. Creating in controller installation namespace",
			"namespace", agentReq.Namespace)
	}
	agentRef, err := common.ParseRefString(agentReq.Name, agentReq.Namespace)
	if err != nil {
		w.RespondWithError(errors.NewBadRequestError("Invalid agent metadata", err))
	}

	log = log.WithValues(
		"teamNamespace", agentRef.Namespace,
		"teamName", agentRef.Name,
	)

	kubeClientWrapper := utils.NewKubeClientWrapper(h.KubeClient)
	kubeClientWrapper.AddInMemory(&agentReq)

	apiTranslator := translator.NewAutogenApiTranslator(
		kubeClientWrapper,
		h.DefaultModelConfig,
	)

	log.V(1).Info("Translating Agent to Autogen format")
	autogenAgent, err := apiTranslator.TranslateGroupChatForAgent(r.Context(), &agentReq)
	if err != nil {
		w.RespondWithError(errors.NewInternalServerError("Failed to translate Agent to Autogen format", err))
		return
	}

	validateReq := autogen_client.ValidationRequest{
		Component: &autogenAgent.Component,
	}

	// Validate the team
	log.V(1).Info("Validating Team")
	validationResp, err := h.AutogenClient.Validate(r.Context(), &validateReq)
	if err != nil {
		w.RespondWithError(errors.NewInternalServerError("Failed to validate Team", err))
		return
	}

	if !validationResp.IsValid {
		log.Info("Team validation failed",
			"errors", validationResp.Errors,
			"warnings", validationResp.Warnings)

		// Improved error message with validation details
		errorMsg := "Team validation failed: "
		if len(validationResp.Errors) > 0 {
			// Convert validation errors to strings
			errorStrings := make([]string, 0, len(validationResp.Errors))
			for _, validationErr := range validationResp.Errors {
				if validationErr != nil {
					// Use the error as a string or extract relevant information
					errorStrings = append(errorStrings, fmt.Sprintf("%v", validationErr))
				}
			}
			errorMsg += strings.Join(errorStrings, ", ")
		} else {
			errorMsg += "unknown validation error"
		}

		w.RespondWithError(errors.NewValidationError(errorMsg, nil))
		return
	}

	// Team is valid, we can store it
	log.V(1).Info("Creating Agent in Kubernetes")
	if err := h.KubeClient.Create(r.Context(), &agentReq); err != nil {
		w.RespondWithError(errors.NewInternalServerError("Failed to create Agent in Kubernetes", err))
		return
	}

	log.Info("Successfully created agent", "agentRef", agentRef)
	data := api.NewResponse(&agentReq, "Successfully created agent", false)
	RespondWithJSON(w, http.StatusCreated, data)
}

// HandleUpdateAgent handles PUT /api/agents/{namespace}/{name} requests using database
func (h *AgentsHandler) HandleUpdateAgent(w ErrorResponseWriter, r *http.Request) {
	log := ctrllog.FromContext(r.Context()).WithName("agents-handler").WithValues("operation", "update-db")

	var agentReq v1alpha1.Agent
	if err := DecodeJSONBody(r, &agentReq); err != nil {
		w.RespondWithError(errors.NewBadRequestError("Invalid request body", err))
		return
	}

	if agentReq.Namespace == "" {
		agentReq.Namespace = common.GetResourceNamespace()
		log.V(4).Info("Namespace not provided in request. Creating in controller installation namespace",
			"namespace", agentReq.Namespace)
	}
	agentRef, err := common.ParseRefString(agentReq.Name, agentReq.Namespace)
	if err != nil {
		w.RespondWithError(errors.NewBadRequestError("Invalid Agent metadata", err))
	}

	log = log.WithValues(
		"agentNamespace", agentRef.Namespace,
		"agentName", agentRef.Name,
	)

	log.V(1).Info("Getting existing Agent")
	existingAgent := &v1alpha1.Agent{}
	err = common.GetObject(
		r.Context(),
		h.KubeClient,
		existingAgent,
		agentRef.Name,
		agentRef.Namespace,
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

	// We set the .spec from the incoming request, so
	// we don't have to copy/set any other fields
	existingAgent.Spec = agentReq.Spec

	if err := h.KubeClient.Update(r.Context(), existingAgent); err != nil {
		w.RespondWithError(errors.NewInternalServerError("Failed to update Agent", err))
		return
	}

	log.Info("Successfully updated agent")
	data := api.NewResponse(existingAgent, "Successfully updated agent", false)
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
