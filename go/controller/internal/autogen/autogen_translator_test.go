package autogen_test

import (
	"context"
	"net/http"
	"os"
	"os/exec"
	"time"

	autogen_client "github.com/kagent-dev/kagent/go/autogen/client"
	"github.com/kagent-dev/kagent/go/controller/api/v1alpha1"
	"github.com/kagent-dev/kagent/go/controller/internal/autogen"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

var (
	openaiApiKey = os.Getenv("OPENAI_API_KEY")
)

const (
	apikeySecretKey = "api-key"
)

var _ = Describe("AutogenClient", func() {
	It("should interact with autogen server", func() {
		ctx := context.Background()

		go func() {
			// start autogen server
			startAutogenServer(ctx)
		}()

		// Make requests to /api/health until it returns 200
		// Do it for max 20 seconds
		c := &http.Client{}
		req, err := http.NewRequest("GET", "http://localhost:8081/api/health", nil)
		Expect(err).NotTo(HaveOccurred())
		var resp *http.Response
		for i := 0; i < 20; i++ {
			resp, err = c.Do(req)
			if err == nil && resp.StatusCode == 200 {
				break
			}
			<-time.After(1 * time.Second)
		}
		Expect(err).NotTo(HaveOccurred())
		Expect(resp.StatusCode).To(Equal(200))

		client := autogen_client.New("http://localhost:8081/api", "ws://localhost:8081/api/ws")

		scheme := scheme.Scheme
		err = v1alpha1.AddToScheme(scheme)
		Expect(err).NotTo(HaveOccurred())

		kubeClient := fake.NewClientBuilder().WithScheme(scheme).Build()

		// add a team
		namespace := "team-ns"

		apikeySecret := &v1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-secret",
				Namespace: namespace,
			},
			Data: map[string][]byte{
				apikeySecretKey: []byte(openaiApiKey),
			},
		}

		modelConfig := &v1alpha1.ModelConfig{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-model",
				Namespace: namespace,
			},
			Spec: v1alpha1.ModelConfigSpec{
				Model:           "gpt-4o",
				Provider:        v1alpha1.OpenAI,
				APIKeySecretRef: apikeySecret.Name,
				APIKeySecretKey: apikeySecretKey,
				OpenAI: &v1alpha1.OpenAIConfig{
					Temperature: "0.7",
					MaxTokens:   1024,
					TopP:        "0.95",
				},
			},
		}

		participant1 := &v1alpha1.Agent{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-participant1",
				Namespace: namespace,
			},
			Spec: v1alpha1.AgentSpec{
				Description:   "a test participant",
				SystemMessage: "You are a test participant",
				ModelConfig:   modelConfig.Name,
				Tools:         nil,
			},
		}

		participant2 := &v1alpha1.Agent{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-participant2",
				Namespace: namespace,
			},
			Spec: v1alpha1.AgentSpec{
				Description:   "a test participant",
				SystemMessage: "You are a test participant",
				ModelConfig:   modelConfig.Name,
				Tools:         nil,
			},
		}

		apiTeam := &v1alpha1.Team{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-team",
				Namespace: namespace,
			},
			Spec: v1alpha1.TeamSpec{
				Participants: []string{
					participant1.Name,
					participant2.Name,
				},
				Description:        "a team that tests things",
				ModelConfig:        modelConfig.Name,
				SelectorTeamConfig: &v1alpha1.SelectorTeamConfig{},
				TerminationCondition: v1alpha1.TerminationCondition{
					MaxMessageTermination: &v1alpha1.MaxMessageTermination{MaxMessages: 10},
				},
				MaxTurns: 10,
			},
		}

		err = kubeClient.Create(ctx, apikeySecret)
		Expect(err).NotTo(HaveOccurred())

		err = kubeClient.Create(ctx, modelConfig)
		Expect(err).NotTo(HaveOccurred())

		err = kubeClient.Create(ctx, participant1)
		Expect(err).NotTo(HaveOccurred())

		err = kubeClient.Create(ctx, participant2)
		Expect(err).NotTo(HaveOccurred())

		err = kubeClient.Create(ctx, apiTeam)
		Expect(err).NotTo(HaveOccurred())

		autogenTeam, err := autogen.NewAutogenApiTranslator(kubeClient, types.NamespacedName{
			Namespace: modelConfig.Namespace,
			Name:      modelConfig.Name,
		}).TranslateGroupChatForTeam(ctx, apiTeam)
		Expect(err).NotTo(HaveOccurred())
		Expect(autogenTeam).NotTo(BeNil())

		listBefore, err := client.ListTeams(autogenTeam.UserID)
		Expect(err).NotTo(HaveOccurred())

		err = client.CreateTeam(autogenTeam)
		Expect(err).NotTo(HaveOccurred())

		list, err := client.ListTeams(autogenTeam.UserID)
		Expect(err).NotTo(HaveOccurred())
		Expect(list).NotTo(BeNil())
		Expect(len(list)).To(Equal(len(listBefore) + 1))

		// check the autogen team that was created is returned
		found := false
		for _, t := range list {
			if t.Id == autogenTeam.Id {
				Expect(t.Component.Label).To(Equal(autogenTeam.Component.Label))
				Expect(t.Component.Provider).To(Equal(autogenTeam.Component.Provider))
				Expect(t.Component.Version).To(Equal(autogenTeam.Component.Version))
				Expect(t.Component.Description).To(Equal(autogenTeam.Component.Description))
				Expect(t.Component.Config).To(Equal(autogenTeam.Component.Config))
				found = true
				break
			}
		}
		Expect(found).To(BeTrue())
	})
})

func startAutogenServer(ctx context.Context) {
	defer GinkgoRecover()
	cmd := exec.CommandContext(ctx, "bash", "-c", "source .venv/bin/activate && uv run kagent-engine serve")
	cmd.Dir = "../../../../python"
	cmd.Stdout = GinkgoWriter
	cmd.Stderr = GinkgoWriter
	err := cmd.Run()
	if err != nil && err.Error() != "context canceled" {
		Expect(err).NotTo(HaveOccurred())
	}
}
