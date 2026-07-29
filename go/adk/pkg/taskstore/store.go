package taskstore

import (
	"context"
	"fmt"

	a2atype "github.com/a2aproject/a2a-go/a2a"
	a2a "github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2acompat/a2av0"
	"github.com/kagent-dev/kagent/go/adk/pkg/controllerclient"
	apiv1alpha1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1alpha1"
	"github.com/kagent-dev/kagent/go/api/structuredobject"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// Constants for partial-event metadata keys (inlined to avoid import cycle).
const (
	metadataKeyKagentPartial    = "kagent_partial"
	metadataKeyKagentAdkPartial = "kagent_adk_partial"
	metadataKeyAdkPartial       = "adk_partial"
	a2aTaskAPIVersion           = "lf.a2a.v1"
	a2aTaskKind                 = "Task"
)

// KAgentTaskStore persists A2A tasks to KAgent via gRPC and implements
// a2asrv.TaskStore.
type KAgentTaskStore struct {
	client *controllerclient.Client
}

func NewKAgentTaskStore(client *controllerclient.Client) *KAgentTaskStore {
	return &KAgentTaskStore{client: client}
}

// isPartialMeta checks if a metadata map has a partial flag set to true.
// It checks the canonical kagent key (kagent_adk_partial) as well as legacy keys
// (adk_partial, kagent_partial) so that events from any prefix are recognised.
func isPartialMeta(meta map[string]any) bool {
	if meta == nil {
		return false
	}
	for _, key := range []string{metadataKeyKagentPartial, metadataKeyAdkPartial, metadataKeyKagentAdkPartial} {
		if partial, ok := meta[key].(bool); ok && partial {
			return true
		}
	}
	return false
}

// cleanPartialEvents removes partial streaming events from history.
func cleanPartialEvents(history []*a2atype.Message) []*a2atype.Message {
	var cleaned []*a2atype.Message
	for _, item := range history {
		if item != nil && isPartialMeta(item.Metadata) {
			continue
		}
		if item != nil && len(item.Parts) > 0 {
			cleaned = append(cleaned, item)
		}
	}
	return cleaned
}

// cleanPartialArtifacts removes partial streaming artifacts.
func cleanPartialArtifacts(artifacts []*a2atype.Artifact) []*a2atype.Artifact {
	var cleaned []*a2atype.Artifact
	for _, a := range artifacts {
		if a != nil && isPartialMeta(a.Metadata) {
			continue
		}
		if a != nil && len(a.Parts) > 0 {
			cleaned = append(cleaned, a)
		}
	}
	return cleaned
}

// Save implements a2asrv.TaskStore.
func (s *KAgentTaskStore) Save(ctx context.Context, task *a2atype.Task, _ a2atype.Event, _ *a2atype.Task, _ a2atype.TaskVersion) (a2atype.TaskVersion, error) {
	if task == nil {
		return a2atype.TaskVersionMissing, fmt.Errorf("task cannot be nil")
	}

	// Work on a shallow copy so the caller's task is not mutated.
	taskCopy := *task
	if taskCopy.History != nil {
		taskCopy.History = cleanPartialEvents(taskCopy.History)
	}
	if taskCopy.Artifacts != nil {
		taskCopy.Artifacts = cleanPartialArtifacts(taskCopy.Artifacts)
	}

	canonicalTask, err := a2av0.ToV1Task(&taskCopy)
	if err != nil {
		return a2atype.TaskVersionMissing, fmt.Errorf("convert task to A2A v1: %w", err)
	}
	encoded, err := structuredobject.FromGo(canonicalTask, a2aTaskAPIVersion, a2aTaskKind, s.client.MaxMessageBytes())
	if err != nil {
		return a2atype.TaskVersionMissing, fmt.Errorf("encode task: %w", err)
	}
	callContext, cancel := s.client.CallContext(ctx, "")
	defer cancel()
	_, err = s.client.TaskService().CreateTask(callContext, &apiv1alpha1.CreateTaskRequest{Task: encoded})
	if err != nil {
		return a2atype.TaskVersionMissing, fmt.Errorf("save task: %w", err)
	}

	return a2atype.TaskVersion(1), nil
}

// Get implements a2asrv.TaskStore.
func (s *KAgentTaskStore) Get(ctx context.Context, taskID a2atype.TaskID) (*a2atype.Task, a2atype.TaskVersion, error) {
	callContext, cancel := s.client.CallContext(ctx, "")
	defer cancel()
	response, err := s.client.TaskService().GetTask(callContext, &apiv1alpha1.GetTaskRequest{TaskId: string(taskID)})
	if err != nil {
		if status.Code(err) == codes.NotFound {
			return nil, a2atype.TaskVersionMissing, a2atype.ErrTaskNotFound
		}
		return nil, a2atype.TaskVersionMissing, fmt.Errorf("get task: %w", err)
	}
	canonicalTask := new(a2a.Task)
	if err := structuredobject.ToGo(response.GetTask(), a2aTaskKind, canonicalTask, s.client.MaxMessageBytes()); err != nil {
		return nil, a2atype.TaskVersionMissing, fmt.Errorf("decode task: %w", err)
	}
	return a2av0.FromV1Task(canonicalTask), a2atype.TaskVersion(1), nil
}

// List implements a2asrv.TaskStore. Listing is not supported against the KAgent task API.
func (s *KAgentTaskStore) List(ctx context.Context, req *a2atype.ListTasksRequest) (*a2atype.ListTasksResponse, error) {
	return nil, fmt.Errorf("task listing is not supported by the KAgent task store")
}
