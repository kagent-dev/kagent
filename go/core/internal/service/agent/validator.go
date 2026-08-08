package agent

import (
	"context"

	"github.com/kagent-dev/kagent/go/api/v1alpha2"
	agenttranslator "github.com/kagent-dev/kagent/go/core/internal/controller/translator/agent"
	"github.com/kagent-dev/kagent/go/core/internal/service/serviceerrors"
	"github.com/kagent-dev/kagent/go/core/internal/utils"
	"github.com/kagent-dev/kagent/go/core/pkg/sandboxbackend"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type ManifestValidatorConfig struct {
	KubeClient         client.Client
	WatchedNamespaces  []string
	DefaultModelConfig types.NamespacedName
	Plugins            []agenttranslator.TranslatorPlugin
	ProxyURL           string
	SandboxBackend     sandboxbackend.Backend
	MCPEgressPlaintext bool
}

func NewManifestValidator(config ManifestValidatorConfig) Validator {
	return func(ctx context.Context, object v1alpha2.AgentObject) error {
		kubeClient := utils.NewKubeClientWrapper(config.KubeClient)
		if err := kubeClient.AddInMemory(object); err != nil {
			return serviceerrors.NewInternal("Failed to add Agent to Kubernetes wrapper", err)
		}

		translator := agenttranslator.NewAdkApiTranslatorWithWatchedNamespaces(
			kubeClient,
			config.WatchedNamespaces,
			config.DefaultModelConfig,
			config.Plugins,
			config.ProxyURL,
			config.SandboxBackend,
			config.MCPEgressPlaintext,
		)
		inputs, err := translator.CompileAgent(ctx, object)
		if err != nil {
			return serviceerrors.NewInvalidArgument("Invalid agent configuration", err)
		}
		if _, err := translator.BuildManifest(ctx, object, inputs); err != nil {
			return serviceerrors.NewInvalidArgument("Invalid agent configuration", err)
		}
		return nil
	}
}
