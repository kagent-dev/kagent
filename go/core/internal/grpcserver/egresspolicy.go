// Copyright 2026 The kagent Authors
// SPDX-License-Identifier: Apache-2.0

package grpcserver

import (
	"context"
	"maps"

	"k8s.io/apimachinery/pkg/types"

	apiv1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1"
	"github.com/kagent-dev/kagent/go/api/structuredobject"
	"github.com/kagent-dev/kagent/go/api/v1alpha3"
	"github.com/kagent-dev/kagent/go/core/internal/service/kubecrud"
	"github.com/kagent-dev/kagent/go/core/internal/service/serviceerrors"
)

type egressPolicyServer struct {
	apiv1.UnimplementedEgressPolicyServiceServer
	service         *kubecrud.Service[*v1alpha3.EgressPolicy, *v1alpha3.EgressPolicyList]
	maxMessageBytes int
}

var _ apiv1.EgressPolicyServiceServer = (*egressPolicyServer)(nil)

func (s *egressPolicyServer) ListEgressPolicies(ctx context.Context, request *apiv1.ListEgressPoliciesRequest) (*apiv1.ListEgressPoliciesResponse, error) {
	items, err := s.service.List(ctx, request.GetNamespace())
	if err != nil {
		return nil, err
	}
	policies := make([]*apiv1.EgressPolicy, 0, len(items))
	for _, item := range items {
		encoded, err := s.encode(item)
		if err != nil {
			return nil, err
		}
		policies = append(policies, encoded)
	}
	return &apiv1.ListEgressPoliciesResponse{EgressPolicies: policies}, nil
}

func (s *egressPolicyServer) GetEgressPolicy(ctx context.Context, request *apiv1.GetEgressPolicyRequest) (*apiv1.GetEgressPolicyResponse, error) {
	result, err := s.service.Get(ctx, egressPolicyRef(request.GetRef()))
	if err != nil {
		return nil, err
	}
	encoded, err := s.encode(result)
	if err != nil {
		return nil, err
	}
	return &apiv1.GetEgressPolicyResponse{EgressPolicy: encoded}, nil
}

func (s *egressPolicyServer) CreateEgressPolicy(ctx context.Context, request *apiv1.CreateEgressPolicyRequest) (*apiv1.CreateEgressPolicyResponse, error) {
	incoming, err := s.decode(request.GetRef(), request.GetResource())
	if err != nil {
		return nil, err
	}
	result, err := s.service.Create(ctx, incoming)
	if err != nil {
		return nil, err
	}
	encoded, err := s.encode(result)
	if err != nil {
		return nil, err
	}
	return &apiv1.CreateEgressPolicyResponse{EgressPolicy: encoded}, nil
}

func (s *egressPolicyServer) UpdateEgressPolicy(ctx context.Context, request *apiv1.UpdateEgressPolicyRequest) (*apiv1.UpdateEgressPolicyResponse, error) {
	incoming, err := s.decode(request.GetRef(), request.GetResource())
	if err != nil {
		return nil, err
	}
	existing, err := s.service.Get(ctx, egressPolicyRef(request.GetRef()))
	if err != nil {
		return nil, err
	}
	existing.Spec = *incoming.Spec.DeepCopy()
	existing.Labels = maps.Clone(incoming.Labels)
	// A caller-supplied resourceVersion makes the write conditional on the version it edited.
	if incoming.ResourceVersion != "" {
		existing.ResourceVersion = incoming.ResourceVersion
	}
	result, err := s.service.Update(ctx, existing)
	if err != nil {
		return nil, err
	}
	encoded, err := s.encode(result)
	if err != nil {
		return nil, err
	}
	return &apiv1.UpdateEgressPolicyResponse{EgressPolicy: encoded}, nil
}

func (s *egressPolicyServer) DeleteEgressPolicy(ctx context.Context, request *apiv1.DeleteEgressPolicyRequest) (*apiv1.DeleteEgressPolicyResponse, error) {
	if err := s.service.Delete(ctx, egressPolicyRef(request.GetRef())); err != nil {
		return nil, err
	}
	return &apiv1.DeleteEgressPolicyResponse{}, nil
}

func (s *egressPolicyServer) decode(ref *apiv1.ResourceReference, resource *apiv1.StructuredObject) (*v1alpha3.EgressPolicy, error) {
	incoming := &v1alpha3.EgressPolicy{}
	if err := structuredobject.ToGo(resource, v1alpha3.EgressPolicyKind, incoming, s.maxMessageBytes); err != nil {
		return nil, serviceerrors.NewInvalidArgument("Invalid EgressPolicy resource", err)
	}
	// Request validation has checked that any supplied metadata agrees with ref.
	incoming.Name = ref.GetName()
	incoming.Namespace = ref.GetNamespace()
	incoming.APIVersion = v1alpha3.GroupVersion.String()
	incoming.Kind = v1alpha3.EgressPolicyKind
	return incoming, nil
}

func (s *egressPolicyServer) encode(object *v1alpha3.EgressPolicy) (*apiv1.EgressPolicy, error) {
	resource, err := structuredobject.FromGo(object, v1alpha3.GroupVersion.String(), v1alpha3.EgressPolicyKind, s.maxMessageBytes)
	if err != nil {
		return nil, serviceerrors.NewInternal("Failed to encode EgressPolicy resource", err)
	}
	return &apiv1.EgressPolicy{
		Ref:      &apiv1.ResourceReference{Namespace: object.Namespace, Name: object.Name},
		Resource: resource,
	}, nil
}

func egressPolicyRef(ref *apiv1.ResourceReference) types.NamespacedName {
	return types.NamespacedName{Namespace: ref.GetNamespace(), Name: ref.GetName()}
}
