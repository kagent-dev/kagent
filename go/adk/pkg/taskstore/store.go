package taskstore

import (
	"context"
	"fmt"

	a2atype "github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2apb/v1/pbconv"
	a2ataskstore "github.com/a2aproject/a2a-go/v2/a2asrv/taskstore"
	"github.com/kagent-dev/kagent/go/adk/pkg/controllerclient"
	apiv1alpha1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1alpha1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const (
	metadataKeyKagentPartial    = "kagent_partial"
	metadataKeyKagentAdkPartial = "kagent_adk_partial"
	metadataKeyAdkPartial       = "adk_partial"
)

// KAgentTaskStore persists A2A tasks to KAgent via gRPC and implements
// a2asrv.TaskStore.
type KAgentTaskStore struct {
	client *controllerclient.Client
}

func NewKAgentTaskStore(client *controllerclient.Client) *KAgentTaskStore {
	return &KAgentTaskStore{client: client}
}

func (s *KAgentTaskStore) saveTask(ctx context.Context, task *a2atype.Task) (a2ataskstore.TaskVersion, error) {
	if task == nil {
		return a2ataskstore.TaskVersionMissing, fmt.Errorf("task cannot be nil")
	}

	taskCopy := *task
	taskCopy.History = make([]*a2atype.Message, 0, len(task.History))
	for _, message := range task.History {
		if message != nil && !isPartial(message.Metadata) {
			taskCopy.History = append(taskCopy.History, message)
		}
	}
	taskCopy.Artifacts = make([]*a2atype.Artifact, 0, len(task.Artifacts))
	for _, artifact := range task.Artifacts {
		if artifact != nil && !isPartial(artifact.Metadata) {
			taskCopy.Artifacts = append(taskCopy.Artifacts, artifact)
		}
	}
	encoded, err := pbconv.ToProtoTask(&taskCopy)
	if err != nil {
		return a2ataskstore.TaskVersionMissing, fmt.Errorf("encode task: %w", err)
	}
	callContext, cancel := s.client.CallContext(ctx, "")
	defer cancel()
	_, err = s.client.TaskService().UpsertTask(callContext, &apiv1alpha1.UpsertTaskRequest{Task: encoded})
	if err != nil {
		return a2ataskstore.TaskVersionMissing, fmt.Errorf("save task: %w", err)
	}

	return a2ataskstore.TaskVersionMissing, nil
}

// Create implements taskstore.Store.
func (s *KAgentTaskStore) Create(ctx context.Context, task *a2atype.Task) (a2ataskstore.TaskVersion, error) {
	return s.saveTask(ctx, task)
}

// Update implements taskstore.Store.
func (s *KAgentTaskStore) Update(ctx context.Context, update *a2ataskstore.UpdateRequest) (a2ataskstore.TaskVersion, error) {
	if update == nil {
		return a2ataskstore.TaskVersionMissing, fmt.Errorf("update request cannot be nil")
	}
	return s.saveTask(ctx, update.Task)
}

// Get implements taskstore.Store.
func (s *KAgentTaskStore) Get(ctx context.Context, taskID a2atype.TaskID) (*a2ataskstore.StoredTask, error) {
	callContext, cancel := s.client.CallContext(ctx, "")
	defer cancel()
	response, err := s.client.TaskService().GetTask(callContext, &apiv1alpha1.GetTaskRequest{TaskId: string(taskID)})
	if err != nil {
		if status.Code(err) == codes.NotFound {
			return nil, a2atype.ErrTaskNotFound
		}
		return nil, fmt.Errorf("get task: %w", err)
	}
	task, err := pbconv.FromProtoTask(response.GetTask())
	if err != nil {
		return nil, fmt.Errorf("decode task: %w", err)
	}
	return &a2ataskstore.StoredTask{
		Task:    task,
		Version: a2ataskstore.TaskVersionMissing,
	}, nil
}

func isPartial(metadata map[string]any) bool {
	for _, key := range []string{metadataKeyKagentPartial, metadataKeyKagentAdkPartial, metadataKeyAdkPartial} {
		if partial, ok := metadata[key].(bool); ok && partial {
			return true
		}
	}
	return false
}

// List implements a2asrv.TaskStore. Listing is not supported against the KAgent task API.
func (s *KAgentTaskStore) List(ctx context.Context, req *a2atype.ListTasksRequest) (*a2atype.ListTasksResponse, error) {
	return nil, fmt.Errorf("task listing is not supported by the KAgent task store")
}
