package grpcserver

import (
	"context"

	apiv1alpha1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1alpha1"
	"github.com/kagent-dev/kagent/go/core/internal/service/serviceerrors"
	systemservice "github.com/kagent-dev/kagent/go/core/internal/service/system"
	"google.golang.org/protobuf/types/known/structpb"
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
		response.WorkerPools = append(response.WorkerPools, &apiv1alpha1.SubstrateWorkerPool{
			Namespace:  workerPool.Namespace,
			Name:       workerPool.Name,
			Replicas:   workerPool.Replicas,
			AteomImage: workerPool.AteomImage,
		})
	}
	for _, actorTemplate := range result.ActorTemplates {
		response.ActorTemplates = append(response.ActorTemplates, &apiv1alpha1.SubstrateActorTemplate{
			Namespace:       actorTemplate.Namespace,
			Name:            actorTemplate.Name,
			Phase:           actorTemplate.Phase,
			GoldenActorId:   actorTemplate.GoldenActorID,
			GoldenSnapshot:  actorTemplate.GoldenSnapshot,
			SandboxClass:    actorTemplate.SandboxClass,
			WorkerSelector:  actorTemplate.WorkerSelector,
			HarnessName:     actorTemplate.HarnessName,
			ManagedByKagent: actorTemplate.ManagedByKagent,
		})
	}
	for _, actor := range result.Actors {
		response.Actors = append(response.Actors, &apiv1alpha1.SubstrateActor{
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
		})
	}
	for _, worker := range result.Workers {
		response.Workers = append(response.Workers, &apiv1alpha1.SubstrateWorker{
			WorkerNamespace: worker.WorkerNamespace,
			WorkerPool:      worker.WorkerPool,
			WorkerPod:       worker.WorkerPod,
			ActorNamespace:  worker.ActorNamespace,
			ActorTemplate:   worker.ActorTemplate,
			ActorId:         worker.ActorID,
			ActorStatus:     worker.ActorStatus,
			Ip:              worker.IP,
			Version:         worker.Version,
		})
	}
	return response, nil
}
