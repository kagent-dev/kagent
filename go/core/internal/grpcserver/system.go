package grpcserver

import (
	"context"

	atev1alpha1 "github.com/agent-substrate/substrate/pkg/api/v1alpha1"
	apiv1alpha1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1alpha1"
	"github.com/kagent-dev/kagent/go/api/structuredobject"
	"github.com/kagent-dev/kagent/go/core/internal/service/serviceerrors"
	systemservice "github.com/kagent-dev/kagent/go/core/internal/service/system"
	"google.golang.org/protobuf/types/known/structpb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type systemServer struct {
	apiv1alpha1.UnimplementedSystemServiceServer
	service         *systemservice.Service
	maxMessageBytes int
}

func newSystemServer(service *systemservice.Service, maxMessageBytes int) *systemServer {
	return &systemServer{service: service, maxMessageBytes: maxMessageBytes}
}

func (s *systemServer) GetVersion(context.Context, *apiv1alpha1.GetVersionRequest) (*apiv1alpha1.GetVersionResponse, error) {
	result := s.service.GetVersion()
	return &apiv1alpha1.GetVersionResponse{
		KagentVersion: result.KAgentVersion,
		GitCommit:     result.GitCommit,
		BuildDate:     result.BuildDate,
	}, nil
}

func (s *systemServer) GetCurrentUser(ctx context.Context, _ *apiv1alpha1.GetCurrentUserRequest) (*apiv1alpha1.GetCurrentUserResponse, error) {
	claims, err := s.service.GetCurrentUser(ctx)
	if err != nil {
		return nil, err
	}
	encodedClaims, err := structpb.NewStruct(claims)
	if err != nil {
		return nil, serviceerrors.NewInternal("Failed to encode current user claims", err)
	}
	return &apiv1alpha1.GetCurrentUserResponse{Claims: encodedClaims}, nil
}

func (s *systemServer) ListNamespaces(ctx context.Context, _ *apiv1alpha1.ListNamespacesRequest) (*apiv1alpha1.ListNamespacesResponse, error) {
	result, err := s.service.ListNamespaces(ctx)
	if err != nil {
		return nil, err
	}
	namespaces := make([]*apiv1alpha1.Namespace, 0, len(result))
	for _, namespace := range result {
		namespaces = append(namespaces, &apiv1alpha1.Namespace{
			Name:   namespace.Name,
			Status: namespace.Status,
		})
	}
	return &apiv1alpha1.ListNamespacesResponse{Namespaces: namespaces}, nil
}

func (s *systemServer) GetSubstrateSummary(ctx context.Context, request *apiv1alpha1.GetSubstrateSummaryRequest) (*apiv1alpha1.GetSubstrateSummaryResponse, error) {
	result, err := s.service.GetSubstrateSummary(ctx, request.GetNamespace(), request.GetAtespace())
	if err != nil {
		return nil, err
	}
	response := &apiv1alpha1.GetSubstrateSummaryResponse{
		Enabled:           result.Enabled,
		AteApiError:       result.ATEAPIError,
		WorkerPools:       make([]*apiv1alpha1.SubstrateWorkerPool, 0, len(result.WorkerPools)),
		ActorTemplates:    make([]*apiv1alpha1.SubstrateActorTemplate, 0, len(result.ActorTemplates)),
		ActorCount:        result.ActorCount,
		WorkerCount:       result.WorkerCount,
		RunningActorCount: result.RunningActorCount,
		BusyWorkerCount:   result.BusyWorkerCount,
		ActorStatusCounts: make([]*apiv1alpha1.SubstrateActorStatusCount, 0, len(result.ActorStatusCounts)),
		ComputedAt:        timestamppb.New(result.ComputedAt),
	}
	for _, workerPool := range result.WorkerPools {
		encoded, err := s.workerPool(&workerPool)
		if err != nil {
			return nil, err
		}
		response.WorkerPools = append(response.WorkerPools, encoded)
	}
	for _, actorTemplate := range result.ActorTemplates {
		response.ActorTemplates = append(response.ActorTemplates, substrateActorTemplateProto(actorTemplate))
	}
	for _, statusCount := range result.ActorStatusCounts {
		response.ActorStatusCounts = append(response.ActorStatusCounts, &apiv1alpha1.SubstrateActorStatusCount{
			State: statusCount.State,
			Count: statusCount.Count,
		})
	}
	return response, nil
}

func (s *systemServer) ListSubstrateActors(ctx context.Context, request *apiv1alpha1.ListSubstrateActorsRequest) (*apiv1alpha1.ListSubstrateActorsResponse, error) {
	result, err := s.service.ListSubstrateActors(ctx, request)
	if err != nil {
		return nil, err
	}
	response := &apiv1alpha1.ListSubstrateActorsResponse{
		Enabled:          result.Enabled,
		AteApiError:      result.ATEAPIError,
		Actors:           result.Actors,
		Page:             &apiv1alpha1.PageResponse{NextPageToken: result.NextPageToken},
		ComputedAt:       timestamppb.New(result.ComputedAt),
		TotalSize:        result.TotalSize,
		AppliedSortField: result.AppliedSortField,
		AppliedSortOrder: result.AppliedSortOrder,
	}
	return response, nil
}

func (s *systemServer) ListSubstrateWorkers(ctx context.Context, request *apiv1alpha1.ListSubstrateWorkersRequest) (*apiv1alpha1.ListSubstrateWorkersResponse, error) {
	result, err := s.service.ListSubstrateWorkers(ctx, request)
	if err != nil {
		return nil, err
	}
	response := &apiv1alpha1.ListSubstrateWorkersResponse{
		Enabled:          result.Enabled,
		AteApiError:      result.ATEAPIError,
		Workers:          result.Workers,
		Page:             &apiv1alpha1.PageResponse{NextPageToken: result.NextPageToken},
		ComputedAt:       timestamppb.New(result.ComputedAt),
		TotalSize:        result.TotalSize,
		AppliedSortField: result.AppliedSortField,
		AppliedSortOrder: result.AppliedSortOrder,
	}
	return response, nil
}

// Row conversions, shared by the whole-inventory read and the paged ones so
// that a column cannot be filled on one path and left blank on the other.

func (s *systemServer) workerPool(workerPool *atev1alpha1.WorkerPool) (*apiv1alpha1.SubstrateWorkerPool, error) {
	resource, err := structuredobject.FromGo(workerPool, atev1alpha1.GroupVersion.String(), "WorkerPool", s.maxMessageBytes)
	if err != nil {
		return nil, serviceerrors.NewInternal("Failed to encode WorkerPool resource", err)
	}
	return &apiv1alpha1.SubstrateWorkerPool{
		Ref:      &apiv1alpha1.ResourceReference{Namespace: workerPool.Namespace, Name: workerPool.Name},
		Resource: resource,
	}, nil
}

func substrateActorTemplateProto(actorTemplate systemservice.SubstrateActorTemplate) *apiv1alpha1.SubstrateActorTemplate {
	return &apiv1alpha1.SubstrateActorTemplate{
		ActorTemplate:   actorTemplate.ActorTemplate,
		HarnessName:     actorTemplate.HarnessName,
		ManagedByKagent: actorTemplate.ManagedByKagent,
	}
}
