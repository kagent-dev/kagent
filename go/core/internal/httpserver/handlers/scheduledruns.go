package handlers

import (
	"net/http"
	"time"

	"github.com/robfig/cron/v3"

	api "github.com/kagent-dev/kagent/go/api/httpapi"
	"github.com/kagent-dev/kagent/go/api/v1alpha2"
	"github.com/kagent-dev/kagent/go/core/internal/httpserver/errors"
	"github.com/kagent-dev/kagent/go/core/internal/utils"
	"github.com/kagent-dev/kagent/go/core/pkg/auth"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"
)

// ScheduledRunTrigger is the interface for triggering scheduled runs manually.
// Implementations run synchronously and return the recorded RunHistoryEntry
// so the handler can include it in the response.
type ScheduledRunTrigger interface {
	TriggerManualRun(key types.NamespacedName) (*v1alpha2.RunHistoryEntry, error)
}

// ScheduledRunsHandler handles ScheduledRun-related requests
type ScheduledRunsHandler struct {
	*Base
	Scheduler ScheduledRunTrigger
}

// NewScheduledRunsHandler creates a new ScheduledRunsHandler
func NewScheduledRunsHandler(base *Base, scheduler ScheduledRunTrigger) *ScheduledRunsHandler {
	return &ScheduledRunsHandler{Base: base, Scheduler: scheduler}
}

// ValidateSchedule validates the cron expression syntax and (optionally) the
// IANA time zone. Both are checked at the API edge so a bad request is
// rejected with 400 before it ever reaches the controller, where the same
// invariants are re-checked against the persisted object.
func ValidateSchedule(schedule, timeZone string) *errors.APIError {
	if schedule == "" {
		return errors.NewBadRequestError("spec.schedule is required (request body must be {\"spec\":{...}})", nil)
	}
	expr := schedule
	if timeZone != "" {
		if _, err := time.LoadLocation(timeZone); err != nil {
			return errors.NewBadRequestError("Invalid time zone: "+err.Error(), nil)
		}
		expr = "CRON_TZ=" + timeZone + " " + schedule
	}
	if _, err := cron.ParseStandard(expr); err != nil {
		return errors.NewBadRequestError("Invalid cron expression: "+err.Error(), nil)
	}
	return nil
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
		w.RespondWithError(errors.NewInternalServerError("Failed to list ScheduledRuns", err))
		return
	}

	log.Info("Successfully listed ScheduledRuns", "count", len(scheduledRunList.Items))
	data := api.NewResponse(scheduledRunList.Items, "Successfully listed ScheduledRuns", false)
	RespondWithJSON(w, http.StatusOK, data)
}

// HandleGetScheduledRun handles GET /api/scheduledruns/{namespace}/{name} requests
func (h *ScheduledRunsHandler) HandleGetScheduledRun(w ErrorResponseWriter, r *http.Request) {
	log := ctrllog.FromContext(r.Context()).WithName("scheduledruns-handler").WithValues("operation", "get")

	namespace, err := GetPathParam(r, "namespace")
	if err != nil {
		w.RespondWithError(errors.NewBadRequestError("Failed to get namespace from path", err))
		return
	}

	name, err := GetPathParam(r, "name")
	if err != nil {
		w.RespondWithError(errors.NewBadRequestError("Failed to get name from path", err))
		return
	}

	log = log.WithValues("namespace", namespace, "name", name)

	if err := Check(h.Authorizer, r, auth.Resource{Type: "ScheduledRun", Name: namespace + "/" + name}); err != nil {
		w.RespondWithError(err)
		return
	}

	sr := &v1alpha2.ScheduledRun{}
	if err := h.KubeClient.Get(r.Context(), client.ObjectKey{Namespace: namespace, Name: name}, sr); err != nil {
		if apierrors.IsNotFound(err) {
			w.RespondWithError(errors.NewNotFoundError("ScheduledRun not found", err))
			return
		}
		w.RespondWithError(errors.NewInternalServerError("Failed to get ScheduledRun", err))
		return
	}

	log.Info("Successfully retrieved ScheduledRun")
	data := api.NewResponse(sr, "Successfully retrieved ScheduledRun", false)
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

	if apiErr := ValidateSchedule(sr.Spec.Schedule, sr.Spec.TimeZone); apiErr != nil {
		w.RespondWithError(apiErr)
		return
	}

	if sr.Namespace == "" {
		sr.Namespace = utils.GetResourceNamespace()
	}

	// Record the creating user so the scheduler can attribute sessions back
	// to them — without this the session is invisible to the UI.
	if userID, err := getUserIDOrAgentUser(r); err == nil && userID != "" {
		if sr.Annotations == nil {
			sr.Annotations = map[string]string{}
		}
		sr.Annotations[v1alpha2.AnnotationCreatedBy] = userID
	}

	log = log.WithValues("namespace", sr.Namespace, "name", sr.Name)

	if err := h.KubeClient.Create(r.Context(), &sr); err != nil {
		w.RespondWithError(errors.NewInternalServerError("Failed to create ScheduledRun", err))
		return
	}

	log.Info("Successfully created ScheduledRun")
	data := api.NewResponse(sr, "Successfully created ScheduledRun", false)
	RespondWithJSON(w, http.StatusCreated, data)
}

// HandleUpdateScheduledRun handles PUT /api/scheduledruns/{namespace}/{name} requests
func (h *ScheduledRunsHandler) HandleUpdateScheduledRun(w ErrorResponseWriter, r *http.Request) {
	log := ctrllog.FromContext(r.Context()).WithName("scheduledruns-handler").WithValues("operation", "update")

	namespace, err := GetPathParam(r, "namespace")
	if err != nil {
		w.RespondWithError(errors.NewBadRequestError("Failed to get namespace from path", err))
		return
	}

	name, err := GetPathParam(r, "name")
	if err != nil {
		w.RespondWithError(errors.NewBadRequestError("Failed to get name from path", err))
		return
	}

	log = log.WithValues("namespace", namespace, "name", name)

	if err := Check(h.Authorizer, r, auth.Resource{Type: "ScheduledRun", Name: namespace + "/" + name}); err != nil {
		w.RespondWithError(err)
		return
	}

	var incoming v1alpha2.ScheduledRun
	if err := DecodeJSONBody(r, &incoming); err != nil {
		w.RespondWithError(errors.NewBadRequestError("Invalid request body", err))
		return
	}

	if apiErr := ValidateSchedule(incoming.Spec.Schedule, incoming.Spec.TimeZone); apiErr != nil {
		w.RespondWithError(apiErr)
		return
	}

	existing := &v1alpha2.ScheduledRun{}
	if err := h.KubeClient.Get(r.Context(), client.ObjectKey{Namespace: namespace, Name: name}, existing); err != nil {
		if apierrors.IsNotFound(err) {
			w.RespondWithError(errors.NewNotFoundError("ScheduledRun not found", err))
			return
		}
		w.RespondWithError(errors.NewInternalServerError("Failed to get ScheduledRun", err))
		return
	}

	existing.Spec = incoming.Spec

	// Preserve created-by annotation across updates; if missing, set from
	// current request user.
	if existing.Annotations == nil {
		existing.Annotations = map[string]string{}
	}
	if existing.Annotations[v1alpha2.AnnotationCreatedBy] == "" {
		if userID, err := getUserIDOrAgentUser(r); err == nil && userID != "" {
			existing.Annotations[v1alpha2.AnnotationCreatedBy] = userID
		}
	}

	if err := h.KubeClient.Update(r.Context(), existing); err != nil {
		w.RespondWithError(errors.NewInternalServerError("Failed to update ScheduledRun", err))
		return
	}

	log.Info("Successfully updated ScheduledRun")
	data := api.NewResponse(existing, "Successfully updated ScheduledRun", false)
	RespondWithJSON(w, http.StatusOK, data)
}

// HandleDeleteScheduledRun handles DELETE /api/scheduledruns/{namespace}/{name} requests
func (h *ScheduledRunsHandler) HandleDeleteScheduledRun(w ErrorResponseWriter, r *http.Request) {
	log := ctrllog.FromContext(r.Context()).WithName("scheduledruns-handler").WithValues("operation", "delete")

	namespace, err := GetPathParam(r, "namespace")
	if err != nil {
		w.RespondWithError(errors.NewBadRequestError("Failed to get namespace from path", err))
		return
	}

	name, err := GetPathParam(r, "name")
	if err != nil {
		w.RespondWithError(errors.NewBadRequestError("Failed to get name from path", err))
		return
	}

	log = log.WithValues("namespace", namespace, "name", name)

	if err := Check(h.Authorizer, r, auth.Resource{Type: "ScheduledRun", Name: namespace + "/" + name}); err != nil {
		w.RespondWithError(err)
		return
	}

	sr := &v1alpha2.ScheduledRun{}
	if err := h.KubeClient.Get(r.Context(), client.ObjectKey{Namespace: namespace, Name: name}, sr); err != nil {
		if apierrors.IsNotFound(err) {
			w.RespondWithError(errors.NewNotFoundError("ScheduledRun not found", err))
			return
		}
		w.RespondWithError(errors.NewInternalServerError("Failed to get ScheduledRun", err))
		return
	}

	if err := h.KubeClient.Delete(r.Context(), sr); err != nil {
		w.RespondWithError(errors.NewInternalServerError("Failed to delete ScheduledRun", err))
		return
	}

	log.Info("Successfully deleted ScheduledRun")
	data := api.NewResponse(struct{}{}, "Successfully deleted ScheduledRun", false)
	RespondWithJSON(w, http.StatusOK, data)
}

// HandleTriggerScheduledRun handles POST /api/scheduledruns/{namespace}/{name}/trigger requests
func (h *ScheduledRunsHandler) HandleTriggerScheduledRun(w ErrorResponseWriter, r *http.Request) {
	log := ctrllog.FromContext(r.Context()).WithName("scheduledruns-handler").WithValues("operation", "trigger")

	namespace, err := GetPathParam(r, "namespace")
	if err != nil {
		w.RespondWithError(errors.NewBadRequestError("Failed to get namespace from path", err))
		return
	}

	name, err := GetPathParam(r, "name")
	if err != nil {
		w.RespondWithError(errors.NewBadRequestError("Failed to get name from path", err))
		return
	}

	log = log.WithValues("namespace", namespace, "name", name)

	if err := Check(h.Authorizer, r, auth.Resource{Type: "ScheduledRun", Name: namespace + "/" + name}); err != nil {
		w.RespondWithError(err)
		return
	}

	sr := &v1alpha2.ScheduledRun{}
	if err := h.KubeClient.Get(r.Context(), client.ObjectKey{Namespace: namespace, Name: name}, sr); err != nil {
		if apierrors.IsNotFound(err) {
			w.RespondWithError(errors.NewNotFoundError("ScheduledRun not found", err))
			return
		}
		w.RespondWithError(errors.NewInternalServerError("Failed to get ScheduledRun", err))
		return
	}

	log.Info("Manually triggering ScheduledRun")
	entry, err := h.Scheduler.TriggerManualRun(types.NamespacedName{Namespace: namespace, Name: name})
	if err != nil {
		w.RespondWithError(errors.NewInternalServerError("Failed to trigger ScheduledRun", err))
		return
	}
	data := api.NewResponse(entry, "ScheduledRun triggered successfully", false)
	RespondWithJSON(w, http.StatusOK, data)
}
