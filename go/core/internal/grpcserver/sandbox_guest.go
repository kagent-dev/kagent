package grpcserver

import (
	"context"

	guestpb "github.com/agent-substrate/env/proto/ateenv/v1alpha"
	sandboxapi "github.com/kagent-dev/kagent/go/api/sandbox"
	"github.com/kagent-dev/kagent/go/core/internal/service/sandbox"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

type sandboxGuestServer struct {
	guestpb.UnimplementedProcessServiceServer
	guestpb.UnimplementedFileSystemServiceServer
	service *sandbox.Service
}

var (
	_ guestpb.ProcessServiceServer    = (*sandboxGuestServer)(nil)
	_ guestpb.FileSystemServiceServer = (*sandboxGuestServer)(nil)
)

func sandboxID(ctx context.Context) (string, error) {
	values := metadata.ValueFromIncomingContext(ctx, sandboxapi.IDHeader)
	if len(values) != 1 || values[0] == "" {
		return "", status.Error(codes.InvalidArgument, "exactly one kagent-sandbox-id metadata value is required")
	}
	return values[0], nil
}

func (s *sandboxGuestServer) StartProcess(ctx context.Context, request *guestpb.StartProcessRequest) (*guestpb.StartProcessResponse, error) {
	id, err := sandboxID(ctx)
	if err != nil {
		return nil, err
	}
	return s.service.StartProcess(ctx, id, request)
}

func (s *sandboxGuestServer) GetProcess(ctx context.Context, request *guestpb.GetProcessRequest) (*guestpb.Process, error) {
	id, err := sandboxID(ctx)
	if err != nil {
		return nil, err
	}
	return s.service.GetProcess(ctx, id, request)
}

func (s *sandboxGuestServer) KillProcess(ctx context.Context, request *guestpb.KillProcessRequest) (*guestpb.KillProcessResponse, error) {
	id, err := sandboxID(ctx)
	if err != nil {
		return nil, err
	}
	return s.service.KillProcess(ctx, id, request)
}

func (s *sandboxGuestServer) StreamProcessOutputs(request *guestpb.StreamProcessOutputsRequest, stream grpc.ServerStreamingServer[guestpb.OutputChunk]) error {
	id, err := sandboxID(stream.Context())
	if err != nil {
		return err
	}
	return s.service.StreamProcessOutputs(stream.Context(), id, request, stream.Send)
}

func (s *sandboxGuestServer) ReadFile(request *guestpb.ReadFileRequest, stream grpc.ServerStreamingServer[guestpb.FileChunk]) error {
	id, err := sandboxID(stream.Context())
	if err != nil {
		return err
	}
	return s.service.ReadFile(stream.Context(), id, request, stream.Send)
}

func (s *sandboxGuestServer) WriteFile(stream grpc.ClientStreamingServer[guestpb.WriteFileRequest, guestpb.WriteFileResponse]) error {
	id, err := sandboxID(stream.Context())
	if err != nil {
		return err
	}
	result, err := s.service.WriteFile(stream.Context(), id, stream.Recv)
	if err != nil {
		return err
	}
	return stream.SendAndClose(result)
}
