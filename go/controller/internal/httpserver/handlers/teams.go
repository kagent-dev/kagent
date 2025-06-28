package handlers

import (
	"fmt"
	"net/http"
	"strings"

	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/kagent-dev/kagent/go/autogen/api"
	autogen_client "github.com/kagent-dev/kagent/go/autogen/client"
	"github.com/kagent-dev/kagent/go/controller/api/v1alpha1"
	"github.com/kagent-dev/kagent/go/controller/internal/autogen"
	"github.com/kagent-dev/kagent/go/controller/internal/client_wrapper"
	"github.com/kagent-dev/kagent/go/controller/internal/httpserver/errors"
	common "github.com/kagent-dev/kagent/go/controller/internal/utils"
)

type TeamResponse struct {
	Id             int                    `json:"id"`
	Agent          *v1alpha1.Agent        `json:"agent"`
	Component      *api.Component         `json:"component"`
	ModelProvider  v1alpha1.ModelProvider `json:"modelProvider"`
	Model          string                 `json:"model"`
	ModelConfigRef string                 `json:"modelConfigRef"`
	MemoryRefs     []string               `json:"memoryRefs"`
	Tools          []*v1alpha1.Tool       `json:"tools"`
}

// TeamsHandler handles team-related requests
type TeamsHandler struct {
	*Base
}

// NewTeamsHandler creates a new TeamsHandler
func NewTeamsHandler(base *Base) *TeamsHandler {
	return &TeamsHandler{Base: base}
}

// HandleListTeams handles GET /api/teams requests
func (h *TeamsHandler) HandleListTeams(w ErrorResponseWriter, r *http.Request) {
	log := ctrllog.FromContext(r.Context()).WithName("teams-handler").WithValues("operation", "list")
	log.Info("Received request to list Teams")

	userID, err := GetUserID(r)
	if err != nil {
		w.RespondWithError(errors.NewBadRequestError("Failed to get user ID", err))
		return
	}
	log = log.WithValues("userID", userID)

	agentList := &v1alpha1.AgentList{}
	if err := h.KubeClient.List(r.Context(), agentList); err != nil {
		w.RespondWithError(errors.NewInternalServerError("Failed to list Teams from Kubernetes", err))
		return
	}

	teamsWithID := make([]TeamResponse, 0)
	for _, team := range agentList.Items {
		teamRef := common.GetObjectRef(&team)
		log.V(1).Info("Processing Team", "teamRef", teamRef)

		autogenTeam, err := h.AutogenClient.GetTeam(teamRef, userID)
		if err != nil {
			if err == autogen_client.NotFoundError {
				log.V(1).Info("Team not found in Autogen", "teamRef", teamRef)
				continue
			}
			w.RespondWithError(errors.NewInternalServerError("Failed to get Team from Autogen", err))
			return
		}

		// Get the ModelConfig for the team
		modelConfig := &v1alpha1.ModelConfig{}
		if err := common.GetObject(
			r.Context(),
			h.KubeClient,
			modelConfig,
			team.Spec.ModelConfig,
			team.Namespace,
		); err != nil {
			modelConfigRef := common.GetObjectRef(modelConfig)
			if k8serrors.IsNotFound(err) {
				log.V(1).Info("ModelConfig not found", "modelConfigRef", modelConfigRef)
				continue
			}
			log.Error(err, "Failed to get ModelConfig", "modelConfigRef", modelConfigRef)
			continue
		}

		// Get the MemoryRefs for the team
		memoryRefs := make([]string, 0, len(team.Spec.Memory))
		for _, memory := range team.Spec.Memory {
			memoryRef, err := common.ParseRefString(memory, team.Namespace)
			if err != nil {
				log.Error(err, "Failed to parse memory reference", "memoryRef", memory)
				continue
			}
			memoryRefs = append(memoryRefs, memoryRef.String())
		}

		// Get the tools for the team
		tools := make([]*v1alpha1.Tool, 0, len(team.Spec.Tools))
		for _, tool := range team.Spec.Tools {
			toolCopy := tool.DeepCopy()

			switch toolCopy.Type {
			case v1alpha1.ToolProviderType_Agent:
				if toolCopy.Agent == nil {
					log.Info("Agent tool has nil Agent field", "tool", toolCopy)
					continue
				}
				if err := updateRef(&toolCopy.Agent.Ref, team.Namespace); err != nil {
					log.Error(err, "Failed to parse agent tool reference", "toolRef", toolCopy.Agent.Ref)
					continue
				}
				tools = append(tools, toolCopy)

			case v1alpha1.ToolProviderType_McpServer:
				if toolCopy.McpServer == nil {
					log.Info("McpServer tool has nil McpServer field", "tool", toolCopy)
					continue
				}
				if err := updateRef(&toolCopy.McpServer.ToolServer, team.Namespace); err != nil {
					log.Error(err, "Failed to parse server tool reference", "toolRef", toolCopy.McpServer.ToolServer)
					continue
				}
				tools = append(tools, toolCopy)

			default:
				log.Info("Unknown tool type", "toolType", toolCopy.Type)
			}
		}

		teamsWithID = append(teamsWithID, TeamResponse{
			Id:             autogenTeam.Id,
			Agent:          &team,
			Component:      autogenTeam.Component,
			ModelProvider:  modelConfig.Spec.Provider,
			Model:          modelConfig.Spec.Model,
			ModelConfigRef: common.GetObjectRef(modelConfig),
			MemoryRefs:     memoryRefs,
			Tools:          tools,
		})
	}

	log.Info("Successfully listed teams", "count", len(teamsWithID))
	RespondWithJSON(w, http.StatusOK, teamsWithID)
}

// HandleUpdateTeam handles PUT /api/teams requests
func (h *TeamsHandler) HandleUpdateTeam(w ErrorResponseWriter, r *http.Request) {
	log := ctrllog.FromContext(r.Context()).WithName("teams-handler").WithValues("operation", "update")
	log.Info("Received request to update Team")

	var teamRequest *v1alpha1.Agent
	if err := DecodeJSONBody(r, &teamRequest); err != nil {
		w.RespondWithError(errors.NewBadRequestError("Invalid request body", err))
		return
	}

	if teamRequest.Namespace == "" {
		teamRequest.Namespace = common.GetResourceNamespace()
		log.V(4).Info("Namespace not provided in request. Creating in controller installation namespace",
			"namespace", teamRequest.Namespace)
	}
	teamRef, err := common.ParseRefString(teamRequest.Name, teamRequest.Namespace)
	if err != nil {
		w.RespondWithError(errors.NewBadRequestError("Invalid Agent metadata", err))
	}

	log = log.WithValues(
		"teamNamespace", teamRef.Namespace,
		"teamName", teamRef.Name,
	)

	log.V(1).Info("Getting existing Team")
	existingTeam := &v1alpha1.Agent{}
	err = common.GetObject(
		r.Context(),
		h.KubeClient,
		existingTeam,
		teamRef.Name,
		teamRef.Namespace,
	)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			log.Info("Team not found")
			w.RespondWithError(errors.NewNotFoundError("Team not found", nil))
			return
		}
		log.Error(err, "Failed to get Team")
		w.RespondWithError(errors.NewInternalServerError("Failed to get Team", err))
		return
	}

	// We set the .spec from the incoming request, so
	// we don't have to copy/set any other fields
	existingTeam.Spec = teamRequest.Spec

	if err := h.KubeClient.Update(r.Context(), existingTeam); err != nil {
		w.RespondWithError(errors.NewInternalServerError("Failed to update Team", err))
		return
	}

	log.Info("Successfully updated Team")
	RespondWithJSON(w, http.StatusOK, teamRequest)
}

// HandleCreateTeam handles POST /api/teams requests
func (h *TeamsHandler) HandleCreateTeam(w ErrorResponseWriter, r *http.Request) {
	log := ctrllog.FromContext(r.Context()).WithName("teams-handler").WithValues("operation", "create")
	log.V(1).Info("Received request to create Team")

	var teamRequest *v1alpha1.Agent
	if err := DecodeJSONBody(r, &teamRequest); err != nil {
		w.RespondWithError(errors.NewBadRequestError("Invalid request body", err))
		return
	}

	if teamRequest.Namespace == "" {
		teamRequest.Namespace = common.GetResourceNamespace()
		log.V(4).Info("Namespace not provided in request. Creating in controller installation namespace",
			"namespace", teamRequest.Namespace)
	}
	teamRef, err := common.ParseRefString(teamRequest.Name, teamRequest.Namespace)
	if err != nil {
		w.RespondWithError(errors.NewBadRequestError("Invalid agent metadata", err))
	}

	log = log.WithValues(
		"teamNamespace", teamRef.Namespace,
		"teamName", teamRef.Name,
	)

	kubeClientWrapper := client_wrapper.NewKubeClientWrapper(h.KubeClient)
	kubeClientWrapper.AddInMemory(teamRequest)

	apiTranslator := autogen.NewAutogenApiTranslator(
		kubeClientWrapper,
		h.DefaultModelConfig,
	)

	log.V(1).Info("Translating Team to Autogen format")
	autogenTeam, err := apiTranslator.TranslateGroupChatForAgent(r.Context(), teamRequest)
	if err != nil {
		w.RespondWithError(errors.NewInternalServerError("Failed to translate Team to Autogen format", err))
		return
	}

	validateReq := autogen_client.ValidationRequest{
		Component: autogenTeam.Component,
	}

	// Validate the team
	log.V(1).Info("Validating Team")
	validationResp, err := h.AutogenClient.Validate(&validateReq)
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
	log.V(1).Info("Creating Team in Kubernetes")
	if err := h.KubeClient.Create(r.Context(), teamRequest); err != nil {
		w.RespondWithError(errors.NewInternalServerError("Failed to create Team in Kubernetes", err))
		return
	}

	log.V(1).Info("Successfully created Team")
	RespondWithJSON(w, http.StatusCreated, teamRequest)
}

// HandleGetTeam handles GET /api/teams/{teamID} requests
func (h *TeamsHandler) HandleGetTeam(w ErrorResponseWriter, r *http.Request) {
	log := ctrllog.FromContext(r.Context()).WithName("teams-handler").WithValues("operation", "get")
	log.Info("Received request to get Team")

	userID, err := GetUserID(r)
	if err != nil {
		w.RespondWithError(errors.NewBadRequestError("Failed to get user ID", err))
		return
	}
	log = log.WithValues("userID", userID)

	teamID, err := GetIntPathParam(r, "teamID")
	if err != nil {
		w.RespondWithError(errors.NewBadRequestError("Failed to get Team ID from path", err))
		return
	}
	log = log.WithValues("teamID", teamID)

	log.Info("Getting Team from Autogen")
	autogenTeam, err := h.AutogenClient.GetTeamByID(teamID, userID)
	if err != nil {
		w.RespondWithError(errors.NewInternalServerError("Failed to get Team from Autogen", err))
		return
	}

	teamLabel := autogenTeam.Component.Label
	log = log.WithValues("teamLabel", teamLabel)

	log.Info("Getting Team from Kubernetes")
	team := &v1alpha1.Agent{}
	if err := common.GetObject(
		r.Context(),
		h.KubeClient,
		team,
		teamLabel,
		common.GetResourceNamespace(),
	); err != nil {
		w.RespondWithError(errors.NewNotFoundError("Team not found in Kubernetes", err))
		return
	}

	// Get the ModelConfig for the team
	log.V(1).Info("Getting ModelConfig", "modelConfigRef", team.Spec.ModelConfig)
	modelConfig := &v1alpha1.ModelConfig{}
	if err := common.GetObject(
		r.Context(),
		h.KubeClient,
		modelConfig,
		team.Spec.ModelConfig,
		team.Namespace,
	); err != nil {
		modelConfigRef := common.GetObjectRef(modelConfig)
		if k8serrors.IsNotFound(err) {
			log.V(1).Info("ModelConfig not found", "modelConfigRef", modelConfigRef)
		}
		log.Error(err, "Failed to get ModelConfig", "modelConfigRef", modelConfigRef)
	}

	// Get the MemoryRefs for the team
	memoryRefs := make([]string, 0, len(team.Spec.Memory))
	for _, memory := range team.Spec.Memory {
		memoryRef, err := common.ParseRefString(memory, team.Namespace)
		if err != nil {
			log.Error(err, "Failed to parse memory reference", "memoryRef", memory)
			continue
		}
		memoryRefs = append(memoryRefs, memoryRef.String())
	}

	// Get the tools for the team
	tools := make([]*v1alpha1.Tool, 0, len(team.Spec.Tools))
	for _, tool := range team.Spec.Tools {
		toolCopy := tool.DeepCopy()

		switch toolCopy.Type {
		case v1alpha1.ToolProviderType_Agent:
			if toolCopy.Agent == nil {
				log.Info("Agent tool has nil Agent field", "tool", toolCopy)
				continue
			}
			if err := updateRef(&toolCopy.Agent.Ref, team.Namespace); err != nil {
				log.Error(err, "Failed to parse agent tool reference", "toolRef", toolCopy.Agent.Ref)
				continue
			}
			tools = append(tools, toolCopy)

		case v1alpha1.ToolProviderType_McpServer:
			if toolCopy.McpServer == nil {
				log.Info("McpServer tool has nil McpServer field", "tool", toolCopy)
				continue
			}
			if err := updateRef(&toolCopy.McpServer.ToolServer, team.Namespace); err != nil {
				log.Error(err, "Failed to parse server tool reference", "toolRef", toolCopy.McpServer.ToolServer)
				continue
			}
			tools = append(tools, toolCopy)

		default:
			log.Info("Unknown tool type", "toolType", toolCopy.Type)
		}
	}

	// Create a new object that contains the Team information from Team and the ID from the autogenTeam
	teamWithID := &TeamResponse{
		Id:             autogenTeam.Id,
		Agent:          team,
		Component:      autogenTeam.Component,
		ModelProvider:  modelConfig.Spec.Provider,
		Model:          modelConfig.Spec.Model,
		ModelConfigRef: common.GetObjectRef(modelConfig),
		MemoryRefs:     memoryRefs,
		Tools:          tools,
	}

	log.Info("Successfully retrieved Team")
	RespondWithJSON(w, http.StatusOK, teamWithID)
}

// HandleDeleteTeam handles DELETE /api/teams/{namespace}/{teamName} requests
func (h *TeamsHandler) HandleDeleteTeam(w ErrorResponseWriter, r *http.Request) {
	log := ctrllog.FromContext(r.Context()).WithName("teams-handler").WithValues("operation", "delete")
	log.Info("Received request to delete Team")

	namespace, err := GetPathParam(r, "namespace")
	if err != nil {
		w.RespondWithError(errors.NewBadRequestError("Failed to get namespace from path", err))
		return
	}

	teamName, err := GetPathParam(r, "teamName")
	if err != nil {
		w.RespondWithError(errors.NewBadRequestError("Failed to get teamName from path", err))
		return
	}

	log = log.WithValues(
		"teamNamespace", namespace,
		"teamName", teamName,
	)

	log.V(1).Info("Getting Team from Kubernetes")
	team := &v1alpha1.Agent{}
	err = common.GetObject(
		r.Context(),
		h.KubeClient,
		team,
		teamName,
		namespace,
	)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			log.Info("Team not found")
			w.RespondWithError(errors.NewNotFoundError("Team not found", nil))
			return
		}
		log.Error(err, "Failed to get Team")
		w.RespondWithError(errors.NewInternalServerError("Failed to get Team", err))
		return
	}

	log.V(1).Info("Deleting Team from Kubernetes")
	if err := h.KubeClient.Delete(r.Context(), team); err != nil {
		w.RespondWithError(errors.NewInternalServerError("Failed to delete Team", err))
		return
	}

	log.Info("Successfully deleted Team")
	w.WriteHeader(http.StatusNoContent)
}
