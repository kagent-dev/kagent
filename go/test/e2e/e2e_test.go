package e2e_test

import (
	"context"
	"fmt"
	"strings"
	"time"

	autogen_client "github.com/kagent-dev/kagent/go/autogen/client"
	"github.com/kagent-dev/kagent/go/cli/exported"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

const (
	GlobalUserID = "admin@kagent.dev"
	WSEndpoint   = "ws://localhost:8081/api/ws"
	APIEndpoint  = "http://localhost:8081/api"
	TestTimeout  = 5 * time.Minute
)

var _ = Describe("E2e", func() {
	// Initialize client once for all tests
	var client *autogen_client.Client

	BeforeEach(func() {
		// Initialize client before each test
		client = autogen_client.New(APIEndpoint, WSEndpoint)
	})

	// Helper function to create a session with an agent
	createAgentSession := func(agentLabel string) (*autogen_client.Session, *autogen_client.Team) {
		teams, err := client.ListTeams(GlobalUserID)
		Expect(err).NotTo(HaveOccurred())

		var agentTeam *autogen_client.Team
		for _, team := range teams {
			if team.Component.Label == agentLabel {
				agentTeam = team
				break
			}
		}
		Expect(agentTeam).NotTo(BeNil(), fmt.Sprintf("Agent with label %s not found", agentLabel))

		sess, err := client.CreateSession(&autogen_client.CreateSession{
			UserID: GlobalUserID,
			TeamID: agentTeam.Id,
			Name:   fmt.Sprintf("e2e-test-%s-%s", agentLabel, time.Now().String()),
		})
		Expect(err).NotTo(HaveOccurred())

		return sess, agentTeam
	}

	// Helper function to run an interactive session with an agent
	runAgentInteraction := func(agentLabel, prompt string) string {
		sess, agentTeam := createAgentSession(agentLabel)

		run, err := client.CreateRun(&autogen_client.CreateRunRequest{
			SessionID: sess.ID,
			UserID:    GlobalUserID,
		})
		Expect(err).NotTo(HaveOccurred())

		wsClient, err := exported.NewWebsocketClient(WSEndpoint, run.ID, exported.DefaultConfig)
		Expect(err).NotTo(HaveOccurred())

		testShell := &TestShell{
			TerminationText: "Operation completed",
			Done:            make(chan struct{}),
		}

		ctx, cancel := context.WithTimeout(context.Background(), TestTimeout)
		defer cancel()

		go func() {
			defer GinkgoRecover()
			err := wsClient.StartInteractive(
				ctx,
				testShell,
				agentTeam,
				prompt+`\nWhen you are finished, end your reply with "Operation completed".`,
			)
			Expect(err).NotTo(HaveOccurred())
		}()

		select {
		case <-testShell.Done:
			// Success case
		case <-ctx.Done():
			Fail(fmt.Sprintf("Timed out waiting for %s agent to respond", agentLabel))
		}

		return testShell.OutputText
	}

	// Existing test
	It("lists cluster pods using the kube agent", func() {
		output := runAgentInteraction("k8s-agent",
			`List all the pods in the cluster.`)

		// Verify key system pods exist in the response
		for _, expectedSubstring := range []string{
			"kagent-",
			"coredns-",
			"etcd-kagent-control-plane",
			"kindnet-",
			"kube-apiserver-kagent-control-plane",
			"kube-controller-manager-kagent-control-plane",
			"kube-proxy-",
			"kube-scheduler-kagent-control-plane",
			"local-path-provisioner-",
		} {
			Expect(output).To(ContainSubstring(expectedSubstring))
		}
	})

	// Kubernetes Agent Test
	FIt("performs basic kubernetes operations using the k8s agent", func() {
		// Create a test namespace
		output := runAgentInteraction("k8s-agent",
			`Create a namespace called "e2e-test-namespace"`)
		Expect(output).To(ContainSubstring("namespace/e2e-test-namespace created"))
		Expect(output).To(ContainSubstring("Operation completed"))

		// Deploy a simple nginx pod
		output = runAgentInteraction("k8s-agent",
			`Create a pod named "nginx-test" in the "e2e-test-namespace" namespace using the nginx image. Add a label "app=nginx" to the pod`)
		Expect(output).To(ContainSubstring("pod/nginx-test created"))
		Expect(output).To(ContainSubstring("Operation completed"))

		// Verify the pod exists
		output = runAgentInteraction("k8s-agent",
			`List all pods in the "e2e-test-namespace" namespace with the label "app=nginx"`)
		Expect(output).To(ContainSubstring("nginx-test"))
		Expect(output).To(ContainSubstring("Operation completed"))

		// Clean up
		output = runAgentInteraction("k8s-agent",
			`Delete the namespace "e2e-test-namespace" and all its resources`)
		Expect(output).To(ContainSubstring("namespace/e2e-test-namespace deleted"))
		Expect(output).To(ContainSubstring("Operation completed"))
	})

	// Helm Agent Test
	It("manages helm repositories and deployments", func() {
		// Add bitnami repo
		output := runAgentInteraction("helm-agent",
			`Add the bitnami helm repository with URL "https://charts.bitnami.com/bitnami"`)
		Expect(output).To(ContainSubstring("bitnami has been added"))
		Expect(output).To(ContainSubstring("Operation completed"))

		// Update repositories
		output = runAgentInteraction("helm-agent",
			`Update the helm repositories`)
		Expect(output).To(ContainSubstring("successfully updated"))
		Expect(output).To(ContainSubstring("Operation completed"))

		// Create namespace for test
		output = runAgentInteraction("k8s-agent",
			`Create a namespace called "helm-test"`)
		Expect(output).To(ContainSubstring("namespace/helm-test created"))
		Expect(output).To(ContainSubstring("Operation completed"))

		// Install a simple chart
		output = runAgentInteraction("helm-agent",
			`Install the nginx chart from the bitnami repository in the "helm-test" namespace. Name the release "nginx-test". Set replicas to 1`)
		Expect(output).To(ContainSubstring("nginx-test has been installed"))
		Expect(output).To(ContainSubstring("Operation completed"))

		// Verify the release exists
		output = runAgentInteraction("helm-agent",
			`List all helm releases in the "helm-test" namespace`)
		Expect(output).To(ContainSubstring("nginx-test"))
		Expect(output).To(ContainSubstring("Operation completed"))

		// Clean up
		output = runAgentInteraction("helm-agent",
			`Uninstall the "nginx-test" release`)
		Expect(output).To(ContainSubstring("release \"nginx-test\" uninstalled"))
		Expect(output).To(ContainSubstring("Operation completed"))

		output = runAgentInteraction("k8s-agent",
			`Delete the namespace "helm-test"`)
		Expect(output).To(ContainSubstring("namespace/helm-test deleted"))
		Expect(output).To(ContainSubstring("Operation completed"))
	})

	// Istio Agent Test (full installation test)
	It("installs istio and configures resources", func() {
		// Create a namespace for istio testing
		output := runAgentInteraction("k8s-agent",
			`Create a namespace called "istio-test" with the label "istio-injection=enabled"`)
		Expect(output).To(ContainSubstring("namespace/istio-test created"))
		Expect(output).To(ContainSubstring("Operation completed"))

		// Install Istio (minimal profile for test purposes)
		output = runAgentInteraction("istio-agent",
			`Install Istio with the minimal profile`)
		Expect(output).To(ContainSubstring("successfully installed"))
		Expect(output).To(ContainSubstring("Operation completed"))

		// Verify Istio installation
		output = runAgentInteraction("istio-agent",
			`Check if Istio is properly installed by listing all pods in the istio-system namespace`)
		Expect(output).To(ContainSubstring("istiod"))
		Expect(output).To(ContainSubstring("Operation completed"))

		// Deploy a simple application
		output = runAgentInteraction("k8s-agent",
			`Deploy a basic nginx application in the "istio-test" namespace with 2 replicas. Name the deployment "nginx-istio-test"`)
		Expect(output).To(ContainSubstring("deployment.apps/nginx-istio-test created"))
		Expect(output).To(ContainSubstring("Operation completed"))

		// Create a service for the application
		output = runAgentInteraction("k8s-agent",
			`Create a service for the "nginx-istio-test" deployment in the "istio-test" namespace. The service should be of type ClusterIP and expose port 80. Name the service "nginx-service"`)
		Expect(output).To(ContainSubstring("service/nginx-service created"))
		Expect(output).To(ContainSubstring("Operation completed"))

		// Create a simple gateway and virtual service
		output = runAgentInteraction("istio-agent",
			`Create a gateway and virtual service for the nginx-service in the istio-test namespace. The gateway should listen on port 80 and the virtual service should route to the nginx-service`)
		Expect(output).To(ContainSubstring("gateway.networking.istio.io"))
		Expect(output).To(ContainSubstring("virtualservice.networking.istio.io"))
		Expect(output).To(ContainSubstring("Operation completed"))

		// We don't cleanup Istio as it may be needed for other tests
		// But we do cleanup the test namespace
		output = runAgentInteraction("k8s-agent",
			`Delete the namespace "istio-test" and all its resources`)
		Expect(output).To(ContainSubstring("namespace/istio-test deleted"))
		Expect(output).To(ContainSubstring("Operation completed"))
	})

	// Argo Rollouts Test
	It("converts deployments to argo rollouts", func() {
		// Setup: Create test namespace
		output := runAgentInteraction("k8s-agent",
			`Create a namespace called "argo-test"`)
		Expect(output).To(ContainSubstring("namespace/argo-test created"))
		Expect(output).To(ContainSubstring("Operation completed"))

		// Verify Argo Rollouts controller is installed
		output = runAgentInteraction("argo-rollouts-conversion-agent",
			`Verify that the Argo Rollouts controller is installed in the cluster. If it's not installed, end with "Argo Rollouts not installed". Otherwise, end with "Operation completed"`)

		// We conditionally proceed based on whether Argo Rollouts is installed
		if strings.Contains(output, "Argo Rollouts not installed") {
			Skip("Skipping test as Argo Rollouts controller is not installed")
		}

		// Create a test deployment
		output = runAgentInteraction("k8s-agent",
			`Create a deployment named "test-app" in the "argo-test" namespace with image "nginx:1.19". Set replicas to 3 and add labels "app=test-app" and "version=v1"`)
		Expect(output).To(ContainSubstring("deployment.apps/test-app created"))
		Expect(output).To(ContainSubstring("Operation completed"))

		// Convert deployment to Argo Rollout
		output = runAgentInteraction("argo-rollouts-conversion-agent",
			`Convert the "test-app" deployment in the "argo-test" namespace to an Argo Rollout with a canary strategy. The canary strategy should have two steps: set 25% weight and then pause for manual promotion`)
		Expect(output).To(ContainSubstring("rollout.argoproj.io/test-app created"))
		Expect(output).To(ContainSubstring("Operation completed"))

		// Create another deployment for reference approach test
		output = runAgentInteraction("k8s-agent",
			`Create a deployment named "ref-app" in the "argo-test" namespace with image "nginx:1.19". Set replicas to 3 and add labels "app=ref-app" and "version=v1"`)
		Expect(output).To(ContainSubstring("deployment.apps/ref-app created"))
		Expect(output).To(ContainSubstring("Operation completed"))

		// Create Rollout that references existing deployment
		output = runAgentInteraction("argo-rollouts-conversion-agent",
			`Create a new rollout that references the existing "ref-app" deployment in the "argo-test" namespace instead of converting it. Use a canary strategy with 50% traffic weight and a 30 second pause`)
		Expect(output).To(ContainSubstring("rollout.argoproj.io"))
		Expect(output).To(ContainSubstring("workloadRef"))
		Expect(output).To(ContainSubstring("Operation completed"))

		// Clean up
		output = runAgentInteraction("k8s-agent",
			`Delete the namespace "argo-test" and all its resources`)
		Expect(output).To(ContainSubstring("namespace/argo-test deleted"))
		Expect(output).To(ContainSubstring("Operation completed"))
	})

	// Observability Agent Test (conditional on having Prometheus/Grafana)
	It("works with prometheus and grafana if available", func() {
		// Check if Prometheus is available
		output := runAgentInteraction("observability-agent",
			`Query Prometheus for the up metric to check if Prometheus is running. If Prometheus is not available, respond with "Prometheus not available". Otherwise, end with "Operation completed"`)

		if strings.Contains(output, "Prometheus not available") {
			Skip("Skipping test as Prometheus is not available")
		}

		// Query basic metrics
		output = runAgentInteraction("observability-agent",
			`Query Prometheus for CPU usage metrics for all pods in the default namespace. Show the results and end with "Operation completed"`)
		Expect(output).To(ContainSubstring("metric"))
		Expect(output).To(ContainSubstring("Operation completed"))

		// Check if Grafana is available
		output = runAgentInteraction("observability-agent",
			`Try to list available Grafana dashboards. If Grafana is not available, respond with "Grafana not available". Otherwise, end with "Operation completed"`)

		if strings.Contains(output, "Grafana not available") {
			Skip("Skipping test as Grafana is not available")
		}

		// Create a simple dashboard
		output = runAgentInteraction("observability-agent",
			`Create a simple Grafana dashboard showing memory and CPU usage for the cluster. Name the dashboard "E2E Test Dashboard"`)
		Expect(output).To(ContainSubstring("dashboard"))
		Expect(output).To(ContainSubstring("created"))
		Expect(output).To(ContainSubstring("Operation completed"))

		// Clean up dashboard
		output = runAgentInteraction("observability-agent",
			`Delete the "E2E Test Dashboard" Grafana dashboard`)
		Expect(output).To(ContainSubstring("deleted"))
		Expect(output).To(ContainSubstring("Operation completed"))
	})
})

// test shell simulates a user shell interface
type TestShell struct {
	InputText       []string
	OutputText      string
	TerminationText string
	Done            chan struct{}
	Finished        bool
}

func (t *TestShell) ReadLineErr() (string, error) {
	// pop the first element from the input text
	if len(t.InputText) == 0 {
		return "", nil
	}
	val := t.InputText[0]
	t.InputText = t.InputText[1:]
	return val, nil
}

func (t *TestShell) Println(val ...interface{}) {
	t.OutputText += fmt.Sprintln(val...)
}

func (t *TestShell) Printf(format string, val ...interface{}) {
	t.OutputText += fmt.Sprintf(format, val...)

	fmt.Println(t.OutputText)
	if strings.Contains(t.OutputText, t.TerminationText) && !t.Finished {
		t.Finished = true
		close(t.Done)
	}
}
