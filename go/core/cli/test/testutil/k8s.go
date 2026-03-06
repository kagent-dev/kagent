package testutil

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"
	"sigs.k8s.io/controller-runtime/pkg/client"
	fakeclient "sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/kagent-dev/kagent/go/api/v1alpha2"
)

// NewFakeK8sClientset creates a fake Kubernetes clientset with optional initial objects.
// Useful for testing kubectl/client-go interactions.
func NewFakeK8sClientset(objects ...runtime.Object) *fake.Clientset {
	return fake.NewClientset(objects...)
}

// NewFakeControllerClient creates a fake controller-runtime client with the kagent scheme.
// Useful for testing reconcilers and CRD interactions.
func NewFakeControllerClient(t *testing.T, objects ...client.Object) client.Client {
	t.Helper()

	scheme := runtime.NewScheme()

	if err := v1alpha2.AddToScheme(scheme); err != nil {
		t.Fatalf("failed to add v1alpha2 to scheme: %v", err)
	}

	if err := corev1.AddToScheme(scheme); err != nil {
		t.Fatalf("failed to add corev1 to scheme: %v", err)
	}

	return fakeclient.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(objects...).
		Build()
}

// CreateTestAgent creates a test Agent resource for testing.
func CreateTestAgent(namespace, name string) *v1alpha2.Agent {
	return &v1alpha2.Agent{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: v1alpha2.AgentSpec{
			Type: v1alpha2.AgentType_Declarative,
			Declarative: &v1alpha2.DeclarativeAgentSpec{
				SystemMessage: "Test agent",
			},
		},
	}
}

// CreateTestNamespace creates a test Namespace resource for testing.
func CreateTestNamespace(name string) *corev1.Namespace {
	return &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
	}
}

// CreateTestSecret creates a test Secret resource for testing.
func CreateTestSecret(namespace, name string, data map[string]string) *corev1.Secret {
	secretData := make(map[string][]byte)
	for k, v := range data {
		secretData[k] = []byte(v)
	}

	return &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Data: secretData,
	}
}
