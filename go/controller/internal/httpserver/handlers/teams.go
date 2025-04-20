package handlers

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/kagent-dev/kagent/go/controller/api/v1alpha1"
	"github.com/kagent-dev/kagent/go/controller/internal/autogen"
	"github.com/kagent-dev/kagent/go/controller/internal/client_wrapper"
	"github.com/kagent-dev/kagent/go/controller/internal/httpserver/errors"
	common "github.com/kagent-dev/kagent/go/controller/internal/utils"
	"k8s.io/apimachinery/pkg/types"

	autogen_client "github.com/kagent-dev/kagent/go/autogen/client"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"
)

// TeamsHandler handles team-related requests
type TeamsHandler struct {
	*Base
}

// NewTeamsHandler creates a new TeamsHandler
func NewTeamsHandler(base *Base) *TeamsHandler {
	return &TeamsHandler{Base: base}
}

func convertToKubernetesIdentifier(name string) string {
	return strings.ReplaceAll(name, "_", "-")
}

// HandleListTeams handles GET /api/teams requests
func (h *TeamsHandler) HandleListTeams(w ErrorResponseWriter, r *http.Request) {
	log := ctrllog.FromContext(r.Context()).WithName("teams-handler").WithValues("operation", "list")

	userID, err := GetUserID(r)
	if err != nil {
		w.RespondWithError(errors.NewBadRequestError("Failed to get user ID", err))
		return
	}
	log = log.WithValues("userID", userID)

	agentList := &v1alpha1.AgentList{}
	if err := h.KubeClient.List(r.Context(), agentList); err != nil {
		w.RespondWithError(errors.NewInternalServerError("Failed to list agents from Kubernetes", err))
		return
	}

	teamsWithID := make([]map[string]interface{}, 0)
	for _, team := range agentList.Items {
		log.V(1).Info("Processing team", "teamName", team.Name)
		autogenTeam, err := h.AutogenClient.GetTeam(convertToKubernetesIdentifier(team.Name), userID)
		if err != nil {
			w.RespondWithError(errors.NewInternalServerError("Failed to get team from Autogen", err))
			return
		}

		if autogenTeam == nil {
			log.V(1).Info("Team not found in Autogen", "teamName", team.Name)
			continue
		}

		// Get the model config for the team
		modelConfig := &v1alpha1.ModelConfig{}
		if err := h.KubeClient.Get(r.Context(), types.NamespacedName{
			Name:      team.Spec.ModelConfigRef,
			Namespace: common.GetResourceNamespace(),
		}, modelConfig); err != nil {
			log.Error(err, "Failed to get model config", "modelConfigRef", team.Spec.ModelConfigRef)
			continue
		}

		if modelConfig == nil {
			log.V(1).Info("Model config not found", "modelConfigRef", team.Spec.ModelConfigRef)
			continue
		}

		teamsWithID = append(teamsWithID, map[string]interface{}{
			"id":        autogenTeam.Id,
			"agent":     team,
			"component": autogenTeam.Component,
			"provider":  modelConfig.Spec.Provider,
			"model":     modelConfig.Spec.Model,
		})
	}

	log.Info("Successfully listed teams", "count", len(teamsWithID))
	RespondWithJSON(w, http.StatusOK, teamsWithID)
}

// HandleUpdateTeam handles PUT /api/teams requests
func (h *TeamsHandler) HandleUpdateTeam(w ErrorResponseWriter, r *http.Request) {
	log := ctrllog.FromContext(r.Context()).WithName("teams-handler").WithValues("operation", "update")

	var teamRequest *v1alpha1.Agent
	if err := DecodeJSONBody(r, &teamRequest); err != nil {
		w.RespondWithError(errors.NewBadRequestError("Invalid request body", err))
		return
	}
	log = log.WithValues("teamName", teamRequest.Name)

	existingTeam := &v1alpha1.Agent{}
	if err := h.KubeClient.Get(r.Context(), types.NamespacedName{
		Name:      teamRequest.Name,
		Namespace: common.GetResourceNamespace(),
	}, existingTeam); err != nil {
		w.RespondWithError(errors.NewInternalServerError("Failed to get team", err))
		return
	}

	// We set the .spec from the incoming request, so
	// we don't have to copy/set any other fields
	existingTeam.Spec = teamRequest.Spec

	if err := h.KubeClient.Update(r.Context(), existingTeam); err != nil {
		w.RespondWithError(errors.NewInternalServerError("Failed to update team", err))
		return
	}

	log.Info("Successfully updated team")
	RespondWithJSON(w, http.StatusOK, teamRequest)
}

// HandleCreateTeam handles POST /api/teams requests
func (h *TeamsHandler) HandleCreateTeam(w ErrorResponseWriter, r *http.Request) {
	log := ctrllog.FromContext(r.Context()).WithName("teams-handler").WithValues("operation", "create")

	var teamRequest *v1alpha1.Agent
	if err := DecodeJSONBody(r, &teamRequest); err != nil {
		w.RespondWithError(errors.NewBadRequestError("Invalid request body", err))
		return
	}
	log = log.WithValues("teamName", teamRequest.Name)

	// Default to kagent namespace
	teamRequest.Namespace = common.GetResourceNamespace()

	kubeClientWrapper := client_wrapper.NewKubeClientWrapper(h.KubeClient)
	kubeClientWrapper.AddInMemory(teamRequest)

	apiTranslator := autogen.NewAutogenApiTranslator(
		kubeClientWrapper,
		h.DefaultModelConfig,
	)

	log.V(1).Info("Translating agent to Autogen format")
	autogenTeam, err := apiTranslator.TranslateGroupChatForAgent(r.Context(), teamRequest)
	if err != nil {
		w.RespondWithError(errors.NewInternalServerError("Failed to translate agent to Autogen format", err))
		return
	}

	validateReq := autogen_client.ValidationRequest{
		Component: autogenTeam.Component,
	}

	// Validate the team
	log.V(1).Info("Validating team")
	validationResp, err := h.AutogenClient.Validate(&validateReq)
	if err != nil {
		w.RespondWithError(errors.NewInternalServerError("Failed to validate team", err))
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
	log.V(1).Info("Creating team in Kubernetes")
	if err := h.KubeClient.Create(r.Context(), teamRequest); err != nil {
		w.RespondWithError(errors.NewInternalServerError("Failed to create team in Kubernetes", err))
		return
	}

	log.Info("Successfully created team")
	RespondWithJSON(w, http.StatusCreated, teamRequest)
}

// HandleGetTeam handles GET /api/teams/{teamID} requests
func (h *TeamsHandler) HandleGetTeam(w ErrorResponseWriter, r *http.Request) {
	log := ctrllog.FromContext(r.Context()).WithName("teams-handler").WithValues("operation", "get")

	userID, err := GetUserID(r)
	if err != nil {
		w.RespondWithError(errors.NewBadRequestError("Failed to get user ID", err))
		return
	}
	log = log.WithValues("userID", userID)

	teamID, err := GetIntPathParam(r, "teamID")
	if err != nil {
		w.RespondWithError(errors.NewBadRequestError("Failed to get team ID from path", err))
		return
	}
	log = log.WithValues("teamID", teamID)

	log.V(1).Info("Getting team from Autogen")
	autogenTeam, err := h.AutogenClient.GetTeamByID(teamID, userID)
	if err != nil {
		w.RespondWithError(errors.NewInternalServerError("Failed to get team from Autogen", err))
		return
	}

	teamLabel := convertToKubernetesIdentifier(autogenTeam.Component.Label)
	log = log.WithValues("teamLabel", teamLabel)

	log.V(1).Info("Getting team from Kubernetes")
	team := &v1alpha1.Agent{}
	if err := h.KubeClient.Get(r.Context(), types.NamespacedName{
		Name:      teamLabel,
		Namespace: common.GetResourceNamespace(),
	}, team); err != nil {
		w.RespondWithError(errors.NewNotFoundError("Team not found in Kubernetes", err))
		return
	}

	// Get the model config for the team
	log.V(1).Info("Getting model config", "modelConfigRef", team.Spec.ModelConfigRef)
	modelConfig := &v1alpha1.ModelConfig{}
	if err := h.KubeClient.Get(r.Context(), types.NamespacedName{
		Name:      team.Spec.ModelConfigRef,
		Namespace: common.GetResourceNamespace(),
	}, modelConfig); err != nil {
		w.RespondWithError(errors.NewInternalServerError("Failed to get model config", err))
		return
	}

	// Create a new object that contains the team information from team and the ID from the autogenTeam
	teamWithID := &map[string]interface{}{
		"id":        autogenTeam.Id,
		"agent":     team,
		"component": autogenTeam.Component,
		"provider":  modelConfig.Spec.Provider,
		"model":     modelConfig.Spec.Model,
	}

	log.Info("Successfully retrieved team")
	RespondWithJSON(w, http.StatusOK, teamWithID)
}

// HandleDeleteTeam handles DELETE /api/teams/{teamLabel} requests
func (h *TeamsHandler) HandleDeleteTeam(w ErrorResponseWriter, r *http.Request) {
	log := ctrllog.FromContext(r.Context()).WithName("teams-handler").WithValues("operation", "delete")

	teamLabel, err := GetPathParam(r, "teamLabel")
	if err != nil {
		w.RespondWithError(errors.NewBadRequestError("Failed to get team label from path", err))
		return
	}
	log = log.WithValues("teamLabel", teamLabel)

	log.V(1).Info("Getting team from Kubernetes")
	team := &v1alpha1.Agent{}
	if err := h.KubeClient.Get(r.Context(), types.NamespacedName{
		Name:      teamLabel,
		Namespace: common.GetResourceNamespace(),
	}, team); err != nil {
		w.RespondWithError(errors.NewNotFoundError("Team not found in Kubernetes", err))
		return
	}

	log.V(1).Info("Deleting team from Kubernetes")
	if err := h.KubeClient.Delete(r.Context(), team); err != nil {
		w.RespondWithError(errors.NewInternalServerError("Failed to delete team", err))
		return
	}

	log.Info("Successfully deleted team")
	w.WriteHeader(http.StatusNoContent)
}
