package grpcserver

import (
	"context"

	apiv1alpha1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1alpha1"
	"github.com/kagent-dev/kagent/go/api/structuredobject"
	"github.com/kagent-dev/kagent/go/api/v1alpha3"
	"github.com/kagent-dev/kagent/go/core/internal/service/kubecrud"
	"github.com/kagent-dev/kagent/go/core/internal/service/serviceerrors"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/types"
)

// harnessKind is the Harness CRD, not the AgentHarness one that agent.go
// serves. See the comment on HarnessService in harnesses.proto.
const harnessKind = "Harness"

// Harness runtime adapter names, as reported in the denormalised runtime field.
const (
	harnessRuntimeKagent = "kagent"
	harnessRuntimeCodex  = "codex"
	harnessRuntimeClaude = "claude"
)

type harnessServer struct {
	apiv1alpha1.UnimplementedHarnessServiceServer
	service         *kubecrud.ScopedService[*v1alpha3.Harness, *v1alpha3.HarnessList]
	maxMessageBytes int
}

func newHarnessServer(service *kubecrud.ScopedService[*v1alpha3.Harness, *v1alpha3.HarnessList], maxMessageBytes int) *harnessServer {
	return &harnessServer{service: service, maxMessageBytes: maxMessageBytes}
}

func (s *harnessServer) ListHarnesses(ctx context.Context, request *apiv1alpha1.ListHarnessesRequest) (*apiv1alpha1.ListHarnessesResponse, error) {
	items, err := s.service.List(ctx, request.GetNamespace())
	if err != nil {
		return nil, err
	}
	harnesses := make([]*apiv1alpha1.Harness, 0, len(items))
	for _, item := range items {
		encoded, err := s.harness(item)
		if err != nil {
			return nil, err
		}
		harnesses = append(harnesses, encoded)
	}
	return &apiv1alpha1.ListHarnessesResponse{Harnesses: harnesses}, nil
}

func (s *harnessServer) CreateHarness(ctx context.Context, request *apiv1alpha1.CreateHarnessRequest) (*apiv1alpha1.CreateHarnessResponse, error) {
	incoming := &v1alpha3.Harness{}
	if err := s.decodeResource(request.GetRef(), request.GetResource(), incoming); err != nil {
		return nil, err
	}
	incoming.Status = v1alpha3.HarnessStatus{}
	result, err := s.service.Create(ctx, incoming)
	if err != nil {
		return nil, err
	}
	encoded, err := s.harness(result)
	if err != nil {
		return nil, err
	}
	return &apiv1alpha1.CreateHarnessResponse{Harness: encoded}, nil
}

func (s *harnessServer) DeleteHarness(ctx context.Context, request *apiv1alpha1.DeleteHarnessRequest) (*apiv1alpha1.DeleteHarnessResponse, error) {
	ref, err := requiredHarnessRef(request.GetRef())
	if err != nil {
		return nil, err
	}
	if err := s.service.Delete(ctx, ref); err != nil {
		return nil, err
	}
	return &apiv1alpha1.DeleteHarnessResponse{}, nil
}

func (s *harnessServer) harness(object *v1alpha3.Harness) (*apiv1alpha1.Harness, error) {
	resource, err := structuredobject.FromGo(object, v1alpha3.GroupVersion.String(), harnessKind, s.maxMessageBytes)
	if err != nil {
		return nil, serviceerrors.NewInternal("Failed to encode Harness resource", err)
	}
	return &apiv1alpha1.Harness{
		Ref:           &apiv1alpha1.ResourceReference{Namespace: object.Namespace, Name: object.Name},
		Resource:      resource,
		Runtime:       harnessRuntime(object),
		WorkloadImage: object.Spec.Workload.Image,
		Ready:         meta.IsStatusConditionTrue(object.Status.Conditions, v1alpha3.HarnessConditionTypeReady),
	}, nil
}

// harnessRuntime reports which adapter the spec selects. The CRD's CEL rule
// admits exactly one, so the empty string means an object that predates or
// violates that rule rather than a fourth kind of runtime.
func harnessRuntime(object *v1alpha3.Harness) string {
	switch {
	case object.Spec.Kagent != nil:
		return harnessRuntimeKagent
	case object.Spec.Codex != nil:
		return harnessRuntimeCodex
	case object.Spec.Claude != nil:
		return harnessRuntimeClaude
	default:
		return ""
	}
}

// decodeResource reads the CR out of the request and forces its metadata to
// agree with the ref, so a payload naming a different object cannot be used to
// write outside the namespace the caller was authorized against.
func (s *harnessServer) decodeResource(ref *apiv1alpha1.ResourceReference, resource *apiv1alpha1.StructuredObject, destination *v1alpha3.Harness) error {
	if ref == nil || ref.GetNamespace() == "" || ref.GetName() == "" {
		return serviceerrors.NewInvalidArgument("Harness namespace and name are required", nil)
	}
	if err := structuredobject.ToGo(resource, harnessKind, destination, s.maxMessageBytes); err != nil {
		return serviceerrors.NewInvalidArgument("Invalid Harness resource", err)
	}
	if destination.GetName() != "" && destination.GetName() != ref.GetName() {
		return serviceerrors.NewInvalidArgument("Harness reference does not match resource metadata", nil)
	}
	if destination.GetNamespace() != "" && destination.GetNamespace() != ref.GetNamespace() {
		return serviceerrors.NewInvalidArgument("Harness reference does not match resource metadata", nil)
	}
	destination.SetName(ref.GetName())
	destination.SetNamespace(ref.GetNamespace())
	return nil
}

func requiredHarnessRef(ref *apiv1alpha1.ResourceReference) (types.NamespacedName, error) {
	if ref == nil || ref.GetNamespace() == "" || ref.GetName() == "" {
		return types.NamespacedName{}, serviceerrors.NewInvalidArgument("Harness namespace and name are required", nil)
	}
	return types.NamespacedName{Namespace: ref.GetNamespace(), Name: ref.GetName()}, nil
}
