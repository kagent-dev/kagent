package app

import (
	"context"
	"iter"
	"testing"
	"time"

	a2atype "github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2asrv"
	"github.com/a2aproject/a2a-go/v2/a2asrv/taskstore"
	"github.com/stretchr/testify/require"
)

func TestHandlerPassesRequestMetadataWithoutActivation(t *testing.T) {
	executor := &mcpMetadataWiringExecutor{metadata: make(chan map[string]any, 1)}
	handler := metadataWiringHandler(executor)
	values := map[string]any{"com.example/session": "session-1", "org.example/options": map[string]any{"any field": true}}
	_, err := handler.SendMessage(metadataWiringContext(t), metadataWiringRequest(values))
	require.NoError(t, err)
	require.Equal(t, values, <-executor.metadata)
}

func TestHandlerRejectsInvalidRequestMetadataBeforeDispatch(t *testing.T) {
	for _, key := range []string{"invalid metadata/key", "progressToken", "io.modelcontextprotocol/clientCapabilities"} {
		executor := &mcpMetadataWiringExecutor{metadata: make(chan map[string]any, 1)}
		handler := metadataWiringHandler(executor)
		_, err := handler.SendMessage(metadataWiringContext(t), metadataWiringRequest(map[string]any{key: "request-only"}))
		require.ErrorIs(t, err, a2atype.ErrInvalidParams)
		require.Empty(t, executor.metadata)
	}
}

func metadataWiringHandler(executor a2asrv.AgentExecutor) a2asrv.RequestHandler {
	options := handlerOptions(nil, &a2asrv.PassthroughCallInterceptor{})
	options = append(options, a2asrv.WithTaskStore(taskstore.NewInMemory(nil)))
	return a2asrv.NewHandler(executor, options...)
}

func metadataWiringContext(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	t.Cleanup(cancel)
	ctx, _ = a2asrv.NewCallContext(ctx, a2asrv.NewServiceParams(nil))
	return ctx
}

func metadataWiringRequest(values map[string]any) *a2atype.SendMessageRequest {
	return &a2atype.SendMessageRequest{Message: a2atype.NewMessage(a2atype.MessageRoleUser, a2atype.NewTextPart("hello")), Metadata: values}
}

type mcpMetadataWiringExecutor struct {
	fakeExecutor
	metadata chan map[string]any
}

func (e *mcpMetadataWiringExecutor) Execute(_ context.Context, request *a2asrv.ExecutorContext) iter.Seq2[a2atype.Event, error] {
	return func(yield func(a2atype.Event, error) bool) {
		if request.StoredTask == nil && !yield(a2atype.NewSubmittedTask(request, request.Message), nil) {
			return
		}
		e.metadata <- request.Metadata
		yield(a2atype.NewStatusUpdateEvent(request, a2atype.TaskStateCompleted, nil), nil)
	}
}
