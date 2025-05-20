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
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
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

	teamsWithID := make([]map[string]interface{}, 0)
	for _, team := range agentList.Items {
		teamFullName := fmt.Sprintf("%s/%s", team.Namespace, team.Name)
		log.V(1).Info("Processing Team", "teamName", teamFullName)
		autogenTeam, err := h.AutogenClient.GetTeam(teamFullName, userID)
		if err != nil {
			w.RespondWithError(errors.NewInternalServerError("Failed to get Team from Autogen", err))
			return
		}

		if autogenTeam == nil {
			log.V(1).Info("Team not found in Autogen", "teamName", teamFullName)
			continue
		}

		// Get the ModelConfig for the team
		modelConfig := &v1alpha1.ModelConfig{}
		if err := common.FetchObjKube(r.Context(), h.KubeClient, modelConfig, team.Spec.ModelConfig, team.Namespace); err != nil {
			log.Error(err, "Failed to get ModelConfig", "modelConfigRef", modelConfig.Namespace, "/", modelConfig.Name)
			continue
		}

		if modelConfig == nil {
			log.V(1).Info("ModelConfig not found", "modelConfigRef", modelConfig.Namespace, "/", modelConfig.Name)
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
	log.Info("Received request to update Team")

	var teamRequest *v1alpha1.Agent

	if err := DecodeJSONBody(r, &teamRequest); err != nil {
		w.RespondWithError(errors.NewBadRequestError("Invalid request body", err))
		return
	}

	log = log.WithValues(
		"namespace", teamRequest.Namespace,
		"configName", teamRequest.Name,
	)

	log.V(1).Info("Getting existing Team")
	existingTeam := &v1alpha1.Agent{}
	err := h.KubeClient.Get(r.Context(), types.NamespacedName{
		Name:      teamRequest.Name,
		Namespace: teamRequest.Namespace,
	}, existingTeam)
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
		log.V(1).Info("Namespace not provided in request. Creating in", teamRequest.Namespace, "namespace")
	}

	log = log.WithValues(
		"namespace", teamRequest.Namespace,
		"teamName", teamRequest.Name,
	)

	kubeClientWrapper := client_wrapper.NewKubeClientWrapper(h.KubeClient)
	kubeClientWrapper.AddInMemory(teamRequest)

	apiTranslator := autogen.NewAutogenApiTranslator(
		kubeClientWrapper,
		h.DefaultModelConfig,
	)

	log.V(1).Info("Translating Team to Autogen format")
	autogenTeam, err := apiTranslator.TranslateGroupChatForAgent(r.Context(), teamRequest)
	log.WithValues(
		"name", teamRequest.Name,
		"namespace", teamRequest.Namespace,
	)
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
	if err := common.FetchObjKube(
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
	if err := common.FetchObjKube(
		r.Context(),
		h.KubeClient,
		modelConfig,
		team.Spec.ModelConfig,
		team.Namespace,
	); err != nil {
		w.RespondWithError(errors.NewInternalServerError("Failed to get ModelConfig", err))
		return
	}

	// Create a new object that contains the Team information from Team and the ID from the autogenTeam
	teamWithID := &map[string]interface{}{
		"id":        autogenTeam.Id,
		"agent":     team,
		"component": autogenTeam.Component,
		"provider":  modelConfig.Spec.Provider,
		"model":     modelConfig.Spec.Model,
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
		"namespace", namespace,
		"teamName", teamName,
	)

	log.V(1).Info("Getting Team from Kubernetes")
	team := &v1alpha1.Agent{}
	if err := h.KubeClient.Get(r.Context(), types.NamespacedName{
		Name:      teamName,
		Namespace: namespace,
	}, team); err != nil {
		w.RespondWithError(errors.NewNotFoundError("Team not found in Kubernetes", err))
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
