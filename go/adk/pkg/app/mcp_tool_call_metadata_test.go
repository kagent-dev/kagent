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
	adkagent "google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/session"

	apia2a "github.com/kagent-dev/kagent/go/api/a2a"
)

func TestHandlerOptionsActivateMCPToolCallMetadata(t *testing.T) {
	for _, scenario := range []struct {
		name      string
		requested bool
	}{
		{name: "Given application handler options, when a client requests tool-call metadata, assert that the executor sees the activated extension", requested: true},
		{name: "Given application handler options, when a client omits activation, assert that the executor sees the extension inactive"},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			executor := &mcpMetadataWiringExecutor{activations: make(chan bool, 1)}
			handler := metadataWiringHandler(executor)
			ctx := metadataWiringContext(t, scenario.requested)

			_, err := handler.SendMessage(ctx, metadataWiringRequest(map[string]any{"com.example/session": "session-1"}))

			require.NoError(t, err)
			require.Len(t, executor.activations, 1)
			require.Equal(t, scenario.requested, <-executor.activations)
		})
	}
}

func TestHandlerOptionsRejectMalformedMCPToolCallMetadata(t *testing.T) {
	for _, scenario := range []struct {
		name   string
		values any
	}{
		{name: "Given an activated metadata extension, when a client sends a string payload, assert that dispatch is rejected", values: "not an object"},
		{name: "Given an activated metadata extension, when a client sends an array payload, assert that dispatch is rejected", values: []any{"session-1"}},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			executor := &mcpMetadataWiringExecutor{activations: make(chan bool, 1)}
			handler := metadataWiringHandler(executor)
			ctx := metadataWiringContext(t, true)

			_, err := handler.SendMessage(ctx, metadataWiringRequest(scenario.values))

			require.ErrorIs(t, err, a2atype.ErrInvalidParams)
			require.Empty(t, executor.activations)
		})
	}
}

func TestBuildAgentCardDeclaresOptionalMCPToolCallMetadata(t *testing.T) {
	agent, err := adkagent.New(adkagent.Config{
		Name: "metadata_agent",
		Run: func(adkagent.InvocationContext) iter.Seq2[*session.Event, error] {
			return func(func(*session.Event, error) bool) {}
		},
	})
	require.NoError(t, err)

	for _, scenario := range []struct {
		name  string
		agent adkagent.Agent
	}{
		{name: "Given an application without skill derivation, when its card is built, assert that tool-call metadata is optional"},
		{name: "Given an ADK agent, when its card is built, assert that tool-call metadata is optional and declared once", agent: agent},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			card := buildAgentCard(AppConfig{AgentCard: a2atype.AgentCard{Name: "metadata-agent"}, Agent: scenario.agent})

			var declarations []a2atype.AgentExtension
			for _, extension := range card.Capabilities.Extensions {
				if extension.URI == apia2a.MCPToolCallMetadataExtensionURI {
					declarations = append(declarations, extension)
				}
			}
			require.Len(t, declarations, 1)
			require.False(t, declarations[0].Required)
		})
	}
}

func metadataWiringHandler(executor a2asrv.AgentExecutor) a2asrv.RequestHandler {
	options := handlerOptions(nil, &a2asrv.PassthroughCallInterceptor{})
	options = append(options, a2asrv.WithTaskStore(taskstore.NewInMemory(nil)))
	return a2asrv.NewHandler(executor, options...)
}

func metadataWiringContext(t *testing.T, requested bool) context.Context {
	t.Helper()
	parameters := map[string][]string{}
	if requested {
		parameters[a2atype.SvcParamExtensions] = []string{apia2a.MCPToolCallMetadataExtensionURI}
	}
	requestContext, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	t.Cleanup(cancel)
	ctx, _ := a2asrv.NewCallContext(requestContext, a2asrv.NewServiceParams(parameters))
	return ctx
}

func metadataWiringRequest(values any) *a2atype.SendMessageRequest {
	return &a2atype.SendMessageRequest{
		Message: a2atype.NewMessage(a2atype.MessageRoleUser, a2atype.NewTextPart("hello")),
		Metadata: map[string]any{
			apia2a.MCPToolCallMetadataExtensionURI: values,
		},
	}
}

type mcpMetadataWiringExecutor struct {
	fakeExecutor
	activations chan bool
}

func (e *mcpMetadataWiringExecutor) Execute(ctx context.Context, request *a2asrv.ExecutorContext) iter.Seq2[a2atype.Event, error] {
	return func(yield func(a2atype.Event, error) bool) {
		if request.StoredTask == nil && !yield(a2atype.NewSubmittedTask(request, request.Message), nil) {
			return
		}
		extension := apia2a.MCPToolCallMetadataExtension()
		extensions, present := a2asrv.ExtensionsFrom(ctx)
		e.activations <- present && extensions.Active(&extension)
		yield(a2atype.NewStatusUpdateEvent(request, a2atype.TaskStateCompleted, nil), nil)
	}
}

var _ a2asrv.AgentExecutor = (*mcpMetadataWiringExecutor)(nil)
