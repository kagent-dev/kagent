package database

import (
	"fmt"

	a2apb "github.com/a2aproject/a2a-go/v2/a2apb/v1"
	"github.com/google/uuid"
	"google.golang.org/protobuf/proto"
)

// forkTaskEvents assigns identities once while copying a checkpoint. Only typed
// A2A references change; opaque metadata and remote-agent references stay intact.
// Callers validate the source history before invoking this function.
func forkTaskEvents(events []sessionTaskEventRow, contextID string) ([]sessionTaskEventRow, error) {
	namespace, err := uuid.Parse(contextID)
	if err != nil {
		return nil, fmt.Errorf("invalid fork context: %w", err)
	}
	ids := make(map[string]string)
	for _, event := range events {
		ids[event.TaskID] = uuid.NewSHA1(namespace, []byte(event.TaskID)).String()
	}
	message := func(m *a2apb.Message) {
		if m == nil {
			return
		}
		m.ContextId = contextID
		if id, ok := ids[m.TaskId]; ok {
			m.TaskId = id
		}
		for i, id := range m.ReferenceTaskIds {
			if replacement, ok := ids[id]; ok {
				m.ReferenceTaskIds[i] = replacement
			}
		}
	}
	result := make([]sessionTaskEventRow, len(events))
	for i, source := range events {
		event := &a2apb.StreamResponse{}
		if err := proto.Unmarshal(source.Data, event); err != nil {
			return nil, fmt.Errorf("decode fork event: %w", err)
		}
		switch payload := event.Payload.(type) {
		case *a2apb.StreamResponse_Task:
			payload.Task.Id, payload.Task.ContextId = ids[source.TaskID], contextID
			message(payload.Task.GetStatus().GetMessage())
			for _, m := range payload.Task.History {
				message(m)
			}
		case *a2apb.StreamResponse_Message:
			message(payload.Message)
		case *a2apb.StreamResponse_StatusUpdate:
			payload.StatusUpdate.TaskId, payload.StatusUpdate.ContextId = ids[source.TaskID], contextID
			message(payload.StatusUpdate.GetStatus().GetMessage())
		case *a2apb.StreamResponse_ArtifactUpdate:
			payload.ArtifactUpdate.TaskId, payload.ArtifactUpdate.ContextId = ids[source.TaskID], contextID
		default:
			return nil, fmt.Errorf("unsupported fork event payload %T", payload)
		}
		data, err := proto.Marshal(event)
		if err != nil {
			return nil, fmt.Errorf("encode fork event: %w", err)
		}
		result[i] = source
		result[i].TaskID, result[i].Data = ids[source.TaskID], data
	}
	return result, nil
}
