package handlers

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/kagent-dev/kagent/go/api/database"
	api "github.com/kagent-dev/kagent/go/api/httpapi"
	"github.com/kagent-dev/kagent/go/api/v1alpha2"
	"github.com/kagent-dev/kagent/go/core/internal/httpserver/errors"
	"github.com/kagent-dev/kagent/go/core/internal/utils"
	"github.com/kagent-dev/kagent/go/core/pkg/auth"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"
)

const maxScheduledRunExecutionPageSize = 100

// ScheduledRunTrigger starts a manual execution.
// Implementations dispatch synchronously and return the recorded execution so
// the handler can include it in the response.
type ScheduledRunTrigger interface {
	TriggerManualExecution(ctx context.Context, key types.NamespacedName) (*v1alpha2.ScheduledRunExecution, error)
}

// ScheduledRunsHandler handles ScheduledRun-related requests
type ScheduledRunsHandler struct {
	*Base
	Trigger ScheduledRunTrigger
}

// NewScheduledRunsHandler creates a new ScheduledRunsHandler
func NewScheduledRunsHandler(base *Base, trigger ScheduledRunTrigger) *ScheduledRunsHandler {
	return &ScheduledRunsHandler{Base: base, Trigger: trigger}
}

func scheduledRunTargetRefsEqual(a, b corev1.TypedLocalObjectReference) bool {
	if a.Kind != b.Kind || a.Name != b.Name {
		return false
	}
	if a.APIGroup == nil || b.APIGroup == nil {
		return a.APIGroup == nil && b.APIGroup == nil
	}
	return *a.APIGroup == *b.APIGroup
}

func (h *ScheduledRunsHandler) scheduledRunKey(r *http.Request) (types.NamespacedName, *errors.APIError) {
	namespace, err := GetPathParam(r, "namespace")
	if err != nil {
		return types.NamespacedName{}, errors.NewBadRequestError("Failed to get namespace from path", err)
	}
	name, err := GetPathParam(r, "name")
	if err != nil {
		return types.NamespacedName{}, errors.NewBadRequestError("Failed to get name from path", err)
	}
	key := types.NamespacedName{Namespace: namespace, Name: name}
	if apiErr := Check(h.Authorizer, r, auth.Resource{Type: "ScheduledRun", Name: key.String()}); apiErr != nil {
		return types.NamespacedName{}, apiErr
	}
	return key, nil
}

// HandleListScheduledRuns handles GET /api/scheduledruns requests
func (h *ScheduledRunsHandler) HandleListScheduledRuns(w ErrorResponseWriter, r *http.Request) {
	log := ctrllog.FromContext(r.Context()).WithName("scheduledruns-handler").WithValues("operation", "list")

	if err := Check(h.Authorizer, r, auth.Resource{Type: "ScheduledRun"}); err != nil {
		w.RespondWithError(err)
		return
	}

	scheduledRunList := &v1alpha2.ScheduledRunList{}
	if err := h.KubeClient.List(r.Context(), scheduledRunList); err != nil {
		w.RespondWithError(errors.NewInternalServerError("Failed to list Scheduled Runs", err))
		return
	}

	log.Info("Successfully listed Scheduled Runs", "count", len(scheduledRunList.Items))
	data := api.NewResponse(scheduledRunList.Items, "Successfully listed Scheduled Runs", false)
	RespondWithJSON(w, http.StatusOK, data)
}

// HandleGetScheduledRun handles GET /api/scheduledruns/{namespace}/{name} requests
func (h *ScheduledRunsHandler) HandleGetScheduledRun(w ErrorResponseWriter, r *http.Request) {
	log := ctrllog.FromContext(r.Context()).WithName("scheduledruns-handler").WithValues("operation", "get")

	key, apiErr := h.scheduledRunKey(r)
	if apiErr != nil {
		w.RespondWithError(apiErr)
		return
	}
	log = log.WithValues("namespace", key.Namespace, "name", key.Name)

	sr := &v1alpha2.ScheduledRun{}
	if err := h.KubeClient.Get(r.Context(), key, sr); err != nil {
		if apierrors.IsNotFound(err) {
			w.RespondWithError(errors.NewNotFoundError("Scheduled Run not found", err))
			return
		}
		w.RespondWithError(errors.NewInternalServerError("Failed to get Scheduled Run", err))
		return
	}

	log.Info("Successfully retrieved Scheduled Run")
	data := api.NewResponse(sr, "Successfully retrieved Scheduled Run", false)
	RespondWithJSON(w, http.StatusOK, data)
}

// HandleListScheduledRunExecutions returns a page of durable execution history
// for one Scheduled Run. Status.recentExecutions remains a short recent summary
// for Kubernetes clients.
func (h *ScheduledRunsHandler) HandleListScheduledRunExecutions(w ErrorResponseWriter, r *http.Request) {
	key, apiErr := h.scheduledRunKey(r)
	if apiErr != nil {
		w.RespondWithError(apiErr)
		return
	}
	if h.DatabaseService == nil {
		w.RespondWithError(errors.NewInternalServerError("Execution storage is not configured", nil))
		return
	}
	var sr v1alpha2.ScheduledRun
	if err := h.KubeClient.Get(r.Context(), key, &sr); err != nil {
		if apierrors.IsNotFound(err) {
			w.RespondWithError(errors.NewNotFoundError("Scheduled Run not found", err))
			return
		}
		w.RespondWithError(errors.NewInternalServerError("Failed to get Scheduled Run", err))
		return
	}
	// Limit is left unset here so the database layer applies its own default
	// page size; only an explicit query param overrides it.
	var options database.ScheduledRunExecutionQueryOptions
	if rawLimit := r.URL.Query().Get("limit"); rawLimit != "" {
		limit, parseErr := strconv.Atoi(rawLimit)
		if parseErr != nil || limit < 1 || limit > maxScheduledRunExecutionPageSize {
			w.RespondWithError(errors.NewBadRequestError(fmt.Sprintf("limit must be between 1 and %d", maxScheduledRunExecutionPageSize), parseErr))
			return
		}
		options.Limit = limit
	}
	if rawBefore := r.URL.Query().Get("before"); rawBefore != "" {
		before, parseErr := time.Parse(time.RFC3339Nano, rawBefore)
		if parseErr != nil {
			w.RespondWithError(errors.NewBadRequestError("before must be an RFC3339 timestamp", parseErr))
			return
		}
		options.Before = before
	}
	if rawBeforeID := r.URL.Query().Get("beforeID"); rawBeforeID != "" {
		if options.Before.IsZero() {
			w.RespondWithError(errors.NewBadRequestError("beforeID requires before", nil))
			return
		}
		options.BeforeID = rawBeforeID
	}
	executions, err := h.DatabaseService.ListScheduledRunExecutions(r.Context(), key.Namespace, key.Name, string(sr.UID), options)
	if err != nil {
		w.RespondWithError(errors.NewInternalServerError("Failed to list executions", err))
		return
	}
	data := api.NewResponse(executions, "Successfully listed executions", false)
	RespondWithJSON(w, http.StatusOK, data)
}

// HandleCreateScheduledRun handles POST /api/scheduledruns requests
func (h *ScheduledRunsHandler) HandleCreateScheduledRun(w ErrorResponseWriter, r *http.Request) {
	log := ctrllog.FromContext(r.Context()).WithName("scheduledruns-handler").WithValues("operation", "create")

	if err := Check(h.Authorizer, r, auth.Resource{Type: "ScheduledRun"}); err != nil {
		w.RespondWithError(err)
		return
	}

	var sr v1alpha2.ScheduledRun
	if err := DecodeJSONBody(r, &sr); err != nil {
		w.RespondWithError(errors.NewBadRequestError("Invalid request body", err))
		return
	}

	if sr.Namespace == "" {
		sr.Namespace = utils.GetResourceNamespace()
	}
	log = log.WithValues("namespace", sr.Namespace, "name", sr.Name)

	if err := h.KubeClient.Create(r.Context(), &sr); err != nil {
		if apierrors.IsInvalid(err) {
			w.RespondWithError(errors.NewBadRequestError("Invalid Scheduled Run", err))
			return
		}
		w.RespondWithError(errors.NewInternalServerError("Failed to create Scheduled Run", err))
		return
	}

	log.Info("Successfully created Scheduled Run")
	data := api.NewResponse(sr, "Successfully created Scheduled Run", false)
	RespondWithJSON(w, http.StatusCreated, data)
}

// HandleUpdateScheduledRun handles PUT /api/scheduledruns/{namespace}/{name} requests
func (h *ScheduledRunsHandler) HandleUpdateScheduledRun(w ErrorResponseWriter, r *http.Request) {
	log := ctrllog.FromContext(r.Context()).WithName("scheduledruns-handler").WithValues("operation", "update")

	key, apiErr := h.scheduledRunKey(r)
	if apiErr != nil {
		w.RespondWithError(apiErr)
		return
	}
	log = log.WithValues("namespace", key.Namespace, "name", key.Name)

	var incoming v1alpha2.ScheduledRun
	if err := DecodeJSONBody(r, &incoming); err != nil {
		w.RespondWithError(errors.NewBadRequestError("Invalid request body", err))
		return
	}

	incoming.Name = key.Name
	incoming.Namespace = key.Namespace
	incoming.TypeMeta = metav1.TypeMeta{
		APIVersion: v1alpha2.GroupVersion.String(),
		Kind:       "ScheduledRun",
	}
	// Status is server-owned; drop anything a client sent so the apply below
	// never clobbers controller- or scheduler-written status fields.
	incoming.Status = v1alpha2.ScheduledRunStatus{}
	// A server-side apply upserts, so it would create the resource on a PUT to a
	// non-existent name. Get first to keep PUT strictly an update.
	var existing v1alpha2.ScheduledRun
	if err := h.KubeClient.Get(r.Context(), key, &existing); err != nil {
		if apierrors.IsNotFound(err) {
			w.RespondWithError(errors.NewNotFoundError("Scheduled Run not found", err))
			return
		}
		w.RespondWithError(errors.NewInternalServerError("Failed to get Scheduled Run", err))
		return
	}
	if !scheduledRunTargetRefsEqual(existing.Spec.TargetRef, incoming.Spec.TargetRef) {
		w.RespondWithError(errors.NewBadRequestError("Scheduled Run targetRef is immutable", nil))
		return
	}
	//nolint:staticcheck // Typed apply configurations are not generated for kagent CRDs yet.
	if err := h.KubeClient.Patch(
		r.Context(),
		&incoming,
		client.Apply,
		client.FieldOwner("kagent-api"),
		// This PUT endpoint is authoritative for fields present in the request.
		// ForceOwnership lets an authorized API update take over those fields
		// without replacing fields the request omitted.
		client.ForceOwnership,
	); err != nil {
		if apierrors.IsNotFound(err) {
			w.RespondWithError(errors.NewNotFoundError("Scheduled Run not found", err))
			return
		}
		if apierrors.IsInvalid(err) {
			w.RespondWithError(errors.NewBadRequestError("Invalid Scheduled Run", err))
			return
		}
		if apierrors.IsConflict(err) {
			w.RespondWithError(errors.NewConflictError("Scheduled Run update conflicted", err))
			return
		}
		w.RespondWithError(errors.NewInternalServerError("Failed to update Scheduled Run", err))
		return
	}

	var persisted v1alpha2.ScheduledRun
	if err := h.KubeClient.Get(r.Context(), key, &persisted); err != nil {
		w.RespondWithError(errors.NewInternalServerError("Failed to load updated Scheduled Run", err))
		return
	}
	log.Info("Successfully updated Scheduled Run")
	data := api.NewResponse(&persisted, "Successfully updated Scheduled Run", false)
	RespondWithJSON(w, http.StatusOK, data)
}

// HandleDeleteScheduledRun handles DELETE /api/scheduledruns/{namespace}/{name} requests
func (h *ScheduledRunsHandler) HandleDeleteScheduledRun(w ErrorResponseWriter, r *http.Request) {
	log := ctrllog.FromContext(r.Context()).WithName("scheduledruns-handler").WithValues("operation", "delete")

	key, apiErr := h.scheduledRunKey(r)
	if apiErr != nil {
		w.RespondWithError(apiErr)
		return
	}
	log = log.WithValues("namespace", key.Namespace, "name", key.Name)

	sr := &v1alpha2.ScheduledRun{}
	if err := h.KubeClient.Get(r.Context(), key, sr); err != nil {
		if apierrors.IsNotFound(err) {
			w.RespondWithError(errors.NewNotFoundError("Scheduled Run not found", err))
			return
		}
		w.RespondWithError(errors.NewInternalServerError("Failed to get Scheduled Run", err))
		return
	}

	if err := h.KubeClient.Delete(r.Context(), sr); err != nil {
		w.RespondWithError(errors.NewInternalServerError("Failed to delete Scheduled Run", err))
		return
	}

	log.Info("Successfully deleted Scheduled Run")
	data := api.NewResponse(struct{}{}, "Successfully deleted Scheduled Run", false)
	RespondWithJSON(w, http.StatusOK, data)
}

// HandleTriggerScheduledRun handles POST /api/scheduledruns/{namespace}/{name}/trigger requests
func (h *ScheduledRunsHandler) HandleTriggerScheduledRun(w ErrorResponseWriter, r *http.Request) {
	log := ctrllog.FromContext(r.Context()).WithName("scheduledruns-handler").WithValues("operation", "trigger")

	key, apiErr := h.scheduledRunKey(r)
	if apiErr != nil {
		w.RespondWithError(apiErr)
		return
	}
	log = log.WithValues("namespace", key.Namespace, "name", key.Name)
	log.Info("Manually triggering execution")
	execution, err := h.Trigger.TriggerManualExecution(r.Context(), key)
	if err != nil {
		if apierrors.IsNotFound(err) {
			w.RespondWithError(errors.NewNotFoundError("Scheduled Run not found", err))
			return
		}
		w.RespondWithError(errors.NewInternalServerError("Failed to trigger execution", err))
		return
	}
	data := api.NewResponse(execution, "Execution triggered successfully", false)
	RespondWithJSON(w, http.StatusOK, data)
}
