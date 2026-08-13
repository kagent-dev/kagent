package agentinstance

import (
	"context"

	dbpkg "github.com/kagent-dev/kagent/go/api/database"
	apiv1alpha1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1alpha1"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type grpcServer struct {
	apiv1alpha1.UnimplementedAgentInstanceServiceServer
	service *Service
}

func RegisterGRPC(registrar grpc.ServiceRegistrar, service *Service) {
	apiv1alpha1.RegisterAgentInstanceServiceServer(registrar, &grpcServer{service: service})
}

func (s *grpcServer) CreateAgentInstance(ctx context.Context, request *apiv1alpha1.CreateAgentInstanceRequest) (*apiv1alpha1.CreateAgentInstanceResponse, error) {
	instance, err := s.service.Create(ctx, request.GetNamespace(), request.GetHarness(), request.GetAgentTemplate(), request.GetRequestId())
	if err != nil {
		return nil, err
	}
	return &apiv1alpha1.CreateAgentInstanceResponse{AgentInstance: instance}, nil
}

func (s *grpcServer) GetAgentInstance(ctx context.Context, request *apiv1alpha1.GetAgentInstanceRequest) (*apiv1alpha1.GetAgentInstanceResponse, error) {
	instance, err := s.service.Get(ctx, request.GetNamespace(), request.GetAgentInstanceId())
	if err != nil {
		return nil, err
	}
	return &apiv1alpha1.GetAgentInstanceResponse{AgentInstance: instance}, nil
}

func (s *grpcServer) ListAgentInstances(ctx context.Context, request *apiv1alpha1.ListAgentInstancesRequest) (*apiv1alpha1.ListAgentInstancesResponse, error) {
	result, err := s.service.List(ctx, ListRequest{
		Namespace: request.GetNamespace(), MatchLabels: request.GetMatchLabels(), AllCreators: request.GetAllCreators(),
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

func (s *grpcServer) DeleteAgentInstance(ctx context.Context, request *apiv1alpha1.DeleteAgentInstanceRequest) (*apiv1alpha1.DeleteAgentInstanceResponse, error) {
	instance, err := s.service.Delete(ctx, request.GetNamespace(), request.GetAgentInstanceId())
	if err != nil {
		return nil, err
	}
	return &apiv1alpha1.DeleteAgentInstanceResponse{AgentInstance: instance}, nil
}

func (s *grpcServer) CreateAgentInstanceShare(ctx context.Context, request *apiv1alpha1.CreateAgentInstanceShareRequest) (*apiv1alpha1.CreateAgentInstanceShareResponse, error) {
	share, token, err := s.service.CreateShare(ctx, request.GetNamespace(), request.GetAgentInstanceId(), sharePermissionName(request.GetPermission()))
	if err != nil {
		return nil, err
	}
	return &apiv1alpha1.CreateAgentInstanceShareResponse{Share: agentInstanceShareProto(share), Token: token}, nil
}

func (s *grpcServer) ListAgentInstanceShares(ctx context.Context, request *apiv1alpha1.ListAgentInstanceSharesRequest) (*apiv1alpha1.ListAgentInstanceSharesResponse, error) {
	result, err := s.service.ListShares(ctx, request.GetNamespace(), request.GetAgentInstanceId(), int(request.GetPage().GetLimit()), request.GetPage().GetPageToken())
	if err != nil {
		return nil, err
	}
	shares := make([]*apiv1alpha1.AgentInstanceShare, 0, len(result.Shares))
	for index := range result.Shares {
		shares = append(shares, agentInstanceShareProto(&result.Shares[index]))
	}
	return &apiv1alpha1.ListAgentInstanceSharesResponse{
		Shares: shares, Page: &apiv1alpha1.PageResponse{NextPageToken: result.NextPageToken},
	}, nil
}

func (s *grpcServer) RevokeAgentInstanceShare(ctx context.Context, request *apiv1alpha1.RevokeAgentInstanceShareRequest) (*apiv1alpha1.RevokeAgentInstanceShareResponse, error) {
	if err := s.service.RevokeShare(ctx, request.GetNamespace(), request.GetShareId()); err != nil {
		return nil, err
	}
	return &apiv1alpha1.RevokeAgentInstanceShareResponse{}, nil
}

func agentInstanceShareProto(share *dbpkg.AgentInstanceShare) *apiv1alpha1.AgentInstanceShare {
	return &apiv1alpha1.AgentInstanceShare{
		Id: share.ID, Namespace: share.Namespace, AgentInstanceId: share.InstanceID,
		Creator: share.Creator, Permission: agentInstanceSharePermission(share.Permission),
		CreatedAt: timestamppb.New(share.CreatedAt),
	}
}

func agentInstanceSharePermission(value string) apiv1alpha1.AgentInstanceSharePermission {
	return apiv1alpha1.AgentInstanceSharePermission(apiv1alpha1.AgentInstanceSharePermission_value["AGENT_INSTANCE_SHARE_PERMISSION_"+value])
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
