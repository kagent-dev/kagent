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
	apikeySecretKey = "apikey"
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
				apikeySecretKey: []byte("fake"),
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
				Model:           "gpt-4o",
				Provider:        v1alpha1.OpenAI,
				APIKeySecretRef: apiKeySecret.Name,
				APIKeySecretKey: apikeySecretKey,
				OpenAI: &v1alpha1.OpenAIConfig{
					Temperature: "0.7",
					MaxTokens:   2048,
					TopP:        "0.95",
				},
			},
		}

		// Agent with required ModelConfig
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
				Description:   "The Kubernetes Expert AI Agent specializing in cluster operations, troubleshooting, and maintenance.",
				SystemMessage: readFileAsString("systemprompts/kube-expert-system-prompt.txt"),
				ModelConfig:   modelConfig.Name, // Added required ModelConfig
				Tools: []*v1alpha1.Tool{
					{Builtin: &v1alpha1.BuiltinTool{Name: "k8s_AnnotateResource"}},
					{Builtin: &v1alpha1.BuiltinTool{Name: "k8s_ApplyManifest"}},
					{Builtin: &v1alpha1.BuiltinTool{Name: "k8s_CheckServiceConnectivity"}},
					{Builtin: &v1alpha1.BuiltinTool{Name: "k8s_CreateResource"}},
					{Builtin: &v1alpha1.BuiltinTool{Name: "k8s_DeleteResource"}},
					{Builtin: &v1alpha1.BuiltinTool{Name: "k8s_DescribeResource"}},
					{Builtin: &v1alpha1.BuiltinTool{Name: "k8s_ExecuteCommand"}},
					{Builtin: &v1alpha1.BuiltinTool{Name: "k8s_GetAvailableAPIResources"}},
					{Builtin: &v1alpha1.BuiltinTool{Name: "k8s_GetClusterConfiguration"}},
					{Builtin: &v1alpha1.BuiltinTool{Name: "k8s_GetEvents"}},
					{Builtin: &v1alpha1.BuiltinTool{Name: "k8s_GetPodLogs"}},
					{Builtin: &v1alpha1.BuiltinTool{Name: "k8s_GetResources"}},
					{Builtin: &v1alpha1.BuiltinTool{Name: "k8s_GetResourceYAML"}},
					{Builtin: &v1alpha1.BuiltinTool{Name: "k8s_LabelResource"}},
					{Builtin: &v1alpha1.BuiltinTool{Name: "k8s_PatchResource"}},
					{Builtin: &v1alpha1.BuiltinTool{Name: "k8s_RemoveAnnotation"}},
					{Builtin: &v1alpha1.BuiltinTool{Name: "k8s_RemoveLabel"}},
					{Builtin: &v1alpha1.BuiltinTool{Name: "k8s_Rollout"}},
					{Builtin: &v1alpha1.BuiltinTool{Name: "k8s_Scale"}},
					{Builtin: &v1alpha1.BuiltinTool{Name: "k8s_GenerateResourceTool"}},
					{Builtin: &v1alpha1.BuiltinTool{Name: "k8s_GenerateResourceToolConfig"}},
					{Builtin: &v1alpha1.BuiltinTool{Name: "istio_ZTunnelConfig"}},
					{Builtin: &v1alpha1.BuiltinTool{Name: "istio_WaypointStatus"}},
					{Builtin: &v1alpha1.BuiltinTool{Name: "istio_ListWaypoints"}},
					{Builtin: &v1alpha1.BuiltinTool{Name: "istio_GenerateWaypoint"}},
					{Builtin: &v1alpha1.BuiltinTool{Name: "istio_DeleteWaypoint"}},
					{Builtin: &v1alpha1.BuiltinTool{Name: "istio_ApplyWaypoint"}},
					{Builtin: &v1alpha1.BuiltinTool{Name: "istio_RemoteClusters"}},
					{Builtin: &v1alpha1.BuiltinTool{Name: "istio_ProxyStatus"}},
					{Builtin: &v1alpha1.BuiltinTool{Name: "istio_GenerateManifest"}},
					{Builtin: &v1alpha1.BuiltinTool{Name: "istio_Install"}},
					{Builtin: &v1alpha1.BuiltinTool{Name: "istio_AnalyzeClusterConfig"}},
					{Builtin: &v1alpha1.BuiltinTool{Name: "istio_ProxyConfig"}},
					// tools with config
					{Builtin: &v1alpha1.BuiltinTool{Name: "docs_QueryTool",
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
