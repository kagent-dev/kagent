package handlers

import (
	"net/http"

	api "github.com/kagent-dev/kagent/go/api/httpapi"
	"github.com/kagent-dev/kagent/go/api/v1alpha2"
	"github.com/kagent-dev/kagent/go/core/internal/httpserver/errors"
	"github.com/kagent-dev/kagent/go/core/internal/utils"
	"github.com/kagent-dev/kagent/go/core/pkg/auth"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"
)

// WorkflowsHandler handles workflow template and run requests.
type WorkflowsHandler struct {
	*Base
}

// NewWorkflowsHandler creates a new WorkflowsHandler.
func NewWorkflowsHandler(base *Base) *WorkflowsHandler {
	return &WorkflowsHandler{Base: base}
}

// HandleListWorkflowTemplates handles GET /api/workflow-templates requests.
func (h *WorkflowsHandler) HandleListWorkflowTemplates(w ErrorResponseWriter, r *http.Request) {
	log := ctrllog.FromContext(r.Context()).WithName("workflows-handler").WithValues("operation", "list-templates")

	if err := Check(h.Authorizer, r, auth.Resource{Type: "WorkflowTemplate"}); err != nil {
		w.RespondWithError(err)
		return
	}

	templateList := &v1alpha2.WorkflowTemplateList{}
	if err := h.KubeClient.List(r.Context(), templateList); err != nil {
		w.RespondWithError(errors.NewInternalServerError("Failed to list WorkflowTemplates", err))
		return
	}

	log.Info("Successfully listed WorkflowTemplates", "count", len(templateList.Items))
	data := api.NewResponse(templateList.Items, "Successfully listed WorkflowTemplates", false)
	RespondWithJSON(w, http.StatusOK, data)
}

// HandleGetWorkflowTemplate handles GET /api/workflow-templates/{namespace}/{name} requests.
func (h *WorkflowsHandler) HandleGetWorkflowTemplate(w ErrorResponseWriter, r *http.Request) {
	log := ctrllog.FromContext(r.Context()).WithName("workflows-handler").WithValues("operation", "get-template")

	name, err := GetPathParam(r, "name")
	if err != nil {
		w.RespondWithError(errors.NewBadRequestError("Failed to get name from path", err))
		return
	}

	namespace, err := GetPathParam(r, "namespace")
	if err != nil {
		w.RespondWithError(errors.NewBadRequestError("Failed to get namespace from path", err))
		return
	}
	log = log.WithValues("namespace", namespace, "name", name)

	if err := Check(h.Authorizer, r, auth.Resource{Type: "WorkflowTemplate", Name: types.NamespacedName{Namespace: namespace, Name: name}.String()}); err != nil {
		w.RespondWithError(err)
		return
	}

	template := &v1alpha2.WorkflowTemplate{}
	if err := h.KubeClient.Get(r.Context(), client.ObjectKey{Namespace: namespace, Name: name}, template); err != nil {
		if apierrors.IsNotFound(err) {
			w.RespondWithError(errors.NewNotFoundError("WorkflowTemplate not found", err))
		} else {
			w.RespondWithError(errors.NewInternalServerError("Failed to get WorkflowTemplate", err))
		}
		return
	}

	log.Info("Successfully retrieved WorkflowTemplate")
	data := api.NewResponse(template, "Successfully retrieved WorkflowTemplate", false)
	RespondWithJSON(w, http.StatusOK, data)
}

// HandleListWorkflowRuns handles GET /api/workflow-runs requests.
func (h *WorkflowsHandler) HandleListWorkflowRuns(w ErrorResponseWriter, r *http.Request) {
	log := ctrllog.FromContext(r.Context()).WithName("workflows-handler").WithValues("operation", "list-runs")

	if err := Check(h.Authorizer, r, auth.Resource{Type: "WorkflowRun"}); err != nil {
		w.RespondWithError(err)
		return
	}

	runList := &v1alpha2.WorkflowRunList{}
	listOpts := []client.ListOption{}

	// Optional filters via query params
	if templateRef := r.URL.Query().Get("templateRef"); templateRef != "" {
		listOpts = append(listOpts, client.MatchingLabels{"kagent.dev/workflow-template": templateRef})
	}

	if err := h.KubeClient.List(r.Context(), runList, listOpts...); err != nil {
		w.RespondWithError(errors.NewInternalServerError("Failed to list WorkflowRuns", err))
		return
	}

	// Optional status filter (post-filter since phase is in status)
	if statusFilter := r.URL.Query().Get("status"); statusFilter != "" {
		filtered := make([]v1alpha2.WorkflowRun, 0, len(runList.Items))
		for _, run := range runList.Items {
			if run.Status.Phase == statusFilter {
				filtered = append(filtered, run)
			}
		}
		runList.Items = filtered
	}

	log.Info("Successfully listed WorkflowRuns", "count", len(runList.Items))
	data := api.NewResponse(runList.Items, "Successfully listed WorkflowRuns", false)
	RespondWithJSON(w, http.StatusOK, data)
}

// HandleGetWorkflowRun handles GET /api/workflow-runs/{namespace}/{name} requests.
func (h *WorkflowsHandler) HandleGetWorkflowRun(w ErrorResponseWriter, r *http.Request) {
	log := ctrllog.FromContext(r.Context()).WithName("workflows-handler").WithValues("operation", "get-run")

	name, err := GetPathParam(r, "name")
	if err != nil {
		w.RespondWithError(errors.NewBadRequestError("Failed to get name from path", err))
		return
	}

	namespace, err := GetPathParam(r, "namespace")
	if err != nil {
		w.RespondWithError(errors.NewBadRequestError("Failed to get namespace from path", err))
		return
	}
	log = log.WithValues("namespace", namespace, "name", name)

	if err := Check(h.Authorizer, r, auth.Resource{Type: "WorkflowRun", Name: types.NamespacedName{Namespace: namespace, Name: name}.String()}); err != nil {
		w.RespondWithError(err)
		return
	}

	run := &v1alpha2.WorkflowRun{}
	if err := h.KubeClient.Get(r.Context(), client.ObjectKey{Namespace: namespace, Name: name}, run); err != nil {
		if apierrors.IsNotFound(err) {
			w.RespondWithError(errors.NewNotFoundError("WorkflowRun not found", err))
		} else {
			w.RespondWithError(errors.NewInternalServerError("Failed to get WorkflowRun", err))
		}
		return
	}

	log.Info("Successfully retrieved WorkflowRun")
	data := api.NewResponse(run, "Successfully retrieved WorkflowRun", false)
	RespondWithJSON(w, http.StatusOK, data)
}

// HandleCreateWorkflowRun handles POST /api/workflow-runs requests.
func (h *WorkflowsHandler) HandleCreateWorkflowRun(w ErrorResponseWriter, r *http.Request) {
	log := ctrllog.FromContext(r.Context()).WithName("workflows-handler").WithValues("operation", "create-run")

	var req api.CreateWorkflowRunRequest
	if err := DecodeJSONBody(r, &req); err != nil {
		w.RespondWithError(errors.NewBadRequestError("Invalid request body", err))
		return
	}

	if req.Name == "" {
		w.RespondWithError(errors.NewBadRequestError("Name is required", nil))
		return
	}

	if req.WorkflowTemplateRef == "" {
		w.RespondWithError(errors.NewBadRequestError("workflowTemplateRef is required", nil))
		return
	}

	if req.Namespace == "" {
		req.Namespace = utils.GetResourceNamespace()
	}

	log = log.WithValues("namespace", req.Namespace, "name", req.Name, "templateRef", req.WorkflowTemplateRef)

	if err := Check(h.Authorizer, r, auth.Resource{Type: "WorkflowRun", Name: types.NamespacedName{Namespace: req.Namespace, Name: req.Name}.String()}); err != nil {
		w.RespondWithError(err)
		return
	}

	// Check if already exists
	existing := &v1alpha2.WorkflowRun{}
	err := h.KubeClient.Get(r.Context(), client.ObjectKey{Namespace: req.Namespace, Name: req.Name}, existing)
	if err == nil {
		w.RespondWithError(errors.NewConflictError("WorkflowRun already exists", nil))
		return
	} else if !apierrors.IsNotFound(err) {
		w.RespondWithError(errors.NewInternalServerError("Failed to check if WorkflowRun exists", err))
		return
	}

	run := &v1alpha2.WorkflowRun{
		ObjectMeta: metav1.ObjectMeta{
			Name:      req.Name,
			Namespace: req.Namespace,
		},
		Spec: v1alpha2.WorkflowRunSpec{
			WorkflowTemplateRef:     req.WorkflowTemplateRef,
			Params:                  req.Params,
			TTLSecondsAfterFinished: req.TTLSecondsAfterFinished,
		},
	}

	if err := h.KubeClient.Create(r.Context(), run); err != nil {
		w.RespondWithError(errors.NewInternalServerError("Failed to create WorkflowRun", err))
		return
	}

	log.Info("Successfully created WorkflowRun")
	data := api.NewResponse(run, "Successfully created WorkflowRun", false)
	RespondWithJSON(w, http.StatusCreated, data)
}

// HandleDeleteWorkflowRun handles DELETE /api/workflow-runs/{namespace}/{name} requests.
func (h *WorkflowsHandler) HandleDeleteWorkflowRun(w ErrorResponseWriter, r *http.Request) {
	log := ctrllog.FromContext(r.Context()).WithName("workflows-handler").WithValues("operation", "delete-run")

	name, err := GetPathParam(r, "name")
	if err != nil {
		w.RespondWithError(errors.NewBadRequestError("Failed to get name from path", err))
		return
	}

	namespace, err := GetPathParam(r, "namespace")
	if err != nil {
		w.RespondWithError(errors.NewBadRequestError("Failed to get namespace from path", err))
		return
	}
	log = log.WithValues("namespace", namespace, "name", name)

	if err := Check(h.Authorizer, r, auth.Resource{Type: "WorkflowRun", Name: types.NamespacedName{Namespace: namespace, Name: name}.String()}); err != nil {
		w.RespondWithError(err)
		return
	}

	run := &v1alpha2.WorkflowRun{}
	if err := h.KubeClient.Get(r.Context(), client.ObjectKey{Namespace: namespace, Name: name}, run); err != nil {
		if apierrors.IsNotFound(err) {
			w.RespondWithError(errors.NewNotFoundError("WorkflowRun not found", err))
		} else {
			w.RespondWithError(errors.NewInternalServerError("Failed to get WorkflowRun", err))
		}
		return
	}

	if err := h.KubeClient.Delete(r.Context(), run); err != nil {
		w.RespondWithError(errors.NewInternalServerError("Failed to delete WorkflowRun", err))
		return
	}

	log.Info("Successfully deleted WorkflowRun")
	data := api.NewResponse(struct{}{}, "Successfully deleted WorkflowRun", false)
	RespondWithJSON(w, http.StatusOK, data)
}
