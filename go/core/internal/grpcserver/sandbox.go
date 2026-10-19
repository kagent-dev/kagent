package grpcserver

import (
	"context"

	apiv1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1"
	"github.com/kagent-dev/kagent/go/core/internal/service/sandbox"
)

type sandboxServer struct {
	apiv1.UnimplementedSandboxServiceServer
	service *sandbox.Service
}

func (s *sandboxServer) CreateSandbox(ctx context.Context, request *apiv1.CreateSandboxRequest) (*apiv1.CreateSandboxResponse, error) {
	result, err := s.service.Create(ctx, request)
	if err != nil {
		return nil, err
	}
	return &apiv1.CreateSandboxResponse{Sandbox: result}, nil
}

func (s *sandboxServer) GetSandbox(ctx context.Context, request *apiv1.GetSandboxRequest) (*apiv1.GetSandboxResponse, error) {
	result, err := s.service.Get(ctx, request.GetSandboxId())
	if err != nil {
		return nil, err
	}
	return &apiv1.GetSandboxResponse{Sandbox: result}, nil
}

func (s *sandboxServer) SuspendSandbox(ctx context.Context, request *apiv1.SuspendSandboxRequest) (*apiv1.SuspendSandboxResponse, error) {
	result, err := s.service.Suspend(ctx, request.GetSandboxId())
	if err != nil {
		return nil, err
	}
	return &apiv1.SuspendSandboxResponse{Sandbox: result}, nil
}

func (s *sandboxServer) ResumeSandbox(ctx context.Context, request *apiv1.ResumeSandboxRequest) (*apiv1.ResumeSandboxResponse, error) {
	result, err := s.service.Resume(ctx, request.GetSandboxId())
	if err != nil {
		return nil, err
	}
	return &apiv1.ResumeSandboxResponse{Sandbox: result}, nil
}

func (s *sandboxServer) DeleteSandbox(ctx context.Context, request *apiv1.DeleteSandboxRequest) (*apiv1.DeleteSandboxResponse, error) {
	result, err := s.service.Delete(ctx, request.GetSandboxId())
	if err != nil {
		return nil, err
	}
	return &apiv1.DeleteSandboxResponse{Sandbox: result}, nil
}

func (s *sandboxServer) ListSandboxes(ctx context.Context, request *apiv1.ListSandboxesRequest) (*apiv1.ListSandboxesResponse, error) {
	return s.service.List(ctx, request)
}
