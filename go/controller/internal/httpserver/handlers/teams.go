package handlers

import (
	"fmt"
	"net/http"
	"strings"

	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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
	Id            int                    `json:"id"`
	Agent         v1alpha1.Agent         `json:"agent"`
	Component     *api.Component         `json:"component"`
	ModelProvider v1alpha1.ModelProvider `json:"modelProvider"`
	Model         string                 `json:"model"`
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
			w.RespondWithError(errors.NewInternalServerError("Failed to get Team from Autogen", err))
			return
		}
		if autogenTeam == nil {
			log.V(1).Info("Team not found in Autogen", "teamName", teamRef)
			continue
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
			modelConfigRef := common.ResourceRefString(modelConfig.Namespace, modelConfig.Name)
			if k8serrors.IsNotFound(err) {
				log.V(1).Info("ModelConfig not found", "modelConfigRef", modelConfigRef)
				continue
			}
			log.Error(err, "Failed to get ModelConfig", "modelConfigRef", modelConfigRef)
			continue
		}

		teamsWithID = append(teamsWithID, TeamResponse{
			Id:            autogenTeam.Id,
			Agent:         team,
			Component:     autogenTeam.Component,
			ModelProvider: modelConfig.Spec.Provider,
			Model:         modelConfig.Spec.Model,
		})
	}

	log.Info("Successfully listed teams", "count", len(teamsWithID))
	RespondWithJSON(w, http.StatusOK, teamsWithID)
}

type UpdateTeamRequest struct {
	TeamRef string             `json:"teamRef"`
	Spec    v1alpha1.AgentSpec `json:"spec"`
}

// HandleUpdateTeam handles PUT /api/teams requests
func (h *TeamsHandler) HandleUpdateTeam(w ErrorResponseWriter, r *http.Request) {
	log := ctrllog.FromContext(r.Context()).WithName("teams-handler").WithValues("operation", "update")
	log.Info("Received request to update Team")

	var teamRequest UpdateTeamRequest
	if err := DecodeJSONBody(r, &teamRequest); err != nil {
		w.RespondWithError(errors.NewBadRequestError("Invalid request body", err))
		return
	}

	teamRef, err := common.ParseRefString(teamRequest.TeamRef, common.GetResourceNamespace())
	if err != nil {
		log.Error(err, "Failed to parse TeamRef")
		w.RespondWithError(errors.NewBadRequestError("Invalid TeamRef", err))
		return
	}
	if !strings.Contains(teamRequest.TeamRef, "/") {
		log.V(4).Info("No namespace provided in ModelConfigRef, using default namespace",
			"defaultNamespace", teamRef.Namespace)
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

type CreateTeamRequest struct {
	TeamRef string             `json:"teamRef"`
	Spec    v1alpha1.AgentSpec `json:"spec"`
}

// HandleCreateTeam handles POST /api/teams requests
func (h *TeamsHandler) HandleCreateTeam(w ErrorResponseWriter, r *http.Request) {
	log := ctrllog.FromContext(r.Context()).WithName("teams-handler").WithValues("operation", "create")
	log.V(1).Info("Received request to create Team")

	var teamRequest CreateTeamRequest
	if err := DecodeJSONBody(r, &teamRequest); err != nil {
		w.RespondWithError(errors.NewBadRequestError("Invalid request body", err))
		return
	}

	teamRef, err := common.ParseRefString(teamRequest.TeamRef, common.GetResourceNamespace())
	if err != nil {
		log.Error(err, "Failed to parse TeamRef")
		w.RespondWithError(errors.NewBadRequestError("Invalid TeamRef", err))
		return
	}
	if teamRef.Namespace == common.GetResourceNamespace() {
		log.V(4).Info("Namespace not provided in request. Creating in", teamRef.Namespace, "namespace")
	}

	log = log.WithValues(
		"teamNamespace", teamRef.Namespace,
		"teamName", teamRef.Name,
	)

	team := &v1alpha1.Agent{
		ObjectMeta: v1.ObjectMeta{
			Name:      teamRef.Name,
			Namespace: teamRef.Namespace,
		},
		Spec: teamRequest.Spec,
	}

	kubeClientWrapper := client_wrapper.NewKubeClientWrapper(h.KubeClient)
	kubeClientWrapper.AddInMemory(team)

	apiTranslator := autogen.NewAutogenApiTranslator(
		kubeClientWrapper,
		h.DefaultModelConfig,
	)

	log.V(1).Info("Translating Team to Autogen format")
	autogenTeam, err := apiTranslator.TranslateGroupChatForAgent(r.Context(), team)
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
	if err := h.KubeClient.Create(r.Context(), team); err != nil {
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
		w.RespondWithError(errors.NewInternalServerError("Failed to get ModelConfig", err))
		return
	}

	// Create a new object that contains the Team information from Team and the ID from the autogenTeam
	teamWithID := &TeamResponse{
		Id:            autogenTeam.Id,
		Agent:         *team,
		Component:     autogenTeam.Component,
		ModelProvider: modelConfig.Spec.Provider,
		Model:         modelConfig.Spec.Model,
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
