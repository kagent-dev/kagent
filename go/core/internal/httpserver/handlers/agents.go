package handlers

import (
	"context"
	"net/http"

	"github.com/go-logr/logr"
	api "github.com/kagent-dev/kagent/go/api/httpapi"
	"github.com/kagent-dev/kagent/go/api/v1alpha2"
	"github.com/kagent-dev/kagent/go/core/internal/controller/reconciler"
	agent_translator "github.com/kagent-dev/kagent/go/core/internal/controller/translator/agent"
	"github.com/kagent-dev/kagent/go/core/internal/httpserver/errors"
	"github.com/kagent-dev/kagent/go/core/internal/utils"
	"github.com/kagent-dev/kagent/go/core/pkg/auth"
	"github.com/kagent-dev/kagent/go/core/pkg/sandboxbackend"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
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

	sandboxAgentList := &v1alpha2.SandboxAgentList{}
	if err := h.KubeClient.List(r.Context(), sandboxAgentList); err != nil {
		w.RespondWithError(errors.NewInternalServerError("Failed to list SandboxAgents from Kubernetes", err))
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

	for _, sa := range sandboxAgentList.Items {
		agentRef := utils.GetObjectRef(&sa)
		log.V(1).Info("Processing SandboxAgent", "agentRef", agentRef)
		agentResponse, _ := h.getSandboxAgentResponse(r.Context(), log, &sa)
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
		if condition.Type == "Ready" && condition.Status == "True" {
			if condition.Reason == reconciler.AgentReadyReasonDeploymentReady || condition.Reason == reconciler.AgentReadyReasonWorkloadReady {
				deploymentReady = true
				break
			}
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
			if apierrors.IsNotFound(err) {
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

func (h *AgentsHandler) getSandboxAgentResponse(ctx context.Context, log logr.Logger, sa *v1alpha2.SandboxAgent) (api.AgentResponse, error) {
	agentView := agent_translator.AgentViewFromSandboxAgent(sa)
	resp, err := h.getAgentResponse(ctx, log, agentView)
	if err != nil {
		return resp, err
	}
	resp.RunInSandbox = true
	return resp, nil
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
	err = h.KubeClient.Get(
		r.Context(),
		client.ObjectKey{
			Namespace: agentNamespace,
			Name:      agentName,
		},
		agent,
	)
	if err != nil {
		if apierrors.IsNotFound(err) {
			sa := &v1alpha2.SandboxAgent{}
			if err2 := h.KubeClient.Get(r.Context(), client.ObjectKey{Namespace: agentNamespace, Name: agentName}, sa); err2 != nil {
				w.RespondWithError(errors.NewNotFoundError("Agent not found", err2))
				return
			}
			agentResponse, err3 := h.getSandboxAgentResponse(r.Context(), log, sa)
			if err3 != nil {
				w.RespondWithError(err3)
				return
			}
			log.Info("Successfully retrieved sandbox agent")
			data := api.NewResponse(agentResponse, "Successfully retrieved agent", false)
			RespondWithJSON(w, http.StatusOK, data)
			return
		}
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
		return
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

	apiTranslator := agent_translator.NewAdkApiTranslator(
		kubeClientWrapper,
		h.DefaultModelConfig,
		nil,
		h.ProxyURL,
		h.SandboxBackend,
	)

	log.V(1).Info("Translating Agent to ADK format")
	_, err = apiTranslator.TranslateAgent(r.Context(), &agentReq, false)
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
		if apierrors.IsNotFound(err) {
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
		if apierrors.IsNotFound(err) {
			sa := &v1alpha2.SandboxAgent{}
			if err2 := h.KubeClient.Get(r.Context(), client.ObjectKey{Namespace: agentNamespace, Name: agentName}, sa); err2 != nil {
				if apierrors.IsNotFound(err2) {
					log.Info("Agent not found")
					w.RespondWithError(errors.NewNotFoundError("Agent not found", nil))
					return
				}
				log.Error(err2, "Failed to get SandboxAgent")
				w.RespondWithError(errors.NewInternalServerError("Failed to get SandboxAgent", err2))
				return
			}
			log.V(1).Info("Deleting SandboxAgent from Kubernetes")
			if err := h.KubeClient.Delete(r.Context(), sa); err != nil {
				w.RespondWithError(errors.NewInternalServerError("Failed to delete SandboxAgent", err))
				return
			}
			log.Info("Successfully deleted sandbox agent")
			data := api.NewResponse(struct{}{}, "Successfully deleted agent", false)
			RespondWithJSON(w, http.StatusOK, data)
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

func normalizeSandboxAgentForAPI(sa *v1alpha2.SandboxAgent) {
	if sa == nil {
		return
	}
	if sa.Spec.Type == "" {
		sa.Spec.Type = v1alpha2.AgentType_Declarative
	}
}

// HandleCreateSandboxAgent handles POST /api/sandboxagents requests.
func (h *AgentsHandler) HandleCreateSandboxAgent(w ErrorResponseWriter, r *http.Request) {
	log := ctrllog.FromContext(r.Context()).WithName("agents-handler").WithValues("operation", "create-sandboxagent")

	var saReq v1alpha2.SandboxAgent
	if err := DecodeJSONBody(r, &saReq); err != nil {
		w.RespondWithError(errors.NewBadRequestError("Invalid request body", err))
		return
	}
	normalizeSandboxAgentForAPI(&saReq)
	if saReq.Namespace == "" {
		saReq.Namespace = utils.GetResourceNamespace()
		log.V(4).Info("Namespace not provided in request. Creating in controller installation namespace",
			"namespace", saReq.Namespace)
	}
	agentRef, err := utils.ParseRefString(saReq.Name, saReq.Namespace)
	if err != nil {
		w.RespondWithError(errors.NewBadRequestError("Invalid sandboxagent metadata", err))
		return
	}

	log = log.WithValues(
		"agentNamespace", agentRef.Namespace,
		"agentName", agentRef.Name,
	)

	if err := Check(h.Authorizer, r, auth.Resource{Type: "Agent", Name: agentRef.String()}); err != nil {
		w.RespondWithError(err)
		return
	}

	if h.SandboxBackend != nil {
		if err := sandboxbackend.EnsureAgentSandboxAPIsRegistered(r.Context(), h.KubeClient); err != nil {
			w.RespondWithError(errors.NewBadRequestError(err.Error(), err))
			return
		}
	}

	kubeClientWrapper := utils.NewKubeClientWrapper(h.KubeClient)
	if err := kubeClientWrapper.AddInMemory(&saReq); err != nil {
		w.RespondWithError(errors.NewInternalServerError("Failed to add SandboxAgent to Kubernetes wrapper", err))
		return
	}

	apiTranslator := agent_translator.NewAdkApiTranslator(
		kubeClientWrapper,
		h.DefaultModelConfig,
		nil,
		h.ProxyURL,
		h.SandboxBackend,
	)

	agentView := agent_translator.AgentViewFromSandboxAgent(&saReq)
	log.V(1).Info("Translating SandboxAgent to ADK format")
	if _, err := apiTranslator.TranslateAgent(r.Context(), agentView, true); err != nil {
		w.RespondWithError(errors.NewInternalServerError("Failed to translate SandboxAgent to ADK format", err))
		return
	}

	if err := h.KubeClient.Create(r.Context(), &saReq); err != nil {
		w.RespondWithError(errors.NewInternalServerError("Failed to create SandboxAgent in Kubernetes", err))
		return
	}

	agentResponse, err := h.getSandboxAgentResponse(r.Context(), log, &saReq)
	if err != nil {
		w.RespondWithError(err)
		return
	}

	log.Info("Successfully created sandbox agent", "agentRef", agentRef)
	data := api.NewResponse(agentResponse, "Successfully created sandbox agent", false)
	RespondWithJSON(w, http.StatusCreated, data)
}

// HandleUpdateSandboxAgent handles PUT /api/sandboxagents/{namespace}/{name} requests.
func (h *AgentsHandler) HandleUpdateSandboxAgent(w ErrorResponseWriter, r *http.Request) {
	log := ctrllog.FromContext(r.Context()).WithName("agents-handler").WithValues("operation", "update-sandboxagent")

	var saReq v1alpha2.SandboxAgent
	if err := DecodeJSONBody(r, &saReq); err != nil {
		w.RespondWithError(errors.NewBadRequestError("Invalid request body", err))
		return
	}
	normalizeSandboxAgentForAPI(&saReq)
	if saReq.Namespace == "" {
		saReq.Namespace = utils.GetResourceNamespace()
	}
	agentRef, err := utils.ParseRefString(saReq.Name, saReq.Namespace)
	if err != nil {
		w.RespondWithError(errors.NewBadRequestError("Invalid SandboxAgent metadata", err))
		return
	}

	agentNamespace, err := GetPathParam(r, "namespace")
	if err != nil {
		w.RespondWithError(errors.NewBadRequestError("Failed to get namespace from path", err))
		return
	}
	agentName, err := GetPathParam(r, "name")
	if err != nil {
		w.RespondWithError(errors.NewBadRequestError("Failed to get name from path", err))
		return
	}

	if agentRef.Namespace != agentNamespace || agentRef.Name != agentName {
		w.RespondWithError(errors.NewBadRequestError("Path does not match request body metadata", nil))
		return
	}

	if err := Check(h.Authorizer, r, auth.Resource{Type: "Agent", Name: agentRef.String()}); err != nil {
		w.RespondWithError(err)
		return
	}

	if h.SandboxBackend != nil {
		if err := sandboxbackend.EnsureAgentSandboxAPIsRegistered(r.Context(), h.KubeClient); err != nil {
			w.RespondWithError(errors.NewBadRequestError(err.Error(), err))
			return
		}
	}

	existing := &v1alpha2.SandboxAgent{}
	if err := h.KubeClient.Get(r.Context(), agentRef, existing); err != nil {
		if apierrors.IsNotFound(err) {
			w.RespondWithError(errors.NewNotFoundError("SandboxAgent not found", nil))
			return
		}
		w.RespondWithError(errors.NewInternalServerError("Failed to get SandboxAgent", err))
		return
	}

	existing.Spec = saReq.Spec

	kubeClientWrapper := utils.NewKubeClientWrapper(h.KubeClient)
	if err := kubeClientWrapper.AddInMemory(existing); err != nil {
		w.RespondWithError(errors.NewInternalServerError("Failed to add SandboxAgent to Kubernetes wrapper", err))
		return
	}

	apiTranslator := agent_translator.NewAdkApiTranslator(
		kubeClientWrapper,
		h.DefaultModelConfig,
		nil,
		h.ProxyURL,
		h.SandboxBackend,
	)
	agentView := agent_translator.AgentViewFromSandboxAgent(existing)
	if _, err := apiTranslator.TranslateAgent(r.Context(), agentView, true); err != nil {
		w.RespondWithError(errors.NewInternalServerError("Failed to translate SandboxAgent to ADK format", err))
		return
	}

	if err := h.KubeClient.Update(r.Context(), existing); err != nil {
		w.RespondWithError(errors.NewInternalServerError("Failed to update SandboxAgent", err))
		return
	}

	agentResponse, err := h.getSandboxAgentResponse(r.Context(), log, existing)
	if err != nil {
		w.RespondWithError(err)
		return
	}

	log.Info("Successfully updated sandbox agent", "agentRef", agentRef)
	data := api.NewResponse(agentResponse, "Successfully updated sandbox agent", false)
	RespondWithJSON(w, http.StatusOK, data)
}
