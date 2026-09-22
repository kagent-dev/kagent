package database

import (
	"cmp"
	"fmt"
	"slices"

	"github.com/a2aproject/a2a-go/v2/a2a"
	a2apb "github.com/a2aproject/a2a-go/v2/a2apb/v1"
	"github.com/a2aproject/a2a-go/v2/a2apb/v1/pbconv"
	corea2a "github.com/kagent-dev/kagent/go/core/internal/a2a"
	"google.golang.org/protobuf/proto"
)

// taskTransition computes a canonical task and replayable event without writing either. It
// makes implicit message status changes explicit, retains unknown fields in unchanged
// protobuf subtrees, and rejects inconsistent task identities or projections using the
// same reducer as replay.
func taskTransition(stored *a2apb.Task, task *a2a.Task, event a2a.Event) (*a2apb.Task, *a2apb.StreamResponse, error) {
	projection := *task
	projection.History = nil
	next, err := pbconv.ToProtoTask(&projection)
	if err != nil {
		return nil, nil, err
	}
	if info := event.TaskInfo(); info.TaskID != task.ID || info.ContextID != task.ContextID {
		return nil, nil, fmt.Errorf("task event changes stored identity")
	}
	update, err := pbconv.ToProtoStreamResponse(event)
	if err != nil {
		return nil, nil, err
	}
	if status := update.GetStatusUpdate(); status != nil && !proto.Equal(status.Status, next.Status) {
		return nil, nil, fmt.Errorf("task status does not match event")
	}
	// The first durable event is always a complete initial task. Subsequent
	// events can then be replayed without a separately supplied task projection.
	if stored == nil {
		update = &a2apb.StreamResponse{Payload: &a2apb.StreamResponse_Task{Task: next}}
	} else {
		previous, err := pbconv.FromProtoTask(stored)
		if err != nil {
			return nil, nil, err
		}
		known, err := pbconv.ToProtoTask(previous)
		if err != nil {
			return nil, nil, err
		}
		status := proto.Clone(stored.Status).(*a2apb.TaskStatus)
		status.State = next.Status.State
		if !proto.Equal(known.Status.Message, next.Status.Message) {
			status.Message = next.Status.Message
		}
		if !proto.Equal(known.Status.Timestamp, next.Status.Timestamp) {
			status.Timestamp = next.Status.Timestamp
		}
		switch payload := update.Payload.(type) {
		case *a2apb.StreamResponse_Task:
			canonical := proto.Clone(stored).(*a2apb.Task)
			canonical.Status = status
			canonical.Artifacts = slices.Clone(next.Artifacts)
			for i, artifact := range canonical.Artifacts {
				old := slices.IndexFunc(stored.Artifacts, func(a *a2apb.Artifact) bool { return a.ArtifactId == artifact.ArtifactId })
				if old >= 0 && proto.Equal(known.Artifacts[old], artifact) {
					canonical.Artifacts[i] = stored.Artifacts[old]
				}
			}
			if !proto.Equal(known.Metadata, next.Metadata) {
				canonical.Metadata = next.Metadata
			}
			canonical.History = nil
			payload.Task = canonical
		case *a2apb.StreamResponse_StatusUpdate:
			payload.StatusUpdate.Status = status
		case *a2apb.StreamResponse_Message:
			// The message is archived separately; this event records its effect.
			update = &a2apb.StreamResponse{Payload: &a2apb.StreamResponse_StatusUpdate{StatusUpdate: &a2apb.TaskStatusUpdateEvent{
				TaskId: next.Id, ContextId: next.ContextId, Status: status,
			}}}
		}
	}
	result, err := corea2a.ApplyTaskEvent(stored, update)
	if err != nil {
		return nil, nil, err
	}
	decoded, err := pbconv.FromProtoTask(result)
	if err != nil {
		return nil, nil, err
	}
	known, err := pbconv.ToProtoTask(decoded)
	if err != nil {
		return nil, nil, err
	}
	if !proto.Equal(known, next) {
		return nil, nil, fmt.Errorf("task projection does not match event")
	}
	return result, update, nil
}

// replayTaskEvents rebuilds tasks solely from ordered immutable events through a
// caller-selected boundary. It validates event identities and creation records and
// restores creation order, retry metadata, and snapshot references. It does not read the
// source's current task rows.
func replayTaskEvents(events []agentInstanceTaskEventRow, contextID string) ([]agentInstanceTaskRow, error) {
	tasks := make(map[string]*a2apb.Task)
	indexes := make(map[string]int)
	var rows []agentInstanceTaskRow
	var sequence int64
	for _, source := range events {
		if source.Sequence <= sequence || source.TaskID == nil {
			return nil, fmt.Errorf("invalid task event sequence or identity at %d", source.Sequence)
		}
		sequence = source.Sequence
		event := &a2apb.StreamResponse{}
		if err := proto.Unmarshal(source.Data, event); err != nil {
			return nil, err
		}
		decoded, err := pbconv.FromProtoStreamResponse(event)
		if err != nil {
			return nil, err
		}
		info := decoded.TaskInfo()
		if string(info.TaskID) != *source.TaskID || info.ContextID != contextID {
			return nil, fmt.Errorf("event %d has inconsistent protocol identity", sequence)
		}
		if message := event.GetMessage(); message != nil {
			if source.MessageID == nil || *source.MessageID != message.MessageId {
				return nil, fmt.Errorf("event %d has inconsistent message identity", sequence)
			}
		} else if source.MessageID != nil {
			return nil, fmt.Errorf("task transition %d has a message index", sequence)
		}
		id := *source.TaskID
		if source.TaskPosition != nil {
			if tasks[id] != nil || event.GetTask() == nil {
				return nil, fmt.Errorf("invalid creation event for task %s", id)
			}
			indexes[id] = len(rows)
			rows = append(rows, agentInstanceTaskRow{
				ID: id, Position: *source.TaskPosition, CreatedAt: source.CreatedAt,
				InitialMessageID: source.InitialMessageID, RequestHash: source.RequestHash,
			})
		} else if tasks[id] == nil {
			return nil, fmt.Errorf("task %s has no creation event", id)
		}
		task, err := corea2a.ApplyTaskEvent(tasks[id], event)
		if err != nil {
			return nil, err
		}
		tasks[id] = task
		row := &rows[indexes[id]]
		if event.GetMessage() == nil {
			row.UpdatedAt = source.CreatedAt
		}
		decodedTask, err := pbconv.FromProtoTask(task)
		if err != nil {
			return nil, err
		}
		row.State, row.StatusTimestamp = string(decodedTask.Status.State), decodedTask.Status.Timestamp
		row.Data, err = proto.Marshal(task)
		if err != nil {
			return nil, err
		}
		if source.SnapshotURI != nil {
			if event.GetMessage() != nil {
				return nil, fmt.Errorf("runtime boundary requires an explicit task transition")
			}
			row.SnapshotAtespace, row.SnapshotURI, row.SnapshotContentScope = source.SnapshotAtespace, source.SnapshotURI, source.SnapshotContentScope
			row.HistorySequence = &source.Sequence
		}
	}
	slices.SortFunc(rows, func(a, b agentInstanceTaskRow) int {
		return cmp.Compare(a.Position, b.Position)
	})
	return rows, nil
}
