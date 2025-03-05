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

		//planningAgent := &v1alpha1.Agent{
		//	ObjectMeta: metav1.ObjectMeta{
		//		Name:      "planning-agent",
		//		Namespace: namespace,
		//	},
		//	TypeMeta: metav1.TypeMeta{
		//		Kind:       "Agent",
		//		APIVersion: "kagent.dev/v1alpha1",
		//	},
		//	Spec: v1alpha1.AgentSpec{
		//		Name:          "planning_agent",
		//		Description:   "The Planning Agent is responsible for planning and scheduling tasks. The planning agent is also responsible for deciding when the user task has been accomplished and terminating the conversation.",
		//		SystemMessage: readFileAsString("systemprompts/planning-agent-system-prompt.txt"),
		//	},
		//}

		//kubectlUser := &v1alpha1.Agent{
		//	ObjectMeta: metav1.ObjectMeta{
		//		Name:      "kubectl-user",
		//		Namespace: namespace,
		//	},
		//	TypeMeta: metav1.TypeMeta{
		//		Kind:       "Agent",
		//		APIVersion: "kagent.dev/v1alpha1",
		//	},
		//	Spec: v1alpha1.AgentSpec{
		//		Name:          "kubectl_execution_agent",
		//		Description:   "The Kubectl User is responsible for running kubectl commands corresponding to user requests.",
		//		SystemMessage: readFileAsString("systemprompts/kubectl-user-system-prompt.txt"),
		//		Tools: []v1alpha1.Tool{
		//			{
		//				Provider: string(v1alpha1.BuiltinTool_KubectlGetPods),
		//			},
		//			{
		//				Provider: string(v1alpha1.BuiltinTool_KubectlGetServices),
		//			},
		//			{
		//				Provider: string(v1alpha1.BuiltinTool_KubectlApplyManifest),
		//			},
		//			{
		//				Provider: string(v1alpha1.BuiltinTool_KubectlGetResources),
		//			},
		//			{
		//				Provider: string(v1alpha1.BuiltinTool_KubectlGetPodLogs),
		//			},
		//			{
		//				Provider: "kagent.tools.docs.QueryTool",
		//				Config: map[string]v1alpha1.AnyType{
		//					"docs_download_url": {
		//						RawMessage: makeRawMsg("https://doc-sqlite-db.s3.sa-east-1.amazonaws.com"),
		//					},
		//				},
		//			},
		//		},
		//	},
		//}

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

		//		apiTeam := &v1alpha1.Team{
		//			ObjectMeta: metav1.ObjectMeta{
		//				Name:      "kube-team",
		//				Namespace: namespace,
		//			},
		//			TypeMeta: metav1.TypeMeta{
		//				Kind:       "Team",
		//				APIVersion: "kagent.dev/v1alpha1",
		//			},
		//			Spec: v1alpha1.TeamSpec{
		//				Participants: []string{
		//					planningAgent.Name,
		//					kubectlUser.Name,
		//					kubeExpert.Name,
		//				},
		//				Description: "A team that debugs kubernetes issues.",
		//				//SelectorTeamConfig: &v1alpha1.SelectorTeamConfig{
		//				//	ModelConfig:    modelConfig.Name,
		//				//	SelectorPrompt: "Please select a team member to help you with your Kubernetes issue.",
		//				//},
		//				MagenticOneTeamConfig: &v1alpha1.MagenticOneTeamConfig{
		//					MaxStalls: 3,
		//					FinalAnswerPrompt: `We are working on the following task:
		//{task}
		//
		//We have completed the task.
		//
		//The above messages contain the conversation that took place to complete the task.
		//
		//Based on the information gathered, provide the final answer to the original request.
		//The answer should be phrased as if you were speaking to the user.`,
		//				},
		//				TerminationCondition: v1alpha1.TerminationCondition{
		//					TextMentionTermination: &v1alpha1.TextMentionTermination{Text: "TERMINATE"},
		//				},
		//				MaxTurns: 10,
		//			},
		//		}

		writeKubeObjects(
			"manifests/kubeobjects.yaml",
			//planningAgent,
			kubeExpert,
			//kubectlUser,
			//apiTeam,
		)

		Expect(true).To(BeTrue())
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
