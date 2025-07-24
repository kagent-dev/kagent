package utils

import (
	"encoding/json"
	"fmt"

	"github.com/kagent-dev/kagent/go/internal/autogen/client"
	autogen_client "github.com/kagent-dev/kagent/go/internal/autogen/client"
	"trpc.group/trpc-go/trpc-a2a-go/protocol"
)

func ConvertMessagesToAutogenEvents(messages []protocol.Message) ([]autogen_client.Event, error) {
	result := make([]client.Event, 0, len(messages))
	for _, message := range messages {
		source := "user"
		if message.Role == protocol.MessageRoleAgent {
			source = "agent"
		}
		for _, part := range message.Parts {
			if textPart, ok := part.(*protocol.TextPart); ok {
				events := autogen_client.NewTextMessage(textPart.Text, source)
				result = append(result, events)
			} else if dataPart, ok := part.(*protocol.DataPart); ok {
				byt, err := json.Marshal(dataPart.Data)
				if err != nil {
					return nil, fmt.Errorf("failed to marshal data part: %w", err)
				}
				parsedEvent, err := autogen_client.ParseEvent(byt)
				if err != nil {
					return nil, fmt.Errorf("failed to parse event: %w", err)
				}
				result = append(result, parsedEvent)
			}
		}
	}
	return result, nil
}

func ConvertAutogenEventsToMessages(taskId, contextId *string, events ...client.Event) []*protocol.Message {
	result := make([]*protocol.Message, 0, len(events))

	for _, event := range events {
		role := protocol.MessageRoleUser
		switch typed := event.(type) {
		case *client.TextMessage:
			if typed.Source != "user" {
				role = protocol.MessageRoleAgent
			}
			result = append(result, newMessage(
				role,
				[]protocol.Part{protocol.NewTextPart(typed.Content)},
				taskId,
				contextId,
				typed.Metadata,
				typed.ModelsUsage,
			))
		case *client.ModelClientStreamingChunkEvent:
			if typed.Source != "user" {
				role = protocol.MessageRoleAgent
			}
			result = append(result, newMessage(
				role,
				[]protocol.Part{protocol.NewDataPart(typed)},
				taskId,
				contextId,
				typed.Metadata,
				typed.ModelsUsage,
			))
		case *client.ToolCallRequestEvent:
			if typed.Source != "user" {
				role = protocol.MessageRoleAgent
			}
			result = append(result, newMessage(
				role,
				[]protocol.Part{protocol.NewDataPart(typed)},
				taskId,
				contextId,
				typed.Metadata,
				typed.ModelsUsage,
			))
		case *client.ToolCallExecutionEvent:
			if typed.Source != "user" {
				role = protocol.MessageRoleAgent
			}
			result = append(result, newMessage(
				role,
				[]protocol.Part{protocol.NewDataPart(typed)},
				taskId,
				contextId,
				typed.Metadata,
				typed.ModelsUsage,
			))
		case *client.MemoryQueryEvent:
			if typed.Source != "user" {
				role = protocol.MessageRoleAgent
			}
			result = append(result, newMessage(
				role,
				[]protocol.Part{protocol.NewDataPart(typed)},
				taskId,
				contextId,
				typed.Metadata,
				typed.ModelsUsage,
			))
		case *client.ToolCallSummaryMessage:
			if typed.Source != "user" {
				role = protocol.MessageRoleAgent
			}
			result = append(result, newMessage(
				role,
				[]protocol.Part{protocol.NewDataPart(typed)},
				taskId,
				contextId,
				typed.Metadata,
				typed.ModelsUsage,
			))
		}
	}
	return result
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
