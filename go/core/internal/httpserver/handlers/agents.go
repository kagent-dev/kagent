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

	agentsWithID := make([]api.AgentResponse, 0)
	h.appendAgentResponses(r.Context(), log, agentObjects(agentList.Items), &agentsWithID)

	log.Info("Successfully listed agents", "count", len(agentsWithID))
	data := api.NewResponse(agentsWithID, "Successfully listed agents", false)
	RespondWithJSON(w, http.StatusOK, data)
}

// HandleListSandboxAgents handles GET /api/sandboxagents requests using database.
func (h *AgentsHandler) HandleListSandboxAgents(w ErrorResponseWriter, r *http.Request) {
	log := ctrllog.FromContext(r.Context()).WithName("agents-handler").WithValues("operation", "list-sandboxagents")

	if err := Check(h.Authorizer, r, auth.Resource{Type: "Agent"}); err != nil {
		w.RespondWithError(err)
		return
	}

	sandboxAgentList := &v1alpha2.SandboxAgentList{}
	if err := h.KubeClient.List(r.Context(), sandboxAgentList); err != nil {
		w.RespondWithError(errors.NewInternalServerError("Failed to list SandboxAgents from Kubernetes", err))
		return
	}

	agentsWithID := make([]api.AgentResponse, 0)
	h.appendAgentResponses(r.Context(), log, sandboxAgentObjects(sandboxAgentList.Items), &agentsWithID)

	log.Info("Successfully listed sandbox agents", "count", len(agentsWithID))
	data := api.NewResponse(agentsWithID, "Successfully listed sandbox agents", false)
	RespondWithJSON(w, http.StatusOK, data)
}

func (h *AgentsHandler) appendAgentResponses(
	ctx context.Context,
	log logr.Logger,
	items []v1alpha2.AgentObject,
	responses *[]api.AgentResponse,
) {
	for _, agent := range items {
		agentRef := utils.GetObjectRef(agent)
		log.V(1).Info("Processing agent", "agentRef", agentRef)

		agentResponse, _ := h.getAgentResponse(ctx, log, agent)
		*responses = append(*responses, agentResponse)
	}
}

func agentObjects(items []v1alpha2.Agent) []v1alpha2.AgentObject {
	out := make([]v1alpha2.AgentObject, 0, len(items))
	for i := range items {
		out = append(out, &items[i])
	}
	return out
}

func sandboxAgentObjects(items []v1alpha2.SandboxAgent) []v1alpha2.AgentObject {
	out := make([]v1alpha2.AgentObject, 0, len(items))
	for i := range items {
		out = append(out, &items[i])
	}
	return out
}

func (h *AgentsHandler) getAgentResponse(ctx context.Context, log logr.Logger, agent v1alpha2.AgentObject) (api.AgentResponse, error) {
	agentRef := utils.GetObjectRef(agent)
	log.V(1).Info("Processing Agent", "agentRef", agentRef)
	spec := agent.GetAgentSpec()
	status := agent.GetAgentStatus()

	deploymentReady := false
	for _, condition := range status.Conditions {
		if condition.Type == "Ready" && condition.Status == "True" {
			if condition.Reason == reconciler.AgentReadyReasonDeploymentReady || condition.Reason == reconciler.AgentReadyReasonWorkloadReady {
				deploymentReady = true
				break
			}
		}
	}

	accepted := false
	for _, condition := range status.Conditions {
		// The exact reason is not important (although "AgentReconciled" is the current one), as long as the agent is accepted
		if condition.Type == "Accepted" && condition.Status == "True" {
			accepted = true
			break
		}
	}
	workloadMode, err := h.resolveWorkloadMode(ctx, agent)
	if err != nil {
		return api.AgentResponse{}, err
	}

	response := api.AgentResponse{
		ID:              utils.ConvertToPythonIdentifier(agentRef),
		Agent:           api.AgentResourceFrom(agent),
		DeploymentReady: deploymentReady,
		Accepted:        accepted,
		WorkloadMode:    workloadMode,
	}

	if spec.Type == v1alpha2.AgentType_Declarative && spec.Declarative != nil {
		// Get the ModelConfig for the team
		modelConfig := &v1alpha2.ModelConfig{}
		objKey := client.ObjectKey{
			Namespace: agent.GetNamespace(),
			Name:      spec.Declarative.ModelConfig,
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
		response.Tools = spec.Declarative.Tools
	}

	return response, nil
}

func (h *AgentsHandler) buildTranslator(kubeClient client.Client) agent_translator.AdkApiTranslator {
	return agent_translator.NewAdkApiTranslatorWithWatchedNamespacesAndWorkloadModeResolver(
		kubeClient,
		h.WatchedNamespaces,
		h.DefaultModelConfig,
		nil,
		h.ProxyURL,
		h.SandboxBackend,
		h.WorkloadModeResolver,
	)
}

func (h *AgentsHandler) validateAgentObject(ctx context.Context, agent v1alpha2.AgentObject) error {
	workloadMode, err := h.resolveWorkloadMode(ctx, agent)
	if err != nil {
		return errors.NewBadRequestError("Invalid agent workload mode", err)
	}
	if workloadMode == v1alpha2.WorkloadModeSandbox && h.SandboxBackend != nil {
		if err := h.SandboxBackend.EnsureAPIsRegistered(ctx, h.KubeClient); err != nil {
			return errors.NewBadRequestError(err.Error(), err)
		}
	}

	kubeClientWrapper := utils.NewKubeClientWrapper(h.KubeClient)
	if err := kubeClientWrapper.AddInMemory(agent); err != nil {
		return errors.NewInternalServerError("Failed to add Agent to Kubernetes wrapper", err)
	}

	apiTranslator := h.buildTranslator(kubeClientWrapper)
	inputs, err := apiTranslator.CompileAgent(ctx, agent)
	if err != nil {
		return errors.NewBadRequestError("Invalid agent configuration", err)
	}
	if _, err := apiTranslator.BuildManifest(ctx, agent, inputs); err != nil {
		return errors.NewBadRequestError("Invalid agent configuration", err)
	}

	return nil
}

func (h *AgentsHandler) resolveWorkloadMode(ctx context.Context, agent v1alpha2.AgentObject) (v1alpha2.WorkloadMode, error) {
	if h.WorkloadModeResolver != nil {
		mode, ok, err := h.WorkloadModeResolver.ResolveWorkloadMode(ctx, agent)
		if err != nil {
			return "", err
		}
		if ok {
			return mode, nil
		}
	}
	return agent.GetWorkloadMode(), nil
}

func (h *AgentsHandler) parseAgentRef(log logr.Logger, agent client.Object, invalidMsg string) (logr.Logger, types.NamespacedName, error) {
	if agent.GetNamespace() == "" {
		agent.SetNamespace(utils.GetResourceNamespace())
		log.V(4).Info("Namespace not provided in request. Creating in controller installation namespace",
			"namespace", agent.GetNamespace())
	}
	agentRef, err := utils.ParseRefString(agent.GetName(), agent.GetNamespace())
	if err != nil {
		return log, types.NamespacedName{}, errors.NewBadRequestError(invalidMsg, err)
	}

	return log.WithValues(
		"agentNamespace", agentRef.Namespace,
		"agentName", agentRef.Name,
	), agentRef, nil
}

func (h *AgentsHandler) getAgentObject(
	ctx context.Context,
	key client.ObjectKey,
	agent v1alpha2.AgentObject,
	notFoundMsg string,
) (v1alpha2.AgentObject, error) {
	if err := h.KubeClient.Get(ctx, key, agent); err != nil {
		if apierrors.IsNotFound(err) {
			return nil, errors.NewNotFoundError(notFoundMsg, err)
		}
		return nil, errors.NewInternalServerError("Failed to get Agent", err)
	}
	return agent, nil
}

func (h *AgentsHandler) handleGetAgentObject(
	w ErrorResponseWriter,
	r *http.Request,
	log logr.Logger,
	agent v1alpha2.AgentObject,
	notFoundMsg string,
	successMessage string,
) {
	agentName, err := GetPathParam(r, "name")
	if err != nil {
		w.RespondWithError(errors.NewBadRequestError("Failed to get name from path", err))
		return
	}
	agentNamespace, err := GetPathParam(r, "namespace")
	if err != nil {
		w.RespondWithError(errors.NewBadRequestError("Failed to get namespace from path", err))
		return
	}
	log = log.WithValues("agentName", agentName, "agentNamespace", agentNamespace)

	if err := Check(h.Authorizer, r, auth.Resource{Type: "Agent", Name: types.NamespacedName{Namespace: agentNamespace, Name: agentName}.String()}); err != nil {
		w.RespondWithError(err)
		return
	}

	obj, err := h.getAgentObject(r.Context(), client.ObjectKey{Namespace: agentNamespace, Name: agentName}, agent, notFoundMsg)
	if err != nil {
		w.RespondWithError(err)
		return
	}

	agentResponse, err := h.getAgentResponse(r.Context(), log, obj)
	if err != nil {
		w.RespondWithError(err)
		return
	}

	log.Info(successMessage)
	RespondWithJSON(w, http.StatusOK, api.NewResponse(agentResponse, successMessage, false))
}

func (h *AgentsHandler) handleDeleteAgentObject(
	w ErrorResponseWriter,
	r *http.Request,
	log logr.Logger,
	agent v1alpha2.AgentObject,
	notFoundMsg string,
	getFailedMsg string,
	deleteFailedMsg string,
	successMessage string,
) {
	agentName, err := GetPathParam(r, "name")
	if err != nil {
		w.RespondWithError(errors.NewBadRequestError("Failed to get name from path", err))
		return
	}
	agentNamespace, err := GetPathParam(r, "namespace")
	if err != nil {
		w.RespondWithError(errors.NewBadRequestError("Failed to get namespace from path", err))
		return
	}
	log = log.WithValues("agentName", agentName, "agentNamespace", agentNamespace)

	if err := Check(h.Authorizer, r, auth.Resource{Type: "Agent", Name: types.NamespacedName{Namespace: agentNamespace, Name: agentName}.String()}); err != nil {
		w.RespondWithError(err)
		return
	}

	if err := h.KubeClient.Get(r.Context(), client.ObjectKey{Namespace: agentNamespace, Name: agentName}, agent); err != nil {
		if apierrors.IsNotFound(err) {
			w.RespondWithError(errors.NewNotFoundError(notFoundMsg, nil))
			return
		}
		w.RespondWithError(errors.NewInternalServerError(getFailedMsg, err))
		return
	}

	if err := h.KubeClient.Delete(r.Context(), agent); err != nil {
		w.RespondWithError(errors.NewInternalServerError(deleteFailedMsg, err))
		return
	}

	log.Info(successMessage)
	RespondWithJSON(w, http.StatusOK, api.NewResponse(struct{}{}, successMessage, false))
}

func (h *AgentsHandler) authorizeAgentRequest(w ErrorResponseWriter, r *http.Request, agentRef types.NamespacedName) bool {
	if err := Check(h.Authorizer, r, auth.Resource{Type: "Agent", Name: agentRef.String()}); err != nil {
		w.RespondWithError(err)
		return false
	}
	return true
}

func respondWithObjectResponse[T any](
	w ErrorResponseWriter,
	status int,
	data T,
	message string,
) {
	RespondWithJSON(w, status, api.NewResponse(data, message, false))
}

func (h *AgentsHandler) handleCreateAgentObject(
	w ErrorResponseWriter,
	r *http.Request,
	log logr.Logger,
	agent v1alpha2.AgentObject,
	invalidMetadataMsg string,
	successMessage string,
	normalize func(v1alpha2.AgentObject),
	responseData func(context.Context, logr.Logger, v1alpha2.AgentObject) (any, error),
) {
	if err := DecodeJSONBody(r, agent); err != nil {
		w.RespondWithError(errors.NewBadRequestError("Invalid request body", err))
		return
	}
	if normalize != nil {
		normalize(agent)
	}

	var err error
	log, agentRef, wrappedErr := h.parseAgentRef(log, agent, invalidMetadataMsg)
	if wrappedErr != nil {
		w.RespondWithError(wrappedErr)
		return
	}
	if !h.authorizeAgentRequest(w, r, agentRef) {
		return
	}

	if err = h.validateAgentObject(r.Context(), agent); err != nil {
		w.RespondWithError(err)
		return
	}
	if err = h.KubeClient.Create(r.Context(), agent); err != nil {
		w.RespondWithError(errors.NewInternalServerError("Failed to create Agent in Kubernetes", err))
		return
	}

	response, err := responseData(r.Context(), log, agent)
	if err != nil {
		w.RespondWithError(err)
		return
	}

	log.Info(successMessage, "agentRef", agentRef)
	respondWithObjectResponse(w, http.StatusCreated, response, successMessage)
}

func (h *AgentsHandler) handleUpdateAgentObject(
	w ErrorResponseWriter,
	r *http.Request,
	log logr.Logger,
	incoming v1alpha2.AgentObject,
	existing v1alpha2.AgentObject,
	invalidMetadataMsg string,
	getFailedMsg string,
	updateFailedMsg string,
	notFoundMsg string,
	successMessage string,
	normalize func(v1alpha2.AgentObject),
	validatePathMatch bool,
	responseData func(context.Context, logr.Logger, v1alpha2.AgentObject) (any, error),
) {
	if err := DecodeJSONBody(r, incoming); err != nil {
		w.RespondWithError(errors.NewBadRequestError("Invalid request body", err))
		return
	}
	if normalize != nil {
		normalize(incoming)
	}

	log, agentRef, wrappedErr := h.parseAgentRef(log, incoming, invalidMetadataMsg)
	if wrappedErr != nil {
		w.RespondWithError(wrappedErr)
		return
	}

	if validatePathMatch {
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
	}

	if !h.authorizeAgentRequest(w, r, agentRef) {
		return
	}

	if err := h.KubeClient.Get(r.Context(), agentRef, existing); err != nil {
		if apierrors.IsNotFound(err) {
			w.RespondWithError(errors.NewNotFoundError(notFoundMsg, nil))
			return
		}
		w.RespondWithError(errors.NewInternalServerError(getFailedMsg, err))
		return
	}

	*existing.GetAgentSpec() = *incoming.GetAgentSpec()

	if err := h.validateAgentObject(r.Context(), existing); err != nil {
		w.RespondWithError(err)
		return
	}
	if err := h.KubeClient.Update(r.Context(), existing); err != nil {
		w.RespondWithError(errors.NewInternalServerError(updateFailedMsg, err))
		return
	}

	response, err := responseData(r.Context(), log, existing)
	if err != nil {
		w.RespondWithError(err)
		return
	}

	log.Info(successMessage, "agentRef", agentRef)
	respondWithObjectResponse(w, http.StatusOK, response, successMessage)
}

// HandleGetAgent handles GET /api/agents/{namespace}/{name} requests using database
func (h *AgentsHandler) HandleGetAgent(w ErrorResponseWriter, r *http.Request) {
	log := ctrllog.FromContext(r.Context()).WithName("agents-handler").WithValues("operation", "get-db")
	h.handleGetAgentObject(w, r, log, &v1alpha2.Agent{}, "Agent not found", "Successfully retrieved agent")
}

// HandleGetSandboxAgent handles GET /api/sandboxagents/{namespace}/{name} requests.
func (h *AgentsHandler) HandleGetSandboxAgent(w ErrorResponseWriter, r *http.Request) {
	log := ctrllog.FromContext(r.Context()).WithName("agents-handler").WithValues("operation", "get-sandboxagent")
	h.handleGetAgentObject(w, r, log, &v1alpha2.SandboxAgent{}, "SandboxAgent not found", "Successfully retrieved sandbox agent")
}

// HandleCreateAgent handles POST /api/agents requests using database
func (h *AgentsHandler) HandleCreateAgent(w ErrorResponseWriter, r *http.Request) {
	log := ctrllog.FromContext(r.Context()).WithName("agents-handler").WithValues("operation", "create-db")
	h.handleCreateAgentObject(
		w,
		r,
		log,
		&v1alpha2.Agent{},
		"Invalid agent metadata",
		"Successfully created agent",
		nil,
		func(_ context.Context, _ logr.Logger, agent v1alpha2.AgentObject) (any, error) {
			return agent, nil
		},
	)
}

// HandleUpdateAgent handles PUT /api/agents/{namespace}/{name} requests using database
func (h *AgentsHandler) HandleUpdateAgent(w ErrorResponseWriter, r *http.Request) {
	log := ctrllog.FromContext(r.Context()).WithName("agents-handler").WithValues("operation", "update-db")
	h.handleUpdateAgentObject(
		w,
		r,
		log,
		&v1alpha2.Agent{},
		&v1alpha2.Agent{},
		"Invalid Agent metadata",
		"Failed to get Agent",
		"Failed to update Agent",
		"Agent not found",
		"Successfully updated agent",
		nil,
		false,
		func(_ context.Context, _ logr.Logger, agent v1alpha2.AgentObject) (any, error) {
			return agent, nil
		},
	)
}

// HandleDeleteAgent handles DELETE /api/agents/{namespace}/{name} requests using database
func (h *AgentsHandler) HandleDeleteAgent(w ErrorResponseWriter, r *http.Request) {
	log := ctrllog.FromContext(r.Context()).WithName("agents-handler").WithValues("operation", "delete-db")
	h.handleDeleteAgentObject(
		w,
		r,
		log,
		&v1alpha2.Agent{},
		"Agent not found",
		"Failed to get Agent",
		"Failed to delete Agent",
		"Successfully deleted agent",
	)
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
	h.handleCreateAgentObject(
		w,
		r,
		log,
		&v1alpha2.SandboxAgent{},
		"Invalid sandboxagent metadata",
		"Successfully created sandbox agent",
		func(agent v1alpha2.AgentObject) {
			normalizeSandboxAgentForAPI(agent.(*v1alpha2.SandboxAgent))
		},
		func(ctx context.Context, log logr.Logger, agent v1alpha2.AgentObject) (any, error) {
			return h.getAgentResponse(ctx, log, agent)
		},
	)
}

// HandleUpdateSandboxAgent handles PUT /api/sandboxagents/{namespace}/{name} requests.
func (h *AgentsHandler) HandleUpdateSandboxAgent(w ErrorResponseWriter, r *http.Request) {
	log := ctrllog.FromContext(r.Context()).WithName("agents-handler").WithValues("operation", "update-sandboxagent")
	h.handleUpdateAgentObject(
		w,
		r,
		log,
		&v1alpha2.SandboxAgent{},
		&v1alpha2.SandboxAgent{},
		"Invalid SandboxAgent metadata",
		"Failed to get SandboxAgent",
		"Failed to update SandboxAgent",
		"SandboxAgent not found",
		"Successfully updated sandbox agent",
		func(agent v1alpha2.AgentObject) {
			normalizeSandboxAgentForAPI(agent.(*v1alpha2.SandboxAgent))
		},
		true,
		func(ctx context.Context, log logr.Logger, agent v1alpha2.AgentObject) (any, error) {
			return h.getAgentResponse(ctx, log, agent)
		},
	)
}

// HandleDeleteSandboxAgent handles DELETE /api/sandboxagents/{namespace}/{name} requests.
func (h *AgentsHandler) HandleDeleteSandboxAgent(w ErrorResponseWriter, r *http.Request) {
	log := ctrllog.FromContext(r.Context()).WithName("agents-handler").WithValues("operation", "delete-sandboxagent")
	h.handleDeleteAgentObject(
		w,
		r,
		log,
		&v1alpha2.SandboxAgent{},
		"SandboxAgent not found",
		"Failed to get SandboxAgent",
		"Failed to delete SandboxAgent",
		"Successfully deleted sandbox agent",
	)
}
