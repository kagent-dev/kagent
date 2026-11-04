package a2a

import (
	"encoding/json"
	"slices"
	"strings"

	a2atype "github.com/a2aproject/a2a-go/v2/a2a"
)

// TaskText extracts the native visible result of a task for MCP and delegation.
func TaskText(task *a2atype.Task) string {
	var text strings.Builder
	if task.Status.Message != nil {
		text.WriteString(ExtractText(task.Status.Message))
	}
	for _, artifact := range task.Artifacts {
		for _, part := range artifact.Parts {
			if part != nil {
				text.WriteString(part.Text())
			}
		}
	}
	if text.Len() == 0 {
		for _, message := range slices.Backward(task.History) {
			if message.Role == a2atype.MessageRoleAgent {
				text.WriteString(ExtractText(message))
				break
			}
		}
	}
	if text.Len() == 0 {
		data, _ := json.Marshal(task)
		return string(data)
	}
	return text.String()
}
