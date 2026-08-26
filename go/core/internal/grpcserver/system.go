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
		ActorStatusCounts: make([]*apiv1alpha1.SubstrateStatusCount, 0, len(result.ActorStatusCounts)),
		ComputedAt:        timestamppb.New(result.ComputedAt),
	}
	for _, workerPool := range result.WorkerPools {
		response.WorkerPools = append(response.WorkerPools, workerPoolToProto(workerPool))
	}
	for _, actorTemplate := range result.ActorTemplates {
		response.ActorTemplates = append(response.ActorTemplates, actorTemplateToProto(actorTemplate))
	}
	for _, statusCount := range result.ActorStatusCounts {
		response.ActorStatusCounts = append(response.ActorStatusCounts, &apiv1alpha1.SubstrateStatusCount{
			Status: statusCount.Status,
			Count:  statusCount.Count,
		})
	}
	return response, nil
}

/*
 * The sort enums, mapped both ways.
 *
 * Written out rather than derived, and keyed by the generated enum so a member
 * added to the proto fails the build here instead of being served as a zero. The
 * response reports the order that was *applied*, which is why the outbound
 * direction exists at all: a caller should be able to say how its rows are sorted
 * rather than assume its request was honoured.
 */
var actorSortFieldFromProto = map[apiv1alpha1.SubstrateActorSortField]systemservice.ActorSortField{
	apiv1alpha1.SubstrateActorSortField_SUBSTRATE_ACTOR_SORT_FIELD_UNSPECIFIED:    systemservice.ActorSortDefault,
	apiv1alpha1.SubstrateActorSortField_SUBSTRATE_ACTOR_SORT_FIELD_STATUS:         systemservice.ActorSortStatus,
	apiv1alpha1.SubstrateActorSortField_SUBSTRATE_ACTOR_SORT_FIELD_ACTOR_ID:       systemservice.ActorSortID,
	apiv1alpha1.SubstrateActorSortField_SUBSTRATE_ACTOR_SORT_FIELD_ACTOR_TEMPLATE: systemservice.ActorSortTemplate,
	apiv1alpha1.SubstrateActorSortField_SUBSTRATE_ACTOR_SORT_FIELD_WORKER_POD:     systemservice.ActorSortWorker,
}

var actorSortFieldToProto = map[systemservice.ActorSortField]apiv1alpha1.SubstrateActorSortField{
	systemservice.ActorSortDefault:  apiv1alpha1.SubstrateActorSortField_SUBSTRATE_ACTOR_SORT_FIELD_UNSPECIFIED,
	systemservice.ActorSortStatus:   apiv1alpha1.SubstrateActorSortField_SUBSTRATE_ACTOR_SORT_FIELD_STATUS,
	systemservice.ActorSortID:       apiv1alpha1.SubstrateActorSortField_SUBSTRATE_ACTOR_SORT_FIELD_ACTOR_ID,
	systemservice.ActorSortTemplate: apiv1alpha1.SubstrateActorSortField_SUBSTRATE_ACTOR_SORT_FIELD_ACTOR_TEMPLATE,
	systemservice.ActorSortWorker:   apiv1alpha1.SubstrateActorSortField_SUBSTRATE_ACTOR_SORT_FIELD_WORKER_POD,
}

var workerSortFieldFromProto = map[apiv1alpha1.SubstrateWorkerSortField]systemservice.WorkerSortField{
	apiv1alpha1.SubstrateWorkerSortField_SUBSTRATE_WORKER_SORT_FIELD_UNSPECIFIED: systemservice.WorkerSortDefault,
	apiv1alpha1.SubstrateWorkerSortField_SUBSTRATE_WORKER_SORT_FIELD_POOL:        systemservice.WorkerSortPool,
	apiv1alpha1.SubstrateWorkerSortField_SUBSTRATE_WORKER_SORT_FIELD_POD:         systemservice.WorkerSortPod,
	apiv1alpha1.SubstrateWorkerSortField_SUBSTRATE_WORKER_SORT_FIELD_ACTOR:       systemservice.WorkerSortActor,
}

var workerSortFieldToProto = map[systemservice.WorkerSortField]apiv1alpha1.SubstrateWorkerSortField{
	systemservice.WorkerSortDefault: apiv1alpha1.SubstrateWorkerSortField_SUBSTRATE_WORKER_SORT_FIELD_UNSPECIFIED,
	systemservice.WorkerSortPool:    apiv1alpha1.SubstrateWorkerSortField_SUBSTRATE_WORKER_SORT_FIELD_POOL,
	systemservice.WorkerSortPod:     apiv1alpha1.SubstrateWorkerSortField_SUBSTRATE_WORKER_SORT_FIELD_POD,
	systemservice.WorkerSortActor:   apiv1alpha1.SubstrateWorkerSortField_SUBSTRATE_WORKER_SORT_FIELD_ACTOR,
}

func sortOrderFromProto(order apiv1alpha1.SubstrateSortOrder) systemservice.SortOrder {
	if order == apiv1alpha1.SubstrateSortOrder_SUBSTRATE_SORT_ORDER_DESCENDING {
		return systemservice.SortDescending
	}
	return systemservice.SortAscending
}

func sortOrderToProto(order systemservice.SortOrder) apiv1alpha1.SubstrateSortOrder {
	if order == systemservice.SortDescending {
		return apiv1alpha1.SubstrateSortOrder_SUBSTRATE_SORT_ORDER_DESCENDING
	}
	return apiv1alpha1.SubstrateSortOrder_SUBSTRATE_SORT_ORDER_ASCENDING
}

func (s *systemServer) ListSubstrateActors(ctx context.Context, request *apiv1alpha1.ListSubstrateActorsRequest) (*apiv1alpha1.ListSubstrateActorsResponse, error) {
	result, err := s.service.ListSubstrateActors(ctx, systemservice.ListActorsRequest{
		Namespace: request.GetNamespace(),
		Filter:    request.GetFilter(),
		Limit:     request.GetPage().GetLimit(),
		PageToken: request.GetPage().GetPageToken(),
		// A field this build does not know maps to the default order rather than
		// being refused: a newer caller asking for a column added later gets a
		// coherent page, and the response says which order it actually got.
		SortField: actorSortFieldFromProto[request.GetSortField()],
		SortOrder: sortOrderFromProto(request.GetSortOrder()),
	})
	if err != nil {
		return nil, err
	}
	response := &apiv1alpha1.ListSubstrateActorsResponse{
		Actors:           make([]*apiv1alpha1.SubstrateActor, 0, len(result.Actors)),
		Page:             &apiv1alpha1.PageResponse{NextPageToken: result.NextPageToken},
		TotalSize:        result.TotalSize,
		AppliedSortField: actorSortFieldToProto[result.SortField],
		AppliedSortOrder: sortOrderToProto(result.SortOrder),
		ComputedAt:       timestamppb.New(result.ComputedAt),
	}
	for _, actor := range result.Actors {
		response.Actors = append(response.Actors, actorToProto(actor))
	}
	return response, nil
}

func (s *systemServer) ListSubstrateWorkers(ctx context.Context, request *apiv1alpha1.ListSubstrateWorkersRequest) (*apiv1alpha1.ListSubstrateWorkersResponse, error) {
	result, err := s.service.ListSubstrateWorkers(ctx, systemservice.ListWorkersRequest{
		Namespace: request.GetNamespace(),
		Filter:    request.GetFilter(),
		Limit:     request.GetPage().GetLimit(),
		PageToken: request.GetPage().GetPageToken(),
		SortField: workerSortFieldFromProto[request.GetSortField()],
		SortOrder: sortOrderFromProto(request.GetSortOrder()),
	})
	if err != nil {
		return nil, err
	}
	response := &apiv1alpha1.ListSubstrateWorkersResponse{
		Workers:          make([]*apiv1alpha1.SubstrateWorker, 0, len(result.Workers)),
		Page:             &apiv1alpha1.PageResponse{NextPageToken: result.NextPageToken},
		TotalSize:        result.TotalSize,
		AppliedSortField: workerSortFieldToProto[result.SortField],
		AppliedSortOrder: sortOrderToProto(result.SortOrder),
		ComputedAt:       timestamppb.New(result.ComputedAt),
	}
	for _, worker := range result.Workers {
		response.Workers = append(response.Workers, workerToProto(worker))
	}
	return response, nil
}

// The four row conversions, shared by GetSubstrateStatus and the paged reads
// that replaced it, so the same record cannot arrive shaped differently
// depending on which RPC a caller used.

func workerPoolToProto(workerPool systemservice.SubstrateWorkerPool) *apiv1alpha1.SubstrateWorkerPool {
	return &apiv1alpha1.SubstrateWorkerPool{
		Namespace:  workerPool.Namespace,
		Name:       workerPool.Name,
		Replicas:   workerPool.Replicas,
		AteomImage: workerPool.AteomImage,
	}
}

func actorTemplateToProto(actorTemplate systemservice.SubstrateActorTemplate) *apiv1alpha1.SubstrateActorTemplate {
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

func actorToProto(actor systemservice.SubstrateActor) *apiv1alpha1.SubstrateActor {
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

func workerToProto(worker systemservice.SubstrateWorker) *apiv1alpha1.SubstrateWorker {
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
		response.WorkerPools = append(response.WorkerPools, workerPoolToProto(workerPool))
	}
	for _, actorTemplate := range result.ActorTemplates {
		response.ActorTemplates = append(response.ActorTemplates, actorTemplateToProto(actorTemplate))
	}
	for _, actor := range result.Actors {
		response.Actors = append(response.Actors, actorToProto(actor))
	}
	for _, worker := range result.Workers {
		response.Workers = append(response.Workers, workerToProto(worker))
	}
	return response, nil
}
