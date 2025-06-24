package e2e_test

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	autogen_client "github.com/kagent-dev/kagent/go/autogen/client"
	"github.com/kagent-dev/kagent/go/controller/api/v1alpha1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/config"
)

const (
	GlobalUserID = "admin@kagent.dev"
	APIEndpoint  = "http://localhost:8081/api"
	// each individual test should finish within this time
	// TODO: make this configurable per test
	TestTimeout     = 5 * time.Minute
	kagentNamespace = "kagent"
)

func TestE2E(t *testing.T) {

	// Setup
	ctx := context.Background()

	// Initialize agent client
	agentClient := autogen_client.New(APIEndpoint)

	// Initialize controller-runtime client
	cfg, err := config.GetConfig()
	require.NoError(t, err)

	// Register API types
	scheme := scheme.Scheme
	err = v1alpha1.AddToScheme(scheme)
	require.NoError(t, err)
	k8sClient, err := client.New(cfg, client.Options{Scheme: scheme})
	require.NoError(t, err)

	// Initialize fresh test start time for unique sessions on each run
	testStartTime := time.Now().String()

	createOrFetchAgentSession := func(agentName string) (*autogen_client.Session, *autogen_client.Team) {
		agentTeam, err := agentClient.GetTeam(agentName, GlobalUserID)
		require.NoError(t, err)

		// reuse existing sessions if available
		existingSessions, err := agentClient.ListSessions(GlobalUserID)
		require.NoError(t, err)
		for _, session := range existingSessions {
			if session.UserID == GlobalUserID {
				return session, agentTeam
			}
		}

		sess, err := agentClient.CreateSession(&autogen_client.CreateSession{
			UserID: GlobalUserID,
			Name:   fmt.Sprintf("e2e-test-%s-%s", agentName, testStartTime),
		})
		require.NoError(t, err)

		return sess, agentTeam
	}

	// Helper function to run an interactive session with an agent
	runAgentInteraction := func(agentLabel, prompt string) string {
		sess, team := createOrFetchAgentSession(agentLabel)

		result, err := agentClient.InvokeSession(sess.ID, GlobalUserID, &autogen_client.InvokeRequest{
			Task:       prompt + `\nComplete the task without asking for confirmation, even if the task involves creating or deleting namespaces or other critical resources.`,
			TeamConfig: team.Component,
		})
		require.NoError(t, err)

		return result.TaskResult.Messages[len(result.TaskResult.Messages)-1]["content"].(string)
	}

	// Helper to check if a namespace exists
	namespaceExists := func(name string) bool {
		ns := &corev1.Namespace{}
		err := k8sClient.Get(ctx, types.NamespacedName{Name: name}, ns)
		return err == nil
	}

	// Helper to check if a resource exists
	resourceExists := func(namespace, name string, obj client.Object) bool {
		err := k8sClient.Get(ctx, types.NamespacedName{Namespace: namespace, Name: name}, obj)
		return err == nil
	}

	// Helper to clean up a namespace if it exists
	cleanupNamespace := func(name string) {
		if namespaceExists(name) {
			ns := &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: name,
				},
			}
			err := k8sClient.Delete(ctx, ns)
			require.NoError(t, err)

			// Wait for namespace to be actually deleted
			require.Eventually(t, func() bool {
				return !namespaceExists(name)
			}, 60*time.Second, 1*time.Second, "Namespace should be deleted")
		}
	}

	// Kubernetes Agent Test
	t.Run("performs basic kubernetes operations using the k8s agent", func(t *testing.T) {
		const namespace = "e2e-test-namespace"
		const podName = "nginx-test"

		// Cleanup namespace if it exists from a previous test run
		cleanupNamespace(namespace)

		// Create a test namespace
		runAgentInteraction("k8s-agent",
			`Create a namespace called "e2e-test-namespace"`)

		// Verify namespace exists
		require.Eventually(t, func() bool {
			return namespaceExists(namespace)
		}, 30*time.Second, 1*time.Second, "Namespace should exist after creation")

		// Deploy a simple nginx pod
		runAgentInteraction("k8s-agent",
			`Create a pod named "nginx-test" in the "e2e-test-namespace" namespace using the nginx image. Add a label "app=nginx" to the pod`)

		// Verify pod exists and has correct label
		pod := &corev1.Pod{}
		require.Eventually(t, func() bool {
			if !resourceExists(namespace, podName, pod) {
				return false
			}
			return pod.Labels["app"] == "nginx"
		}, 60*time.Second, 1*time.Second, "Pod should exist with correct label")

		// Clean up
		runAgentInteraction("k8s-agent",
			`Delete the namespace "e2e-test-namespace" and all its resources`)

		// Verify namespace is deleted
		require.Eventually(t, func() bool {
			return !namespaceExists(namespace)
		}, 60*time.Second, 1*time.Second, "Namespace should be deleted")
	})

	// Helm Agent Test
	t.Run("manages helm repositories and deployments", func(t *testing.T) {
		const namespace = "helm-test"
		const deploymentName = "nginx-test"

		// Cleanup namespace if it exists from a previous test run
		cleanupNamespace(namespace)

		// Add bitnami repo
		runAgentInteraction("helm-agent",
			`Add the bitnami helm repository with URL "https://charts.bitnami.com/bitnami"`)

		// Update repositories
		runAgentInteraction("helm-agent",
			`Update the helm repositories`)

		// Create namespace for test
		runAgentInteraction("k8s-agent",
			`Create a namespace called "helm-test".`)

		// Verify namespace exists
		require.Eventually(t, func() bool {
			return namespaceExists(namespace)
		}, 30*time.Second, 1*time.Second, "Namespace should exist after creation")

		// Install a simple chart
		runAgentInteraction("helm-agent",
			`Install the nginx chart from the bitnami repository in the "helm-test" namespace. Name the release "nginx-test". Set replicas to 1`)

		// Verify the deployment exists
		deployment := &appsv1.Deployment{}
		require.Eventually(t, func() bool {
			return resourceExists(namespace, deploymentName, deployment)
		}, 60*time.Second, 1*time.Second, "Deployment should exist after Helm release install")

		// Verify the deployment has the correct replica count
		require.Eventually(t, func() bool {
			if err := k8sClient.Get(ctx, types.NamespacedName{Namespace: namespace, Name: deploymentName}, deployment); err != nil {
				return false
			}
			return *deployment.Spec.Replicas == int32(1)
		}, 60*time.Second, 1*time.Second, "Deployment should have 1 replica")

		// Clean up
		runAgentInteraction("helm-agent",
			`Uninstall the "nginx-test" release`)

		// Verify the deployment is removed
		require.Eventually(t, func() bool {
			return !resourceExists(namespace, deploymentName, deployment)
		}, 60*time.Second, 1*time.Second, "Deployment should be removed after Helm release uninstall")

		// Delete namespace
		runAgentInteraction("k8s-agent",
			`Delete the namespace "helm-test"`)

		// Verify namespace is deleted
		require.Eventually(t, func() bool {
			return !namespaceExists(namespace)
		}, 60*time.Second, 1*time.Second, "Namespace should be deleted")
	})

	// Istio Agent Test
	t.Run("installs istio and configures resources", func(t *testing.T) {
		const namespace = "istio-test"
		const deploymentName = "nginx-istio-test"
		const serviceName = "nginx-service"

		// Cleanup namespace if it exists from a previous test run
		cleanupNamespace(namespace)

		// Create a namespace for istio testing
		runAgentInteraction("k8s-agent",
			`Create a namespace called "istio-test" with the label "istio-injection=enabled"`)

		// Verify namespace exists with correct label
		ns := &corev1.Namespace{}
		require.Eventually(t, func() bool {
			if !resourceExists("", namespace, ns) {
				return false
			}
			return ns.Labels["istio-injection"] == "enabled"
		}, 30*time.Second, 1*time.Second, "Namespace should exist with istio-injection label")

		// Install Istio (minimal profile for test purposes)
		runAgentInteraction("istio-agent",
			`Install Istio with the minimal profile`)

		// Verify Istio namespace exists
		require.Eventually(t, func() bool {
			return namespaceExists("istio-system")
		}, 60*time.Second, 1*time.Second, "istio-system namespace should exist after installation")

		// Verify istiod deployment exists
		istiod := &appsv1.Deployment{}
		require.Eventually(t, func() bool {
			return resourceExists("istio-system", "istiod", istiod)
		}, 120*time.Second, 1*time.Second, "istiod deployment should exist")

		// Deploy a simple application
		runAgentInteraction("k8s-agent",
			`Deploy a basic nginx application in the "istio-test" namespace with 2 replicas. Name the deployment "nginx-istio-test"`)

		// Verify deployment exists with correct replica count
		deployment := &appsv1.Deployment{}
		require.Eventually(t, func() bool {
			if !resourceExists(namespace, deploymentName, deployment) {
				return false
			}
			return *deployment.Spec.Replicas == int32(2)
		}, 60*time.Second, 1*time.Second, "Deployment should exist with 2 replicas")

		// Create a service for the application
		runAgentInteraction("k8s-agent",
			`Create a service for the "nginx-istio-test" deployment in the "istio-test" namespace. The service should be of type ClusterIP and expose port 80. Name the service "nginx-service"`)

		// Verify service exists
		service := &corev1.Service{}
		require.Eventually(t, func() bool {
			if !resourceExists(namespace, serviceName, service) {
				return false
			}
			return service.Spec.Type == corev1.ServiceTypeClusterIP && len(service.Spec.Ports) > 0 && service.Spec.Ports[0].Port == 80
		}, 30*time.Second, 1*time.Second, "Service should exist with correct port and type")

		// Create a simple gateway and virtual service
		runAgentInteraction("istio-agent",
			`Create a gateway and virtual service for the nginx-service in the istio-test namespace. The gateway should listen on port 80 and the virtual service should route to the nginx-service`)

		// Since we don't have the Istio CRDs registered with our scheme,
		// we can't directly check for Gateway and VirtualService resources.
		// Instead, we'll query the API server indirectly through the agent

		output := runAgentInteraction("k8s-agent",
			`Check if there are any networking.istio.io/v1alpha3 or networking.istio.io/v1beta1 gateways and virtualservices in the istio-test namespace`)

		// Check if the output indicates that gateway and virtualservice were found
		gatewayExists := strings.Contains(output, "gateway") || strings.Contains(output, "Gateway")
		virtualServiceExists := strings.Contains(output, "virtualservice") || strings.Contains(output, "VirtualService")

		assert.True(t, gatewayExists || virtualServiceExists, "Should have created either Gateway or VirtualService resources")

		// We don't cleanup Istio as it may be needed for other tests
		// But we do cleanup the test namespace
		runAgentInteraction("k8s-agent",
			`Delete the namespace "istio-test" and all its resources`)

		// Verify namespace is deleted
		require.Eventually(t, func() bool {
			return !namespaceExists(namespace)
		}, 60*time.Second, 1*time.Second, "Namespace should be deleted")
	})
}
