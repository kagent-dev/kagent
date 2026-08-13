package tools

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/kagent-dev/kagent/go/adk/pkg/controllerclient"
	apiv1alpha1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1alpha1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type shareTestServer struct {
	apiv1alpha1.UnimplementedSessionServiceServer
	create func(context.Context, *apiv1alpha1.CreateSessionShareRequest) (*apiv1alpha1.CreateSessionShareResponse, error)
	list   func(context.Context, *apiv1alpha1.ListSessionSharesRequest) (*apiv1alpha1.ListSessionSharesResponse, error)
	delete func(context.Context, *apiv1alpha1.DeleteSessionShareRequest) (*apiv1alpha1.DeleteSessionShareResponse, error)
}

func (server *shareTestServer) CreateSessionShare(ctx context.Context, request *apiv1alpha1.CreateSessionShareRequest) (*apiv1alpha1.CreateSessionShareResponse, error) {
	return server.create(ctx, request)
}

func (server *shareTestServer) ListSessionShares(ctx context.Context, request *apiv1alpha1.ListSessionSharesRequest) (*apiv1alpha1.ListSessionSharesResponse, error) {
	return server.list(ctx, request)
}

func (server *shareTestServer) DeleteSessionShare(ctx context.Context, request *apiv1alpha1.DeleteSessionShareRequest) (*apiv1alpha1.DeleteSessionShareResponse, error) {
	return server.delete(ctx, request)
}

func newShareControllerClient(t *testing.T, service *shareTestServer) *controllerclient.Client {
	t.Helper()
	listener := bufconn.Listen(1024 * 1024)
	server := grpc.NewServer()
	apiv1alpha1.RegisterSessionServiceServer(server, service)
	go func() { _ = server.Serve(listener) }()

	client, err := controllerclient.New(controllerclient.Config{
		Target:    "passthrough:///bufnet",
		AgentName: "test__NS__app",
		DialOptions: []grpc.DialOption{grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) {
			return listener.Dial()
		})},
	})
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, client.Close())
		server.Stop()
		require.NoError(t, listener.Close())
	})
	return client
}

func TestParseAppName(t *testing.T) {
	tests := []struct {
		name          string
		input         string
		wantNamespace string
		wantName      string
	}{
		{
			name:          "standard format with underscores",
			input:         "kagent__NS__my_agent",
			wantNamespace: "kagent",
			wantName:      "my-agent",
		},
		{
			name:          "custom namespace and agent name",
			input:         "default__NS__test_agent",
			wantNamespace: "default",
			wantName:      "test-agent",
		},
		{
			name:          "no separator returns empty namespace",
			input:         "noseperator",
			wantNamespace: "",
			wantName:      "noseperator",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotNamespace, gotName := parseAppName(tt.input)
			if gotNamespace != tt.wantNamespace {
				t.Errorf("parseAppName(%q) namespace = %q, want %q", tt.input, gotNamespace, tt.wantNamespace)
			}
			if gotName != tt.wantName {
				t.Errorf("parseAppName(%q) name = %q, want %q", tt.input, gotName, tt.wantName)
			}
		})
	}
}

func TestShareClient_ShareURL_WithUIURL(t *testing.T) {
	c := &shareClient{
		uiURL:   "https://example.com",
		appName: "kagent__NS__myagent",
	}

	got := c.shareURL("abc123", "sess-1")
	want := "https://example.com/agents/kagent/myagent/chat/sess-1?share=abc123"
	if got != want {
		t.Errorf("shareURL() = %q, want %q", got, want)
	}
}

func TestShareClient_ShareURL_WithoutUIURL(t *testing.T) {
	c := &shareClient{
		uiURL:   "",
		appName: "kagent__NS__myagent",
	}

	got := c.shareURL("abc123", "sess-1")
	want := "/agents/kagent/myagent/chat/sess-1?share=abc123"
	if got != want {
		t.Errorf("shareURL() = %q, want %q", got, want)
	}
}

func TestNewShareTools_HaveCorrectNames(t *testing.T) {
	controllerClient := &controllerclient.Client{}
	tests := []struct {
		toolName    string
		constructor func(*controllerclient.Client, string) (interface{ Name() string }, error)
	}{
		{
			toolName: "create_share_link",
			constructor: func(client *controllerclient.Client, app string) (interface{ Name() string }, error) {
				return NewCreateShareLinkTool(client, app)
			},
		},
		{
			toolName: "list_share_links",
			constructor: func(client *controllerclient.Client, app string) (interface{ Name() string }, error) {
				return NewListShareLinksTool(client, app)
			},
		},
		{
			toolName: "delete_share_link",
			constructor: func(client *controllerclient.Client, app string) (interface{ Name() string }, error) {
				return NewDeleteShareLinkTool(client, app)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.toolName, func(t *testing.T) {
			tool, err := tt.constructor(controllerClient, "test__NS__app")
			if err != nil {
				t.Fatalf("constructor for %q returned error: %v", tt.toolName, err)
			}
			if tool.Name() != tt.toolName {
				t.Errorf("tool.Name() = %q, want %q", tool.Name(), tt.toolName)
			}
		})
	}
}

func TestShareClientCreateShareUsesGRPC(t *testing.T) {
	readOnly := false
	controllerClient := newShareControllerClient(t, &shareTestServer{create: func(ctx context.Context, request *apiv1alpha1.CreateSessionShareRequest) (*apiv1alpha1.CreateSessionShareResponse, error) {
		assert.Equal(t, "session-1", request.GetSessionId())
		require.NotNil(t, request.ReadOnly)
		assert.False(t, request.GetReadOnly())
		incoming, _ := metadata.FromIncomingContext(ctx)
		assert.Equal(t, []string{"user-1"}, incoming.Get("x-user-id"))
		assert.Equal(t, []string{"test__NS__app"}, incoming.Get("x-agent-name"))
		_, hasDeadline := ctx.Deadline()
		assert.True(t, hasDeadline)
		return &apiv1alpha1.CreateSessionShareResponse{Share: &apiv1alpha1.SessionShare{
			Token:     "token-1",
			SessionId: "session-1",
			ReadOnly:  false,
		}}, nil
	}})
	client := &shareClient{controllerClient: controllerClient, appName: "test__NS__app"}

	share, err := client.createShare(t.Context(), "user-1", "session-1", &readOnly)
	require.NoError(t, err)
	assert.Equal(t, "token-1", share.GetToken())
	assert.False(t, share.GetReadOnly())
}

func TestShareClientCreateSharePreservesAbsentReadOnly(t *testing.T) {
	controllerClient := newShareControllerClient(t, &shareTestServer{create: func(_ context.Context, request *apiv1alpha1.CreateSessionShareRequest) (*apiv1alpha1.CreateSessionShareResponse, error) {
		assert.Nil(t, request.ReadOnly)
		return &apiv1alpha1.CreateSessionShareResponse{Share: &apiv1alpha1.SessionShare{Token: "token-1", ReadOnly: true}}, nil
	}})
	client := &shareClient{controllerClient: controllerClient}

	share, err := client.createShare(t.Context(), "user-1", "session-1", nil)
	require.NoError(t, err)
	assert.True(t, share.GetReadOnly())
}

func TestShareClientListSharesPreservesOutputShape(t *testing.T) {
	createdAt := time.Date(2026, time.March, 10, 12, 30, 0, 0, time.UTC)
	controllerClient := newShareControllerClient(t, &shareTestServer{list: func(ctx context.Context, request *apiv1alpha1.ListSessionSharesRequest) (*apiv1alpha1.ListSessionSharesResponse, error) {
		assert.Equal(t, "session-1", request.GetSessionId())
		incoming, _ := metadata.FromIncomingContext(ctx)
		assert.Equal(t, []string{"user-1"}, incoming.Get("x-user-id"))
		return &apiv1alpha1.ListSessionSharesResponse{Shares: []*apiv1alpha1.SessionShare{{
			Id:        42,
			Token:     "token-1",
			SessionId: "session-1",
			UserId:    "user-1",
			ReadOnly:  true,
			CreatedAt: timestamppb.New(createdAt),
		}}}, nil
	}})
	client := &shareClient{controllerClient: controllerClient}

	shares, err := client.listShares(t.Context(), "user-1", "session-1")
	require.NoError(t, err)
	require.Len(t, shares, 1)
	assert.Equal(t, map[string]any{
		"id":         int64(42),
		"token":      "token-1",
		"session_id": "session-1",
		"user_id":    "user-1",
		"read_only":  true,
		"created_at": "2026-03-10T12:30:00Z",
	}, shares[0])
}

func TestShareClientDeleteShareUsesGRPC(t *testing.T) {
	controllerClient := newShareControllerClient(t, &shareTestServer{delete: func(ctx context.Context, request *apiv1alpha1.DeleteSessionShareRequest) (*apiv1alpha1.DeleteSessionShareResponse, error) {
		assert.Equal(t, "session-1", request.GetSessionId())
		assert.Equal(t, "token-1", request.GetToken())
		incoming, _ := metadata.FromIncomingContext(ctx)
		assert.Equal(t, []string{"user-1"}, incoming.Get("x-user-id"))
		return &apiv1alpha1.DeleteSessionShareResponse{}, nil
	}})
	client := &shareClient{controllerClient: controllerClient}

	require.NoError(t, client.deleteShare(t.Context(), "user-1", "session-1", "token-1"))
}

func TestShareClientReturnsRPCError(t *testing.T) {
	controllerClient := newShareControllerClient(t, &shareTestServer{list: func(context.Context, *apiv1alpha1.ListSessionSharesRequest) (*apiv1alpha1.ListSessionSharesResponse, error) {
		return nil, status.Error(codes.NotFound, "session not found")
	}})
	client := &shareClient{controllerClient: controllerClient}

	_, err := client.listShares(t.Context(), "user-1", "missing")
	require.Error(t, err)
	assert.Equal(t, codes.NotFound, status.Code(err))
}
