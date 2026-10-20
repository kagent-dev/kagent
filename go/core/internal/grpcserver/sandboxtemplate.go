// Copyright 2026 The kagent Authors
// SPDX-License-Identifier: Apache-2.0

package grpcserver

import (
	"context"

	"k8s.io/apimachinery/pkg/types"

	apiv1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1"
	"github.com/kagent-dev/kagent/go/api/structuredobject"
	"github.com/kagent-dev/kagent/go/api/v1alpha3"
	"github.com/kagent-dev/kagent/go/core/internal/service/kubecrud"
	"github.com/kagent-dev/kagent/go/core/internal/service/serviceerrors"
)

type sandboxTemplateServer struct {
	apiv1.UnimplementedSandboxTemplateServiceServer
	service         *kubecrud.Service[*v1alpha3.SandboxTemplate, *v1alpha3.SandboxTemplateList]
	maxMessageBytes int
}

var _ apiv1.SandboxTemplateServiceServer = (*sandboxTemplateServer)(nil)

func (s *sandboxTemplateServer) ListSandboxTemplates(ctx context.Context, request *apiv1.ListSandboxTemplatesRequest) (*apiv1.ListSandboxTemplatesResponse, error) {
	items, err := s.service.List(ctx, request.GetNamespace())
	if err != nil {
		return nil, err
	}
	templates := make([]*apiv1.SandboxTemplate, 0, len(items))
	for _, item := range items {
		encoded, err := s.encode(item)
		if err != nil {
			return nil, err
		}
		templates = append(templates, encoded)
	}
	return &apiv1.ListSandboxTemplatesResponse{SandboxTemplates: templates}, nil
}

func (s *sandboxTemplateServer) CreateSandboxTemplate(ctx context.Context, request *apiv1.CreateSandboxTemplateRequest) (*apiv1.CreateSandboxTemplateResponse, error) {
	incoming := &v1alpha3.SandboxTemplate{}
	if err := structuredobject.ToGo(request.GetResource(), v1alpha3.SandboxTemplateKind, incoming, s.maxMessageBytes); err != nil {
		return nil, serviceerrors.NewInvalidArgument("Invalid SandboxTemplate resource", err)
	}
	// Request validation has checked that any supplied metadata agrees with ref.
	incoming.Name = request.GetRef().GetName()
	incoming.Namespace = request.GetRef().GetNamespace()
	incoming.APIVersion = v1alpha3.GroupVersion.String()
	incoming.Kind = v1alpha3.SandboxTemplateKind
	result, err := s.service.Create(ctx, incoming)
	if err != nil {
		return nil, err
	}
	encoded, err := s.encode(result)
	if err != nil {
		return nil, err
	}
	return &apiv1.CreateSandboxTemplateResponse{SandboxTemplate: encoded}, nil
}

func (s *sandboxTemplateServer) DeleteSandboxTemplate(ctx context.Context, request *apiv1.DeleteSandboxTemplateRequest) (*apiv1.DeleteSandboxTemplateResponse, error) {
	ref := types.NamespacedName{Namespace: request.GetRef().GetNamespace(), Name: request.GetRef().GetName()}
	if err := s.service.Delete(ctx, ref); err != nil {
		return nil, err
	}
	return &apiv1.DeleteSandboxTemplateResponse{}, nil
}

func (s *sandboxTemplateServer) encode(object *v1alpha3.SandboxTemplate) (*apiv1.SandboxTemplate, error) {
	resource, err := structuredobject.FromGo(object, v1alpha3.GroupVersion.String(), v1alpha3.SandboxTemplateKind, s.maxMessageBytes)
	if err != nil {
		return nil, serviceerrors.NewInternal("Failed to encode SandboxTemplate resource", err)
	}
	return &apiv1.SandboxTemplate{
		Ref:           &apiv1.ResourceReference{Namespace: object.Namespace, Name: object.Name},
		Resource:      resource,
		WorkloadImage: object.Spec.Workload.Image,
	}, nil
}
