package handlers

import (
	"net/http"
	"strings"

	"github.com/kagent-dev/kagent/go/controller/api/v1alpha1"
	"github.com/kagent-dev/kagent/go/controller/internal/autogen"
	"github.com/kagent-dev/kagent/go/controller/internal/client_wrapper"
	"k8s.io/apimachinery/pkg/types"

	autogen_client "github.com/kagent-dev/kagent/go/autogen/client"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"
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

func convertToKubernetesIdentifier(name string) string {
	return strings.ReplaceAll(name, "_", "-")
}

// HandleListTeams handles GET /api/teams requests
func (h *TeamsHandler) HandleListTeams(w http.ResponseWriter, r *http.Request) {
	userID, err := GetUserID(r)
	if err != nil {
		RespondWithError(w, http.StatusBadRequest, err.Error())
		return
	}

	agentList := &v1alpha1.AgentList{}
	if err := h.KubeClient.List(r.Context(), agentList); err != nil {
		RespondWithError(w, http.StatusInternalServerError, err.Error())
		return
	}

	teamsWithID := make([]map[string]interface{}, 0)
	for _, team := range agentList.Items {
		autogenTeam, err := h.AutogenClient.GetTeam(convertToKubernetesIdentifier(team.Name), userID)
		if err != nil {
			RespondWithError(w, http.StatusInternalServerError, err.Error())
			return
		}

		if autogenTeam == nil {
			continue
		}

		// Get the model config for the team
		modelConfig := &v1alpha1.ModelConfig{}
		if err := h.KubeClient.Get(r.Context(), types.NamespacedName{
			Name:      team.Spec.ModelConfigRef,
			Namespace: DefaultResourceNamespace,
		}, modelConfig); err != nil {
			continue
		}

		if modelConfig == nil {
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

	RespondWithJSON(w, http.StatusOK, teamsWithID)
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
		ctrllog.Log.Error(err, "Failed to get team")
		RespondWithError(w, http.StatusInternalServerError, err.Error())
		return
	}

	// We set the .spec from the incoming request, so
	// we don't have to copy/set any other fields
	existingTeam.Spec = teamRequest.Spec

	if err := h.KubeClient.Update(r.Context(), existingTeam); err != nil {
		ctrllog.Log.Error(err, "Failed to update team")
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
		ctrllog.Log.Info("Invalid team", "errors", validationResp.Errors, "warnings", validationResp.Warnings)

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

// HandleGetTeam handles GET /api/teams/{teamID} requests
func (h *TeamsHandler) HandleGetTeam(w http.ResponseWriter, r *http.Request) {
	userID, err := GetUserID(r)
	if err != nil {
		RespondWithError(w, http.StatusBadRequest, err.Error())
		return
	}

	teamID, err := GetIntPathParam(r, "teamID")
	if err != nil {
		RespondWithError(w, http.StatusBadRequest, err.Error())
		return
	}

	autogenTeam, err := h.AutogenClient.GetTeamByID(teamID, userID)
	if err != nil {
		RespondWithError(w, http.StatusInternalServerError, err.Error())
		return
	}

	teamLabel := convertToKubernetesIdentifier(*autogenTeam.Component.Label)

	team := &v1alpha1.Agent{}
	if err := h.KubeClient.Get(r.Context(), types.NamespacedName{
		Name:      teamLabel,
		Namespace: DefaultResourceNamespace,
	}, team); err != nil {
		RespondWithError(w, http.StatusNotFound, "Team not found")
		return
	}

	// Get the model config for the team
	modelConfig := &v1alpha1.ModelConfig{}
	if err := h.KubeClient.Get(r.Context(), types.NamespacedName{
		Name:      team.Spec.ModelConfigRef,
		Namespace: DefaultResourceNamespace,
	}, modelConfig); err != nil {
		RespondWithError(w, http.StatusInternalServerError, err.Error())
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

	RespondWithJSON(w, http.StatusOK, teamWithID)
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
