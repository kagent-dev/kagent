package grpcserver

import (
	"context"

	atev1alpha1 "go.ate.dev/substrate/pkg/api/v1alpha1"
	"google.golang.org/protobuf/types/known/structpb"
	"google.golang.org/protobuf/types/known/timestamppb"

	apiv1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1"
	"github.com/kagent-dev/kagent/go/api/structuredobject"
	"github.com/kagent-dev/kagent/go/core/internal/service/serviceerrors"
	systemservice "github.com/kagent-dev/kagent/go/core/internal/service/system"
)

type systemServer struct {
	apiv1.UnimplementedSystemServiceServer
	service         *systemservice.Service
	maxMessageBytes int
}

func newSystemServer(service *systemservice.Service, maxMessageBytes int) *systemServer {
	return &systemServer{service: service, maxMessageBytes: maxMessageBytes}
}

func (s *systemServer) GetVersion(context.Context, *apiv1.GetVersionRequest) (*apiv1.GetVersionResponse, error) {
	result := s.service.GetVersion()
	return &apiv1.GetVersionResponse{
		KagentVersion: result.KAgentVersion,
		GitCommit:     result.GitCommit,
		BuildDate:     result.BuildDate,
	}, nil
}

func (s *systemServer) GetCurrentUser(ctx context.Context, _ *apiv1.GetCurrentUserRequest) (*apiv1.GetCurrentUserResponse, error) {
	claims, err := s.service.GetCurrentUser(ctx)
	if err != nil {
		return nil, err
	}
	encodedClaims, err := structpb.NewStruct(claims)
	if err != nil {
		return nil, serviceerrors.NewInternal("Failed to encode current user claims", err)
	}
	return &apiv1.GetCurrentUserResponse{Claims: encodedClaims}, nil
}

func (s *systemServer) ListNamespaces(ctx context.Context, _ *apiv1.ListNamespacesRequest) (*apiv1.ListNamespacesResponse, error) {
	result, err := s.service.ListNamespaces(ctx)
	if err != nil {
		return nil, err
	}
	namespaces := make([]*apiv1.Namespace, 0, len(result))
	for _, namespace := range result {
		namespaces = append(namespaces, &apiv1.Namespace{
			Name:   namespace.Name,
			Status: namespace.Status,
		})
	}
	return &apiv1.ListNamespacesResponse{Namespaces: namespaces}, nil
}

func (s *systemServer) GetSubstrateSummary(ctx context.Context, request *apiv1.GetSubstrateSummaryRequest) (*apiv1.GetSubstrateSummaryResponse, error) {
	result, err := s.service.GetSubstrateSummary(ctx, request.GetNamespace(), request.GetAtespace())
	if err != nil {
		return nil, err
	}
	response := &apiv1.GetSubstrateSummaryResponse{
		AteApiError:       result.ATEAPIError,
		WorkerPools:       make([]*apiv1.SubstrateWorkerPool, 0, len(result.WorkerPools)),
		ActorTemplates:    result.ActorTemplates,
		ActorCount:        result.ActorCount,
		WorkerCount:       result.WorkerCount,
		RunningActorCount: result.RunningActorCount,
		BusyWorkerCount:   result.BusyWorkerCount,
		ActorStatusCounts: make([]*apiv1.SubstrateActorStatusCount, 0, len(result.ActorStatusCounts)),
		ComputedAt:        timestamppb.New(result.ComputedAt),
	}
	for _, workerPool := range result.WorkerPools {
		encoded, err := s.workerPool(&workerPool)
		if err != nil {
			return nil, err
		}
		response.WorkerPools = append(response.WorkerPools, encoded)
	}
	for _, statusCount := range result.ActorStatusCounts {
		response.ActorStatusCounts = append(response.ActorStatusCounts, &apiv1.SubstrateActorStatusCount{
			State: statusCount.State,
			Count: statusCount.Count,
		})
	}
	return response, nil
}

func (s *systemServer) ListSubstrateActors(ctx context.Context, request *apiv1.ListSubstrateActorsRequest) (*apiv1.ListSubstrateActorsResponse, error) {
	result, err := s.service.ListSubstrateActors(ctx, request)
	if err != nil {
		return nil, err
	}
	response := &apiv1.ListSubstrateActorsResponse{
		AteApiError: result.ATEAPIError,
		Actors:      result.Actors,
		Page:        &apiv1.PageResponse{NextPageToken: result.NextPageToken},
		ComputedAt:  timestamppb.New(result.ComputedAt),
	}
	return response, nil
}

func (s *systemServer) ListSubstrateWorkers(ctx context.Context, request *apiv1.ListSubstrateWorkersRequest) (*apiv1.ListSubstrateWorkersResponse, error) {
	result, err := s.service.ListSubstrateWorkers(ctx, request)
	if err != nil {
		return nil, err
	}
	response := &apiv1.ListSubstrateWorkersResponse{
		AteApiError: result.ATEAPIError,
		Workers:     result.Workers,
		Page:        &apiv1.PageResponse{NextPageToken: result.NextPageToken},
		ComputedAt:  timestamppb.New(result.ComputedAt),
	}
	return response, nil
}

func (s *systemServer) workerPool(workerPool *atev1alpha1.WorkerPool) (*apiv1.SubstrateWorkerPool, error) {
	resource, err := structuredobject.FromGo(workerPool, atev1alpha1.GroupVersion.String(), "WorkerPool", s.maxMessageBytes)
	if err != nil {
		return nil, serviceerrors.NewInternal("Failed to encode WorkerPool resource", err)
	}
	return &apiv1.SubstrateWorkerPool{
		Ref:      &apiv1.ResourceReference{Namespace: workerPool.Namespace, Name: workerPool.Name},
		Resource: resource,
	}, nil
}
