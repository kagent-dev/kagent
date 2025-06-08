package autogen_test

import (
	"context"
	"net/http"
	"os"
	"os/exec"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	autogen_client "github.com/kagent-dev/kagent/go/autogen/client"
	"github.com/kagent-dev/kagent/go/controller/api/v1alpha1"
	"github.com/kagent-dev/kagent/go/controller/internal/autogen"
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

func TestConfigMapAndSecretValueResolution(t *testing.T) {
	ctx := context.Background()
	scheme := scheme.Scheme
	err := v1alpha1.AddToScheme(scheme)
	require.NoError(t, err)

	kubeClient := fake.NewClientBuilder().WithScheme(scheme).Build()
	translator := autogen.NewAutogenApiTranslator(kubeClient, types.NamespacedName{
		Namespace: "default",
		Name:      "default-model",
	})
	namespace := "test-namespace"

	t.Run("should retrieve value from ConfigMap", func(t *testing.T) {
		configMap := &v1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-config",
				Namespace: namespace,
			},
			Data: map[string]string{
				"test-key": "test-value",
			},
		}
		err := kubeClient.Create(ctx, configMap)
		require.NoError(t, err)

		// Create a ToolServerConfig with ConfigMapKeyRef
		toolServer := &v1alpha1.ToolServer{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-tool-server",
				Namespace: namespace,
			},
			Spec: v1alpha1.ToolServerSpec{
				Config: v1alpha1.ToolServerConfig{
					Stdio: &v1alpha1.StdioMcpServerConfig{
						Command: "echo",
						Args:    []string{"hello"},
						EnvFrom: []v1alpha1.ValueRef{
							{
								Name: "TEST_ENV",
								ValueFrom: &v1alpha1.ValueSource{
									Type:     v1alpha1.ConfigMapValueSource,
									ValueRef: "test-config",
									Key:      "test-key",
								},
							},
						},
					},
				},
			},
		}

		result, err := translator.TranslateToolServer(ctx, toolServer)
		require.NoError(t, err)

		assert.Equal(t, "kagent.tool_servers.StdioMcpToolServer", result.Component.Provider)

		config := result.Component.Config
		assert.Contains(t, config, "env")

		env := config["env"].(map[string]interface{})
		assert.Contains(t, env, "TEST_ENV")
		assert.Equal(t, "test-value", env["TEST_ENV"])
	})

	t.Run("should retrieve value from Secret", func(t *testing.T) {
		secret := &v1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-secret",
				Namespace: namespace,
			},
			Data: map[string][]byte{
				"test-key": []byte("secret-value"),
			},
		}
		err := kubeClient.Create(ctx, secret)
		require.NoError(t, err)

		// Create a ToolServerConfig with SecretKeyRef
		toolServer := &v1alpha1.ToolServer{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-tool-server",
				Namespace: namespace,
			},
			Spec: v1alpha1.ToolServerSpec{
				Config: v1alpha1.ToolServerConfig{
					Stdio: &v1alpha1.StdioMcpServerConfig{
						Command: "echo",
						Args:    []string{"hello"},
						EnvFrom: []v1alpha1.ValueRef{
							{
								Name: "TEST_ENV",
								ValueFrom: &v1alpha1.ValueSource{
									Type:     v1alpha1.SecretValueSource,
									ValueRef: "test-secret",
									Key:      "test-key",
								},
							},
						},
					},
				},
			},
		}

		result, err := translator.TranslateToolServer(ctx, toolServer)
		require.NoError(t, err)

		assert.Equal(t, "kagent.tool_servers.StdioMcpToolServer", result.Component.Provider)

		config := result.Component.Config
		assert.Contains(t, config, "env")

		env := config["env"].(map[string]interface{})
		assert.Contains(t, env, "TEST_ENV")
		assert.Equal(t, "secret-value", env["TEST_ENV"])
	})

	t.Run("should fail if both ConfigMap and Secret don't exist", func(t *testing.T) {
		// No ConfigMap or Secret created

		// Create a ToolServerConfig with both ConfigMapKeyRef and SecretKeyRef pointing to non-existent resources
		toolServer := &v1alpha1.ToolServer{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-tool-server",
				Namespace: namespace,
			},
			Spec: v1alpha1.ToolServerSpec{
				Config: v1alpha1.ToolServerConfig{
					Stdio: &v1alpha1.StdioMcpServerConfig{
						Command: "echo",
						Args:    []string{"hello"},
						EnvFrom: []v1alpha1.ValueRef{
							{
								Name: "TEST_ENV",
								ValueFrom: &v1alpha1.ValueSource{
									Type:     v1alpha1.ConfigMapValueSource,
									ValueRef: "nonexistent-config",
									Key:      "test-key",
								},
							},
						},
					},
				},
			},
		}

		_, err := translator.TranslateToolServer(ctx, toolServer)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed to resolve environment variable TEST_ENV")
	})
}

func TestAutogenClient(t *testing.T) {
	t.Run("should interact with autogen server", func(t *testing.T) {
		ctx := context.Background()

		go func() {
			// start autogen server
			startAutogenServer(ctx, t)
		}()

		// Make requests to /api/health until it returns 200
		// Do it for max 20 seconds
		c := &http.Client{}
		req, err := http.NewRequest("GET", "http://localhost:8081/api/health", nil)
		require.NoError(t, err)

		var resp *http.Response
		for i := 0; i < 20; i++ {
			resp, err = c.Do(req)
			if err == nil && resp.StatusCode == 200 {
				break
			}
			<-time.After(1 * time.Second)
		}
		require.NoError(t, err)
		assert.Equal(t, 200, resp.StatusCode)

		client := autogen_client.New("http://localhost:8081/api")

		scheme := scheme.Scheme
		err = v1alpha1.AddToScheme(scheme)
		require.NoError(t, err)

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
		require.NoError(t, err)

		err = kubeClient.Create(ctx, modelConfig)
		require.NoError(t, err)

		err = kubeClient.Create(ctx, participant1)
		require.NoError(t, err)

		err = kubeClient.Create(ctx, participant2)
		require.NoError(t, err)

		err = kubeClient.Create(ctx, apiTeam)
		require.NoError(t, err)

		autogenTeam, err := autogen.NewAutogenApiTranslator(kubeClient, types.NamespacedName{
			Namespace: modelConfig.Namespace,
			Name:      modelConfig.Name,
		}).TranslateGroupChatForTeam(ctx, apiTeam)
		require.NoError(t, err)
		assert.NotNil(t, autogenTeam)

		listBefore, err := client.ListTeams(autogenTeam.UserID)
		require.NoError(t, err)

		err = client.CreateTeam(autogenTeam)
		require.NoError(t, err)

		list, err := client.ListTeams(autogenTeam.UserID)
		require.NoError(t, err)
		assert.NotNil(t, list)
		assert.Equal(t, len(listBefore)+1, len(list))

		// check the autogen team that was created is returned
		found := false
		for _, team := range list {
			if team.Id == autogenTeam.Id {
				assert.Equal(t, autogenTeam.Component.Label, team.Component.Label)
				assert.Equal(t, autogenTeam.Component.Provider, team.Component.Provider)
				assert.Equal(t, autogenTeam.Component.Version, team.Component.Version)
				assert.Equal(t, autogenTeam.Component.Description, team.Component.Description)
				// Note: Comparing maps requires deep comparison, simplified here
				found = true
				break
			}
		}
		assert.True(t, found, "Expected to find the created team in the list")
	})
}

func startAutogenServer(ctx context.Context, t *testing.T) {
	cmd := exec.CommandContext(ctx, "bash", "-c", "source .venv/bin/activate && uv run kagent-engine serve")
	cmd.Dir = "../../../../python"
	err := cmd.Run()
	if err != nil && err.Error() != "context canceled" {
		t.Logf("Autogen server error: %v", err)
	}
}
