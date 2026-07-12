package handlers

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/robfig/cron/v3"

	api "github.com/kagent-dev/kagent/go/api/httpapi"
	"github.com/kagent-dev/kagent/go/api/v1alpha2"
	"github.com/kagent-dev/kagent/go/core/internal/httpserver/errors"
	"github.com/kagent-dev/kagent/go/core/internal/scheduledrun"
	"github.com/kagent-dev/kagent/go/core/internal/utils"
	"github.com/kagent-dev/kagent/go/core/pkg/auth"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/retry"
	"sigs.k8s.io/controller-runtime/pkg/client"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"
)

const maxScheduledRunHistory = 100

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

// validateSchedule validates the cron expression syntax and the IANA time
// zone. Both are checked at the API edge so a bad request is
// rejected with 400 before it ever reaches the controller, where the same
// invariants are re-checked against the persisted object.
func validateSchedule(schedule, timeZone string) *errors.APIError {
	schedule = strings.TrimSpace(schedule)
	timeZone = strings.TrimSpace(timeZone)
	if schedule == "" {
		return errors.NewBadRequestError("spec.schedule is required (request body must be {\"spec\":{...}})", nil)
	}
	if timeZone == "" {
		timeZone = v1alpha2.DefaultScheduledRunTimeZone
	}
	if _, err := time.LoadLocation(timeZone); err != nil {
		return errors.NewBadRequestError("Invalid time zone: "+err.Error(), nil)
	}
	expr := "CRON_TZ=" + timeZone + " " + schedule
	if _, err := cron.ParseStandard(expr); err != nil {
		return errors.NewBadRequestError("Invalid cron expression: "+err.Error(), nil)
	}
	return nil
}

func normalizeScheduledRun(sr *v1alpha2.ScheduledRun) {
	sr.Spec.Schedule = strings.TrimSpace(sr.Spec.Schedule)
	sr.Spec.TimeZone = strings.TrimSpace(sr.Spec.TimeZone)
	if sr.Spec.TimeZone == "" {
		sr.Spec.TimeZone = v1alpha2.DefaultScheduledRunTimeZone
	}
	if sr.Spec.TargetRef.Kind == "" {
		sr.Spec.TargetRef.Kind = v1alpha2.ScheduledRunTargetKindAgent
	}
	if sr.Spec.TargetRef.APIGroup == nil || *sr.Spec.TargetRef.APIGroup == "" {
		apiGroup := v1alpha2.ScheduledRunTargetAPIGroup
		sr.Spec.TargetRef.APIGroup = &apiGroup
	}
	if sr.Spec.MaxRunHistory == 0 {
		sr.Spec.MaxRunHistory = v1alpha2.DefaultScheduledRunMaxRunHistory
	}
}

func validateScheduledRunPrompt(prompt string) *errors.APIError {
	if strings.TrimSpace(prompt) == "" {
		return errors.NewBadRequestError("spec.prompt is required", nil)
	}
	return nil
}

func validateScheduledRunMaxHistory(maxRunHistory int) *errors.APIError {
	if maxRunHistory < 0 || maxRunHistory > maxScheduledRunHistory {
		return errors.NewBadRequestError(
			fmt.Sprintf("spec.maxRunHistory must be between 1 and %d, or 0 to use the default", maxScheduledRunHistory),
			nil,
		)
	}
	return nil
}

func (h *ScheduledRunsHandler) validateScheduledRunObject(r *http.Request, sr *v1alpha2.ScheduledRun) *errors.APIError {
	if apiErr := validateSchedule(sr.Spec.Schedule, sr.Spec.TimeZone); apiErr != nil {
		return apiErr
	}
	if apiErr := validateScheduledRunPrompt(sr.Spec.Prompt); apiErr != nil {
		return apiErr
	}
	if apiErr := validateScheduledRunMaxHistory(sr.Spec.MaxRunHistory); apiErr != nil {
		return apiErr
	}
	return h.validateScheduledRunTarget(r, sr.Namespace, sr.Spec.TargetRef)
}

func (h *ScheduledRunsHandler) validateScheduledRunTarget(r *http.Request, srNamespace string, ref corev1.TypedLocalObjectReference) *errors.APIError {
	if err := scheduledrun.ValidateTargetRef(ref); err != nil {
		return errors.NewBadRequestError(err.Error(), nil)
	}
	kind := scheduledrun.TargetKind(ref)
	key := scheduledrun.TargetKey(srNamespace, ref)
	target, err := scheduledrun.NewTargetObject(kind)
	if err != nil {
		return errors.NewBadRequestError(err.Error(), nil)
	}
	if apiErr := h.checkScheduledRunTargetAccess(r, key); apiErr != nil {
		return apiErr
	}
	if err := h.KubeClient.Get(r.Context(), key, target); err != nil {
		if apierrors.IsNotFound(err) {
			return errors.NewNotFoundError(fmt.Sprintf("%s %s not found", kind, key), err)
		}
		return errors.NewInternalServerError(fmt.Sprintf("Failed to get %s %s", kind, key), err)
	}
	return nil
}

func (h *ScheduledRunsHandler) checkScheduledRunTargetAccess(r *http.Request, key types.NamespacedName) *errors.APIError {
	principal, err := GetPrincipal(r)
	if err != nil {
		return errors.NewBadRequestError("Failed to get user ID", err)
	}
	if err := h.Authorizer.Check(r.Context(), principal, auth.VerbGet, auth.Resource{Type: "Agent", Name: key.String()}); err != nil {
		return errors.NewForbiddenError("Not authorized to reference target agent", err)
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

	if sr.Namespace == "" {
		sr.Namespace = utils.GetResourceNamespace()
	}
	normalizeScheduledRun(&sr)

	if apiErr := h.validateScheduledRunObject(r, &sr); apiErr != nil {
		w.RespondWithError(apiErr)
		return
	}

	log = log.WithValues("namespace", sr.Namespace, "name", sr.Name)

	if err := h.KubeClient.Create(r.Context(), &sr); err != nil {
		if apierrors.IsInvalid(err) {
			w.RespondWithError(errors.NewBadRequestError("Invalid ScheduledRun", err))
			return
		}
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

	preserveMaxRunHistory := incoming.Spec.MaxRunHistory == 0
	incoming.Name = name
	incoming.Namespace = namespace
	normalizeScheduledRun(&incoming)
	if apiErr := h.validateScheduledRunObject(r, &incoming); apiErr != nil {
		w.RespondWithError(apiErr)
		return
	}

	var updated *v1alpha2.ScheduledRun
	if err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
		existing := &v1alpha2.ScheduledRun{}
		if err := h.KubeClient.Get(r.Context(), client.ObjectKey{Namespace: namespace, Name: name}, existing); err != nil {
			return err
		}

		updatedSpec := incoming.Spec
		if preserveMaxRunHistory {
			updatedSpec.MaxRunHistory = existing.Spec.MaxRunHistory
			if updatedSpec.MaxRunHistory == 0 {
				updatedSpec.MaxRunHistory = v1alpha2.DefaultScheduledRunMaxRunHistory
			}
		}
		existing.Spec = updatedSpec

		if err := h.KubeClient.Update(r.Context(), existing); err != nil {
			return err
		}
		updated = existing
		return nil
	}); err != nil {
		if apierrors.IsNotFound(err) {
			w.RespondWithError(errors.NewNotFoundError("ScheduledRun not found", err))
			return
		}
		if apierrors.IsInvalid(err) {
			w.RespondWithError(errors.NewBadRequestError("Invalid ScheduledRun", err))
			return
		}
		w.RespondWithError(errors.NewInternalServerError("Failed to update ScheduledRun", err))
		return
	}

	log.Info("Successfully updated ScheduledRun")
	data := api.NewResponse(updated, "Successfully updated ScheduledRun", false)
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
	if apiErr := h.validateScheduledRunTarget(r, sr.Namespace, sr.Spec.TargetRef); apiErr != nil {
		w.RespondWithError(apiErr)
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
