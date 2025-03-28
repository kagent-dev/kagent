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
					{BuiltinTool: v1alpha1.BuiltinTool{Provider: "kagent.tools.k8s.AnnotateResource"}},
					{BuiltinTool: v1alpha1.BuiltinTool{Provider: "kagent.tools.k8s.ApplyManifest"}},
					{BuiltinTool: v1alpha1.BuiltinTool{Provider: "kagent.tools.k8s.CheckServiceConnectivity"}},
					{BuiltinTool: v1alpha1.BuiltinTool{Provider: "kagent.tools.k8s.CreateResource"}},
					{BuiltinTool: v1alpha1.BuiltinTool{Provider: "kagent.tools.k8s.DeleteResource"}},
					{BuiltinTool: v1alpha1.BuiltinTool{Provider: "kagent.tools.k8s.DescribeResource"}},
					{BuiltinTool: v1alpha1.BuiltinTool{Provider: "kagent.tools.k8s.ExecuteCommand"}},
					{BuiltinTool: v1alpha1.BuiltinTool{Provider: "kagent.tools.k8s.GetAvailableAPIResources"}},
					{BuiltinTool: v1alpha1.BuiltinTool{Provider: "kagent.tools.k8s.GetClusterConfiguration"}},
					{BuiltinTool: v1alpha1.BuiltinTool{Provider: "kagent.tools.k8s.GetEvents"}},
					{BuiltinTool: v1alpha1.BuiltinTool{Provider: "kagent.tools.k8s.GetPodLogs"}},
					{BuiltinTool: v1alpha1.BuiltinTool{Provider: "kagent.tools.k8s.GetResources"}},
					{BuiltinTool: v1alpha1.BuiltinTool{Provider: "kagent.tools.k8s.GetResourceYAML"}},
					{BuiltinTool: v1alpha1.BuiltinTool{Provider: "kagent.tools.k8s.LabelResource"}},
					{BuiltinTool: v1alpha1.BuiltinTool{Provider: "kagent.tools.k8s.PatchResource"}},
					{BuiltinTool: v1alpha1.BuiltinTool{Provider: "kagent.tools.k8s.RemoveAnnotation"}},
					{BuiltinTool: v1alpha1.BuiltinTool{Provider: "kagent.tools.k8s.RemoveLabel"}},
					{BuiltinTool: v1alpha1.BuiltinTool{Provider: "kagent.tools.k8s.Rollout"}},
					{BuiltinTool: v1alpha1.BuiltinTool{Provider: "kagent.tools.k8s.Scale"}},
					{BuiltinTool: v1alpha1.BuiltinTool{Provider: "kagent.tools.k8s.GenerateResourceTool"}},
					{BuiltinTool: v1alpha1.BuiltinTool{Provider: "kagent.tools.k8s.GenerateResourceToolConfig"}},
					{BuiltinTool: v1alpha1.BuiltinTool{Provider: "kagent.tools.istio.ZTunnelConfig"}},
					{BuiltinTool: v1alpha1.BuiltinTool{Provider: "kagent.tools.istio.WaypointStatus"}},
					{BuiltinTool: v1alpha1.BuiltinTool{Provider: "kagent.tools.istio.ListWaypoints"}},
					{BuiltinTool: v1alpha1.BuiltinTool{Provider: "kagent.tools.istio.GenerateWaypoint"}},
					{BuiltinTool: v1alpha1.BuiltinTool{Provider: "kagent.tools.istio.DeleteWaypoint"}},
					{BuiltinTool: v1alpha1.BuiltinTool{Provider: "kagent.tools.istio.ApplyWaypoint"}},
					{BuiltinTool: v1alpha1.BuiltinTool{Provider: "kagent.tools.istio.RemoteClusters"}},
					{BuiltinTool: v1alpha1.BuiltinTool{Provider: "kagent.tools.istio.ProxyStatus"}},
					{BuiltinTool: v1alpha1.BuiltinTool{Provider: "kagent.tools.istio.GenerateManifest"}},
					{BuiltinTool: v1alpha1.BuiltinTool{Provider: "kagent.tools.istio.Install"}},
					{BuiltinTool: v1alpha1.BuiltinTool{Provider: "kagent.tools.istio.AnalyzeClusterConfig"}},
					{BuiltinTool: v1alpha1.BuiltinTool{Provider: "kagent.tools.istio.ProxyConfig"}},
					// tools with config
					{BuiltinTool: v1alpha1.BuiltinTool{Provider: "kagent.tools.docs.QueryTool",
						Config: map[string]v1alpha1.AnyType{
							"docs_download_url": {
								RawMessage: makeRawMsg("https://doc-sqlite-db.s3.sa-east-1.amazonaws.com"),
							},
						},
					}},
				},
			},
		}

		toolServer := &v1alpha1.ToolServer{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "asdf",
				Namespace: namespace,
			},
			TypeMeta: metav1.TypeMeta{
				Kind:       "ToolServer",
				APIVersion: "kagent.dev/v1alpha1",
			},
			Spec: v1alpha1.ToolServerSpec{
				Description: "a t",
				Config: v1alpha1.ToolServerConfig{
					//Stdio: &v1alpha1.StdioMcpServerConfig{
					//	Command: "npx",
					//	Args: []string{
					//		"-y",
					//		"@modelcontextprotocol/server-everything",
					//	},
					//	Env:    nil,
					//	Stderr: "",
					//	Cwd:    "",
					//},
					Sse: &v1alpha1.SseMcpServerConfig{
						URL: "https://www.mcp.run/api/mcp/sse?nonce=WrRYKc7jwXSnlwalvjHlzA&username=ilackarms&profile=ilackarms%2Fdefault&sig=GvCWTGTiNh0I_ZqOCx7CeID0KEIVZJnWGpP58eXNUuw",
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
			toolServer,
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
