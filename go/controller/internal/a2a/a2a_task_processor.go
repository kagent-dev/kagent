package a2a

import (
	"context"
	"fmt"

	"github.com/kagent-dev/kagent/go/controller/utils/a2autils"
	ctrl "sigs.k8s.io/controller-runtime"
	"trpc.group/trpc-go/trpc-a2a-go/protocol"
	"trpc.group/trpc-go/trpc-a2a-go/taskmanager"
)

var (
	processorLog = ctrl.Log.WithName("a2a_task_processor")
)

type TaskHandler func(ctx context.Context, task string, contextID string) (string, error)

type a2aTaskProcessor struct {
	// handleTask is a function that processes the input text.
	// in production this is done by handing off the input text by a call to
	// the underlying agentic framework (e.g.: autogen)
	handleTask TaskHandler
}

var _ taskmanager.MessageProcessor = &a2aTaskProcessor{}

// newA2ATaskProcessor creates a new A2A task processor.
func newA2ATaskProcessor(handleTask TaskHandler) taskmanager.MessageProcessor {
	return &a2aTaskProcessor{
		handleTask: handleTask,
	}
}

func (a *a2aTaskProcessor) ProcessMessage(
	ctx context.Context,
	message protocol.Message,
	options taskmanager.ProcessOptions,
	handle taskmanager.TaskHandler,
) (*taskmanager.MessageProcessingResult, error) {

	if options.Streaming {
		return nil, fmt.Errorf("streaming not yet supported")
	}

	// Extract text from the incoming message.
	taskID := message.TaskID
	text := a2autils.ExtractText(message)
	if text == "" {
		err := fmt.Errorf("input message must contain text")
		message := protocol.NewMessage(
			protocol.MessageRoleAgent,
			[]protocol.Part{protocol.NewTextPart(err.Error())},
		)
		return &taskmanager.MessageProcessingResult{
			Result: &message,
		}, nil
	}

	processorLog.Info("Processing task", "taskID", taskID, "text", text)

	// Process the input text (in this simple example, we'll just reverse it).
	sessionID := handle.GetContextID()
	result, err := a.handleTask(ctx, text, sessionID)
	if err != nil {
		message := protocol.NewMessage(
			protocol.MessageRoleAgent,
			[]protocol.Part{protocol.NewTextPart(err.Error())},
		)
		return &taskmanager.MessageProcessingResult{
			Result: &message,
		}, nil
	}

	// Create response message.
	responseMessage := protocol.NewMessage(
		protocol.MessageRoleAgent,
		[]protocol.Part{protocol.NewTextPart(fmt.Sprintf("Processed result: %s", result))},
	)

	return &taskmanager.MessageProcessingResult{
		Result: &responseMessage,
	}, nil
}
