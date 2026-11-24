package a2agateway

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	a2a "github.com/a2aproject/a2a-go/v2/a2a"
	a2apb "github.com/a2aproject/a2a-go/v2/a2apb/v1"
	"github.com/stretchr/testify/require"
	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"k8s.io/apimachinery/pkg/types"
)

func TestGatewayJSONRPCVersion(t *testing.T) {
	handler := NewHTTPHandler(New(pagedPushConfigs{}, nil, ""), httpTestAuthenticator{}, nil)
	list := func(t *testing.T, version *string) int {
		t.Helper()
		request := httptest.NewRequest(http.MethodPost, HTTPPathPrefix+gatewayTestAgent, strings.NewReader(
			`{"jsonrpc":"2.0","id":"list","method":"ListTaskPushNotificationConfigs","params":{"taskId":"task","pageSize":1}}`,
		))
		request.Header.Set("Authorization", "Bearer valid")
		if version != nil {
			request.Header.Set(a2a.SvcParamVersion, *version)
		}
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		require.Equal(t, http.StatusOK, response.Code)
		var envelope struct {
			ID     string          `json:"id"`
			Result json.RawMessage `json:"result"`
			Error  *struct {
				Code int `json:"code"`
			} `json:"error"`
		}
		require.NoError(t, json.Unmarshal(response.Body.Bytes(), &envelope))
		require.Equal(t, "list", envelope.ID)
		if envelope.Error != nil {
			return envelope.Error.Code
		}
		require.NotEmpty(t, envelope.Result)
		return 0
	}
	t.Run("Given a v1 gateway, when a client sends version 1.0, assert that JSON-RPC returns a result", func(t *testing.T) {
		require.Zero(t, list(t, new("1.0")))
	})
	for _, scenario := range rejectedGatewayVersions {
		t.Run("Given a v1 gateway, when a client sends "+scenario.input+", assert that JSON-RPC returns -32009", func(t *testing.T) {
			require.Equal(t, -32009, list(t, scenario.version))
		})
	}
}

func TestGatewayGRPCPushConfigListVersion(t *testing.T) {
	address := startCoreTestServer(t, New(pagedPushConfigs{}, nil, ""), nil)
	conn, err := grpc.NewClient(address, grpc.WithTransportCredentials(insecure.NewCredentials()))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, conn.Close()) })
	client := a2apb.NewA2AServiceClient(conn)

	list := func(t *testing.T, version *string) (*a2apb.ListTaskPushNotificationConfigsResponse, error) {
		t.Helper()
		ctx := metadata.AppendToOutgoingContext(t.Context(), "authorization", "Bearer valid")
		if version != nil {
			ctx = metadata.AppendToOutgoingContext(ctx, a2a.SvcParamVersion, *version)
		}
		return client.ListTaskPushNotificationConfigs(ctx, &a2apb.ListTaskPushNotificationConfigsRequest{
			Tenant: gatewayTestAgent, TaskId: "task", PageSize: 1,
		})
	}
	t.Run("Given a paged push-config list, when a client sends version 1.0, assert that gRPC returns the next page token", func(t *testing.T) {
		response, err := list(t, new("1.0"))
		require.NoError(t, err)
		require.Equal(t, "next-page", response.GetNextPageToken())
	})
	for _, scenario := range rejectedGatewayVersions {
		t.Run("Given a v1 gateway, when a client sends "+scenario.input+", assert that gRPC returns VERSION_NOT_SUPPORTED", func(t *testing.T) {
			_, err := list(t, scenario.version)
			require.Error(t, err)
			rpcStatus := status.Convert(err)
			require.Equal(t, codes.FailedPrecondition, rpcStatus.Code())
			details := rpcStatus.Details()
			require.True(t, slices.ContainsFunc(details, func(detail any) bool {
				info, ok := detail.(*errdetails.ErrorInfo)
				return ok && info.GetReason() == "VERSION_NOT_SUPPORTED" && info.GetDomain() == a2a.ProtocolDomain
			}), "expected A2A version error in %v", details)
		})
	}
}

var rejectedGatewayVersions = []struct {
	input   string
	version *string
}{
	{input: "a request without a version"},
	{input: "an empty version", version: new("")},
	{input: "version 0.3", version: new("0.3")},
	{input: "version 2.0", version: new("2.0")},
}

type pagedPushConfigs struct {
	interactionService
}

var _ interactionService = pagedPushConfigs{}

func (pagedPushConfigs) ListTaskPushConfigs(context.Context, types.NamespacedName, *a2a.ListTaskPushConfigRequest) (*a2a.ListTaskPushConfigResponse, error) {
	return &a2a.ListTaskPushConfigResponse{NextPageToken: "next-page"}, nil
}
