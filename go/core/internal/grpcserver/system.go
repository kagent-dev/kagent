package grpcserver

import (
	"context"

	apiv1alpha1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1alpha1"
	"github.com/kagent-dev/kagent/go/core/internal/service/serviceerrors"
	systemservice "github.com/kagent-dev/kagent/go/core/internal/service/system"
	"google.golang.org/protobuf/types/known/structpb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type systemServer struct {
	apiv1alpha1.UnimplementedSystemServiceServer
	service *systemservice.Service
}

func newSystemServer(service *systemservice.Service) *systemServer {
	return &systemServer{service: service}
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

func (s *systemServer) GetSubstrateStatus(ctx context.Context, request *apiv1alpha1.GetSubstrateStatusRequest) (*apiv1alpha1.GetSubstrateStatusResponse, error) {
	result, err := s.service.GetSubstrateStatus(ctx, request.GetNamespace())
	if err != nil {
		return nil, err
	}
	response := &apiv1alpha1.GetSubstrateStatusResponse{
		Enabled:        result.Enabled,
		AteApiError:    result.ATEAPIError,
		WorkerPools:    make([]*apiv1alpha1.SubstrateWorkerPool, 0, len(result.WorkerPools)),
		ActorTemplates: make([]*apiv1alpha1.SubstrateActorTemplate, 0, len(result.ActorTemplates)),
		Actors:         make([]*apiv1alpha1.SubstrateActor, 0, len(result.Actors)),
		Workers:        make([]*apiv1alpha1.SubstrateWorker, 0, len(result.Workers)),
	}
	for _, workerPool := range result.WorkerPools {
		response.WorkerPools = append(response.WorkerPools, substrateWorkerPoolProto(workerPool))
	}
	for _, actorTemplate := range result.ActorTemplates {
		response.ActorTemplates = append(response.ActorTemplates, substrateActorTemplateProto(actorTemplate))
	}
	for _, actor := range result.Actors {
		response.Actors = append(response.Actors, substrateActorProto(actor))
	}
	for _, worker := range result.Workers {
		response.Workers = append(response.Workers, substrateWorkerProto(worker))
	}
	return response, nil
}

func (s *systemServer) GetSubstrateSummary(ctx context.Context, request *apiv1alpha1.GetSubstrateSummaryRequest) (*apiv1alpha1.GetSubstrateSummaryResponse, error) {
	result, err := s.service.GetSubstrateSummary(ctx, request.GetNamespace())
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
		response.WorkerPools = append(response.WorkerPools, substrateWorkerPoolProto(workerPool))
	}
	for _, actorTemplate := range result.ActorTemplates {
		response.ActorTemplates = append(response.ActorTemplates, substrateActorTemplateProto(actorTemplate))
	}
	for _, statusCount := range result.ActorStatusCounts {
		response.ActorStatusCounts = append(response.ActorStatusCounts, &apiv1alpha1.SubstrateActorStatusCount{
			Status: statusCount.Status,
			Count:  statusCount.Count,
		})
	}
	return response, nil
}

func (s *systemServer) ListSubstrateActors(ctx context.Context, request *apiv1alpha1.ListSubstrateActorsRequest) (*apiv1alpha1.ListSubstrateActorsResponse, error) {
	result, err := s.service.ListSubstrateActors(ctx, systemservice.SubstrateListInput{
		Namespace: request.GetNamespace(),
		PageSize:  request.GetPageSize(),
		PageToken: request.GetPageToken(),
	})
	if err != nil {
		return nil, err
	}
	response := &apiv1alpha1.ListSubstrateActorsResponse{
		Enabled:       result.Enabled,
		AteApiError:   result.ATEAPIError,
		Actors:        make([]*apiv1alpha1.SubstrateActor, 0, len(result.Actors)),
		NextPageToken: result.NextPageToken,
		ComputedAt:    timestamppb.New(result.ComputedAt),
	}
	for _, actor := range result.Actors {
		response.Actors = append(response.Actors, substrateActorProto(actor))
	}
	return response, nil
}

func (s *systemServer) ListSubstrateWorkers(ctx context.Context, request *apiv1alpha1.ListSubstrateWorkersRequest) (*apiv1alpha1.ListSubstrateWorkersResponse, error) {
	result, err := s.service.ListSubstrateWorkers(ctx, systemservice.SubstrateListInput{
		Namespace: request.GetNamespace(),
		PageSize:  request.GetPageSize(),
		PageToken: request.GetPageToken(),
	})
	if err != nil {
		return nil, err
	}
	response := &apiv1alpha1.ListSubstrateWorkersResponse{
		Enabled:       result.Enabled,
		AteApiError:   result.ATEAPIError,
		Workers:       make([]*apiv1alpha1.SubstrateWorker, 0, len(result.Workers)),
		NextPageToken: result.NextPageToken,
		ComputedAt:    timestamppb.New(result.ComputedAt),
	}
	for _, worker := range result.Workers {
		response.Workers = append(response.Workers, substrateWorkerProto(worker))
	}
	return response, nil
}

// The four row conversions, shared by the whole-inventory read and the paged ones so
// that a column cannot be filled on one path and left blank on the other.

func substrateWorkerPoolProto(workerPool systemservice.SubstrateWorkerPool) *apiv1alpha1.SubstrateWorkerPool {
	return &apiv1alpha1.SubstrateWorkerPool{
		Namespace:  workerPool.Namespace,
		Name:       workerPool.Name,
		Replicas:   workerPool.Replicas,
		AteomImage: workerPool.AteomImage,
	}
}

func substrateActorTemplateProto(actorTemplate systemservice.SubstrateActorTemplate) *apiv1alpha1.SubstrateActorTemplate {
	return &apiv1alpha1.SubstrateActorTemplate{
		Namespace:       actorTemplate.Namespace,
		Name:            actorTemplate.Name,
		Phase:           actorTemplate.Phase,
		GoldenActorId:   actorTemplate.GoldenActorID,
		GoldenSnapshot:  actorTemplate.GoldenSnapshot,
		SandboxClass:    actorTemplate.SandboxClass,
		WorkerSelector:  actorTemplate.WorkerSelector,
		HarnessName:     actorTemplate.HarnessName,
		ManagedByKagent: actorTemplate.ManagedByKagent,
	}
}

func substrateActorProto(actor systemservice.SubstrateActor) *apiv1alpha1.SubstrateActor {
	return &apiv1alpha1.SubstrateActor{
		ActorId:                actor.ActorID,
		Atespace:               actor.Atespace,
		Status:                 actor.Status,
		ActorTemplateNamespace: actor.ActorTemplateNamespace,
		ActorTemplateName:      actor.ActorTemplateName,
		AteomPodNamespace:      actor.AteomPodNamespace,
		AteomPodName:           actor.AteomPodName,
		AteomPodIp:             actor.AteomPodIP,
		LatestSnapshot:         actor.LatestSnapshot,
		WorkerPoolName:         actor.WorkerPoolName,
		InProgressSnapshot:     actor.InProgressSnapshot,
		Version:                actor.Version,
	}
}

func substrateWorkerProto(worker systemservice.SubstrateWorker) *apiv1alpha1.SubstrateWorker {
	return &apiv1alpha1.SubstrateWorker{
		WorkerNamespace: worker.WorkerNamespace,
		WorkerPool:      worker.WorkerPool,
		WorkerPod:       worker.WorkerPod,
		ActorNamespace:  worker.ActorNamespace,
		ActorTemplate:   worker.ActorTemplate,
		ActorId:         worker.ActorID,
		Ip:              worker.IP,
		Version:         worker.Version,
	}
}
