package a2a

import (
	"context"
	"fmt"

	"github.com/kagent-dev/kagent/go/internal/autogen/client"
	"github.com/kagent-dev/kagent/go/internal/utils"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"trpc.group/trpc-go/trpc-a2a-go/protocol"
	"trpc.group/trpc-go/trpc-a2a-go/taskmanager"
)

var (
	processorLog = ctrl.Log.WithName("a2a_task_processor")
)

type MessageHandler interface {
	HandleMessage(ctx context.Context, task string, contextID *string) ([]client.Event, error)
	HandleMessageStream(ctx context.Context, task string, contextID *string) (<-chan client.Event, error)
}

type a2aMessageProcessor struct {
	// msgHandler is a function that processes the input text.
	// in production this is done by handing off the input text by a call to
	// the underlying agentic framework (e.g.: autogen)
	msgHandler MessageHandler
}

var _ taskmanager.MessageProcessor = &a2aMessageProcessor{}

// newA2AMessageProcessor creates a new A2A message processor.
func newA2AMessageProcessor(taskHandler MessageHandler) taskmanager.MessageProcessor {
	return &a2aMessageProcessor{
		msgHandler: taskHandler,
	}
}

func (a *a2aMessageProcessor) ProcessMessage(
	ctx context.Context,
	message protocol.Message,
	options taskmanager.ProcessOptions,
	handle taskmanager.TaskHandler,
) (*taskmanager.MessageProcessingResult, error) {

	// Extract text from the incoming message.
	text := ExtractText(message)
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

	taskID, err := handle.BuildTask(message.TaskID, message.ContextID)
	if err != nil {
		return nil, err
	}

	processorLog.Info("Processing task", "taskID", taskID, "contextID", message.ContextID, "text", text)

	if !options.Streaming {
		defer handle.CleanTask(&taskID)

		if err := handle.UpdateTaskState(&taskID, protocol.TaskStateWorking, &message); err != nil {
			processorLog.Error(err, "Failed to update task state to working")
		}

		// Process the input text (in this simple example, we'll just reverse it).
		result, err := a.msgHandler.HandleMessage(ctx, text, message.ContextID)
		if err != nil {
			if err := handle.UpdateTaskState(&taskID, protocol.TaskStateFailed, &message); err != nil {
				processorLog.Error(err, "Failed to update task state to failed")
			}

			return &taskmanager.MessageProcessingResult{
				Result: buildError(err),
			}, nil
		}

		if err := handle.UpdateTaskState(&taskID, protocol.TaskStateCompleted, &message); err != nil {
			processorLog.Error(err, "Failed to update task state to completed")
		}

		textResult := client.GetLastStringMessage(result)

		// Create response message.
		responseMessage := protocol.NewMessage(
			protocol.MessageRoleAgent,
			[]protocol.Part{protocol.NewTextPart(textResult)},
		)

		return &taskmanager.MessageProcessingResult{
			Result: &responseMessage,
		}, nil
	}

	taskSubscriber, err := handle.SubscribeTask(ptr.To(taskID))
	if err != nil {
		return nil, err
	}

	events, err := a.msgHandler.HandleMessageStream(ctx, text, message.ContextID)
	if err != nil {
		if err := handle.UpdateTaskState(&taskID, protocol.TaskStateFailed, &message); err != nil {
			processorLog.Error(err, "Failed to update task state to failed")
		}

		return nil, err
	}

	go func() {
		defer handle.CleanTask(&taskID)

		if err := handle.UpdateTaskState(&taskID, protocol.TaskStateWorking, &message); err != nil {
			processorLog.Error(err, "Failed to update task state to working")
		}

		for event := range events {
			events := utils.ConvertAutogenEventsToMessages(&taskID, message.ContextID, event)
			for _, event := range events {
				event := protocol.StreamingMessageEvent{
					Result: event,
				}
				err := taskSubscriber.Send(event)
				if err != nil {
					processorLog.Error(err, "Failed to send event to task subscriber")
				}
			}
		}

		if err := handle.UpdateTaskState(&taskID, protocol.TaskStateCompleted, &message); err != nil {
			processorLog.Error(err, "Failed to update task state to completed")
		}
	}()

	return &taskmanager.MessageProcessingResult{
		StreamingEvents: taskSubscriber,
	}, nil
}

func buildError(err error) *protocol.Message {
	return &protocol.Message{
		Parts: []protocol.Part{protocol.NewTextPart(err.Error())},
	}
}
