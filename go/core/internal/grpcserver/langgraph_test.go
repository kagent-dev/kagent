package grpcserver

import (
	"cmp"
	"context"
	"net"
	"slices"
	"testing"

	"github.com/kagent-dev/kagent/go/api/database"
	apiv1alpha1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1alpha1"
	authimpl "github.com/kagent-dev/kagent/go/core/internal/httpserver/auth"
	langgraphservice "github.com/kagent-dev/kagent/go/core/internal/service/langgraph"
	"github.com/prometheus/client_golang/prometheus"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/test/bufconn"
)

type generatedClientLangGraphStore struct {
	checkpoints      []*database.LangGraphCheckpoint
	writes           []*database.LangGraphCheckpointWrite
	lastUserID       string
	lastCheckpointID *string
	lastLimit        int
	deletedUserID    string
	deletedThreadID  string
}

func (s *generatedClientLangGraphStore) StoreCheckpoint(_ context.Context, value *database.LangGraphCheckpoint) error {
	copy := *value
	s.checkpoints = append(s.checkpoints, &copy)
	return nil
}

func (s *generatedClientLangGraphStore) StoreCheckpointWrites(_ context.Context, values []*database.LangGraphCheckpointWrite) error {
	for _, value := range values {
		copy := *value
		s.writes = append(s.writes, &copy)
	}
	return nil
}

func (s *generatedClientLangGraphStore) ListCheckpoints(
	_ context.Context,
	userID string,
	threadID string,
	checkpointNS string,
	checkpointID *string,
	limit int,
) ([]*database.LangGraphCheckpointTuple, error) {
	s.lastUserID = userID
	s.lastCheckpointID = checkpointID
	s.lastLimit = limit

	result := make([]*database.LangGraphCheckpointTuple, 0)
	for _, checkpoint := range s.checkpoints {
		if checkpoint.UserID != userID || checkpoint.ThreadID != threadID || checkpoint.CheckpointNS != checkpointNS {
			continue
		}
		if checkpointID != nil && checkpoint.CheckpointID != *checkpointID {
			continue
		}
		checkpointWrites := make([]*database.LangGraphCheckpointWrite, 0)
		for _, write := range s.writes {
			if write.UserID == userID && write.ThreadID == threadID && write.CheckpointNS == checkpointNS && write.CheckpointID == checkpoint.CheckpointID {
				copy := *write
				checkpointWrites = append(checkpointWrites, &copy)
			}
		}
		slices.SortFunc(checkpointWrites, func(a, b *database.LangGraphCheckpointWrite) int {
			return cmp.Compare(a.WriteIdx, b.WriteIdx)
		})
		checkpointCopy := *checkpoint
		result = append(result, &database.LangGraphCheckpointTuple{Checkpoint: &checkpointCopy, Writes: checkpointWrites})
		if limit > 0 && len(result) == limit {
			break
		}
	}
	return result, nil
}

func (s *generatedClientLangGraphStore) DeleteCheckpoint(_ context.Context, userID, threadID string) error {
	s.deletedUserID = userID
	s.deletedThreadID = threadID
	checkpoints := s.checkpoints[:0]
	for _, checkpoint := range s.checkpoints {
		if checkpoint.UserID != userID || checkpoint.ThreadID != threadID {
			checkpoints = append(checkpoints, checkpoint)
		}
	}
	s.checkpoints = checkpoints
	return nil
}

func TestLangGraphGeneratedClient(t *testing.T) {
	store := &generatedClientLangGraphStore{}
	listener := bufconn.Listen(DefaultMaxMessageSize)
	server, err := New(Config{
		Listener:         listener,
		Registerer:       prometheus.NewRegistry(),
		Authenticator:    &authimpl.UnsecureAuthenticator{},
		LangGraphService: langgraphservice.NewService(store),
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	serverContext, cancelServer := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { done <- server.Start(serverContext) }()
	t.Cleanup(func() {
		cancelServer()
		if err := <-done; err != nil {
			t.Errorf("gRPC server shutdown error = %v", err)
		}
	})

	connection, err := grpc.NewClient(
		"passthrough:///bufnet",
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) { return listener.Dial() }),
	)
	if err != nil {
		t.Fatalf("grpc.NewClient() error = %v", err)
	}
	t.Cleanup(func() { _ = connection.Close() })

	client := apiv1alpha1.NewLangGraphServiceClient(connection)
	userContext := metadata.NewOutgoingContext(t.Context(), metadata.Pairs("x-user-id", "user-a"))
	parentID := "checkpoint-parent"
	checkpointBytes := []byte{0x00, 0xff, 0x10, 0x80}
	metadataBytes := []byte(`{"source":"loop"}`)
	_, err = client.PutCheckpoint(userContext, &apiv1alpha1.PutCheckpointRequest{
		Checkpoint: &apiv1alpha1.LangGraphCheckpoint{
			ThreadId:           "thread-1",
			CheckpointNs:       "",
			CheckpointId:       "checkpoint-1",
			ParentCheckpointId: &parentID,
			Checkpoint:         checkpointBytes,
			Metadata:           metadataBytes,
			Type:               "msgpack",
			Version:            4,
		},
	})
	if err != nil {
		t.Fatalf("PutCheckpoint() error = %v", err)
	}
	if len(store.checkpoints) != 1 || store.checkpoints[0].UserID != "user-a" || store.checkpoints[0].Checkpoint != "AP8QgA==" {
		t.Fatalf("stored checkpoint = %+v", store.checkpoints)
	}

	_, err = client.PutWrites(userContext, &apiv1alpha1.PutWritesRequest{
		Writes: &apiv1alpha1.LangGraphCheckpointWrites{
			ThreadId:     "thread-1",
			CheckpointId: "checkpoint-1",
			TaskId:       "task-1",
			Writes: []*apiv1alpha1.LangGraphCheckpointWrite{
				{Idx: 1, Channel: "messages", Type: "msgpack", Value: []byte{0xfe, 0x01}},
				{Idx: 0, Channel: "state", Type: "json", Value: []byte(`{"ready":true}`)},
			},
		},
	})
	if err != nil {
		t.Fatalf("PutWrites() error = %v", err)
	}
	if len(store.writes) != 2 || store.writes[0].UserID != "user-a" || store.writes[0].TaskID != "task-1" || store.writes[0].Value != "/gE=" {
		t.Fatalf("stored writes = %+v", store.writes)
	}

	limit := int32(1)
	checkpointID := "checkpoint-1"
	listed, err := client.ListCheckpoints(userContext, &apiv1alpha1.ListCheckpointsRequest{
		ThreadId:     "thread-1",
		CheckpointId: &checkpointID,
		Limit:        &limit,
	})
	if err != nil {
		t.Fatalf("ListCheckpoints() error = %v", err)
	}
	if store.lastUserID != "user-a" || store.lastCheckpointID == nil || *store.lastCheckpointID != checkpointID || store.lastLimit != 1 {
		t.Fatalf("ListCheckpoints() store arguments = user %q, checkpoint %v, limit %d", store.lastUserID, store.lastCheckpointID, store.lastLimit)
	}
	if len(listed.GetCheckpoints()) != 1 {
		t.Fatalf("ListCheckpoints() count = %d, want 1", len(listed.GetCheckpoints()))
	}
	tuple := listed.GetCheckpoints()[0]
	if string(tuple.GetCheckpoint().GetCheckpoint()) != string(checkpointBytes) || string(tuple.GetCheckpoint().GetMetadata()) != string(metadataBytes) {
		t.Fatalf("ListCheckpoints() checkpoint = %+v", tuple.GetCheckpoint())
	}
	if len(tuple.GetWrites().GetWrites()) != 2 || tuple.GetWrites().GetWrites()[0].GetTaskId() != "task-1" || tuple.GetWrites().GetWrites()[1].GetTaskId() != "task-1" {
		t.Fatalf("ListCheckpoints() writes = %+v", tuple.GetWrites().GetWrites())
	}
	if tuple.GetWrites().GetWrites()[0].GetIdx() != 0 || string(tuple.GetWrites().GetWrites()[1].GetValue()) != string([]byte{0xfe, 0x01}) {
		t.Fatalf("ListCheckpoints() ordered writes = %+v", tuple.GetWrites().GetWrites())
	}

	otherContext := metadata.NewOutgoingContext(t.Context(), metadata.Pairs("x-user-id", "user-b"))
	other, err := client.ListCheckpoints(otherContext, &apiv1alpha1.ListCheckpointsRequest{ThreadId: "thread-1"})
	if err != nil {
		t.Fatalf("ListCheckpoints(other user) error = %v", err)
	}
	if len(other.GetCheckpoints()) != 0 {
		t.Fatalf("ListCheckpoints(other user) = %+v, want no records", other.GetCheckpoints())
	}

	if _, err := client.DeleteThread(userContext, &apiv1alpha1.DeleteThreadRequest{ThreadId: "thread-1"}); err != nil {
		t.Fatalf("DeleteThread() error = %v", err)
	}
	if store.deletedUserID != "user-a" || store.deletedThreadID != "thread-1" || len(store.checkpoints) != 0 {
		t.Fatalf("DeleteThread() = user %q, thread %q, remaining %+v", store.deletedUserID, store.deletedThreadID, store.checkpoints)
	}
}
