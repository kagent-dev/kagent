package e2e_test

import (
	"encoding/json"
	"os"

	"sigs.k8s.io/yaml"

	"github.com/kagent-dev/kagent/go/controller/api/v1alpha1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	apikeySecretKey = "api-key"
)

var (
	openaiApiKey = os.Getenv("OPENAI_API_KEY")
)

var _ = Describe("E2e", func() {
	It("configures the agent", func() {

		// add a team
		namespace := "team-ns"

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
				Tools: []v1alpha1.Tool{
					{
						Provider: "kagent.tools.k8s.AnnotateResource",
					},
					{
						Provider: "kagent.tools.k8s.ApplyManifest",
					},
					{
						Provider: "kagent.tools.k8s.CheckServiceConnectivity",
					},
					{
						Provider: "kagent.tools.k8s.CreateResource",
					},
					{
						Provider: "kagent.tools.k8s.DeleteResource",
					},
					{
						Provider: "kagent.tools.k8s.DescribeResource",
					},
					{
						Provider: "kagent.tools.k8s.ExecuteCommand",
					},
					{
						Provider: "kagent.tools.k8s.GetAvailableAPIResources",
					},
					{
						Provider: "kagent.tools.k8s.GetClusterConfiguration",
					},
					{
						Provider: "kagent.tools.k8s.GetEvents",
					},
					{
						Provider: "kagent.tools.k8s.GetPodLogs",
					},
					{
						Provider: "kagent.tools.k8s.GetResources",
					},
					{
						Provider: "kagent.tools.k8s.GetResourceYAML",
					},
					{
						Provider: "kagent.tools.k8s.LabelResource",
					},
					{
						Provider: "kagent.tools.k8s.PatchResource",
					},
					{
						Provider: "kagent.tools.k8s.RemoveAnnotation",
					},
					{
						Provider: "kagent.tools.k8s.RemoveLabel",
					},
					{
						Provider: "kagent.tools.k8s.Rollout",
					},
					{
						Provider: "kagent.tools.k8s.Scale",
					},
					{
						Provider: "kagent.tools.k8s.GenerateResourceTool",
					},
					{
						Provider: "kagent.tools.k8s.GenerateResourceToolConfig",
					},
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

		writeKubeObjects(
			"manifests/kubeobjects.yaml",
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

func readFileAsString(path string) string {
	bytes, err := os.ReadFile(path)
	Expect(err).NotTo(HaveOccurred())
	return string(bytes)
}
