package handlers

import (
	"fmt"
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

// ScheduledRunTrigger is the interface for triggering scheduled runs manually
type ScheduledRunTrigger interface {
	TriggerManualRun(key types.NamespacedName)
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

const MinScheduleInterval = time.Hour

// ValidateScheduleFrequency ensures the cron expression doesn't fire more frequently than once per hour.
func ValidateScheduleFrequency(schedule string) *errors.APIError {
	sched, err := cron.ParseStandard(schedule)
	if err != nil {
		return errors.NewBadRequestError(fmt.Sprintf("Invalid cron expression: %v", err), nil)
	}
	now := time.Now()
	first := sched.Next(now)
	second := sched.Next(first)
	interval := second.Sub(first)
	if interval < MinScheduleInterval {
		return errors.NewBadRequestError(
			fmt.Sprintf("Schedule frequency too high: minimum interval is 1 hour, got %v", interval),
			nil,
		)
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

	if apiErr := ValidateScheduleFrequency(sr.Spec.Schedule); apiErr != nil {
		w.RespondWithError(apiErr)
		return
	}

	if sr.Namespace == "" {
		sr.Namespace = utils.GetResourceNamespace()
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

	if apiErr := ValidateScheduleFrequency(incoming.Spec.Schedule); apiErr != nil {
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
	h.Scheduler.TriggerManualRun(types.NamespacedName{Namespace: namespace, Name: name})
	data := api.NewResponse(struct{}{}, "ScheduledRun triggered successfully", false)
	RespondWithJSON(w, http.StatusAccepted, data)
}
