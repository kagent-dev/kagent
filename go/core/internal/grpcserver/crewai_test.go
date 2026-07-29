package grpcserver

import (
	"context"
	"encoding/json"
	"net"
	"strings"
	"testing"

	"github.com/kagent-dev/kagent/go/api/database"
	apiv1alpha1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1alpha1"
	"github.com/kagent-dev/kagent/go/api/structuredobject"
	authimpl "github.com/kagent-dev/kagent/go/core/internal/httpserver/auth"
	crewaiservice "github.com/kagent-dev/kagent/go/core/internal/service/crewai"
	"github.com/prometheus/client_golang/prometheus"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
)

type generatedClientCrewAIStore struct {
	memories            []*database.CrewAIAgentMemory
	states              []*database.CrewAIFlowState
	lastMemoryUserID    string
	lastTaskDescription string
	lastMemoryLimit     int
	resetUserID         string
	resetThreadID       string
}

func (s *generatedClientCrewAIStore) StoreCrewAIMemory(_ context.Context, value *database.CrewAIAgentMemory) error {
	copy := *value
	s.memories = append(s.memories, &copy)
	return nil
}

func (s *generatedClientCrewAIStore) SearchCrewAIMemoryByTask(
	_ context.Context,
	userID string,
	threadID string,
	taskDescription string,
	limit int,
) ([]*database.CrewAIAgentMemory, error) {
	s.lastMemoryUserID = userID
	s.lastTaskDescription = taskDescription
	s.lastMemoryLimit = limit
	result := make([]*database.CrewAIAgentMemory, 0)
	for _, value := range s.memories {
		if value.UserID != userID || value.ThreadID != threadID || !strings.Contains(value.MemoryData, taskDescription) {
			continue
		}
		copy := *value
		result = append(result, &copy)
		if limit > 0 && len(result) == limit {
			break
		}
	}
	return result, nil
}

func (s *generatedClientCrewAIStore) ResetCrewAIMemory(_ context.Context, userID, threadID string) error {
	s.resetUserID = userID
	s.resetThreadID = threadID
	memories := s.memories[:0]
	for _, value := range s.memories {
		if value.UserID != userID || value.ThreadID != threadID {
			memories = append(memories, value)
		}
	}
	s.memories = memories
	return nil
}

func (s *generatedClientCrewAIStore) StoreCrewAIFlowState(_ context.Context, value *database.CrewAIFlowState) error {
	copy := *value
	s.states = append(s.states, &copy)
	return nil
}

func (s *generatedClientCrewAIStore) GetCrewAIFlowState(_ context.Context, userID, threadID string) (*database.CrewAIFlowState, error) {
	for index := len(s.states) - 1; index >= 0; index-- {
		value := s.states[index]
		if value.UserID == userID && value.ThreadID == threadID {
			copy := *value
			return &copy, nil
		}
	}
	return nil, nil
}

func TestCrewAIGeneratedClient(t *testing.T) {
	store := &generatedClientCrewAIStore{}
	listener := bufconn.Listen(DefaultMaxMessageSize)
	server, err := New(Config{
		Listener:      listener,
		Registerer:    prometheus.NewRegistry(),
		Authenticator: &authimpl.UnsecureAuthenticator{},
		CrewAIService: crewaiservice.NewService(store),
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

	client := apiv1alpha1.NewCrewAIServiceClient(connection)
	userContext := metadata.NewOutgoingContext(t.Context(), metadata.Pairs("x-user-id", "user-a"))
	memoryData := map[string]any{
		"task_description": "research grpc migration",
		"score":            0.875,
		"metadata":         map[string]any{"sources": []any{"design", "tests"}},
	}
	memoryObject, err := structuredobject.FromGo(memoryData, crewAIAPIVersion, crewAIMemoryDataKind, DefaultMaxMessageSize)
	if err != nil {
		t.Fatalf("structuredobject.FromGo(memory) error = %v", err)
	}
	if _, err := client.StoreMemory(userContext, &apiv1alpha1.StoreMemoryRequest{
		ThreadId:   "thread-1",
		MemoryData: memoryObject,
	}); err != nil {
		t.Fatalf("StoreMemory() error = %v", err)
	}
	if len(store.memories) != 1 || store.memories[0].UserID != "user-a" || store.memories[0].ThreadID != "thread-1" {
		t.Fatalf("stored memories = %+v", store.memories)
	}
	storedMemory := map[string]any{}
	if err := json.Unmarshal([]byte(store.memories[0].MemoryData), &storedMemory); err != nil {
		t.Fatalf("stored memory JSON error = %v", err)
	}
	if storedMemory["task_description"] != memoryData["task_description"] || storedMemory["score"] != memoryData["score"] {
		t.Fatalf("stored memory data = %+v", storedMemory)
	}

	limit := int32(3)
	listed, err := client.GetMemory(userContext, &apiv1alpha1.GetMemoryRequest{
		ThreadId:        "thread-1",
		TaskDescription: "grpc migration",
		Limit:           &limit,
	})
	if err != nil {
		t.Fatalf("GetMemory() error = %v", err)
	}
	if store.lastMemoryUserID != "user-a" || store.lastTaskDescription != "grpc migration" || store.lastMemoryLimit != 3 {
		t.Fatalf("GetMemory() store arguments = user %q, task %q, limit %d", store.lastMemoryUserID, store.lastTaskDescription, store.lastMemoryLimit)
	}
	if len(listed.GetMemories()) != 1 || listed.GetMemories()[0].GetUserId() != "user-a" {
		t.Fatalf("GetMemory() = %+v", listed.GetMemories())
	}
	decodedMemory := map[string]any{}
	if err := structuredobject.ToGo(listed.GetMemories()[0].GetMemoryData(), crewAIMemoryDataKind, &decodedMemory, DefaultMaxMessageSize); err != nil {
		t.Fatalf("structuredobject.ToGo(memory) error = %v", err)
	}
	if decodedMemory["task_description"] != memoryData["task_description"] {
		t.Fatalf("decoded memory = %+v", decodedMemory)
	}

	wrongKind, err := structuredobject.FromGo(memoryData, crewAIAPIVersion, crewAIFlowStateDataKind, DefaultMaxMessageSize)
	if err != nil {
		t.Fatalf("structuredobject.FromGo(wrong kind) error = %v", err)
	}
	_, err = client.StoreMemory(userContext, &apiv1alpha1.StoreMemoryRequest{ThreadId: "thread-1", MemoryData: wrongKind})
	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf("StoreMemory(wrong kind) error = %v, want InvalidArgument", err)
	}

	stateData := map[string]any{"step": "complete", "nested": map[string]any{"attempt": 2}}
	stateObject, err := structuredobject.FromGo(stateData, crewAIAPIVersion, crewAIFlowStateDataKind, DefaultMaxMessageSize)
	if err != nil {
		t.Fatalf("structuredobject.FromGo(state) error = %v", err)
	}
	if _, err := client.StoreFlowState(userContext, &apiv1alpha1.StoreFlowStateRequest{
		ThreadId:   "thread-1",
		MethodName: "finish",
		StateData:  stateObject,
	}); err != nil {
		t.Fatalf("StoreFlowState() error = %v", err)
	}
	if len(store.states) != 1 || store.states[0].UserID != "user-a" || store.states[0].MethodName != "finish" {
		t.Fatalf("stored flow states = %+v", store.states)
	}
	gotState, err := client.GetFlowState(userContext, &apiv1alpha1.GetFlowStateRequest{ThreadId: "thread-1"})
	if err != nil {
		t.Fatalf("GetFlowState() error = %v", err)
	}
	decodedState := map[string]any{}
	if err := structuredobject.ToGo(gotState.GetState().GetStateData(), crewAIFlowStateDataKind, &decodedState, DefaultMaxMessageSize); err != nil {
		t.Fatalf("structuredobject.ToGo(state) error = %v", err)
	}
	if gotState.GetState().GetMethodName() != "finish" || decodedState["step"] != "complete" {
		t.Fatalf("GetFlowState() = %+v, data = %+v", gotState.GetState(), decodedState)
	}

	otherContext := metadata.NewOutgoingContext(t.Context(), metadata.Pairs("x-user-id", "user-b"))
	_, err = client.GetFlowState(otherContext, &apiv1alpha1.GetFlowStateRequest{ThreadId: "thread-1"})
	if status.Code(err) != codes.NotFound {
		t.Fatalf("GetFlowState(other user) error = %v, want NotFound", err)
	}

	if _, err := client.ResetMemory(userContext, &apiv1alpha1.ResetMemoryRequest{ThreadId: "thread-1"}); err != nil {
		t.Fatalf("ResetMemory() error = %v", err)
	}
	if store.resetUserID != "user-a" || store.resetThreadID != "thread-1" || len(store.memories) != 0 {
		t.Fatalf("ResetMemory() = user %q, thread %q, remaining %+v", store.resetUserID, store.resetThreadID, store.memories)
	}
}
