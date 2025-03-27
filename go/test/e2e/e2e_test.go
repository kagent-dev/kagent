package e2e_test

import (
	"encoding/json"
	"os"

	"sigs.k8s.io/yaml"

	"github.com/kagent-dev/kagent/go/controller/api/v1alpha1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	apikeySecretKey = "api-key"
)

var (
	openaiApiKey = os.Getenv("OPENAI_API_KEY")
)

var _ = Describe("E2e", func() {
	It("configures the agent and model", func() {
		// add a team
		namespace := "team-ns"

		// Create API Key Secret
		apiKeySecret := &v1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "openai-api-key-secret",
				Namespace: namespace,
			},
			TypeMeta: metav1.TypeMeta{
				Kind:       "Secret",
				APIVersion: "v1",
			},
			Data: map[string][]byte{
				apikeySecretKey: []byte(openaiApiKey),
			},
		}

		// Create ModelConfig
		modelConfig := &v1alpha1.ModelConfig{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "gpt-model-config",
				Namespace: namespace,
			},
			TypeMeta: metav1.TypeMeta{
				Kind:       "ModelConfig",
				APIVersion: "kagent.dev/v1alpha1",
			},
			Spec: v1alpha1.ModelConfigSpec{
				Model:            "gpt-4o",
				Provider:         v1alpha1.OpenAI,
				APIKeySecretName: apiKeySecret.Name,
				APIKeySecretKey:  apikeySecretKey,
				ProviderOpenAI: &v1alpha1.OpenAIConfig{
					Temperature: "0.7",
					MaxTokens:   ptrToInt(2048),
					TopP:        "0.95",
				},
			},
		}

		// Agent with required ModelConfigRef
		kubeExpert := &v1alpha1.Agent{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "kube-expert",
				Namespace: namespace,
			},
			TypeMeta: metav1.TypeMeta{
				Kind:       "Agent",
				APIVersion: "kagent.dev/v1alpha1",
			},
			Spec: v1alpha1.AgentSpec{
				Description:    "The Kubernetes Expert AI Agent specializing in cluster operations, troubleshooting, and maintenance.",
				SystemMessage:  readFileAsString("systemprompts/kube-expert-system-prompt.txt"),
				ModelConfigRef: modelConfig.Name, // Added required ModelConfigRef
				Tools: []*v1alpha1.Tool{
					{Provider: "kagent.tools.k8s.AnnotateResource"},
					{Provider: "kagent.tools.k8s.ApplyManifest"},
					{Provider: "kagent.tools.k8s.CheckServiceConnectivity"},
					{Provider: "kagent.tools.k8s.CreateResource"},
					{Provider: "kagent.tools.k8s.DeleteResource"},
					{Provider: "kagent.tools.k8s.DescribeResource"},
					{Provider: "kagent.tools.k8s.ExecuteCommand"},
					{Provider: "kagent.tools.k8s.GetAvailableAPIResources"},
					{Provider: "kagent.tools.k8s.GetClusterConfiguration"},
					{Provider: "kagent.tools.k8s.GetEvents"},
					{Provider: "kagent.tools.k8s.GetPodLogs"},
					{Provider: "kagent.tools.k8s.GetResources"},
					{Provider: "kagent.tools.k8s.GetResourceYAML"},
					{Provider: "kagent.tools.k8s.LabelResource"},
					{Provider: "kagent.tools.k8s.PatchResource"},
					{Provider: "kagent.tools.k8s.RemoveAnnotation"},
					{Provider: "kagent.tools.k8s.RemoveLabel"},
					{Provider: "kagent.tools.k8s.Rollout"},
					{Provider: "kagent.tools.k8s.Scale"},
					{Provider: "kagent.tools.k8s.GenerateResourceTool"},
					{Provider: "kagent.tools.k8s.GenerateResourceToolConfig"},
					{Provider: "kagent.tools.istio.ZTunnelConfig"},
					{Provider: "kagent.tools.istio.WaypointStatus"},
					{Provider: "kagent.tools.istio.ListWaypoints"},
					{Provider: "kagent.tools.istio.GenerateWaypoint"},
					{Provider: "kagent.tools.istio.DeleteWaypoint"},
					{Provider: "kagent.tools.istio.ApplyWaypoint"},
					{Provider: "kagent.tools.istio.RemoteClusters"},
					{Provider: "kagent.tools.istio.ProxyStatus"},
					{Provider: "kagent.tools.istio.GenerateManifest"},
					{Provider: "kagent.tools.istio.Install"},
					{Provider: "kagent.tools.istio.AnalyzeClusterConfig"},
					{Provider: "kagent.tools.istio.ProxyConfig"},
					// tools with config
					{
						Provider: "kagent.tools.docs.QueryTool",
						Config: map[string]v1alpha1.AnyType{
							"docs_download_url": {
								RawMessage: makeRawMsg("https://doc-sqlite-db.s3.sa-east-1.amazonaws.com"),
							},
						},
					},
				},
			},
		}

		// Write Secret
		writeKubeObjects(
			"manifests/api-key-secret.yaml",
			apiKeySecret,
		)

		// Write ModelConfig
		writeKubeObjects(
			"manifests/gpt-model-config.yaml",
			modelConfig,
		)

		// Write Agent
		writeKubeObjects(
			"manifests/kube-expert-agent.yaml",
			kubeExpert,
		)
	})
})

func makeRawMsg(v interface{}) json.RawMessage {
	data, err := json.Marshal(v)
	Expect(err).NotTo(HaveOccurred())
	return data
}

func writeKubeObjects(file string, objects ...metav1.Object) {
	var bytes []byte
	for _, obj := range objects {
		data, err := yaml.Marshal(obj)
		Expect(err).NotTo(HaveOccurred())
		bytes = append(bytes, data...)
		bytes = append(bytes, []byte("---\n")...)
	}

	err := os.WriteFile(file, bytes, 0644)
	Expect(err).NotTo(HaveOccurred())
}

func ptrToInt(v int) *int {
	return &v
}

func readFileAsString(path string) string {
	bytes, err := os.ReadFile(path)
	Expect(err).NotTo(HaveOccurred())
	return string(bytes)
}
