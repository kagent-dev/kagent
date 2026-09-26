package a2agateway

import (
	"context"
	"strings"

	a2a "github.com/a2aproject/a2a-go/v2/a2a"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/validation"
)

// route resolves the Agent selected at the transport boundary. Both transports
// supply the SDK's routing metadata, so the gateway never inspects HTTP paths or
// chooses between a payload tenant and a URL.
func route(ctx context.Context) (types.NamespacedName, error) {
	tenant, _ := a2a.TenantFrom(ctx)
	namespace, name, ok := strings.Cut(tenant, "/")
	if !ok || len(validation.IsDNS1123Label(namespace)) != 0 || len(validation.IsDNS1123Subdomain(name)) != 0 {
		return types.NamespacedName{}, a2a.NewError(a2a.ErrInvalidRequest, "Agent tenant must be namespace/name")
	}
	return types.NamespacedName{Namespace: namespace, Name: name}, nil
}
