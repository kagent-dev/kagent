package cli

import (
	"bufio"
	"context"
	"fmt"
	"maps"
	"os"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/kagent-dev/kagent/go/api/v1alpha2"
	"github.com/kagent-dev/kagent/go/cli/internal/agent/frameworks/common"
	commonexec "github.com/kagent-dev/kagent/go/cli/internal/common/exec"
	commonimage "github.com/kagent-dev/kagent/go/cli/internal/common/image"
	"github.com/kagent-dev/kagent/go/cli/internal/config"
	"github.com/kagent-dev/kmcp/api/v1alpha1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/clientcmd"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/yaml"
)

const (
	// Default namespace for deployments
	defaultNamespace = "default"

	// Default images for MCP servers
	defaultNodeImage = "node:24-alpine3.21"
	defaultUVImage   = "ghcr.io/astral-sh/uv:python3.12-alpine"

	// Default timeouts
	defaultTimeout        = 5 * time.Second
	defaultSSEReadTimeout = 5 * time.Minute

	// Environment variable pattern for matching ${VAR} or $VAR
	envVarPattern = `\$\{([^}]+)\}|\$([A-Za-z_][A-Za-z0-9_]*)`
)

// DeployCfg contains all configuration options for deploying an agent to Kubernetes.
type DeployCfg struct {
	// ProjectDir is the path to the agent project directory (must contain kagent.yaml)
	ProjectDir string

	// Image is the Docker image name (e.g., "registry/name:tag"). If empty, defaults to localhost:5001/name:latest
	Image string

	// EnvFile is the path to a .env file containing environment variables to be loaded into the agent.
	// This MUST include the model provider API key (e.g., ANTHROPIC_API_KEY, OPENAI_API_KEY, GOOGLE_API_KEY).
	// A Secret will be created with these values and mounted in the agent deployment.
	EnvFile string

	// Platform specifies the target platform for Docker builds (e.g., "linux/amd64", "linux/arm64")
	Platform string

	// Config contains CLI configuration (namespace, verbosity, etc.)
	Config *config.Config

	// DryRun when true, outputs YAML manifests without actually creating resources
	DryRun bool
}

// DeployCmd deploys an agent to Kubernetes
func DeployCmd(ctx context.Context, cfg *DeployCfg) error {
	// Step 1: Validate and load project
	manifest, err := validateAndLoadProject(cfg)
	if err != nil {
		return err
	}

	// Step 2: Validate deployment requirements
	apiKeyEnvVar, err := validateDeploymentRequirements(manifest)
	if err != nil {
		return err
	}

	// Step 3: Extract environment variable references from manifest
	requiredEnvVars := extractEnvVarsFromManifest(manifest)

	// Step 4: Validate environment variables and prompt user if needed
	if err := validateAndPromptEnvVars(cfg, requiredEnvVars, apiKeyEnvVar); err != nil {
		return err
	}

	// Step 5: Build Docker image (skip in dry-run mode)
	if err := buildAndPushImage(cfg); err != nil {
		return err
	}

	// Step 6: Setup Kubernetes client and namespace
	k8sClient, err := setupKubernetesEnvironment(cfg)
	if err != nil {
		return err
	}

	// Step 7: Handle env file secret (contains API key and other env vars)
	envData, err := handleEnvFileSecret(ctx, k8sClient, cfg, manifest)
	if err != nil {
		return err
	}

	// Step 8: Deploy Agent CRD
	if err := createAgentCRD(ctx, k8sClient, cfg, manifest, envData, IsVerbose(cfg.Config)); err != nil {
		return err
	}

	// Step 9: Deploy MCP servers if defined
	if err := deployMCPServersIfNeeded(ctx, k8sClient, cfg, manifest); err != nil {
		return err
	}

	// Step 10: Restart deployment if not in dry-run mode
	if !cfg.DryRun {
		if err := restartAgentDeployment(ctx, k8sClient, cfg, manifest); err != nil {
			fmt.Printf("Warning: failed to restart deployment: %v\n", err)
		}
	}

	printDeploymentResult(cfg, manifest)
	return nil
}

// validateAndLoadProject validates the project directory and loads the manifest
func validateAndLoadProject(cfg *DeployCfg) (*common.AgentManifest, error) {
	if cfg.ProjectDir == "" {
		return nil, fmt.Errorf("project directory is required")
	}

	if _, err := os.Stat(cfg.ProjectDir); os.IsNotExist(err) {
		return nil, fmt.Errorf("project directory does not exist: %s", cfg.ProjectDir)
	}

	manifest, err := LoadManifest(cfg.ProjectDir)
	if err != nil {
		return nil, fmt.Errorf("failed to load kagent.yaml: %v", err)
	}

	return manifest, nil
}

// buildAndPushImage builds and pushes the Docker image (skipped in dry-run mode)
func buildAndPushImage(cfg *DeployCfg) error {
	if cfg.DryRun {
		return nil
	}

	fmt.Println("Building Docker image...")
	buildCfg := &BuildCfg{
		ProjectDir:     cfg.ProjectDir,
		Image:          cfg.Image,
		Push:           true, // Always push when deploying
		Platform:       cfg.Platform,
		Config:         cfg.Config,
		SkipMCPServers: true, // Don't build MCP servers during deploy
	}

	if err := BuildCmd(buildCfg); err != nil {
		return fmt.Errorf("failed to build Docker image: %v", err)
	}

	return nil
}

// validateDeploymentRequirements validates deployment-specific requirements
func validateDeploymentRequirements(manifest *common.AgentManifest) (string, error) {
	if manifest.ModelProvider == "" {
		return "", fmt.Errorf("model provider is required in kagent.yaml")
	}

	apiKeyEnvVar := getAPIKeyEnvVar(manifest.ModelProvider)
	if apiKeyEnvVar == "" {
		return "", fmt.Errorf("unsupported model provider: %s", manifest.ModelProvider)
	}

	return apiKeyEnvVar, nil
}

// setupKubernetesEnvironment creates Kubernetes client and sets up namespace
func setupKubernetesEnvironment(cfg *DeployCfg) (client.Client, error) {
	k8sClient, err := createKubernetesClient()
	if err != nil && !cfg.DryRun {
		return nil, fmt.Errorf("failed to create Kubernetes client: %v", err)
	}

	// Set default namespace if not provided
	if cfg.Config.Namespace == "" {
		cfg.Config.Namespace = defaultNamespace
	}

	return k8sClient, nil
}

// envFileData holds both the secret name and the parsed env var keys
type envFileData struct {
	SecretName string
	EnvVarKeys []string
}

// extractEnvVarsFromManifest extracts all environment variable references from the manifest
func extractEnvVarsFromManifest(manifest *common.AgentManifest) []string {
	envVarRegex := regexp.MustCompile(envVarPattern)
	envVarSet := make(map[string]bool)

	// Extract from MCP servers
	for _, mcpServer := range manifest.McpServers {
		if mcpServer.URL != "" {
			matches := envVarRegex.FindAllStringSubmatch(mcpServer.URL, -1)
			for _, match := range matches {
				varName := extractEnvVarName(match)
				envVarSet[varName] = true
			}
		}

		// Check headers
		for _, headerValue := range mcpServer.Headers {
			matches := envVarRegex.FindAllStringSubmatch(headerValue, -1)
			for _, match := range matches {
				varName := extractEnvVarName(match)
				envVarSet[varName] = true
			}
		}

		// Check env vars
		for _, envVar := range mcpServer.Env {
			// Parse KEY=VALUE format
			parts := strings.SplitN(envVar, "=", 2)
			if len(parts) == 2 {
				matches := envVarRegex.FindAllStringSubmatch(parts[1], -1)
				for _, match := range matches {
					varName := extractEnvVarName(match)
					envVarSet[varName] = true
				}
			}
		}
	}

	// Convert set to sorted slice
	envVars := make([]string, 0, len(envVarSet))
	for varName := range envVarSet {
		envVars = append(envVars, varName)
	}
	slices.Sort(envVars)

	return envVars
}

// promptUserConfirmation prompts the user with a yes/no question and returns an error if they decline
func promptUserConfirmation(message string) error {
	fmt.Print(message)
	var response string
	if _, err := fmt.Scanln(&response); err != nil {
		return fmt.Errorf("failed to read input: %v", err)
	}

	response = strings.ToLower(strings.TrimSpace(response))
	if response != "y" && response != "yes" {
		return fmt.Errorf("deployment cancelled by user")
	}
	fmt.Println()
	return nil
}

// validateAndPromptEnvVars validates environment variables and prompts user if needed
func validateAndPromptEnvVars(cfg *DeployCfg, requiredEnvVars []string, apiKeyEnvVar string) error {
	if cfg.EnvFile == "" {
		return fmt.Errorf("--env-file is required and must contain %s for the model provider", apiKeyEnvVar)
	}

	// Env file provided, check if it contains the API key
	envFileVars, err := parseEnvFile(cfg.EnvFile)
	if err != nil {
		return fmt.Errorf("failed to parse env file for validation: %v", err)
	}

	if _, exists := envFileVars[apiKeyEnvVar]; !exists {
		return fmt.Errorf(".env file must contain %s for the model provider", apiKeyEnvVar)
	}

	// Check for other missing variables referenced in kagent.yaml
	missingVars := []string{}
	for _, varName := range requiredEnvVars {
		if varName == apiKeyEnvVar {
			continue
		}
		if _, exists := envFileVars[varName]; !exists {
			missingVars = append(missingVars, varName)
		}
	}

	if len(missingVars) > 0 {
		fmt.Printf("\n⚠️  Warning: The following variables are referenced in kagent.yaml but missing from %s:\n", cfg.EnvFile)
		for _, varName := range missingVars {
			fmt.Printf("  - %s\n", varName)
		}
		fmt.Printf("\nWithout these variables, your MCP servers or agents may fail to start or work correctly.\n")
		fmt.Printf("Consider adding them to your .env file or ensure they're available at runtime.\n")

		if !cfg.DryRun {
			if err := promptUserConfirmation("\nContinue anyway? (y/N): "); err != nil {
				return err
			}
		}
	}

	return nil
}

// handleEnvFileSecret manages environment file secret creation
func handleEnvFileSecret(ctx context.Context, k8sClient client.Client, cfg *DeployCfg, manifest *common.AgentManifest) (*envFileData, error) {
	if cfg.EnvFile == "" {
		return nil, nil
	}

	envVars, err := parseEnvFile(cfg.EnvFile)
	if err != nil {
		return nil, fmt.Errorf("failed to parse env file: %v", err)
	}

	secretName := fmt.Sprintf("%s-env", manifest.Name)
	if err := createEnvFileSecret(ctx, k8sClient, cfg.Config.Namespace, secretName, envVars, IsVerbose(cfg.Config), cfg.DryRun); err != nil {
		return nil, fmt.Errorf("failed to create env file secret: %v", err)
	}

	keys := make([]string, 0, len(envVars))
	for k := range envVars {
		keys = append(keys, k)
	}

	return &envFileData{
		SecretName: secretName,
		EnvVarKeys: keys,
	}, nil
}

// parseEnvFile reads and parses a .env file, returning a map of environment variables
func parseEnvFile(filePath string) (map[string]string, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to open env file: %v", err)
	}
	defer func() { _ = file.Close() }()

	envVars := make(map[string]string)
	scanner := bufio.NewScanner(file)
	lineNum := 0

	for scanner.Scan() {
		lineNum++
		line := strings.TrimSpace(scanner.Text())

		// Skip empty lines and comments
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		// Parse KEY=VALUE format
		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			return nil, fmt.Errorf("invalid format at line %d: expected KEY=VALUE, got %q", lineNum, line)
		}

		key := strings.TrimSpace(parts[0])
		value := strings.TrimSpace(parts[1])

		// Remove surrounding quotes if present
		value = strings.Trim(value, `"'`)

		if key == "" {
			return nil, fmt.Errorf("empty key at line %d", lineNum)
		}

		envVars[key] = value
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("error reading env file: %v", err)
	}

	return envVars, nil
}

// createEnvFileSecret creates a Kubernetes secret from env file variables
func createEnvFileSecret(ctx context.Context, k8sClient client.Client, namespace, secretName string, envVars map[string]string, verbose bool, dryRun bool) error {
	// Convert string map to byte map
	secretData := make(map[string][]byte)
	for k, v := range envVars {
		secretData[k] = []byte(v)
	}

	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      secretName,
			Namespace: namespace,
		},
		Data: secretData,
	}
	secret.SetGroupVersionKind(corev1.SchemeGroupVersion.WithKind("Secret"))

	// In dry-run mode, just output the YAML
	if dryRun {
		return outputYAML(secret)
	}

	existingSecret := &corev1.Secret{}
	err := k8sClient.Get(ctx, client.ObjectKey{Namespace: namespace, Name: secretName}, existingSecret)

	if err != nil {
		if apierrors.IsNotFound(err) {
			if err := k8sClient.Create(ctx, secret); err != nil {
				return fmt.Errorf("failed to create env file secret: %v", err)
			}
			if verbose {
				fmt.Printf("Created env file secret '%s' in namespace '%s' with %d variables\n", secretName, namespace, len(envVars))
			}
			return nil
		}
		return fmt.Errorf("failed to check if env file secret exists: %v", err)
	}

	existingSecret.Data = secretData
	if err := k8sClient.Update(ctx, existingSecret); err != nil {
		return fmt.Errorf("failed to update existing env file secret: %v", err)
	}
	if verbose {
		fmt.Printf("Updated existing env file secret '%s' in namespace '%s' with %d variables\n", secretName, namespace, len(envVars))
	}
	return nil
}

// waitForDeployment polls for a deployment to be created, with a timeout
func waitForDeployment(ctx context.Context, k8sClient client.Client, namespace, name string, timeout time.Duration, config *config.Config) (*appsv1.Deployment, error) {
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	timeoutTimer := time.NewTimer(timeout)
	defer timeoutTimer.Stop()

	deployment := &appsv1.Deployment{}

	for {
		select {
		case <-timeoutTimer.C:
			return nil, apierrors.NewNotFound(appsv1.Resource("deployment"), name)
		case <-ticker.C:
			err := k8sClient.Get(ctx, types.NamespacedName{
				Name:      name,
				Namespace: namespace,
			}, deployment)

			if err == nil {
				if IsVerbose(config) {
					fmt.Printf("Deployment '%s' found in namespace '%s'\n", name, namespace)
				}
				return deployment, nil
			}

			if !apierrors.IsNotFound(err) {
				return nil, fmt.Errorf("error checking for deployment: %v", err)
			}
		}
	}
}

// restartAgentDeployment restarts the agent deployment using kubectl rollout restart
func restartAgentDeployment(ctx context.Context, k8sClient client.Client, cfg *DeployCfg, manifest *common.AgentManifest) error {
	deploymentName := manifest.Name
	namespace := cfg.Config.Namespace

	_, err := waitForDeployment(ctx, k8sClient, namespace, deploymentName, 30*time.Second, cfg.Config)
	if err != nil {
		if apierrors.IsNotFound(err) {
			if IsVerbose(cfg.Config) {
				fmt.Printf("Deployment '%s' not found after timeout, it may still be being created by the controller\n", deploymentName)
			}
			return nil
		}
		return fmt.Errorf("failed to wait for deployment: %v", err)
	}

	kubectl := commonexec.NewKubectlExecutor(IsVerbose(cfg.Config), namespace)

	if err := kubectl.RolloutRestart(deploymentName); err != nil {
		return fmt.Errorf("failed to restart deployment: %v", err)
	}

	if err := kubectl.WaitForDeployment(deploymentName, 2*time.Minute); err != nil {
		if IsVerbose(cfg.Config) {
			fmt.Printf("Warning: failed to check rollout status: %v\n", err)
		}
	}

	return nil
}

// deployMCPServersIfNeeded deploys MCP servers if any are defined in the manifest
func deployMCPServersIfNeeded(ctx context.Context, k8sClient client.Client, cfg *DeployCfg, manifest *common.AgentManifest) error {
	if len(manifest.McpServers) == 0 {
		return nil
	}

	if IsVerbose(cfg.Config) && !cfg.DryRun {
		fmt.Printf("Deploying %d MCP server(s)...\n", len(manifest.McpServers))
	}

	if err := deployMCPServers(ctx, k8sClient, cfg, manifest); err != nil {
		return fmt.Errorf("failed to deploy MCP servers: %v", err)
	}

	return nil
}

// printDeploymentResult prints the appropriate success/dry-run message
func printDeploymentResult(cfg *DeployCfg, manifest *common.AgentManifest) {
	if !cfg.DryRun {
		fmt.Printf("\n✅ Successfully deployed agent '%s' to namespace '%s'\n", manifest.Name, cfg.Config.Namespace)
		fmt.Printf("\nTo check the deployment status:\n")
		fmt.Printf("  kubectl get agent %s -n %s\n", manifest.Name, cfg.Config.Namespace)
		fmt.Printf("  kubectl get pods -l kagent=%s -n %s\n", manifest.Name, cfg.Config.Namespace)
	}
}

// outputYAML serializes a Kubernetes object to YAML and prints it (for dry-run mode)
func outputYAML(obj client.Object) error {
	yamlBytes, err := yaml.Marshal(obj)
	if err != nil {
		return fmt.Errorf("failed to marshal object to YAML: %v", err)
	}

	fmt.Println("---")
	fmt.Print(string(yamlBytes))
	return nil
}

// getAPIKeyEnvVar returns the environment variable name for the given model provider
func getAPIKeyEnvVar(modelProvider string) string {
	switch modelProvider {
	case strings.ToLower(string(v1alpha2.ModelProviderAnthropic)):
		return "ANTHROPIC_API_KEY"
	case strings.ToLower(string(v1alpha2.ModelProviderOpenAI)):
		return "OPENAI_API_KEY"
	case strings.ToLower(string(v1alpha2.ModelProviderGemini)):
		return "GOOGLE_API_KEY"
	default:
		return ""
	}
}

// createKubernetesClient creates a Kubernetes client
func createKubernetesClient() (client.Client, error) {
	// Use the standard kubeconfig loading rules
	loadingRules := clientcmd.NewDefaultClientConfigLoadingRules()
	configOverrides := &clientcmd.ConfigOverrides{}

	clientConfig := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(loadingRules, configOverrides)

	config, err := clientConfig.ClientConfig()
	if err != nil {
		return nil, fmt.Errorf("failed to get Kubernetes config: %v", err)
	}

	schemes := runtime.NewScheme()
	if err := scheme.AddToScheme(schemes); err != nil {
		return nil, fmt.Errorf("failed to add core scheme: %v", err)
	}
	if err := v1alpha1.AddToScheme(schemes); err != nil {
		return nil, fmt.Errorf("failed to add kagent v1alpha1 scheme: %v", err)
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

// createSecret creates or updates a Kubernetes secret with the specified key-value pair
func createSecret(ctx context.Context, k8sClient client.Client, namespace, secretName, key, value string, verbose bool, dryRun bool) error {
	secret := buildSecret(namespace, secretName, key, value)

	// In dry-run mode, just output the YAML
	if dryRun {
		return outputYAML(secret)
	}
	return createOrUpdateSecret(ctx, k8sClient, secret, key, value, verbose)
}

// buildSecret constructs a Kubernetes Secret object
func buildSecret(namespace, name, key, value string) *corev1.Secret {
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Data: map[string][]byte{
			key: []byte(value),
		},
	}
	secret.SetGroupVersionKind(corev1.SchemeGroupVersion.WithKind("Secret"))
	return secret
}

// createOrUpdateSecret creates a new secret or updates an existing one
func createOrUpdateSecret(ctx context.Context, k8sClient client.Client, secret *corev1.Secret, key, value string, verbose bool) error {
	existingSecret := &corev1.Secret{}
	err := k8sClient.Get(ctx, client.ObjectKey{
		Namespace: secret.Namespace,
		Name:      secret.Name,
	}, existingSecret)

	if err != nil {
		if apierrors.IsNotFound(err) {
			// Create new secret
			if err := k8sClient.Create(ctx, secret); err != nil {
				return fmt.Errorf("failed to create secret: %v", err)
			}
			if verbose {
				fmt.Printf("Created secret '%s' in namespace '%s'\n", secret.Name, secret.Namespace)
			}
			return nil
		}
		return fmt.Errorf("failed to check if secret exists: %v", err)
	}

	// Secret exists, update it
	existingSecret.Data[key] = []byte(value)
	if err := k8sClient.Update(ctx, existingSecret); err != nil {
		return fmt.Errorf("failed to update existing secret: %v", err)
	}
	if verbose {
		fmt.Printf("Updated existing secret '%s' in namespace '%s'\n", secret.Name, secret.Namespace)
	}
	return nil
}

// createAgentCRD creates or updates the Agent CRD
func createAgentCRD(ctx context.Context, k8sClient client.Client, cfg *DeployCfg, manifest *common.AgentManifest, envData *envFileData, verbose bool) error {
	imageName := determineImageName(cfg.Image, manifest.Name)
	agent := buildAgentCRD(cfg.Config.Namespace, manifest, imageName, envData)

	// In dry-run mode, just output the YAML
	if cfg.DryRun {
		return outputYAML(agent)
	}

	// Create or update the agent
	return createOrUpdateAgent(ctx, k8sClient, agent, cfg.Config.Namespace, manifest.Name, verbose)
}

// determineImageName returns the image name to use, either from config or default
func determineImageName(configImage, agentName string) string {
	return commonimage.ConstructImageName(configImage, agentName)
}

// buildAgentCRD constructs an Agent CRD object
func buildAgentCRD(namespace string, manifest *common.AgentManifest, imageName string, envData *envFileData) *v1alpha2.Agent {
	var envVars []corev1.EnvVar

	// Add all environment variables from the env file secret
	if envData != nil {
		for _, key := range envData.EnvVarKeys {
			envVars = append(envVars, corev1.EnvVar{
				Name: key,
				ValueFrom: &corev1.EnvVarSource{
					SecretKeyRef: &corev1.SecretKeySelector{
						LocalObjectReference: corev1.LocalObjectReference{
							Name: envData.SecretName,
						},
						Key: key,
					},
				},
			})
		}
	}

	deploymentSpec := v1alpha2.ByoDeploymentSpec{
		Image: imageName,
		SharedDeploymentSpec: v1alpha2.SharedDeploymentSpec{
			Env: envVars,
		},
	}

	agent := &v1alpha2.Agent{
		ObjectMeta: metav1.ObjectMeta{
			Name:      manifest.Name,
			Namespace: namespace,
		},
		Spec: v1alpha2.AgentSpec{
			Type:        v1alpha2.AgentType_BYO,
			Description: manifest.Description,
			BYO: &v1alpha2.BYOAgentSpec{
				Deployment: &deploymentSpec,
			},
		},
	}
	agent.SetGroupVersionKind(v1alpha2.GroupVersion.WithKind("Agent"))
	return agent
}

// createOrUpdateAgent creates a new agent or updates an existing one
func createOrUpdateAgent(ctx context.Context, k8sClient client.Client, agent *v1alpha2.Agent, namespace, name string, verbose bool) error {
	existingAgent := &v1alpha2.Agent{}
	err := k8sClient.Get(ctx, client.ObjectKey{Namespace: namespace, Name: name}, existingAgent)

	if err != nil {
		if apierrors.IsNotFound(err) {
			// Agent does not exist, create it
			if err := k8sClient.Create(ctx, agent); err != nil {
				return fmt.Errorf("failed to create agent: %v", err)
			}
			if verbose {
				fmt.Printf("Created agent '%s' in namespace '%s'\n", name, namespace)
			}
			return nil
		}
		return fmt.Errorf("failed to check if agent exists: %v", err)
	}

	// Agent exists, update it
	existingAgent.Spec = agent.Spec
	if err := k8sClient.Update(ctx, existingAgent); err != nil {
		return fmt.Errorf("failed to update existing agent: %v", err)
	}
	if verbose {
		fmt.Printf("Updated existing agent '%s' in namespace '%s'\n", name, namespace)
	}
	return nil
}

// deployMCPServers deploys all MCP servers defined in the manifest
func deployMCPServers(ctx context.Context, k8sClient client.Client, cfg *DeployCfg, manifest *common.AgentManifest) error {
	verbose := IsVerbose(cfg.Config)

	for i, mcpServer := range manifest.McpServers {
		if verbose && !cfg.DryRun {
			fmt.Printf("Deploying MCP server '%s' (type: %s)...\n", mcpServer.Name, mcpServer.Type)
		}

		switch mcpServer.Type {
		case "remote":
			// Deploy RemoteMCPServer (v1alpha2)
			if err := deployRemoteMCPServer(ctx, k8sClient, cfg.Config.Namespace, &mcpServer, verbose, cfg.DryRun); err != nil {
				return fmt.Errorf("failed to deploy remote MCP server '%s': %v", mcpServer.Name, err)
			}
		case "command":
			// Deploy MCPServer (v1alpha1)
			if err := deployCommandMCPServer(ctx, k8sClient, cfg.Config.Namespace, &mcpServer, verbose, cfg.DryRun); err != nil {
				return fmt.Errorf("failed to deploy command MCP server '%s': %v", mcpServer.Name, err)
			}
		default:
			return fmt.Errorf("mcpServers[%d]: unsupported type '%s'", i, mcpServer.Type)
		}
	}

	return nil
}

// deployRemoteMCPServer creates or updates a RemoteMCPServer resource
func deployRemoteMCPServer(ctx context.Context, k8sClient client.Client, namespace string, mcpServer *common.McpServerType, verbose bool, dryRun bool) error {
	// Process headers and create necessary secrets
	headerRefs, err := createSecretsForHeaders(ctx, k8sClient, namespace, mcpServer, verbose, dryRun)
	if err != nil {
		return fmt.Errorf("failed to create secrets for headers: %v", err)
	}

	remoteMCPServer := buildRemoteMCPServer(namespace, mcpServer, headerRefs)

	if dryRun {
		return outputYAML(remoteMCPServer)
	}
	return createOrUpdateRemoteMCPServer(ctx, k8sClient, remoteMCPServer, namespace, mcpServer.Name, verbose)
}

// buildRemoteMCPServer constructs a RemoteMCPServer CRD object
func buildRemoteMCPServer(namespace string, mcpServer *common.McpServerType, headerRefs []v1alpha2.ValueRef) *v1alpha2.RemoteMCPServer {
	timeout := metav1.Duration{Duration: defaultTimeout}
	sseReadTimeout := metav1.Duration{Duration: defaultSSEReadTimeout}
	terminateOnClose := true

	remoteMCPServer := &v1alpha2.RemoteMCPServer{
		ObjectMeta: metav1.ObjectMeta{
			Name:      mcpServer.Name,
			Namespace: namespace,
		},
		Spec: v1alpha2.RemoteMCPServerSpec{
			Description:      fmt.Sprintf("Remote MCP server: %s", mcpServer.Name),
			Protocol:         v1alpha2.RemoteMCPServerProtocolStreamableHttp,
			URL:              mcpServer.URL,
			HeadersFrom:      headerRefs,
			Timeout:          &timeout,
			SseReadTimeout:   &sseReadTimeout,
			TerminateOnClose: &terminateOnClose,
		},
	}
	remoteMCPServer.SetGroupVersionKind(v1alpha2.GroupVersion.WithKind("RemoteMCPServer"))
	return remoteMCPServer
}

// createOrUpdateRemoteMCPServer creates a new RemoteMCPServer or updates an existing one
func createOrUpdateRemoteMCPServer(ctx context.Context, k8sClient client.Client, remoteMCPServer *v1alpha2.RemoteMCPServer, namespace, name string, verbose bool) error {
	existingRemoteMCPServer := &v1alpha2.RemoteMCPServer{}
	err := k8sClient.Get(ctx, client.ObjectKey{Namespace: namespace, Name: name}, existingRemoteMCPServer)

	if err != nil {
		if apierrors.IsNotFound(err) {
			// Create new RemoteMCPServer
			if err := k8sClient.Create(ctx, remoteMCPServer); err != nil {
				return fmt.Errorf("failed to create RemoteMCPServer: %v", err)
			}
			if verbose {
				fmt.Printf("Created RemoteMCPServer '%s' in namespace '%s'\n", name, namespace)
			}
			return nil
		}
		return fmt.Errorf("failed to check if RemoteMCPServer exists: %v", err)
	}

	// RemoteMCPServer exists, update it
	existingRemoteMCPServer.Spec = remoteMCPServer.Spec
	if err := k8sClient.Update(ctx, existingRemoteMCPServer); err != nil {
		return fmt.Errorf("failed to update existing RemoteMCPServer: %v", err)
	}
	if verbose {
		fmt.Printf("Updated existing RemoteMCPServer '%s' in namespace '%s'\n", name, namespace)
	}
	return nil
}

// deployCommandMCPServer creates or updates an MCPServer resource for command/stdio type
func deployCommandMCPServer(ctx context.Context, k8sClient client.Client, namespace string, mcpServer *common.McpServerType, verbose bool, dryRun bool) error {
	// Process environment variables and create necessary secrets
	envMap, secretRefs, err := createSecretsForEnv(ctx, k8sClient, namespace, mcpServer, verbose, dryRun)
	if err != nil {
		return fmt.Errorf("failed to create secrets for env vars: %v", err)
	}

	image := determineCommandMCPServerImage(mcpServer)
	mcpServerResource := buildCommandMCPServer(namespace, mcpServer, image, envMap, secretRefs)

	if dryRun {
		return outputYAML(mcpServerResource)
	}

	return createOrUpdateMCPServer(ctx, k8sClient, mcpServerResource, namespace, mcpServer.Name, verbose)
}

// determineCommandMCPServerImage returns the appropriate Docker image based on the command
func determineCommandMCPServerImage(mcpServer *common.McpServerType) string {
	if mcpServer.Image != "" {
		return mcpServer.Image
	}

	switch {
	case strings.HasPrefix(mcpServer.Command, "npx"):
		return defaultNodeImage
	case strings.HasPrefix(mcpServer.Command, "uvx"):
		return defaultUVImage
	default:
		return defaultNodeImage
	}
}

// buildCommandMCPServer constructs an MCPServer CRD object
func buildCommandMCPServer(namespace string, mcpServer *common.McpServerType, image string, envMap map[string]string, secretRefs []corev1.LocalObjectReference) *v1alpha1.MCPServer {
	mcpServerResource := &v1alpha1.MCPServer{
		ObjectMeta: metav1.ObjectMeta{
			Name:      mcpServer.Name,
			Namespace: namespace,
		},
		Spec: v1alpha1.MCPServerSpec{
			TransportType:  v1alpha1.TransportTypeStdio,
			StdioTransport: &v1alpha1.StdioTransport{},
			Deployment: v1alpha1.MCPServerDeployment{
				Image:      image,
				Port:       3000,
				Cmd:        mcpServer.Command,
				Args:       mcpServer.Args,
				Env:        envMap,
				SecretRefs: secretRefs,
			},
		},
	}
	mcpServerResource.SetGroupVersionKind(v1alpha1.GroupVersion.WithKind("MCPServer"))
	return mcpServerResource
}

// createOrUpdateMCPServer creates a new MCPServer or updates an existing one
func createOrUpdateMCPServer(ctx context.Context, k8sClient client.Client, mcpServerResource *v1alpha1.MCPServer, namespace, name string, verbose bool) error {
	existingMCPServer := &v1alpha1.MCPServer{}
	err := k8sClient.Get(ctx, client.ObjectKey{Namespace: namespace, Name: name}, existingMCPServer)

	if err != nil {
		if apierrors.IsNotFound(err) {
			// Create new MCPServer
			if err := k8sClient.Create(ctx, mcpServerResource); err != nil {
				return fmt.Errorf("failed to create MCPServer: %v", err)
			}
			if verbose {
				fmt.Printf("Created MCPServer '%s' in namespace '%s'\n", name, namespace)
			}
			return nil
		}
		return fmt.Errorf("failed to check if MCPServer exists: %v", err)
	}

	// MCPServer exists, update it
	existingMCPServer.Spec = mcpServerResource.Spec
	if err := k8sClient.Update(ctx, existingMCPServer); err != nil {
		return fmt.Errorf("failed to update existing MCPServer: %v", err)
	}
	if verbose {
		fmt.Printf("Updated existing MCPServer '%s' in namespace '%s'\n", name, namespace)
	}
	return nil
}

// createSecretsForHeaders creates secrets for header values that reference environment variables
func createSecretsForHeaders(ctx context.Context, k8sClient client.Client, namespace string, mcpServer *common.McpServerType, verbose bool, dryRun bool) ([]v1alpha2.ValueRef, error) {
	var headerRefs []v1alpha2.ValueRef
	envVarRegex := regexp.MustCompile(envVarPattern)

	for headerName, headerValue := range mcpServer.Headers {
		headerRef, err := processHeaderValue(ctx, k8sClient, namespace, mcpServer.Name, headerName, headerValue, envVarRegex, verbose, dryRun)
		if err != nil {
			return nil, err
		}
		headerRefs = append(headerRefs, headerRef)
	}

	return headerRefs, nil
}

// processHeaderValue processes a single header value and creates a secret if needed
func processHeaderValue(ctx context.Context, k8sClient client.Client, namespace, serverName, headerName, headerValue string, envVarRegex *regexp.Regexp, verbose bool, dryRun bool) (v1alpha2.ValueRef, error) {
	// Check if the header value contains environment variable references
	matches := envVarRegex.FindStringSubmatch(headerValue)
	if len(matches) == 0 {
		return v1alpha2.ValueRef{
			Name:  headerName,
			Value: headerValue,
		}, nil
	}

	envVarName := extractEnvVarName(matches)
	envValue := os.Getenv(envVarName)
	if envValue == "" {
		return v1alpha2.ValueRef{}, fmt.Errorf("environment variable '%s' referenced in header '%s' is not set", envVarName, headerName)
	}

	// Replace the environment variable reference with the actual value
	// This preserves the full header value like "Bearer ${GITHUB_TOKEN}" -> "Bearer <token>"
	fullHeaderValue := envVarRegex.ReplaceAllString(headerValue, envValue)

	// Create a secret for the full header value
	secretName := fmt.Sprintf("%s-%s", serverName, sanitizeForSecretName(headerName))
	secretKey := sanitizeForSecretKey(headerName)

	if err := createSecret(ctx, k8sClient, namespace, secretName, secretKey, fullHeaderValue, verbose, dryRun); err != nil {
		return v1alpha2.ValueRef{}, fmt.Errorf("failed to create secret for header '%s': %v", headerName, err)
	}

	// Return the header reference pointing to the secret
	return v1alpha2.ValueRef{
		Name: headerName,
		ValueFrom: &v1alpha2.ValueSource{
			Type: v1alpha2.SecretValueSource,
			Name: secretName,
			Key:  secretKey,
		},
	}, nil
}

// extractEnvVarName extracts the environment variable name from regex matches
func extractEnvVarName(matches []string) string {
	if matches[1] != "" {
		return matches[1] // ${VAR_NAME} format
	}
	return matches[2] // $VAR_NAME format
}

// sanitizeForSecretName converts a header name to a valid Kubernetes secret name
func sanitizeForSecretName(headerName string) string {
	return strings.ToLower(strings.ReplaceAll(headerName, "-", ""))
}

// sanitizeForSecretKey converts a header name to a valid secret key
func sanitizeForSecretKey(headerName string) string {
	return strings.ToLower(strings.ReplaceAll(headerName, "-", "_"))
}

// createSecretsForEnv creates secrets for environment variables and returns env map and secret refs
func createSecretsForEnv(ctx context.Context, k8sClient client.Client, namespace string, mcpServer *common.McpServerType, verbose bool, dryRun bool) (map[string]string, []corev1.LocalObjectReference, error) {
	envMap := make(map[string]string)
	secretData := make(map[string][]byte)
	envVarRegex := regexp.MustCompile(envVarPattern)

	for _, envVar := range mcpServer.Env {
		envKey, envValue, err := parseEnvVar(envVar)
		if err != nil {
			return nil, nil, err
		}

		// Check if the value references an environment variable
		matches := envVarRegex.FindStringSubmatch(envValue)
		if len(matches) > 0 {
			// Environment variable reference - needs to go into a secret
			actualValue, err := resolveEnvVarReference(matches, envKey)
			if err != nil {
				return nil, nil, err
			}
			secretData[strings.ToLower(envKey)] = []byte(actualValue)
		} else {
			// Static value, add to env map
			envMap[envKey] = envValue
		}
	}

	var secretRefs []corev1.LocalObjectReference
	if len(secretData) > 0 {
		secretName := fmt.Sprintf("%s-env", mcpServer.Name)

		if dryRun {
			if err := outputEnvSecret(namespace, secretName, secretData); err != nil {
				return nil, nil, err
			}
		} else {
			if err := createOrUpdateEnvSecret(ctx, k8sClient, namespace, secretName, secretData, verbose); err != nil {
				return nil, nil, err
			}
		}

		secretRefs = append(secretRefs, corev1.LocalObjectReference{Name: secretName})
	}

	return envMap, secretRefs, nil
}

// parseEnvVar parses an environment variable in KEY=VALUE format
func parseEnvVar(envVar string) (key, value string, err error) {
	parts := strings.SplitN(envVar, "=", 2)
	if len(parts) != 2 {
		return "", "", fmt.Errorf("invalid env var format '%s', expected KEY=VALUE", envVar)
	}
	return parts[0], parts[1], nil
}

// resolveEnvVarReference resolves an environment variable reference and returns its actual value
func resolveEnvVarReference(matches []string, targetEnvKey string) (string, error) {
	refEnvVarName := extractEnvVarName(matches)
	actualValue := os.Getenv(refEnvVarName)
	if actualValue == "" {
		return "", fmt.Errorf("environment variable '%s' referenced in env var '%s' is not set", refEnvVarName, targetEnvKey)
	}
	return actualValue, nil
}

// outputEnvSecret outputs a secret containing environment variables (for dry-run mode)
func outputEnvSecret(namespace, secretName string, secretData map[string][]byte) error {
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      secretName,
			Namespace: namespace,
		},
		Data: secretData,
	}
	secret.SetGroupVersionKind(corev1.SchemeGroupVersion.WithKind("Secret"))
	if err := outputYAML(secret); err != nil {
		return fmt.Errorf("failed to output secret YAML: %v", err)
	}
	return nil
}

// createOrUpdateEnvSecret creates or updates a secret containing multiple environment variables
func createOrUpdateEnvSecret(ctx context.Context, k8sClient client.Client, namespace, secretName string, secretData map[string][]byte, verbose bool) error {
	existingSecret := &corev1.Secret{}
	err := k8sClient.Get(ctx, client.ObjectKey{Namespace: namespace, Name: secretName}, existingSecret)

	if err != nil {
		if apierrors.IsNotFound(err) {
			// Secret doesn't exist, create it with all data
			secret := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      secretName,
					Namespace: namespace,
				},
				Data: secretData,
			}
			secret.SetGroupVersionKind(corev1.SchemeGroupVersion.WithKind("Secret"))

			if err := k8sClient.Create(ctx, secret); err != nil {
				return fmt.Errorf("failed to create env secret: %v", err)
			}
			if verbose {
				fmt.Printf("Created env secret '%s' in namespace '%s'\n", secretName, namespace)
			}
			return nil
		}
		return fmt.Errorf("failed to get existing secret: %v", err)
	}

	// Secret exists, merge the new data with existing data
	maps.Copy(existingSecret.Data, secretData)

	if err := k8sClient.Update(ctx, existingSecret); err != nil {
		return fmt.Errorf("failed to update existing secret: %v", err)
	}
	if verbose {
		fmt.Printf("Updated env secret '%s' in namespace '%s'\n", secretName, namespace)
	}
	return nil
}
