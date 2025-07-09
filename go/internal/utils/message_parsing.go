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
