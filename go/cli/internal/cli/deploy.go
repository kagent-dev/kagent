package cli

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/kagent-dev/kagent/go/api/v1alpha2"
	"github.com/kagent-dev/kagent/go/cli/internal/config"
	"github.com/kagent-dev/kagent/go/cli/internal/frameworks/common"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type DeployCfg struct {
	ProjectDir   string
	Image        string
	APIKey       string
	APIKeySecret string
	Config       *config.Config
}

// DeployCmd deploys an agent to Kubernetes
func DeployCmd(cfg *DeployCfg) error {
	// Validate project directory
	if cfg.ProjectDir == "" {
		return fmt.Errorf("project directory is required")
	}

	// Check if project directory exists
	if _, err := os.Stat(cfg.ProjectDir); os.IsNotExist(err) {
		return fmt.Errorf("project directory does not exist: %s", cfg.ProjectDir)
	}

	// Load the kagent.yaml manifest
	manifest, err := loadManifest(cfg.ProjectDir)
	if err != nil {
		return fmt.Errorf("failed to load kagent.yaml: %v", err)
	}

	// Determine the API key environment variable name based on model provider
	apiKeyEnvVar := getAPIKeyEnvVar(manifest.ModelProvider)
	if apiKeyEnvVar == "" {
		return fmt.Errorf("unsupported model provider: %s", manifest.ModelProvider)
	}

	// Create Kubernetes client
	k8sClient, err := createKubernetesClient()
	if err != nil {
		return fmt.Errorf("failed to create Kubernetes client: %v", err)
	}

	// Handle secret creation or reference
	var secretName string
	if cfg.APIKeySecret != "" {
		// Use existing secret
		secretName = cfg.APIKeySecret
		// Verify the secret exists
		if err := verifySecretExists(k8sClient, cfg.Config.Namespace, secretName, apiKeyEnvVar); err != nil {
			return fmt.Errorf("failed to verify secret '%s': %v", secretName, err)
		}
		fmt.Printf("Using existing secret '%s' in namespace '%s'\n", secretName, cfg.Config.Namespace)
	} else if cfg.APIKey != "" {
		// Create new secret with provided API key
		secretName = fmt.Sprintf("%s-%s", manifest.Name, strings.ToLower(manifest.ModelProvider))
		if err := createSecret(k8sClient, cfg.Config.Namespace, secretName, apiKeyEnvVar, cfg.APIKey); err != nil {
			return fmt.Errorf("failed to create secret: %v", err)
		}
	} else {
		return fmt.Errorf("either --api-key or --api-key-secret must be provided")
	}

	// Create the Agent CRD
	if err := createAgentCRD(k8sClient, cfg, manifest, secretName, apiKeyEnvVar); err != nil {
		return fmt.Errorf("failed to create Agent CRD: %v", err)
	}

	fmt.Printf("Successfully deployed agent '%s' to namespace '%s'\n", manifest.Name, cfg.Config.Namespace)
	return nil
}

// loadManifest loads the kagent.yaml file from the project directory
func loadManifest(projectDir string) (*common.AgentManifest, error) {
	// Use the Manager to load the manifest
	manager := common.NewManifestManager(projectDir)
	manifest, err := manager.Load()
	if err != nil {
		return nil, fmt.Errorf("failed to load kagent.yaml: %v", err)
	}

	// Additional validation for deploy-specific requirements
	if manifest.ModelProvider == "" {
		return nil, fmt.Errorf("model provider is required in kagent.yaml")
	}

	return manifest, nil
}

// getAPIKeyEnvVar returns the environment variable name for the given model provider
func getAPIKeyEnvVar(modelProvider string) string {
	switch modelProvider {
	case string(v1alpha2.ModelProviderAnthropic):
		return "ANTHROPIC_API_KEY"
	case string(v1alpha2.ModelProviderOpenAI):
		return "OPENAI_API_KEY"
	case string(v1alpha2.ModelProviderAzureOpenAI):
		return "AZURE_API_KEY"
	case string(v1alpha2.ModelProviderGemini):
		return "GOOGLE_API_KEY"
	default:
		return ""
	}
}

// createKubernetesClient creates a Kubernetes client
func createKubernetesClient() (client.Client, error) {
	// Try to load from kubeconfig first
	kubeconfig := os.Getenv("KUBECONFIG")
	if kubeconfig == "" {
		home := os.Getenv("HOME")
		if home != "" {
			kubeconfig = filepath.Join(home, ".kube", "config")
		}
	}

	var config *rest.Config
	var err error

	if kubeconfig != "" && fileExists(kubeconfig) {
		config, err = clientcmd.BuildConfigFromFlags("", kubeconfig)
	} else {
		// Try in-cluster config
		config, err = rest.InClusterConfig()
	}

	if err != nil {
		return nil, fmt.Errorf("failed to get Kubernetes config: %v", err)
	}

	schemes := runtime.NewScheme()
	if err := scheme.AddToScheme(schemes); err != nil {
		return nil, fmt.Errorf("failed to add core scheme: %v", err)
	}
	if err := v1alpha2.AddToScheme(schemes); err != nil {
		return nil, fmt.Errorf("failed to add kagent v1alpha2 scheme: %v", err)
	}

	k8sClient, err := client.New(config, client.Options{Scheme: schemes})
	if err != nil {
		return nil, fmt.Errorf("failed to create Kubernetes client: %v", err)
	}

	return k8sClient, nil
}

// verifySecretExists verifies that a secret exists and contains the required key
func verifySecretExists(k8sClient client.Client, namespace, secretName, apiKeyEnvVar string) error {
	secret := &corev1.Secret{}
	err := k8sClient.Get(context.Background(), client.ObjectKey{Namespace: namespace, Name: secretName}, secret)
	if err != nil {
		if errors.IsNotFound(err) {
			return fmt.Errorf("secret '%s' not found in namespace '%s'", secretName, namespace)
		}
		return fmt.Errorf("failed to check if secret exists: %v", err)
	}

	// Verify the secret contains the required key
	if _, exists := secret.Data[apiKeyEnvVar]; !exists {
		return fmt.Errorf("secret '%s' does not contain key '%s'", secretName, apiKeyEnvVar)
	}

	return nil
}

// createSecret creates a Kubernetes secret with the API key
func createSecret(k8sClient client.Client, namespace, secretName, apiKeyEnvVar, apiKeyValue string) error {
	// Check if secret already exists
	existingSecret := &corev1.Secret{}
	err := k8sClient.Get(context.Background(), client.ObjectKey{Namespace: namespace, Name: secretName}, existingSecret)

	if err == nil {
		// Secret exists, update it
		existingSecret.Data[apiKeyEnvVar] = []byte(apiKeyValue)
		if err := k8sClient.Update(context.Background(), existingSecret); err != nil {
			return fmt.Errorf("failed to update existing secret: %v", err)
		}
		fmt.Printf("Updated existing secret '%s' in namespace '%s'\n", secretName, namespace)
		return nil
	}

	if !errors.IsNotFound(err) {
		return fmt.Errorf("failed to check if secret exists: %v", err)
	}

	// Create new secret
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      secretName,
			Namespace: namespace,
		},
		Data: map[string][]byte{
			apiKeyEnvVar: []byte(apiKeyValue),
		},
	}

	if err := k8sClient.Create(context.Background(), secret); err != nil {
		return fmt.Errorf("failed to create secret: %v", err)
	}

	fmt.Printf("Created secret '%s' in namespace '%s'\n", secretName, namespace)
	return nil
}

// createAgentCRD creates the Agent CRD
func createAgentCRD(k8sClient client.Client, cfg *DeployCfg, manifest *common.AgentManifest, secretName, apiKeyEnvVar string) error {
	// Determine image name
	imageName := cfg.Image
	if imageName == "" {
		// Use default registry and tag
		registry := "localhost:5001"
		tag := "latest"
		imageName = fmt.Sprintf("%s/%s:%s", registry, manifest.Name, tag)
	}

	// Create the Agent CRD
	agent := &v1alpha2.Agent{
		ObjectMeta: metav1.ObjectMeta{
			Name:      manifest.Name,
			Namespace: cfg.Config.Namespace,
		},
		Spec: v1alpha2.AgentSpec{
			Type:        v1alpha2.AgentType_BYO,
			Description: manifest.Description,
			BYO: &v1alpha2.BYOAgentSpec{
				Deployment: &v1alpha2.ByoDeploymentSpec{
					Image: imageName,
					SharedDeploymentSpec: v1alpha2.SharedDeploymentSpec{
						Env: []corev1.EnvVar{
							{
								Name: apiKeyEnvVar,
								ValueFrom: &corev1.EnvVarSource{
									SecretKeyRef: &corev1.SecretKeySelector{
										LocalObjectReference: corev1.LocalObjectReference{
											Name: secretName,
										},
										Key: apiKeyEnvVar,
									},
								},
							},
						},
					},
				},
			},
		},
	}

	// Check if agent already exists
	existingAgent := &v1alpha2.Agent{}
	err := k8sClient.Get(context.Background(), client.ObjectKey{Namespace: cfg.Config.Namespace, Name: manifest.Name}, existingAgent)

	if err == nil {
		// Agent exists, update it
		existingAgent.Spec = agent.Spec
		if err := k8sClient.Update(context.Background(), existingAgent); err != nil {
			return fmt.Errorf("failed to update existing agent: %v", err)
		}
		fmt.Printf("Updated existing agent '%s' in namespace '%s'\n", manifest.Name, cfg.Config.Namespace)
		return nil
	}

	if !errors.IsNotFound(err) {
		return fmt.Errorf("failed to check if agent exists: %v", err)
	}

	// Create new agent
	if err := k8sClient.Create(context.Background(), agent); err != nil {
		return fmt.Errorf("failed to create agent: %v", err)
	}

	fmt.Printf("Created agent '%s' in namespace '%s'\n", manifest.Name, cfg.Config.Namespace)
	return nil
}

// fileExists checks if a file exists
func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
