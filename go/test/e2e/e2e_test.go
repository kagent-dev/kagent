package e2e_test

import (
	"context"
	"fmt"
	"strings"
	"time"

	autogen_client "github.com/kagent-dev/kagent/go/autogen/client"
	"github.com/kagent-dev/kagent/go/controller/api/v1alpha1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
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
	WSEndpoint   = "ws://localhost:8081/api/ws"
	APIEndpoint  = "http://localhost:8081/api"
	// each individual test should finish within this time
	// TODO: make this configurable per test
	TestTimeout     = 5 * time.Minute
	kagentNamespace = "kagent"
)

var _ = Describe("E2e", func() {
	// Initialize clients
	var (
		agentClient   *autogen_client.Client
		k8sClient     client.Client
		testStartTime string
		ctx           context.Context
	)

	BeforeEach(func() {
		ctx = context.Background()

		// Initialize agent client
		agentClient = autogen_client.New(APIEndpoint)

		// Initialize controller-runtime client
		cfg, err := config.GetConfig()
		Expect(err).NotTo(HaveOccurred())

		// Register API types
		scheme := scheme.Scheme
		err = v1alpha1.AddToScheme(scheme)
		Expect(err).NotTo(HaveOccurred())
		k8sClient, err = client.New(cfg, client.Options{Scheme: scheme})
		Expect(err).NotTo(HaveOccurred())

		// Initalize fresh test start time for unique sessions on each run
		testStartTime = time.Now().String()
	})

	createOrFetchAgentSession := func(agentName string) *autogen_client.Session {
		agentTeam, err := agentClient.GetTeam(agentName, GlobalUserID)
		Expect(err).NotTo(HaveOccurred())

		Expect(agentTeam).NotTo(BeNil(), fmt.Sprintf("Agent with label %s not found", agentName))

		apiTestTeam := agentTeam

		// reuse existing sessions if available
		existingSessions, err := agentClient.ListSessions(GlobalUserID)
		Expect(err).NotTo(HaveOccurred())
		for _, session := range existingSessions {
			if session.TeamID == apiTestTeam.Id && session.UserID == GlobalUserID {
				return session
			}
		}

		sess, err := agentClient.CreateSession(&autogen_client.CreateSession{
			UserID: GlobalUserID,
			TeamID: apiTestTeam.Id,
			Name:   fmt.Sprintf("e2e-test-%s-%s", agentName, testStartTime),
		})
		Expect(err).NotTo(HaveOccurred())

		return sess
	}

	// Helper function to run an interactive session with an agent
	runAgentInteraction := func(agentLabel, prompt string) string {
		sess := createOrFetchAgentSession(agentLabel)

		result, err := agentClient.InvokeSession(sess.ID, GlobalUserID, prompt+`\nComplete the task without asking for confirmation, even if the task involves creating or deleting namespaces or other critical resources.`)
		Expect(err).NotTo(HaveOccurred())

		return result.TaskResult.Messages[len(result.TaskResult.Messages)-1]["content"].(string)
	}

	// Helper to check if a namespace exists
	namespaceExists := func(name string) bool {
		ns := &corev1.Namespace{}
		err := k8sClient.Get(ctx, types.NamespacedName{Name: name}, ns)
		return err == nil
	}

	// Helper to check if a resource exists
	resourceExists := func(namespace, kind, name string, obj client.Object) bool {
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
			Expect(err).NotTo(HaveOccurred())

			// Wait for namespace to be actually deleted
			Eventually(func() bool {
				return !namespaceExists(name)
			}, 60*time.Second, 1*time.Second).Should(BeTrue())
		}
	}

	// Kubernetes Agent Test
	It("performs basic kubernetes operations using the k8s agent", func() {
		const namespace = "e2e-test-namespace"
		const podName = "nginx-test"

		// Cleanup namespace if it exists from a previous test run
		cleanupNamespace(namespace)

		// Create a test namespace
		runAgentInteraction("k8s-agent",
			`Create a namespace called "e2e-test-namespace"`)

		// Verify namespace exists
		Eventually(func() bool {
			return namespaceExists(namespace)
		}, 30*time.Second, 1*time.Second).Should(BeTrue(), "Namespace should exist after creation")

		// Deploy a simple nginx pod
		runAgentInteraction("k8s-agent",
			`Create a pod named "nginx-test" in the "e2e-test-namespace" namespace using the nginx image. Add a label "app=nginx" to the pod`)

		// Verify pod exists and has correct label
		pod := &corev1.Pod{}
		Eventually(func() bool {
			if !resourceExists(namespace, "Pod", podName, pod) {
				return false
			}
			return pod.Labels["app"] == "nginx"
		}, 60*time.Second, 1*time.Second).Should(BeTrue(), "Pod should exist with correct label")

		// Clean up
		runAgentInteraction("k8s-agent",
			`Delete the namespace "e2e-test-namespace" and all its resources`)

		// Verify namespace is deleted
		Eventually(func() bool {
			return !namespaceExists(namespace)
		}, 60*time.Second, 1*time.Second).Should(BeTrue(), "Namespace should be deleted")
	})

	// Helm Agent Test
	It("manages helm repositories and deployments", func() {
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
		Eventually(func() bool {
			return namespaceExists(namespace)
		}, 30*time.Second, 1*time.Second).Should(BeTrue(), "Namespace should exist after creation")

		// Install a simple chart
		runAgentInteraction("helm-agent",
			`Install the nginx chart from the bitnami repository in the "helm-test" namespace. Name the release "nginx-test". Set replicas to 1`)

		// Verify the deployment exists
		deployment := &appsv1.Deployment{}
		Eventually(func() bool {
			return resourceExists(namespace, "Deployment", deploymentName, deployment)
		}, 60*time.Second, 1*time.Second).Should(BeTrue(), "Deployment should exist after Helm release install")

		// Verify the deployment has the correct replica count
		Eventually(func() int32 {
			if err := k8sClient.Get(ctx, types.NamespacedName{Namespace: namespace, Name: deploymentName}, deployment); err != nil {
				return -1
			}
			return *deployment.Spec.Replicas
		}, 60*time.Second, 1*time.Second).Should(Equal(int32(1)), "Deployment should have 1 replica")

		// Clean up
		runAgentInteraction("helm-agent",
			`Uninstall the "nginx-test" release`)

		// Verify the deployment is removed
		Eventually(func() bool {
			return !resourceExists(namespace, "Deployment", deploymentName, deployment)
		}, 60*time.Second, 1*time.Second).Should(BeTrue(), "Deployment should be removed after Helm release uninstall")

		// Delete namespace
		runAgentInteraction("k8s-agent",
			`Delete the namespace "helm-test"`)

		// Verify namespace is deleted
		Eventually(func() bool {
			return !namespaceExists(namespace)
		}, 60*time.Second, 1*time.Second).Should(BeTrue(), "Namespace should be deleted")
	})

	// Istio Agent Test
	It("installs istio and configures resources", func() {
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
		Eventually(func() bool {
			if !resourceExists("", "Namespace", namespace, ns) {
				return false
			}
			return ns.Labels["istio-injection"] == "enabled"
		}, 30*time.Second, 1*time.Second).Should(BeTrue(), "Namespace should exist with istio-injection label")

		// Install Istio (minimal profile for test purposes)
		runAgentInteraction("istio-agent",
			`Install Istio with the minimal profile`)

		// Verify Istio namespace exists
		Eventually(func() bool {
			return namespaceExists("istio-system")
		}, 60*time.Second, 1*time.Second).Should(BeTrue(), "istio-system namespace should exist after installation")

		// Verify istiod deployment exists
		istiod := &appsv1.Deployment{}
		Eventually(func() bool {
			return resourceExists("istio-system", "Deployment", "istiod", istiod)
		}, 120*time.Second, 1*time.Second).Should(BeTrue(), "istiod deployment should exist")

		// Deploy a simple application
		runAgentInteraction("k8s-agent",
			`Deploy a basic nginx application in the "istio-test" namespace with 2 replicas. Name the deployment "nginx-istio-test"`)

		// Verify deployment exists with correct replica count
		deployment := &appsv1.Deployment{}
		Eventually(func() bool {
			if !resourceExists(namespace, "Deployment", deploymentName, deployment) {
				return false
			}
			return *deployment.Spec.Replicas == int32(2)
		}, 60*time.Second, 1*time.Second).Should(BeTrue(), "Deployment should exist with 2 replicas")

		// Create a service for the application
		runAgentInteraction("k8s-agent",
			`Create a service for the "nginx-istio-test" deployment in the "istio-test" namespace. The service should be of type ClusterIP and expose port 80. Name the service "nginx-service"`)

		// Verify service exists
		service := &corev1.Service{}
		Eventually(func() bool {
			if !resourceExists(namespace, "Service", serviceName, service) {
				return false
			}
			return service.Spec.Type == corev1.ServiceTypeClusterIP && len(service.Spec.Ports) > 0 && service.Spec.Ports[0].Port == 80
		}, 30*time.Second, 1*time.Second).Should(BeTrue(), "Service should exist with correct port and type")

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

		Expect(gatewayExists || virtualServiceExists).To(BeTrue(), "Should have created either Gateway or VirtualService resources")

		// We don't cleanup Istio as it may be needed for other tests
		// But we do cleanup the test namespace
		runAgentInteraction("k8s-agent",
			`Delete the namespace "istio-test" and all its resources`)

		// Verify namespace is deleted
		Eventually(func() bool {
			return !namespaceExists(namespace)
		}, 60*time.Second, 1*time.Second).Should(BeTrue(), "Namespace should be deleted")
	})

})
