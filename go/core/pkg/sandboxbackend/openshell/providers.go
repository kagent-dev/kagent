package openshell

import (
	"context"
	"fmt"
	"strings"

	"github.com/kagent-dev/kagent/go/api/openshell/gen/datamodelv1"
	openshellv1 "github.com/kagent-dev/kagent/go/api/openshell/gen/openshellv1"
	"github.com/kagent-dev/kagent/go/core/pkg/sandboxbackend/openshell/channels"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const genericProviderType = "generic"

// GatewayProviderDef describes an OpenShell gateway provider to create or update.
type GatewayProviderDef struct {
	Name        string
	Type        string
	Credentials map[string]string
}

// UpsertGatewayProvider creates or updates a single OpenShell gateway provider.
func UpsertGatewayProvider(ctx context.Context, osCli openshellv1.OpenShellClient, def GatewayProviderDef) error {
	if osCli == nil {
		return fmt.Errorf("openshell client is nil")
	}
	name := strings.TrimSpace(def.Name)
	if name == "" {
		return fmt.Errorf("provider name is empty")
	}
	providerType := strings.TrimSpace(def.Type)
	if providerType == "" {
		providerType = genericProviderType
	}

	getResp, err := osCli.GetProvider(ctx, &openshellv1.GetProviderRequest{Name: name})
	exists := false
	if err != nil {
		if status.Code(err) != codes.NotFound {
			return fmt.Errorf("GetProvider %s: %w", name, err)
		}
	} else if getResp.GetProvider() != nil {
		exists = true
	}

	providerProto := &datamodelv1.Provider{
		Metadata:    &datamodelv1.ObjectMeta{Name: name},
		Type:        providerType,
		Credentials: def.Credentials,
	}

	if exists {
		_, err = osCli.UpdateProvider(ctx, &openshellv1.UpdateProviderRequest{Provider: providerProto})
		if err != nil {
			return fmt.Errorf("UpdateProvider %s: %w", name, err)
		}
		return nil
	}
	_, err = osCli.CreateProvider(ctx, &openshellv1.CreateProviderRequest{Provider: providerProto})
	if err != nil {
		return fmt.Errorf("CreateProvider %s: %w", name, err)
	}
	return nil
}

// UpsertGatewayProviders upserts each provider definition.
func UpsertGatewayProviders(ctx context.Context, osCli openshellv1.OpenShellClient, defs []GatewayProviderDef) error {
	for _, def := range defs {
		if err := UpsertGatewayProvider(ctx, osCli, def); err != nil {
			return err
		}
	}
	return nil
}

func messagingDefsToGateway(defs []channels.MessagingProviderDef) []GatewayProviderDef {
	out := make([]GatewayProviderDef, 0, len(defs))
	for _, d := range defs {
		out = append(out, GatewayProviderDef{
			Name:        d.Name,
			Type:        genericProviderType,
			Credentials: d.Credentials,
		})
	}
	return out
}
