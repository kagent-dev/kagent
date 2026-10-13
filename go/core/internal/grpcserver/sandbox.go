package grpcserver

import (
	"context"

	apiv1alpha1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1alpha1"
	"github.com/kagent-dev/kagent/go/core/internal/service/sandbox"
)

type sandboxServer struct {
	apiv1alpha1.UnimplementedSandboxServiceServer
	service *sandbox.Service
}

func (s *sandboxServer) CreateSandbox(ctx context.Context, request *apiv1alpha1.CreateSandboxRequest) (*apiv1alpha1.CreateSandboxResponse, error) {
	result, err := s.service.Create(ctx, request)
	if err != nil {
		return nil, err
	}
	return &apiv1alpha1.CreateSandboxResponse{Sandbox: result}, nil
}

func (s *sandboxServer) GetSandbox(ctx context.Context, request *apiv1alpha1.GetSandboxRequest) (*apiv1alpha1.GetSandboxResponse, error) {
	result, err := s.service.Get(ctx, request.GetSandboxId())
	if err != nil {
		return nil, err
	}
	return &apiv1alpha1.GetSandboxResponse{Sandbox: result}, nil
}

func (s *sandboxServer) SuspendSandbox(ctx context.Context, request *apiv1alpha1.SuspendSandboxRequest) (*apiv1alpha1.SuspendSandboxResponse, error) {
	result, err := s.service.Suspend(ctx, request.GetSandboxId())
	if err != nil {
		return nil, err
	}
	return &apiv1alpha1.SuspendSandboxResponse{Sandbox: result}, nil
}

func (s *sandboxServer) ResumeSandbox(ctx context.Context, request *apiv1alpha1.ResumeSandboxRequest) (*apiv1alpha1.ResumeSandboxResponse, error) {
	result, err := s.service.Resume(ctx, request.GetSandboxId())
	if err != nil {
		return nil, err
	}
	return &apiv1alpha1.ResumeSandboxResponse{Sandbox: result}, nil
}

func (s *sandboxServer) DeleteSandbox(ctx context.Context, request *apiv1alpha1.DeleteSandboxRequest) (*apiv1alpha1.DeleteSandboxResponse, error) {
	result, err := s.service.Delete(ctx, request.GetSandboxId())
	if err != nil {
		return nil, err
	}
	return &apiv1alpha1.DeleteSandboxResponse{Sandbox: result}, nil
}

func (s *sandboxServer) ListSandboxes(ctx context.Context, request *apiv1alpha1.ListSandboxesRequest) (*apiv1alpha1.ListSandboxesResponse, error) {
	return s.service.List(ctx, request)
}
