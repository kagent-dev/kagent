package agent_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/kagent-dev/kagent/go/api/v1alpha3"
	agentsvc "github.com/kagent-dev/kagent/go/core/internal/service/agent"
	"github.com/kagent-dev/kagent/go/core/internal/service/serviceerrors"
)

func TestNewManifestValidator_UnknownAgentTypeIsInvalidArgument(t *testing.T) {
	scheme := newScheme(t)
	kube := fake.NewClientBuilder().WithScheme(scheme).Build()

	validator := agentsvc.NewManifestValidator(agentsvc.ManifestValidatorConfig{
		KubeClient:         kube,
		DefaultModelConfig: types.NamespacedName{Namespace: "default", Name: "default-model"},
	})

	agent := &v1alpha3.SandboxAgent{
		ObjectMeta: metav1.ObjectMeta{Name: "agent-1", Namespace: "default"},
		Spec:       v1alpha3.SandboxAgentSpec{Type: "not-a-real-type"},
	}

	err := validator(context.Background(), agent)
	require.Error(t, err)
	assert.True(t, serviceerrors.IsCode(err, serviceerrors.CodeInvalidArgument))
}

func TestNewManifestValidator_DeclarativeMissingModelConfigIsInvalidArgument(t *testing.T) {
	scheme := newScheme(t)
	kube := fake.NewClientBuilder().WithScheme(scheme).Build()

	validator := agentsvc.NewManifestValidator(agentsvc.ManifestValidatorConfig{
		KubeClient:         kube,
		DefaultModelConfig: types.NamespacedName{Namespace: "default", Name: "default-model"},
	})

	agent := &v1alpha3.SandboxAgent{
		ObjectMeta: metav1.ObjectMeta{Name: "agent-1", Namespace: "default"},
		Spec: v1alpha3.SandboxAgentSpec{
			Type: v1alpha3.AgentType_Declarative,
			Declarative: &v1alpha3.DeclarativeAgentSpec{
				// ModelConfig deliberately omitted / points at a ModelConfig
				// that does not exist in the fake client.
				ModelConfig: "missing-model-config",
			},
		},
	}

	err := validator(context.Background(), agent)
	require.Error(t, err, "compiling against a nonexistent ModelConfig must fail")
	assert.True(t, serviceerrors.IsCode(err, serviceerrors.CodeInvalidArgument))
}

func TestNewManifestValidator_ByoMissingSpecIsInvalidArgument(t *testing.T) {
	scheme := newScheme(t)
	kube := fake.NewClientBuilder().WithScheme(scheme).Build()

	validator := agentsvc.NewManifestValidator(agentsvc.ManifestValidatorConfig{
		KubeClient:         kube,
		DefaultModelConfig: types.NamespacedName{Namespace: "default", Name: "default-model"},
	})

	agent := &v1alpha3.SandboxAgent{
		ObjectMeta: metav1.ObjectMeta{Name: "agent-1", Namespace: "default"},
		Spec: v1alpha3.SandboxAgentSpec{
			Type: v1alpha3.AgentType_BYO,
			// BYO field deliberately left nil - required for this type.
		},
	}

	err := validator(context.Background(), agent)
	require.Error(t, err, "BYO type without a BYO spec must fail validation")
	assert.True(t, serviceerrors.IsCode(err, serviceerrors.CodeInvalidArgument))
}
