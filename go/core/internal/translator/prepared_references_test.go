package translator_test

import (
	"testing"

	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/types"

	"github.com/kagent-dev/kagent/go/core/internal/translator"
)

func TestPreparedReferencesFromProvenance(t *testing.T) {
	for _, test := range []struct {
		name, snapshot string
		invalid        bool
		want           []translator.PreparedReference
	}{
		{name: "no shared inputs", snapshot: `{"inputs":[]}`},
		{name: "qualified and local identities", snapshot: `{"inputs":[{"apiVersion":"api.kagent.dev/v1alpha3","kind":"ModelConfig","name":"shared/model"},{"apiVersion":"api.kagent.dev/v1alpha3","kind":"RemoteMCPServer","name":"tools"},{"apiVersion":"api.kagent.dev/v1alpha3","kind":"ModelConfig","name":"shared/model"},{"apiVersion":"v1","kind":"ConfigMap","name":"headers"}]}`, want: []translator.PreparedReference{{Kind: "ModelConfig", Name: types.NamespacedName{Namespace: "shared", Name: "model"}}, {Kind: "RemoteMCPServer", Name: types.NamespacedName{Namespace: "team-a", Name: "tools"}}}},
		{name: "missing inputs", snapshot: `{}`, invalid: true},
		{name: "null inputs", snapshot: `{"inputs":null}`, invalid: true},
		{name: "malformed snapshot", snapshot: `{`, invalid: true},
		{name: "invalid identity", snapshot: `{"inputs":[{"apiVersion":"api.kagent.dev/v1alpha3","kind":"ModelConfig","name":"bad/too/many"}]}`, invalid: true},
		{name: "wrong API version", snapshot: `{"inputs":[{"apiVersion":"other/v1","kind":"ModelConfig","name":"shared/model"}]}`, invalid: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			refs, err := translator.PreparedReferencesFromProvenance([]byte(test.snapshot), "team-a")
			if test.invalid {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.Equal(t, test.want, refs)
		})
	}
}
