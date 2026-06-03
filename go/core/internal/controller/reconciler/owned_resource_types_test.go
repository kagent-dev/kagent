package reconciler

import (
	"context"
	"reflect"
	"testing"

	"github.com/kagent-dev/kagent/go/api/v1alpha2"
	agenttranslator "github.com/kagent-dev/kagent/go/core/internal/controller/translator/agent"
	pkgtranslator "github.com/kagent-dev/kagent/go/core/pkg/translator"
	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// stubTranslator implements agenttranslator.AdkApiTranslator; only
// GetOwnedResourceTypes is exercised by these tests.
type stubTranslator struct{ owned []client.Object }

func (s *stubTranslator) CompileAgent(context.Context, v1alpha2.AgentObject) (*agenttranslator.AgentManifestInputs, error) {
	return nil, nil
}

func (s *stubTranslator) BuildManifest(context.Context, v1alpha2.AgentObject, *agenttranslator.AgentManifestInputs) (*agenttranslator.AgentOutputs, error) {
	return nil, nil
}

func (s *stubTranslator) GetOwnedResourceTypes() []client.Object { return s.owned }

// stubRMSPlugin implements pkgtranslator.RemoteMCPServerPlugin.
type stubRMSPlugin struct{ owned []client.Object }

func (s *stubRMSPlugin) ProcessRemoteMCPServer(context.Context, *v1alpha2.RemoteMCPServer, *pkgtranslator.RemoteMCPServerOutputs) error {
	return nil
}

func (s *stubRMSPlugin) GetOwnedResourceTypes() []client.Object { return s.owned }

// TestGetOwnedResourceTypes_Composition verifies that the owned types are the
// union of the agent translator's and every registered RemoteMCPServer plugin's
// types. Deduplication is SetupOwnerIndexes' responsibility (see
// TestDistinctByType in the utils package), so an overlapping type may
// legitimately appear more than once here.
func TestGetOwnedResourceTypes_Composition(t *testing.T) {
	// ConfigMap overlaps between the translator and the plugin; Secret is
	// plugin-only.
	tr := &stubTranslator{owned: []client.Object{&corev1.ConfigMap{}}}
	plugin := &stubRMSPlugin{owned: []client.Object{&corev1.ConfigMap{}, &corev1.Secret{}}}

	r := NewKagentReconciler(
		tr,
		nil, // kube — unused by GetOwnedResourceTypes
		nil, // dbClient — unused
		types.NamespacedName{},
		nil,   // watchedNamespaces
		nil,   // sandboxBackend
		false, // mcpEgressPlaintext
		[]pkgtranslator.RemoteMCPServerPlugin{plugin},
	)

	counts := map[reflect.Type]int{}
	for _, o := range r.GetOwnedResourceTypes() {
		counts[reflect.TypeOf(o)]++
	}
	assert.Contains(t, counts, reflect.TypeFor[*corev1.ConfigMap](), "translator type must be present")
	assert.Contains(t, counts, reflect.TypeFor[*corev1.Secret](), "plugin-only type must be present")
	assert.Equal(t, 2, counts[reflect.TypeFor[*corev1.ConfigMap]()], "overlapping type appears once per owning source")
}
