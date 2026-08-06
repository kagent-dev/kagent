package a2a

import (
	"context"
	"testing"

	a2atype "github.com/a2aproject/a2a-go/v2/a2a"
	a2aclient "github.com/a2aproject/a2a-go/v2/a2aclient"
	"github.com/a2aproject/a2a-go/v2/a2aext"
	"github.com/a2aproject/a2a-go/v2/a2asrv"
	"github.com/stretchr/testify/require"
)

const testExtensionURI = "https://kagent.dev/extensions/hitl/v1"

type captureServiceParamsInterceptor struct {
	a2aclient.PassthroughInterceptor
	params []a2aclient.ServiceParams
}

func (i *captureServiceParamsInterceptor) Before(
	ctx context.Context,
	req *a2aclient.Request,
) (context.Context, any, error) {
	params := make(a2aclient.ServiceParams, len(req.ServiceParams))
	for key, values := range req.ServiceParams {
		params[key] = append([]string(nil), values...)
	}
	i.params = append(i.params, params)
	return ctx, &a2atype.Task{
		ID:        "task-1",
		ContextID: "context-1",
		Status:    a2atype.TaskStatus{State: a2atype.TaskStateCompleted},
	}, nil
}

func newExtensionCaptureProxy(t *testing.T) (a2asrv.RequestHandler, *captureServiceParamsInterceptor) {
	t.Helper()
	capture := &captureServiceParamsInterceptor{}
	client, err := a2aclient.NewFromEndpoints(
		t.Context(),
		[]*a2atype.AgentInterface{{
			URL:             "http://agent.invalid",
			ProtocolBinding: a2atype.TransportProtocolJSONRPC,
			ProtocolVersion: a2atype.Version,
		}},
		a2aclient.WithCallInterceptors(
			a2aext.NewClientPropagator(nil),
			capture,
		),
	)
	require.NoError(t, err)

	card := &a2atype.AgentCard{
		Capabilities: a2atype.AgentCapabilities{
			Extensions: []a2atype.AgentExtension{{URI: testExtensionURI}},
		},
	}
	return newProxyRequestHandler(client, card, nil), capture
}

func proxyCallContext(t *testing.T, requestExtension bool) context.Context {
	t.Helper()
	params := map[string][]string{}
	if requestExtension {
		params[a2atype.SvcParamExtensions] = []string{testExtensionURI}
	}
	ctx, _ := a2asrv.NewCallContext(t.Context(), a2asrv.NewServiceParams(params))
	return ctx
}

func proxySendRequest() *a2atype.SendMessageRequest {
	return &a2atype.SendMessageRequest{
		Message: &a2atype.Message{
			ID:        "message-1",
			ContextID: "context-1",
			Role:      a2atype.MessageRoleUser,
			Parts:     a2atype.ContentParts{a2atype.NewTextPart("hello")},
		},
	}
}

func TestProxyPropagatesNegotiatedExtensions(t *testing.T) {
	tests := []struct {
		name      string
		streaming bool
	}{
		{name: "non-streaming"},
		{name: "streaming", streaming: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler, capture := newExtensionCaptureProxy(t)
			ctx := proxyCallContext(t, true)

			if tt.streaming {
				events := handler.SendStreamingMessage(ctx, proxySendRequest())
				for _, err := range events {
					require.NoError(t, err)
				}
			} else {
				_, err := handler.SendMessage(ctx, proxySendRequest())
				require.NoError(t, err)
			}

			require.Len(t, capture.params, 1)
			require.Equal(t, []string{testExtensionURI}, capture.params[0].Get(a2atype.SvcParamExtensions))
		})
	}
}

func TestProxyDoesNotActivateUnrequestedExtensions(t *testing.T) {
	handler, capture := newExtensionCaptureProxy(t)

	_, err := handler.SendMessage(proxyCallContext(t, false), proxySendRequest())
	require.NoError(t, err)

	require.Len(t, capture.params, 1)
	require.Empty(t, capture.params[0].Get(a2atype.SvcParamExtensions))
}
