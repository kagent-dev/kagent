package a2a

import (
	"encoding/json"
	"slices"
	"strings"

	a2atype "github.com/a2aproject/a2a-go/v2/a2a"

	apia2a "github.com/kagent-dev/kagent/go/api/a2a"
)

// TaskText is TaskReply, or the task as JSON when the task has no reply yet:
// an async invocation answers a submitted or working task, and a client that
// reads only the text content still needs its ID and state. The session
// tools use TaskReply alone, since a task dump would fill their byte budget.
func TaskText(task *a2atype.Task) string {
	if reply := TaskReply(task); reply != "" {
		return reply
	}
	data, _ := json.Marshal(task)
	return string(data)
}

// TaskReply is the text a caller reads as the task's answer: the status
// message and the artifacts, else the latest agent message of the history.
// Tool activity is not part of the reply; a structured result is its JSON.
func TaskReply(task *a2atype.Task) string {
	var texts []string
	if task.Status.Message != nil {
		texts = appendReplyText(texts, task.Status.Message.Parts)
	}
	for _, artifact := range task.Artifacts {
		if artifact != nil {
			texts = appendReplyText(texts, artifact.Parts)
		}
	}
	if len(texts) == 0 {
		for _, message := range slices.Backward(task.History) {
			if message != nil && message.Role == a2atype.MessageRoleAgent {
				texts = appendReplyText(texts, message.Parts)
				break
			}
		}
	}
	return strings.Join(texts, "\n")
}

// appendReplyText appends one message's or artifact's text. Its text parts
// are concatenated, since runtimes stream a reply as appended parts; a
// structured result stands on its own.
func appendReplyText(texts []string, parts a2atype.ContentParts) []string {
	var text strings.Builder
	flush := func() {
		if text.Len() > 0 {
			texts = append(texts, text.String())
			text.Reset()
		}
	}
	for _, part := range parts {
		if part == nil {
			continue
		}
		if apia2a.IsStructuredOutputPart(part) {
			flush()
			if encoded, err := apia2a.StructuredOutputJSON(part); err == nil {
				texts = append(texts, encoded)
			}
			continue
		}
		text.WriteString(part.Text())
	}
	flush()
	return texts
}
