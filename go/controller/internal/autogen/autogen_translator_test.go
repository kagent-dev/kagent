package autogen_test

import (
	"context"
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

		// go func() {
		// 	// start autogen server
		// 	startAutogenServer(ctx)
		// }()

		// sleep for a while to allow autogen server to start
		<-time.After(3 * time.Second)

		client := autogen_client.New("http://localhost:8081/api", "ws://localhost:8081/api/ws")

		scheme := scheme.Scheme
		err := v1alpha1.AddToScheme(scheme)
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
				Model:            "gpt-4o",
				APIKeySecretName: apikeySecret.Name,
				APIKeySecretKey:  apikeySecretKey,
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

		err = client.CreateTeam(autogenTeam)
		Expect(err).NotTo(HaveOccurred())

		list, err := client.ListTeams(autogenTeam.UserID)
		Expect(err).NotTo(HaveOccurred())
		Expect(list).NotTo(BeNil())
		Expect(len(list)).To(Equal(1))
		Expect(list[0].Id).To(Equal(autogenTeam.Id))
	})
})

func startAutogenServer(ctx context.Context) {
	defer GinkgoRecover()
	cmd := exec.CommandContext(ctx, "bash", "-c", "source .venv/bin/activate && uv run autogenstudio ui")
	cmd.Dir = "../../../../python"
	cmd.Stdout = GinkgoWriter
	cmd.Stderr = GinkgoWriter
	err := cmd.Run()
	if err != nil && err.Error() != "context canceled" {
		Expect(err).NotTo(HaveOccurred())
	}
}
