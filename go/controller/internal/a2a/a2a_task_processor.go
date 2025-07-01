package a2a

import (
	"context"
	"fmt"

	"github.com/kagent-dev/kagent/go/autogen/client"
	"github.com/kagent-dev/kagent/go/controller/utils/a2autils"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"trpc.group/trpc-go/trpc-a2a-go/protocol"
	"trpc.group/trpc-go/trpc-a2a-go/taskmanager"
)

var (
	processorLog = ctrl.Log.WithName("a2a_task_processor")
)

type MessageHandler interface {
	HandleMessage(ctx context.Context, task string, contextID string) ([]client.Event, error)
	HandleMessageStream(ctx context.Context, task string, contextID string) (<-chan client.Event, error)
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

	processorLog.Info("Processing task", "taskID", message.TaskID, "contextID", message.ContextID, "text", text)

	if !options.Streaming {
		// Process the input text (in this simple example, we'll just reverse it).
		contextID := handle.GetContextID()
		result, err := a.msgHandler.HandleMessage(ctx, text, contextID)
		if err != nil {
			message := protocol.NewMessage(
				protocol.MessageRoleAgent,
				[]protocol.Part{protocol.NewTextPart(err.Error())},
			)
			return &taskmanager.MessageProcessingResult{
				Result: &message,
			}, nil
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

	events, err := a.msgHandler.HandleMessageStream(ctx, text, handle.GetContextID())
	if err != nil {
		return nil, err
	}

	taskID, err := handle.BuildTask(message.TaskID, message.ContextID)
	if err != nil {
		return nil, err
	}

	taskSubscriber, err := handle.SubScribeTask(ptr.To(taskID))
	if err != nil {
		return nil, err
	}

	go func() {
		defer func() {
			if taskSubscriber != nil {
				taskSubscriber.Close()
			}

			handle.CleanTask(&taskID)
		}()

		// Send task status update - working
		workingEvent := protocol.StreamingMessageEvent{
			Result: &protocol.TaskStatusUpdateEvent{
				TaskID: taskID,
				Kind:   protocol.KindTaskStatusUpdate,
				Status: protocol.TaskStatus{
					State: protocol.TaskStateWorking,
				},
			},
		}
		err = taskSubscriber.Send(workingEvent)
		if err != nil {
			processorLog.Error(err, "Failed to send working event to task subscriber")
		}

		for event := range events {
			err := taskSubscriber.Send(convertAutogenTypeToA2AType(event, &taskID, message.ContextID))
			if err != nil {
				processorLog.Error(err, "Failed to send event to task subscriber")
			}
		}

		// Send task completion
		completedEvent := protocol.StreamingMessageEvent{
			Result: &protocol.TaskStatusUpdateEvent{
				TaskID: taskID,
				Kind:   protocol.KindTaskStatusUpdate,
				Status: protocol.TaskStatus{
					State: protocol.TaskStateCompleted,
				},
				Final: true,
			},
		}
		err = taskSubscriber.Send(completedEvent)
		if err != nil {
			processorLog.Error(err, "Failed to send completed event to task subscriber")
		}
	}()

	return &taskmanager.MessageProcessingResult{
		StreamingEvents: taskSubscriber,
	}, nil
}

func convertAutogenTypeToA2AType(event client.Event, taskId, contextId *string) protocol.StreamingMessageEvent {
	switch typed := event.(type) {
	case *client.TextMessage:
		return protocol.StreamingMessageEvent{
			Result: newMessage(
				protocol.MessageRoleAgent,
				[]protocol.Part{protocol.NewTextPart(typed.Content)},
				taskId,
				contextId,
				typed.Metadata,
				typed.ModelsUsage,
			),
		}
	case *client.ModelClientStreamingChunkEvent:
		return protocol.StreamingMessageEvent{
			Result: newMessage(
				protocol.MessageRoleAgent,
				[]protocol.Part{protocol.NewTextPart(typed.Content)},
				taskId,
				contextId,
				typed.Metadata,
				typed.ModelsUsage,
			),
		}
	case *client.ToolCallRequestEvent:
		return protocol.StreamingMessageEvent{
			Result: newMessage(
				protocol.MessageRoleAgent,
				[]protocol.Part{protocol.NewDataPart(typed.Content)},
				taskId,
				contextId,
				typed.Metadata,
				typed.ModelsUsage,
			),
		}
	case *client.ToolCallExecutionEvent:
		return protocol.StreamingMessageEvent{
			Result: newMessage(
				protocol.MessageRoleAgent,
				[]protocol.Part{protocol.NewDataPart(typed.Content)},
				taskId,
				contextId,
				typed.Metadata,
				typed.ModelsUsage,
			),
		}
	case *client.MemoryQueryEvent:
		return protocol.StreamingMessageEvent{
			Result: newMessage(
				protocol.MessageRoleAgent,
				[]protocol.Part{protocol.NewDataPart(typed.Content)},
				taskId,
				contextId,
				typed.Metadata,
				typed.ModelsUsage,
			),
		}
	case *client.ToolCallSummaryMessage:
		return protocol.StreamingMessageEvent{
			Result: newMessage(
				protocol.MessageRoleAgent,
				[]protocol.Part{protocol.NewDataPart(typed.ToolCalls), protocol.NewDataPart(typed.Results)},
				taskId,
				contextId,
				typed.Metadata,
				typed.ModelsUsage,
			),
		}
	default:
		return protocol.StreamingMessageEvent{
			Result: &protocol.Message{
				Parts: []protocol.Part{protocol.NewTextPart(fmt.Sprintf("Unsupported event type: %T", event))},
			},
		}
	}
}

func newMessage(
	role protocol.MessageRole,
	parts []protocol.Part,
	taskId,
	contextId *string,
	metadata map[string]string,
	modelsUsage *client.ModelsUsage,
) *protocol.Message {
	msg := protocol.NewMessageWithContext(
		role,
		parts,
		taskId,
		contextId,
	)
	msg.Metadata = buildMetadata(metadata, modelsUsage)
	return &msg
}

func buildMetadata(metadata map[string]string, modelsUsage *client.ModelsUsage) map[string]interface{} {
	result := make(map[string]interface{})
	for k, v := range metadata {
		result[k] = v
	}
	if modelsUsage != nil {
		result["usage"] = modelsUsage.ToMap()
	}
	return result
}
