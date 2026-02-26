package handlers

import (
	"net/http"

	"github.com/kagent-dev/kagent/go/api/v1alpha2"
	"github.com/kagent-dev/kagent/go/internal/httpserver/errors"
	"github.com/kagent-dev/kagent/go/internal/utils"
	"github.com/kagent-dev/kagent/go/pkg/auth"
	"github.com/kagent-dev/kagent/go/pkg/client/api"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"
)

// AgentCronJobsHandler handles agentcronjob-related requests
type AgentCronJobsHandler struct {
	*Base
}

// NewAgentCronJobsHandler creates a new AgentCronJobsHandler
func NewAgentCronJobsHandler(base *Base) *AgentCronJobsHandler {
	return &AgentCronJobsHandler{Base: base}
}

// HandleListCronJobs handles GET /api/cronjobs requests
func (h *AgentCronJobsHandler) HandleListCronJobs(w ErrorResponseWriter, r *http.Request) {
	log := ctrllog.FromContext(r.Context()).WithName("cronjobs-handler").WithValues("operation", "list")

	if err := Check(h.Authorizer, r, auth.Resource{Type: "AgentCronJob"}); err != nil {
		w.RespondWithError(err)
		return
	}

	cronJobList := &v1alpha2.AgentCronJobList{}
	if err := h.KubeClient.List(r.Context(), cronJobList); err != nil {
		w.RespondWithError(errors.NewInternalServerError("Failed to list AgentCronJobs", err))
		return
	}

	log.Info("Successfully listed AgentCronJobs", "count", len(cronJobList.Items))
	data := api.NewResponse(cronJobList.Items, "Successfully listed AgentCronJobs", false)
	RespondWithJSON(w, http.StatusOK, data)
}

// HandleGetCronJob handles GET /api/cronjobs/{namespace}/{name} requests
func (h *AgentCronJobsHandler) HandleGetCronJob(w ErrorResponseWriter, r *http.Request) {
	log := ctrllog.FromContext(r.Context()).WithName("cronjobs-handler").WithValues("operation", "get")

	name, err := GetPathParam(r, "name")
	if err != nil {
		w.RespondWithError(errors.NewBadRequestError("Failed to get name from path", err))
		return
	}
	log = log.WithValues("name", name)

	namespace, err := GetPathParam(r, "namespace")
	if err != nil {
		w.RespondWithError(errors.NewBadRequestError("Failed to get namespace from path", err))
		return
	}
	log = log.WithValues("namespace", namespace)

	if err := Check(h.Authorizer, r, auth.Resource{Type: "AgentCronJob", Name: types.NamespacedName{Namespace: namespace, Name: name}.String()}); err != nil {
		w.RespondWithError(err)
		return
	}

	cronJob := &v1alpha2.AgentCronJob{}
	if err := h.KubeClient.Get(r.Context(), client.ObjectKey{
		Namespace: namespace,
		Name:      name,
	}, cronJob); err != nil {
		if apierrors.IsNotFound(err) {
			w.RespondWithError(errors.NewNotFoundError("AgentCronJob not found", err))
		} else {
			w.RespondWithError(errors.NewInternalServerError("Failed to get AgentCronJob", err))
		}
		return
	}

	log.Info("Successfully retrieved AgentCronJob")
	data := api.NewResponse(cronJob, "Successfully retrieved AgentCronJob", false)
	RespondWithJSON(w, http.StatusOK, data)
}

// HandleCreateCronJob handles POST /api/cronjobs requests
func (h *AgentCronJobsHandler) HandleCreateCronJob(w ErrorResponseWriter, r *http.Request) {
	log := ctrllog.FromContext(r.Context()).WithName("cronjobs-handler").WithValues("operation", "create")

	var cronJobReq v1alpha2.AgentCronJob
	if err := DecodeJSONBody(r, &cronJobReq); err != nil {
		w.RespondWithError(errors.NewBadRequestError("Invalid request body", err))
		return
	}

	if cronJobReq.Namespace == "" {
		cronJobReq.Namespace = utils.GetResourceNamespace()
		log.V(4).Info("Namespace not provided, using default", "namespace", cronJobReq.Namespace)
	}

	if cronJobReq.Name == "" {
		w.RespondWithError(errors.NewBadRequestError("Name is required", nil))
		return
	}

	if cronJobReq.Spec.Schedule == "" || cronJobReq.Spec.Prompt == "" || cronJobReq.Spec.AgentRef == "" {
		w.RespondWithError(errors.NewBadRequestError("Schedule, Prompt, and AgentRef are required", nil))
		return
	}

	log = log.WithValues("namespace", cronJobReq.Namespace, "name", cronJobReq.Name)

	if err := Check(h.Authorizer, r, auth.Resource{Type: "AgentCronJob", Name: types.NamespacedName{Namespace: cronJobReq.Namespace, Name: cronJobReq.Name}.String()}); err != nil {
		w.RespondWithError(err)
		return
	}

	// Check if already exists
	existing := &v1alpha2.AgentCronJob{}
	err := h.KubeClient.Get(r.Context(), client.ObjectKey{
		Namespace: cronJobReq.Namespace,
		Name:      cronJobReq.Name,
	}, existing)
	if err == nil {
		w.RespondWithError(errors.NewConflictError("AgentCronJob already exists", nil))
		return
	} else if !apierrors.IsNotFound(err) {
		w.RespondWithError(errors.NewInternalServerError("Failed to check if AgentCronJob exists", err))
		return
	}

	if err := h.KubeClient.Create(r.Context(), &cronJobReq); err != nil {
		w.RespondWithError(errors.NewInternalServerError("Failed to create AgentCronJob", err))
		return
	}

	log.Info("Successfully created AgentCronJob")
	data := api.NewResponse(&cronJobReq, "Successfully created AgentCronJob", false)
	RespondWithJSON(w, http.StatusCreated, data)
}

// HandleUpdateCronJob handles PUT /api/cronjobs/{namespace}/{name} requests
func (h *AgentCronJobsHandler) HandleUpdateCronJob(w ErrorResponseWriter, r *http.Request) {
	log := ctrllog.FromContext(r.Context()).WithName("cronjobs-handler").WithValues("operation", "update")

	name, err := GetPathParam(r, "name")
	if err != nil {
		w.RespondWithError(errors.NewBadRequestError("Failed to get name from path", err))
		return
	}
	log = log.WithValues("name", name)

	namespace, err := GetPathParam(r, "namespace")
	if err != nil {
		w.RespondWithError(errors.NewBadRequestError("Failed to get namespace from path", err))
		return
	}
	log = log.WithValues("namespace", namespace)

	if err := Check(h.Authorizer, r, auth.Resource{Type: "AgentCronJob", Name: types.NamespacedName{Namespace: namespace, Name: name}.String()}); err != nil {
		w.RespondWithError(err)
		return
	}

	var cronJobReq v1alpha2.AgentCronJob
	if err := DecodeJSONBody(r, &cronJobReq); err != nil {
		w.RespondWithError(errors.NewBadRequestError("Invalid request body", err))
		return
	}

	existing := &v1alpha2.AgentCronJob{}
	if err := h.KubeClient.Get(r.Context(), client.ObjectKey{
		Namespace: namespace,
		Name:      name,
	}, existing); err != nil {
		if apierrors.IsNotFound(err) {
			w.RespondWithError(errors.NewNotFoundError("AgentCronJob not found", err))
		} else {
			w.RespondWithError(errors.NewInternalServerError("Failed to get AgentCronJob", err))
		}
		return
	}

	existing.Spec = cronJobReq.Spec

	if err := h.KubeClient.Update(r.Context(), existing); err != nil {
		w.RespondWithError(errors.NewInternalServerError("Failed to update AgentCronJob", err))
		return
	}

	log.Info("Successfully updated AgentCronJob")
	data := api.NewResponse(existing, "Successfully updated AgentCronJob", false)
	RespondWithJSON(w, http.StatusOK, data)
}

// HandleDeleteCronJob handles DELETE /api/cronjobs/{namespace}/{name} requests
func (h *AgentCronJobsHandler) HandleDeleteCronJob(w ErrorResponseWriter, r *http.Request) {
	log := ctrllog.FromContext(r.Context()).WithName("cronjobs-handler").WithValues("operation", "delete")

	name, err := GetPathParam(r, "name")
	if err != nil {
		w.RespondWithError(errors.NewBadRequestError("Failed to get name from path", err))
		return
	}
	log = log.WithValues("name", name)

	namespace, err := GetPathParam(r, "namespace")
	if err != nil {
		w.RespondWithError(errors.NewBadRequestError("Failed to get namespace from path", err))
		return
	}
	log = log.WithValues("namespace", namespace)

	if err := Check(h.Authorizer, r, auth.Resource{Type: "AgentCronJob", Name: types.NamespacedName{Namespace: namespace, Name: name}.String()}); err != nil {
		w.RespondWithError(err)
		return
	}

	cronJob := &v1alpha2.AgentCronJob{}
	if err := h.KubeClient.Get(r.Context(), client.ObjectKey{
		Namespace: namespace,
		Name:      name,
	}, cronJob); err != nil {
		if apierrors.IsNotFound(err) {
			w.RespondWithError(errors.NewNotFoundError("AgentCronJob not found", err))
		} else {
			w.RespondWithError(errors.NewInternalServerError("Failed to get AgentCronJob", err))
		}
		return
	}

	if err := h.KubeClient.Delete(r.Context(), cronJob); err != nil {
		w.RespondWithError(errors.NewInternalServerError("Failed to delete AgentCronJob", err))
		return
	}

	log.Info("Successfully deleted AgentCronJob")
	data := api.NewResponse(struct{}{}, "Successfully deleted AgentCronJob", false)
	RespondWithJSON(w, http.StatusOK, data)
}
