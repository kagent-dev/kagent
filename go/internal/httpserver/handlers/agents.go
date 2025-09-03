package handlers

import (
	"context"
	"net/http"

	"github.com/go-logr/logr"
	"github.com/kagent-dev/kagent/go/api/v1alpha2"
	"github.com/kagent-dev/kagent/go/internal/controller/translator"
	"github.com/kagent-dev/kagent/go/internal/httpserver/errors"
	"github.com/kagent-dev/kagent/go/internal/utils"
	"github.com/kagent-dev/kagent/go/pkg/auth"
	"github.com/kagent-dev/kagent/go/pkg/client/api"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
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

	if err := Check(h.Authorizer, r, auth.Resource{Type: "Agent"}); err != nil {
		w.RespondWithError(err)
		return
	}

	agentList := &v1alpha2.AgentList{}
	if err := h.KubeClient.List(r.Context(), agentList); err != nil {
		w.RespondWithError(errors.NewInternalServerError("Failed to list Agents from Kubernetes", err))
		return
	}

	agentsWithID := make([]api.AgentResponse, 0)
	for _, agent := range agentList.Items {
		agentRef := utils.GetObjectRef(&agent)
		log.V(1).Info("Processing Agent", "agentRef", agentRef)

		// When listing agents, we don't want a failure when a single agent has an issue, so we ignore the error.
		// The getAgentResponse should return its reconciliation status in the agentResponse.
		agentResponse, _ := h.getAgentResponse(r.Context(), log, &agent)

		agentsWithID = append(agentsWithID, agentResponse)
	}

	log.Info("Successfully listed agents", "count", len(agentsWithID))
	data := api.NewResponse(agentsWithID, "Successfully listed agents", false)
	RespondWithJSON(w, http.StatusOK, data)
}

func (h *AgentsHandler) getAgentResponse(ctx context.Context, log logr.Logger, agent *v1alpha2.Agent) (api.AgentResponse, error) {

	agentRef := utils.GetObjectRef(agent)
	log.V(1).Info("Processing Agent", "agentRef", agentRef)

	deploymentReady := false
	for _, condition := range agent.Status.Conditions {
		if condition.Type == "Ready" && condition.Reason == "DeploymentReady" && condition.Status == "True" {
			deploymentReady = true
			break
		}
	}

	accepted := false
	for _, condition := range agent.Status.Conditions {
		// The exact reason is not important (although "AgentReconciled" is the current one), as long as the agent is accepted
		if condition.Type == "Accepted" && condition.Status == "True" {
			accepted = true
			break
		}
	}

	response := api.AgentResponse{
		ID:              utils.ConvertToPythonIdentifier(agentRef),
		Agent:           agent,
		DeploymentReady: deploymentReady,
		Accepted:        accepted,
	}

	if agent.Spec.Type == v1alpha2.AgentType_Declarative {
		// Get the ModelConfig for the team
		modelConfig := &v1alpha2.ModelConfig{}
		objKey := client.ObjectKey{
			Namespace: agent.Namespace,
			Name:      agent.Spec.Declarative.ModelConfig,
		}
		if err := h.KubeClient.Get(
			ctx,
			objKey,
			modelConfig,
		); err != nil {
			if k8serrors.IsNotFound(err) {
				log.V(1).Info("ModelConfig not found", "modelConfigRef", objKey)
			} else {
				log.Error(err, "Failed to get ModelConfig", "modelConfigRef", objKey)
			}
			return response, err
		}
		response.ModelProvider = modelConfig.Spec.Provider
		response.Model = modelConfig.Spec.Model
		response.ModelConfigRef = utils.GetObjectRef(modelConfig)
		response.Tools = agent.Spec.Declarative.Tools
	}

	return response, nil
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

	if err := Check(h.Authorizer, r, auth.Resource{Type: "Agent", Name: types.NamespacedName{Namespace: agentNamespace, Name: agentName}.String()}); err != nil {
		w.RespondWithError(err)
		return
	}
	agent := &v1alpha2.Agent{}
	if err := h.KubeClient.Get(
		r.Context(),
		client.ObjectKey{
			Namespace: agentNamespace,
			Name:      agentName,
		},
		agent,
	); err != nil {
		w.RespondWithError(errors.NewNotFoundError("Agent not found", err))
		return
	}

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

	var agentReq v1alpha2.Agent
	if err := DecodeJSONBody(r, &agentReq); err != nil {
		w.RespondWithError(errors.NewBadRequestError("Invalid request body", err))
		return
	}
	if agentReq.Namespace == "" {
		agentReq.Namespace = utils.GetResourceNamespace()
		log.V(4).Info("Namespace not provided in request. Creating in controller installation namespace",
			"namespace", agentReq.Namespace)
	}
	agentRef, err := utils.ParseRefString(agentReq.Name, agentReq.Namespace)
	if err != nil {
		w.RespondWithError(errors.NewBadRequestError("Invalid agent metadata", err))
	}

	log = log.WithValues(
		"agentNamespace", agentRef.Namespace,
		"agentName", agentRef.Name,
	)

	if err := Check(h.Authorizer, r, auth.Resource{Type: "Agent", Name: agentRef.String()}); err != nil {
		w.RespondWithError(err)
		return
	}

	kubeClientWrapper := utils.NewKubeClientWrapper(h.KubeClient)
	if err := kubeClientWrapper.AddInMemory(&agentReq); err != nil {
		w.RespondWithError(errors.NewInternalServerError("Failed to add Agent to Kubernetes wrapper", err))
		return
	}

	apiTranslator := translator.NewAdkApiTranslator(
		kubeClientWrapper,
		h.DefaultModelConfig,
		nil,
	)

	log.V(1).Info("Translating Agent to ADK format")
	_, err = apiTranslator.TranslateAgent(r.Context(), &agentReq)
	if err != nil {
		w.RespondWithError(errors.NewInternalServerError("Failed to translate Agent to ADK format", err))
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

	var agentReq v1alpha2.Agent
	if err := DecodeJSONBody(r, &agentReq); err != nil {
		w.RespondWithError(errors.NewBadRequestError("Invalid request body", err))
		return
	}

	if agentReq.Namespace == "" {
		agentReq.Namespace = utils.GetResourceNamespace()
		log.V(4).Info("Namespace not provided in request. Creating in controller installation namespace",
			"namespace", agentReq.Namespace)
	}
	agentRef, err := utils.ParseRefString(agentReq.Name, agentReq.Namespace)
	if err != nil {
		w.RespondWithError(errors.NewBadRequestError("Invalid Agent metadata", err))
	}

	log = log.WithValues(
		"agentNamespace", agentRef.Namespace,
		"agentName", agentRef.Name,
	)

	if err := Check(h.Authorizer, r, auth.Resource{Type: "Agent", Name: agentRef.String()}); err != nil {
		w.RespondWithError(err)
		return
	}

	log.V(1).Info("Getting existing Agent")
	existingAgent := &v1alpha2.Agent{}
	err = h.KubeClient.Get(
		r.Context(),
		agentRef,
		existingAgent,
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

	if err := Check(h.Authorizer, r, auth.Resource{Type: "Agent", Name: types.NamespacedName{Namespace: agentNamespace, Name: agentName}.String()}); err != nil {
		w.RespondWithError(err)
		return
	}

	log = log.WithValues("agentNamespace", agentNamespace)

	log.V(1).Info("Getting Agent from Kubernetes")
	agent := &v1alpha2.Agent{}
	err = h.KubeClient.Get(
		r.Context(),
		client.ObjectKey{
			Namespace: agentNamespace,
			Name:      agentName,
		},
		agent,
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
