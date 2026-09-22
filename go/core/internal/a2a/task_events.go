package a2a

import (
	"fmt"
	"slices"

	a2apb "github.com/a2aproject/a2a-go/v2/a2apb/v1"
	"github.com/a2aproject/a2a-go/v2/a2apb/v1/pbconv"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/structpb"
)

// ApplyTaskEvent computes the next task state without mutating the stored task. Messages
// contribute to history but do not change this projection; explicit task events do. It
// rejects identity changes, missing prerequisites, and task snapshots containing
// unarchived history.
func ApplyTaskEvent(stored *a2apb.Task, event *a2apb.StreamResponse) (*a2apb.Task, error) {
	decoded, err := pbconv.FromProtoStreamResponse(event)
	if err != nil {
		return nil, err
	}
	if stored != nil {
		info := decoded.TaskInfo()
		if string(info.TaskID) != stored.Id || info.ContextID != stored.ContextId {
			return nil, fmt.Errorf("task event changes stored identity")
		}
	}
	if snapshot := event.GetTask(); snapshot != nil {
		next := proto.Clone(snapshot).(*a2apb.Task)
		if len(next.History) != 0 {
			return nil, fmt.Errorf("task snapshot contains unarchived history")
		}
		return next, nil
	}
	if stored == nil {
		return nil, fmt.Errorf("task event has no creation snapshot")
	}
	next := proto.Clone(stored).(*a2apb.Task)
	switch payload := event.Payload.(type) {
	case *a2apb.StreamResponse_StatusUpdate:
		next.Status = proto.Clone(payload.StatusUpdate.Status).(*a2apb.TaskStatus)
		if metadata := payload.StatusUpdate.Metadata; metadata != nil {
			if next.Metadata == nil {
				next.Metadata = &structpb.Struct{}
			}
			proto.Merge(next.Metadata, metadata)
		}
	case *a2apb.StreamResponse_ArtifactUpdate:
		update := payload.ArtifactUpdate
		artifact := proto.Clone(update.Artifact).(*a2apb.Artifact)
		index := slices.IndexFunc(next.Artifacts, func(a *a2apb.Artifact) bool { return a.ArtifactId == artifact.ArtifactId })
		switch {
		case update.Append:
			if index < 0 {
				return nil, fmt.Errorf("no artifact found for append")
			}
			existing := next.Artifacts[index]
			existing.Parts = append(existing.Parts, artifact.Parts...)
			if artifact.Metadata != nil {
				if existing.Metadata == nil {
					existing.Metadata = &structpb.Struct{}
				}
				proto.Merge(existing.Metadata, artifact.Metadata)
			}
		case index < 0:
			next.Artifacts = append(next.Artifacts, artifact)
		default:
			next.Artifacts[index] = artifact
		}
	case *a2apb.StreamResponse_Message:
	default:
		return nil, fmt.Errorf("unsupported task event %T", event.Payload)
	}
	return next, nil
}
