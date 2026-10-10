package grpcserver

import (
	"context"
	"errors"

	"github.com/a2aproject/a2a-go/v2/a2a"
	a2apb "github.com/a2aproject/a2a-go/v2/a2apb/v1"
	"github.com/a2aproject/a2a-go/v2/a2apb/v1/pbconv"
	"github.com/a2aproject/a2a-go/v2/a2asrv"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// pushListGRPCHandler works around the upstream gRPC handler dropping
// ListTaskPushConfigsResponse.NextPageToken during protobuf conversion.
// See https://github.com/a2aproject/a2a-go/issues/454 Keep this adapter while
// Kagent's push configuration list is paginated, or later pages become hidden.
type pushListGRPCHandler struct {
	a2apb.A2AServiceServer
	handler a2asrv.RequestHandler
}

func (h *pushListGRPCHandler) ListTaskPushNotificationConfigs(ctx context.Context, pbReq *a2apb.ListTaskPushNotificationConfigsRequest) (*a2apb.ListTaskPushNotificationConfigsResponse, error) {
	if pbReq == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}
	req, err := pbconv.FromProtoListTaskPushConfigRequest(pbReq)
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}
	response, err := h.handler.ListTaskPushConfigs(ctx, req)
	if err != nil {
		return nil, pushListGRPCError(err)
	}
	return pbconv.ToProtoListTaskPushConfigResponse(response)
}

func pushListGRPCError(err error) error {
	switch {
	case errors.Is(err, a2a.ErrInvalidParams), errors.Is(err, a2a.ErrInvalidRequest):
		return status.Error(codes.InvalidArgument, err.Error())
	case errors.Is(err, a2a.ErrUnauthenticated):
		return status.Error(codes.Unauthenticated, err.Error())
	case errors.Is(err, a2a.ErrUnauthorized):
		return status.Error(codes.PermissionDenied, err.Error())
	case errors.Is(err, a2a.ErrTaskNotFound):
		return status.Error(codes.NotFound, err.Error())
	default:
		return status.Error(codes.Internal, "failed to list task push configurations")
	}
}
