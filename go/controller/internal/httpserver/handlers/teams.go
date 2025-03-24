package handlers

import (
	"net/http"
	"strconv"

	"github.com/kagent-dev/kagent/go/controller/api/v1alpha1"
	"github.com/kagent-dev/kagent/go/controller/internal/autogen"
	"github.com/kagent-dev/kagent/go/controller/internal/client_wrapper"
	"k8s.io/apimachinery/pkg/types"

	autogen_client "github.com/kagent-dev/kagent/go/autogen/client"
)

// DefaultResourceNamespace is the default namespace for resources
const DefaultResourceNamespace = "kagent"

// TeamsHandler handles team-related requests
type TeamsHandler struct {
	*Base
}

// NewTeamsHandler creates a new TeamsHandler
func NewTeamsHandler(base *Base) *TeamsHandler {
	return &TeamsHandler{Base: base}
}

// HandleListTeams handles GET /api/teams requests
func (h *TeamsHandler) HandleListTeams(w http.ResponseWriter, r *http.Request) {
	userID, err := GetUserID(r)
	if err != nil {
		RespondWithError(w, http.StatusBadRequest, err.Error())
		return
	}

	teams, err := h.AutogenClient.ListTeams(userID)
	if err != nil {
		RespondWithError(w, http.StatusInternalServerError, err.Error())
		return
	}

	RespondWithJSON(w, http.StatusOK, teams)
}

// HandleUpdateTeam handles PUT /api/teams requests
func (h *TeamsHandler) HandleUpdateTeam(w http.ResponseWriter, r *http.Request) {
	var teamRequest *v1alpha1.Agent

	if err := DecodeJSONBody(r, &teamRequest); err != nil {
		RespondWithError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	existingTeam := &v1alpha1.Agent{}
	if err := h.KubeClient.Get(r.Context(), types.NamespacedName{
		Name:      teamRequest.Name,
		Namespace: DefaultResourceNamespace,
	}, existingTeam); err != nil {
		RespondWithError(w, http.StatusInternalServerError, err.Error())
		return
	}

	// The incoming request doesn't have a namespace set, so we
	// use the namespace from the existing resource
	teamRequest.SetNamespace(existingTeam.GetNamespace())

	// We set the .spec from the incoming request, so
	// we don't have to copy/set any other fields
	existingTeam.Spec = teamRequest.Spec

	if err := h.KubeClient.Update(r.Context(), existingTeam); err != nil {
		RespondWithError(w, http.StatusInternalServerError, err.Error())
		return
	}

	RespondWithJSON(w, http.StatusOK, teamRequest)
}

// HandleCreateTeam handles POST /api/teams requests
func (h *TeamsHandler) HandleCreateTeam(w http.ResponseWriter, r *http.Request) {
	var teamRequest *v1alpha1.Agent

	if err := DecodeJSONBody(r, &teamRequest); err != nil {
		RespondWithError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	// Default to kagent namespace
	teamRequest.Namespace = DefaultResourceNamespace

	kubeClientWrapper := client_wrapper.NewKubeClientWrapper(h.KubeClient)
	kubeClientWrapper.AddInMemory(teamRequest)

	apiTranslator := autogen.NewAutogenApiTranslator(
		kubeClientWrapper,
		h.DefaultModelConfig,
	)

	autogenTeam, err := apiTranslator.TranslateGroupChatForAgent(r.Context(), teamRequest)
	if err != nil {
		RespondWithError(w, http.StatusInternalServerError, err.Error())
		return
	}

	validateReq := autogen_client.ValidationRequest{
		Component: autogenTeam.Component,
	}

	// Validate the team
	validationResp, err := h.AutogenClient.Validate(&validateReq)
	if err != nil {
		RespondWithError(w, http.StatusNotAcceptable, err.Error())
		return
	}

	if !validationResp.IsValid {
		RespondWithError(w, http.StatusNotAcceptable, "Invalid team")
		return
	}

	// Team is valid, we can store it
	if err := h.KubeClient.Create(r.Context(), teamRequest); err != nil {
		RespondWithError(w, http.StatusInternalServerError, err.Error())
		return
	}

	RespondWithJSON(w, http.StatusCreated, teamRequest)
}

// HandleGetTeam handles GET /api/teams/{teamLabel} requests
func (h *TeamsHandler) HandleGetTeam(w http.ResponseWriter, r *http.Request) {
	teamLabel, err := GetPathParam(r, "teamLabel")
	if err != nil {
		RespondWithError(w, http.StatusBadRequest, err.Error())
		return
	}

	userID, err := GetUserID(r)
	if err != nil {
		RespondWithError(w, http.StatusBadRequest, err.Error())
		return
	}

	teamID, err := strconv.Atoi(teamLabel)
	if err != nil {
		RespondWithError(w, http.StatusBadRequest, "Invalid team_label, must be an integer")
		return
	}

	team, err := h.AutogenClient.GetTeamByID(teamID, userID)
	if err != nil {
		RespondWithError(w, http.StatusInternalServerError, err.Error())
		return
	}

	RespondWithJSON(w, http.StatusOK, team)
}

// HandleDeleteTeam handles DELETE /api/teams/{teamLabel} requests
func (h *TeamsHandler) HandleDeleteTeam(w http.ResponseWriter, r *http.Request) {
	teamLabel, err := GetPathParam(r, "teamLabel")
	if err != nil {
		RespondWithError(w, http.StatusBadRequest, err.Error())
		return
	}

	team := &v1alpha1.Agent{}
	if err := h.KubeClient.Get(r.Context(), types.NamespacedName{
		Name:      teamLabel,
		Namespace: DefaultResourceNamespace,
	}, team); err != nil {
		RespondWithError(w, http.StatusNotFound, "Team not found")
		return
	}

	if err := h.KubeClient.Delete(r.Context(), team); err != nil {
		RespondWithError(w, http.StatusInternalServerError, err.Error())
		return
	}

	w.WriteHeader(http.StatusNoContent)
}
