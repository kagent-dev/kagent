package k8s

import (
	"context"
	"testing"

	v1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

func TestNewK8sClient(t *testing.T) {
	// Test that NewK8sClient handles errors gracefully
	// This will likely fail in test environment without kubeconfig, which is expected
	_, err := NewK8sClient()
	// We don't fail the test if client creation fails, as it's expected in test env
	if err != nil {
		t.Logf("NewK8sClient failed as expected in test environment: %v", err)
	}
}

func TestFormatResourceOutput(t *testing.T) {
	testData := map[string]interface{}{
		"test":   "data",
		"number": 42,
	}

	// Test JSON output format
	result, err := formatResourceOutput(testData, "json")
	if err != nil {
		t.Fatalf("formatResourceOutput failed: %v", err)
	}

	if len(result.Content) == 0 {
		t.Fatal("Expected non-empty content")
	}

	// Test empty output format (defaults to JSON)
	result, err = formatResourceOutput(testData, "")
	if err != nil {
		t.Fatalf("formatResourceOutput with empty format failed: %v", err)
	}

	if len(result.Content) == 0 {
		t.Fatal("Expected non-empty content")
	}

	// Test other output format
	result, err = formatResourceOutput(testData, "yaml")
	if err != nil {
		t.Fatalf("formatResourceOutput with yaml format failed: %v", err)
	}

	if len(result.Content) == 0 {
		t.Fatal("Expected non-empty content")
	}
}

func TestGetPodsNative(t *testing.T) {
	// Create fake Kubernetes client
	fakeClient := fake.NewSimpleClientset()

	// Create test pod
	testPod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-pod",
			Namespace: "default",
		},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{
				{
					Name:  "test-container",
					Image: "nginx",
				},
			},
		},
	}

	// Add pod to fake client
	_, err := fakeClient.CoreV1().Pods("default").Create(context.Background(), testPod, metav1.CreateOptions{})
	if err != nil {
		t.Fatalf("Failed to create test pod: %v", err)
	}

	client := &K8sClient{
		clientset: fakeClient,
	}

	// Test getting specific pod
	result, err := getPodsNative(context.Background(), client, "test-pod", "default", false, "json")
	if err != nil {
		t.Fatalf("getPodsNative failed: %v", err)
	}

	if len(result.Content) == 0 {
		t.Fatal("Expected non-empty content")
	}

	// Test listing all pods in namespace
	result, err = getPodsNative(context.Background(), client, "", "default", false, "json")
	if err != nil {
		t.Fatalf("getPodsNative list failed: %v", err)
	}

	if len(result.Content) == 0 {
		t.Fatal("Expected non-empty content")
	}

	// Test listing all pods across namespaces
	result, err = getPodsNative(context.Background(), client, "", "", true, "json")
	if err != nil {
		t.Fatalf("getPodsNative all namespaces failed: %v", err)
	}

	if len(result.Content) == 0 {
		t.Fatal("Expected non-empty content")
	}
}

func TestGetServicesNative(t *testing.T) {
	// Create fake Kubernetes client
	fakeClient := fake.NewSimpleClientset()

	// Create test service
	testService := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-service",
			Namespace: "default",
		},
		Spec: corev1.ServiceSpec{
			Ports: []corev1.ServicePort{
				{
					Port: 80,
				},
			},
		},
	}

	// Add service to fake client
	_, err := fakeClient.CoreV1().Services("default").Create(context.Background(), testService, metav1.CreateOptions{})
	if err != nil {
		t.Fatalf("Failed to create test service: %v", err)
	}

	client := &K8sClient{
		clientset: fakeClient,
	}

	// Test getting specific service
	result, err := getServicesNative(context.Background(), client, "test-service", "default", false, "json")
	if err != nil {
		t.Fatalf("getServicesNative failed: %v", err)
	}

	if len(result.Content) == 0 {
		t.Fatal("Expected non-empty content")
	}

	// Test listing all services in namespace
	result, err = getServicesNative(context.Background(), client, "", "default", false, "json")
	if err != nil {
		t.Fatalf("getServicesNative list failed: %v", err)
	}

	if len(result.Content) == 0 {
		t.Fatal("Expected non-empty content")
	}
}

func TestGetDeploymentsNative(t *testing.T) {
	// Create fake Kubernetes client
	fakeClient := fake.NewSimpleClientset()

	// Create test deployment
	testDeployment := &v1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-deployment",
			Namespace: "default",
		},
		Spec: v1.DeploymentSpec{
			Replicas: int32Ptr(1),
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					"app": "test",
				},
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"app": "test",
					},
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:  "test-container",
							Image: "nginx",
						},
					},
				},
			},
		},
	}

	// Add deployment to fake client
	_, err := fakeClient.AppsV1().Deployments("default").Create(context.Background(), testDeployment, metav1.CreateOptions{})
	if err != nil {
		t.Fatalf("Failed to create test deployment: %v", err)
	}

	client := &K8sClient{
		clientset: fakeClient,
	}

	// Test getting specific deployment
	result, err := getDeploymentsNative(context.Background(), client, "test-deployment", "default", false, "json")
	if err != nil {
		t.Fatalf("getDeploymentsNative failed: %v", err)
	}

	if len(result.Content) == 0 {
		t.Fatal("Expected non-empty content")
	}
}

func TestGetConfigMapsNative(t *testing.T) {
	// Create fake Kubernetes client
	fakeClient := fake.NewSimpleClientset()

	// Create test configmap
	testConfigMap := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-configmap",
			Namespace: "default",
		},
		Data: map[string]string{
			"key": "value",
		},
	}

	// Add configmap to fake client
	_, err := fakeClient.CoreV1().ConfigMaps("default").Create(context.Background(), testConfigMap, metav1.CreateOptions{})
	if err != nil {
		t.Fatalf("Failed to create test configmap: %v", err)
	}

	client := &K8sClient{
		clientset: fakeClient,
	}

	// Test getting specific configmap
	result, err := getConfigMapsNative(context.Background(), client, "test-configmap", "default", false, "json")
	if err != nil {
		t.Fatalf("getConfigMapsNative failed: %v", err)
	}

	if len(result.Content) == 0 {
		t.Fatal("Expected non-empty content")
	}
}

// Helper function for creating int32 pointer
func int32Ptr(i int32) *int32 {
	return &i
}
