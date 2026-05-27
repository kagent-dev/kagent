package handlers

import (
	"net/http"

	a2a "github.com/a2aproject/a2a-go/v2/a2a"
	api "github.com/kagent-dev/kagent/go/api/httpapi"
	"github.com/kagent-dev/kagent/go/core/internal/httpserver/errors"
	"github.com/kagent-dev/kagent/go/core/internal/utils"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"
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

	if err := utils.NegotiateA2AWireVersion(r); err != nil {
		w.RespondWithError(errors.NewBadRequestError("Unsupported A2A version", err))
		return
	}

	task, err := h.DatabaseService.GetTask(r.Context(), taskID)
	if err != nil {
		w.RespondWithError(errors.NewNotFoundError("Task not found", err))
		return
	}

	log.Info("Successfully retrieved task")
	response := api.NewResponse(task, "Successfully retrieved task", false)
	RespondWithJSON(w, http.StatusOK, response)
}

func (h *TasksHandler) HandleCreateTask(w ErrorResponseWriter, r *http.Request) {
	log := ctrllog.FromContext(r.Context()).WithName("tasks-handler").WithValues("operation", "create-task")

	if err := utils.NegotiateA2AWireVersion(r); err != nil {
		w.RespondWithError(errors.NewBadRequestError("Unsupported A2A version", err))
		return
	}

	task := a2a.Task{}
	if err := DecodeJSONBody(r, &task); err != nil {
		w.RespondWithError(errors.NewBadRequestError("Invalid request body", err))
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
	response := api.NewResponse(task, "Successfully created task", false)
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
