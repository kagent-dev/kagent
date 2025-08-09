package handlers

import (
	"net/http"

	"github.com/kagent-dev/kagent/go/internal/httpserver/errors"
	"github.com/kagent-dev/kagent/go/pkg/client/api"
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

	task, err := h.DatabaseService.GetTask(taskID)
	if err != nil {
		w.RespondWithError(errors.NewNotFoundError("Task not found", err))
		return
	}

	log.Info("Successfully retrieved task")
	data := api.NewResponse(task, "Successfully retrieved task", false)
	RespondWithJSON(w, http.StatusOK, data)
}

func (h *TasksHandler) HandleCreateTask(w ErrorResponseWriter, r *http.Request) {
	log := ctrllog.FromContext(r.Context()).WithName("tasks-handler").WithValues("operation", "create-task")

	task := protocol.Task{}
	if err := DecodeJSONBody(r, &task); err != nil {
		w.RespondWithError(errors.NewBadRequestError("Invalid request body", err))
		return
	}
	if task.ID == "" {
		task.ID = protocol.GenerateTaskID()
	}
	log = log.WithValues("task_id", task.ID)

	if err := h.DatabaseService.StoreTask(&task); err != nil {
		w.RespondWithError(errors.NewInternalServerError("Failed to create task", err))
		return
	}

	log.Info("Successfully created task")
	data := api.NewResponse(task, "Successfully created task", false)
	RespondWithJSON(w, http.StatusCreated, data)
}

func (h *TasksHandler) HandleDeleteTask(w ErrorResponseWriter, r *http.Request) {
	log := ctrllog.FromContext(r.Context()).WithName("tasks-handler").WithValues("operation", "delete-task")

	taskID, err := GetPathParam(r, "task_id")
	if err != nil {
		w.RespondWithError(errors.NewBadRequestError("Failed to get task ID from path", err))
		return
	}
	log = log.WithValues("task_id", taskID)

	if err := h.DatabaseService.DeleteTask(taskID); err != nil {
		w.RespondWithError(errors.NewInternalServerError("Failed to delete task", err))
		return
	}

	log.Info("Successfully deleted task")
	w.WriteHeader(http.StatusNoContent)
}
