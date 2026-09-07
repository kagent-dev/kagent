package grpcserver

import (
	"context"

	apiv1alpha1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1alpha1"
	"github.com/kagent-dev/kagent/go/core/internal/service/agentinstance"
)

type agentInstanceServer struct {
	apiv1alpha1.UnimplementedAgentInstanceServiceServer
	service *agentinstance.Service
}

func (s *agentInstanceServer) CreateAgentInstance(ctx context.Context, request *apiv1alpha1.CreateAgentInstanceRequest) (*apiv1alpha1.CreateAgentInstanceResponse, error) {
	instance, err := s.service.Create(ctx, request.GetHarness(), request.GetAgentTemplate(), request.GetRequestId(), request.GetName())
	if err != nil {
		return nil, err
	}
	return &apiv1alpha1.CreateAgentInstanceResponse{AgentInstance: instance}, nil
}

func (s *agentInstanceServer) GetAgentInstance(ctx context.Context, request *apiv1alpha1.GetAgentInstanceRequest) (*apiv1alpha1.GetAgentInstanceResponse, error) {
	instance, err := s.service.Get(ctx, request.GetAgentInstanceId())
	if err != nil {
		return nil, err
	}
	return &apiv1alpha1.GetAgentInstanceResponse{AgentInstance: instance}, nil
}

func (s *agentInstanceServer) ListAgentInstances(ctx context.Context, request *apiv1alpha1.ListAgentInstancesRequest) (*apiv1alpha1.ListAgentInstancesResponse, error) {
	result, err := s.service.List(ctx, agentinstance.ListRequest{
		MatchLabels: request.GetMatchLabels(), AllCreators: request.GetAllCreators(),
		AgentTemplate: request.GetAgentTemplate(), Harness: request.GetHarness(),
		PageSize: int(request.GetPage().GetLimit()), PageToken: request.GetPage().GetPageToken(),
	})
	if err != nil {
		return nil, err
	}
	return &apiv1alpha1.ListAgentInstancesResponse{
		AgentInstances: result.Instances,
		Page:           &apiv1alpha1.PageResponse{NextPageToken: result.NextPageToken},
	}, nil
}

func (s *agentInstanceServer) UpdateAgentInstanceName(ctx context.Context, request *apiv1alpha1.UpdateAgentInstanceNameRequest) (*apiv1alpha1.UpdateAgentInstanceNameResponse, error) {
	instance, err := s.service.Rename(ctx, request.GetAgentInstanceId(), request.GetName())
	if err != nil {
		return nil, err
	}
	return &apiv1alpha1.UpdateAgentInstanceNameResponse{AgentInstance: instance}, nil
}

func (s *agentInstanceServer) SuspendAgentInstance(ctx context.Context, request *apiv1alpha1.SuspendAgentInstanceRequest) (*apiv1alpha1.SuspendAgentInstanceResponse, error) {
	instance, err := s.service.Suspend(ctx, request.GetAgentInstanceId())
	if err != nil {
		return nil, err
	}
	return &apiv1alpha1.SuspendAgentInstanceResponse{AgentInstance: instance}, nil
}

func (s *agentInstanceServer) ResumeAgentInstance(ctx context.Context, request *apiv1alpha1.ResumeAgentInstanceRequest) (*apiv1alpha1.ResumeAgentInstanceResponse, error) {
	instance, err := s.service.Resume(ctx, request.GetAgentInstanceId())
	if err != nil {
		return nil, err
	}
	return &apiv1alpha1.ResumeAgentInstanceResponse{AgentInstance: instance}, nil
}

func (s *agentInstanceServer) DeleteAgentInstance(ctx context.Context, request *apiv1alpha1.DeleteAgentInstanceRequest) (*apiv1alpha1.DeleteAgentInstanceResponse, error) {
	instance, err := s.service.Delete(ctx, request.GetAgentInstanceId())
	if err != nil {
		return nil, err
	}
	return &apiv1alpha1.DeleteAgentInstanceResponse{AgentInstance: instance}, nil
}

func (s *agentInstanceServer) CreateAgentInstanceShare(ctx context.Context, request *apiv1alpha1.CreateAgentInstanceShareRequest) (*apiv1alpha1.CreateAgentInstanceShareResponse, error) {
	share, token, err := s.service.CreateShare(ctx, request.GetAgentInstanceId(), sharePermissionName(request.GetPermission()))
	if err != nil {
		return nil, err
	}
	return &apiv1alpha1.CreateAgentInstanceShareResponse{Share: share.AgentInstanceShare, Token: token}, nil
}

func (s *agentInstanceServer) ListAgentInstanceShares(ctx context.Context, request *apiv1alpha1.ListAgentInstanceSharesRequest) (*apiv1alpha1.ListAgentInstanceSharesResponse, error) {
	result, err := s.service.ListShares(ctx, request.GetAgentInstanceId(), int(request.GetPage().GetLimit()), request.GetPage().GetPageToken())
	if err != nil {
		return nil, err
	}
	shares := make([]*apiv1alpha1.AgentInstanceShare, 0, len(result.Shares))
	for index := range result.Shares {
		shares = append(shares, result.Shares[index].AgentInstanceShare)
	}
	return &apiv1alpha1.ListAgentInstanceSharesResponse{
		Shares: shares, Page: &apiv1alpha1.PageResponse{NextPageToken: result.NextPageToken},
	}, nil
}

func (s *agentInstanceServer) RevokeAgentInstanceShare(ctx context.Context, request *apiv1alpha1.RevokeAgentInstanceShareRequest) (*apiv1alpha1.RevokeAgentInstanceShareResponse, error) {
	if err := s.service.RevokeShare(ctx, request.GetShareId()); err != nil {
		return nil, err
	}
	return &apiv1alpha1.RevokeAgentInstanceShareResponse{}, nil
}

func sharePermissionName(value apiv1alpha1.AgentInstanceSharePermission) string {
	switch value {
	case apiv1alpha1.AgentInstanceSharePermission_AGENT_INSTANCE_SHARE_PERMISSION_READ_ONLY:
		return "READ_ONLY"
	case apiv1alpha1.AgentInstanceSharePermission_AGENT_INSTANCE_SHARE_PERMISSION_READ_WRITE:
		return "READ_WRITE"
	default:
		return ""
	}
}
