package a2a

import (
	"context"
	"fmt"

	common "github.com/kagent-dev/kagent/go/controller/internal/utils"
	"github.com/kagent-dev/kagent/go/controller/utils/a2autils"
	ctrl "sigs.k8s.io/controller-runtime"
	"trpc.group/trpc-go/trpc-a2a-go/protocol"
	"trpc.group/trpc-go/trpc-a2a-go/taskmanager"
)

var (
	processorLog = ctrl.Log.WithName("a2a_task_processor")
)

type TaskHandler func(ctx context.Context, task string, sessionID *string) (string, error)

type a2aTaskProcessor struct {
	// handleTask is a function that processes the input text.
	// in production this is done by handing off the input text by a call to
	// the underlying agentic framework (e.g.: autogen)
	handleTask TaskHandler
}

var _ taskmanager.TaskProcessor = &a2aTaskProcessor{}

// newA2ATaskProcessor creates a new A2A task processor.
func newA2ATaskProcessor(handleTask TaskHandler) taskmanager.TaskProcessor {
	return &a2aTaskProcessor{
		handleTask: handleTask,
	}
}

func (a *a2aTaskProcessor) Process(
	ctx context.Context,
	taskID string,
	message protocol.Message,
	handle taskmanager.TaskHandle,
) error {

	// Extract text from the incoming message.
	text := a2autils.ExtractText(message)
	if text == "" {
		err := fmt.Errorf("input message must contain text")
		a.handleErr(taskID, err, handle)
		return err
	}

	processorLog.Info("Processing task", "taskID", taskID, "text", text)

	// Process the input text (in this simple example, we'll just reverse it).
	sessionID := handle.GetSessionID()
	result, err := a.handleTask(ctx, text, sessionID)
	if err != nil {
		a.handleErr(taskID, err, handle)
		return err
	}

	// Create response message.
	responseMessage := protocol.NewMessage(
		protocol.MessageRoleAgent,
		[]protocol.Part{protocol.NewTextPart(fmt.Sprintf("Processed result: %s", result))},
	)

	// Update task status to completed.
	if err := handle.UpdateStatus(protocol.TaskStateCompleted, &responseMessage); err != nil {
		return fmt.Errorf("failed to update task status: %w", err)
	}

	// Add the processed text as an artifact.
	artifact := protocol.Artifact{
		Name:        common.MakePtr("Task Result"),
		Description: common.MakePtr("The result of the task processing"),
		Index:       0,
		Parts:       []protocol.Part{protocol.NewTextPart(result)},
		LastChunk:   common.MakePtr(true),
	}

	if err := handle.AddArtifact(artifact); err != nil {
		processorLog.Error(err, "Error adding artifact", "taskID", taskID)
	}

	return nil
}

func (a a2aTaskProcessor) handleErr(
	taskID string,
	err error,
	handle taskmanager.TaskHandle,
) {
	processorLog.Error(err, "Task failed", "taskID", taskID)

	// Update status to Failed via handle.
	failedMessage := protocol.NewMessage(
		protocol.MessageRoleAgent,
		[]protocol.Part{protocol.NewTextPart(err.Error())},
	)
	updateStatusErr := handle.UpdateStatus(protocol.TaskStateFailed, &failedMessage)
	if updateStatusErr != nil {
		processorLog.Error(updateStatusErr, "Failed to update task status", "taskID", taskID)
	}
}
