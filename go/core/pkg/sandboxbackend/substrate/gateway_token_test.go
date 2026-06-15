package substrate

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/kagent-dev/kagent/go/api/v1alpha2"
)

func TestResolveGatewayTokenRejectsEmptySecretValue(t *testing.T) {
	t.Parallel()

	scheme := runtime.NewScheme()
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(v1alpha2.AddToScheme(scheme))

	const ns = "kagent"
	ah := &v1alpha2.AgentHarness{
		ObjectMeta: metav1.ObjectMeta{Name: "claw", Namespace: ns},
		Spec: v1alpha2.AgentHarnessSpec{
			Substrate: &v1alpha2.AgentHarnessSubstrateSpec{
				GatewayTokenSecretRef: &v1alpha2.TypedLocalReference{Name: "openclaw-token"},
			},
		},
	}

	for _, tt := range []struct {
		name  string
		value []byte
	}{
		{name: "empty", value: []byte{}},
		{name: "whitespace", value: []byte("  \t\n  ")},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			secret := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{Name: "openclaw-token", Namespace: ns},
				Data:       map[string][]byte{GatewayTokenSecretKey: tt.value},
			}
			kube := fake.NewClientBuilder().WithScheme(scheme).WithObjects(secret).Build()

			_, err := ResolveGatewayToken(context.Background(), kube, ah)
			require.Error(t, err)
			require.Contains(t, err.Error(), `key "token" must not be empty`)
		})
	}
}

func TestResolveGatewayTokenAcceptsNonemptySecretValue(t *testing.T) {
	t.Parallel()

	scheme := runtime.NewScheme()
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(v1alpha2.AddToScheme(scheme))

	const ns = "kagent"
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "openclaw-token", Namespace: ns},
		Data:       map[string][]byte{GatewayTokenSecretKey: []byte("  secret-token  ")},
	}
	kube := fake.NewClientBuilder().WithScheme(scheme).WithObjects(secret).Build()
	ah := &v1alpha2.AgentHarness{
		ObjectMeta: metav1.ObjectMeta{Name: "claw", Namespace: ns},
		Spec: v1alpha2.AgentHarnessSpec{
			Substrate: &v1alpha2.AgentHarnessSubstrateSpec{
				GatewayTokenSecretRef: &v1alpha2.TypedLocalReference{Name: "openclaw-token"},
			},
		},
	}

	token, err := ResolveGatewayToken(context.Background(), kube, ah)
	require.NoError(t, err)
	require.Equal(t, "secret-token", token)
}
