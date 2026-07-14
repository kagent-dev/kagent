package handlers

import (
	"fmt"
	"net/http"

	a2a "github.com/a2aproject/a2a-go/v2/a2a"
	api "github.com/kagent-dev/kagent/go/api/httpapi"
	"github.com/kagent-dev/kagent/go/core/internal/httpserver/errors"
	"github.com/kagent-dev/kagent/go/core/internal/utils"
	"github.com/kagent-dev/kagent/go/core/pkg/a2acompat/trpcv0"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"
	"trpc.group/trpc-go/trpc-a2a-go/protocol"
)

// TasksHandler handles task-related requests
type TasksHandler struct {
	*Base
}

// NewTasksHandler creates a new TasksHandler
func NewTasksHandler(base *Base) *TasksHandler {
	return &TasksHandler{Base: base}
}

func (h *TasksHandler) HandleGetTask(w ErrorResponseWriter, r *http.Request) {
	log := ctrllog.FromContext(r.Context()).WithName("tasks-handler").WithValues("operation", "get-task")

	taskID, err := GetPathParam(r, "task_id")
	if err != nil {
		w.RespondWithError(errors.NewBadRequestError("Failed to get task ID from path", err))
		return
	}
	log = log.WithValues("task_id", taskID)

	task, err := h.DatabaseService.GetTask(r.Context(), taskID)
	if err != nil {
		w.RespondWithError(errors.NewNotFoundError("Task not found", err))
		return
	}
	wireVersion, err := utils.NegotiateA2AWireVersion(r)
	if err != nil {
		w.RespondWithError(errors.NewBadRequestError("Unsupported A2A version", err))
		return
	}

	log.Info("Successfully retrieved task")
	// TODO(0.11.0): Remove legacy API conversion after legacy wire support is no longer supported.
	// Currently this will return either legacy or v1 task depending on the wire version
	var data any
	switch wireVersion {
	case utils.A2AWireVersionLegacy:
		legacyTask, convErr := trpcv0.ToLegacyTask(task)
		if convErr != nil {
			w.RespondWithError(errors.NewInternalServerError("Failed to convert task", convErr))
			return
		}
		data = legacyTask
	case utils.A2AWireVersionV1:
		data = task
	default:
		w.RespondWithError(errors.NewBadRequestError("Unsupported A2A version", fmt.Errorf("unknown negotiated wire version %q", wireVersion)))
		return
	}
	response := api.NewResponse(data, "Successfully retrieved task", false)
	RespondWithJSON(w, http.StatusOK, response)
}

func (h *TasksHandler) HandleCreateTask(w ErrorResponseWriter, r *http.Request) {
	log := ctrllog.FromContext(r.Context()).WithName("tasks-handler").WithValues("operation", "create-task")

	wireVersion, err := utils.NegotiateA2AWireVersion(r)
	if err != nil {
		w.RespondWithError(errors.NewBadRequestError("Unsupported A2A version", err))
		return
	}

	task := a2a.Task{}
	// TODO(0.11.0): Remove legacy API conversion after legacy wire support is no longer supported.
	switch wireVersion {
	case utils.A2AWireVersionLegacy:
		legacyTask := protocol.Task{}
		if err := DecodeJSONBody(r, &legacyTask); err != nil {
			w.RespondWithError(errors.NewBadRequestError("Invalid request body", err))
			return
		}
		converted, convErr := trpcv0.ToV1Task(&legacyTask)
		if convErr != nil {
			w.RespondWithError(errors.NewBadRequestError("Invalid legacy task payload", convErr))
			return
		}
		if converted != nil {
			task = *converted
		}
	case utils.A2AWireVersionV1:
		if err := DecodeJSONBody(r, &task); err != nil {
			w.RespondWithError(errors.NewBadRequestError("Invalid request body", err))
			return
		}
	default:
		w.RespondWithError(errors.NewBadRequestError("Unsupported A2A version", fmt.Errorf("unknown negotiated wire version %q", wireVersion)))
		return
	}
	if task.ID == "" {
		task.ID = a2a.NewTaskID()
	}
	log = log.WithValues("task_id", task.ID)

	if err := h.DatabaseService.StoreTask(r.Context(), &task); err != nil {
		w.RespondWithError(errors.NewInternalServerError("Failed to create task", err))
		return
	}

	log.Info("Successfully created task")
	var data any
	switch wireVersion {
	case utils.A2AWireVersionLegacy:
		legacyTask, convErr := trpcv0.ToLegacyTask(&task)
		if convErr != nil {
			w.RespondWithError(errors.NewInternalServerError("Failed to convert task", convErr))
			return
		}
		data = legacyTask
	case utils.A2AWireVersionV1:
		data = task
	default:
		w.RespondWithError(errors.NewBadRequestError("Unsupported A2A version", fmt.Errorf("unknown negotiated wire version %q", wireVersion)))
		return
	}
	response := api.NewResponse(data, "Successfully created task", false)
	RespondWithJSON(w, http.StatusCreated, response)
}

func (h *TasksHandler) HandleDeleteTask(w ErrorResponseWriter, r *http.Request) {
	log := ctrllog.FromContext(r.Context()).WithName("tasks-handler").WithValues("operation", "delete-task")

	taskID, err := GetPathParam(r, "task_id")
	if err != nil {
		w.RespondWithError(errors.NewBadRequestError("Failed to get task ID from path", err))
		return
	}
	log = log.WithValues("task_id", taskID)

	if err := h.DatabaseService.DeleteTask(r.Context(), taskID); err != nil {
		w.RespondWithError(errors.NewInternalServerError("Failed to delete task", err))
		return
	}

	log.Info("Successfully deleted task")
	w.WriteHeader(http.StatusNoContent)
}
