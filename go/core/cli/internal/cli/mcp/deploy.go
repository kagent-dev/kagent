package mcp

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/yaml"

	commonexec "github.com/kagent-dev/kagent/go/core/cli/internal/common/exec"
	commonk8s "github.com/kagent-dev/kagent/go/core/cli/internal/common/k8s"
	"github.com/kagent-dev/kagent/go/core/cli/internal/config"
	"github.com/kagent-dev/kagent/go/core/cli/internal/mcp/manifests"
	"github.com/kagent-dev/kmcp/api/v1alpha1"
)

const (
	transportHTTP  = "http"
	transportStdio = "stdio"
)

// DeployCfg contains configuration for MCP deploy command
type DeployCfg struct {
	Namespace   string
	DryRun      bool
	Output      string
	Image       string
	Transport   string
	Port        int
	Command     string
	Args        []string
	Env         []string
	Force       bool
	File        string
	Environment string
	NoInspector bool
	// Package subcommand specific
	PackageManager string
	PackageName    string
	PackageSecrets []string
}

var DeployCmd = &cobra.Command{
	Use:   "deploy",
	Short: "Deploy MCP server to Kubernetes",
	Long: `Deploy an MCP server to Kubernetes by generating MCPServer CRDs.

This command generates MCPServer Custom Resource Definitions (CRDs) based on:
- Project configuration from manifest.yaml
- Docker image built with 'kagent mcp build --docker'
- Deployment configuration options

The generated MCPServer will include:
- Docker image reference from the build
- Transport configuration (stdio/http)
- Port and command configuration
- Environment variables and secrets

The command can also apply Kubernetes secret YAML files to the cluster before deploying the MCPServer.
The secrets will be referenced in the MCPServer CRD for mounting as volumes to the MCP server container.
Secret namespace will be overridden with the deployment namespace to avoid the need for reference grants
to enable cross-namespace references.

Examples:
  kagent mcp deploy                               # Deploy with project name to cluster
  kagent mcp deploy my-server                     # Deploy with custom name
  kagent mcp deploy --namespace staging           # Deploy to staging namespace
  kagent mcp deploy --dry-run                     # Generate manifest without applying to cluster
  kagent mcp deploy --image custom:tag            # Use custom image
  kagent mcp deploy --transport http              # Use HTTP transport
  kagent mcp deploy --output deploy.yaml          # Save to file
  kagent mcp deploy --file /path/to/manifest.yaml # Use custom manifest.yaml file
  kagent mcp deploy --environment staging         # Target environment for deployment (e.g., staging, production)`,
	Args: cobra.MaximumNArgs(1),
	RunE: runDeployMCP,
}

var deployCfg = &DeployCfg{}

func init() {
	// Get current namespace from kubeconfig
	currentNamespace, err := commonk8s.GetCurrentNamespace()
	if err != nil {
		// Fallback to default if unable to get current namespace
		currentNamespace = "default"
	}

	// MCP deployment flags
	DeployCmd.Flags().StringVarP(&deployCfg.Namespace, "namespace", "n", currentNamespace, "Kubernetes namespace")
	DeployCmd.Flags().BoolVar(&deployCfg.DryRun, "dry-run", false, "Generate manifest without applying to cluster")
	DeployCmd.Flags().StringVarP(&deployCfg.Output, "output", "", "", "Output file for the generated YAML")
	DeployCmd.Flags().StringVar(&deployCfg.Image, "image", "", "Docker image to deploy (overrides build image)")
	DeployCmd.Flags().StringVar(&deployCfg.Transport, "transport", "", "Transport type (stdio, http)")
	DeployCmd.Flags().IntVar(&deployCfg.Port, "port", 0, "Container port (default: from project config)")
	DeployCmd.Flags().StringVar(&deployCfg.Command, "command", "", "Command to run (overrides project config)")
	DeployCmd.Flags().StringSliceVar(&deployCfg.Args, "args", []string{}, "Command arguments")
	DeployCmd.Flags().StringSliceVar(&deployCfg.Env, "env", []string{}, "Environment variables (KEY=VALUE)")
	DeployCmd.Flags().BoolVar(&deployCfg.Force, "force", false, "Force deployment even if validation fails")
	DeployCmd.Flags().StringVarP(&deployCfg.File, "file", "f", "", "Path to manifest.yaml file (default: current directory)")
	DeployCmd.Flags().BoolVar(&deployCfg.NoInspector, "no-inspector", true, "Do not start the MCP inspector after deployment")
	DeployCmd.Flags().StringVar(
		&deployCfg.Environment,
		"environment",
		"staging",
		"Target environment for deployment (e.g., staging, production)",
	)

	// Add package subcommand
	DeployCmd.AddCommand(packageDeployCmd)
}

// package subcommand - supports both npm and uvx
var packageDeployCmd = &cobra.Command{
	Use:   "package",
	Short: "Deploy an MCP server using a package manager (npx, uvx)",
	Long: `Deploy an MCP server using a package manager to run Model Context Protocol servers.

This subcommand creates an MCPServer Custom Resource Definition (CRD) that runs
an MCP server using npx (for npm packages) or uvx (for Python packages).

The deployment name, manager, and args are required. The package manager must be either 'npx' or 'uvx'.

Examples:
  kagent mcp deploy package --deployment-name github-server --manager npx --args @modelcontextprotocol/server-github                             # Deploy GitHub MCP server
  kagent mcp deploy package --deployment-name github-server --manager npx --args @modelcontextprotocol/server-github  --dry-run                  # Print YAML without deploying
  kagent mcp deploy package --deployment-name my-server --manager npx --args my-package --env "KEY1=value1,KEY2=value2"                          # Set environment variables
  kagent mcp deploy package --deployment-name github-server --manager npx --args @modelcontextprotocol/server-github  --secrets secret1,secret2  # Mount Kubernetes secrets
  kagent mcp deploy package --deployment-name my-server --manager npx --args my-package --no-inspector                                           # Deploy without starting inspector
  kagent mcp deploy package --deployment-name my-server --manager uvx --args mcp-server-git                                                      # Use UV and write managed tools and installables to /tmp directories`,
	Args: cobra.NoArgs,
	RunE: runPackageDeploy,
}

func init() {
	// package subcommand flags
	packageDeployCmd.Flags().StringVar(&deployCfg.PackageName, "deployment-name", "", "Name for the deployment (required)")
	packageDeployCmd.Flags().StringVar(&deployCfg.PackageManager, "manager", "", "Package manager to use (npx or uvx) (required)")
	packageDeployCmd.Flags().StringSliceVar(&deployCfg.PackageSecrets, "secrets", []string{}, "List of Kubernetes secret names to mount")

	// Add common deployment flags
	packageDeployCmd.Flags().StringSliceVar(&deployCfg.Args, "args", []string{}, "Arguments to pass to the package manager (e.g., package names) (required)")
	packageDeployCmd.Flags().StringSliceVar(&deployCfg.Env, "env", []string{}, "Environment variables (KEY=VALUE)")
	packageDeployCmd.Flags().BoolVar(&deployCfg.DryRun, "dry-run", false, "Generate manifest without applying to cluster")
	packageDeployCmd.Flags().StringVarP(&deployCfg.Namespace, "namespace", "n", "", "Kubernetes namespace")
	packageDeployCmd.Flags().StringVar(&deployCfg.Image, "image", "", "Docker image to deploy (overrides default)")
	packageDeployCmd.Flags().StringVar(&deployCfg.Transport, "transport", "", "Transport type (stdio, http)")
	packageDeployCmd.Flags().IntVar(&deployCfg.Port, "port", 0, "Container port (default: 3000)")
	packageDeployCmd.Flags().BoolVar(&deployCfg.NoInspector, "no-inspector", true, "Do not start the MCP inspector after deployment")
	packageDeployCmd.Flags().StringVarP(&deployCfg.Output, "output", "", "", "Output file for the generated YAML")

	// Mark required flags
	_ = packageDeployCmd.MarkFlagRequired("deployment-name")
	_ = packageDeployCmd.MarkFlagRequired("manager")
	_ = packageDeployCmd.MarkFlagRequired("args")
}

func runPackageDeploy(_ *cobra.Command, args []string) error {
	// Validate package manager
	if deployCfg.PackageManager != "npx" && deployCfg.PackageManager != "uvx" {
		return fmt.Errorf("unsupported package manager: %s (must be 'npx' or 'uvx')", deployCfg.PackageManager)
	}

	// Validate args
	if len(deployCfg.Args) == 0 {
		return fmt.Errorf("args are required (e.g., --args package-name)")
	}

	// Parse environment variables
	envMap := parseEnvVars(deployCfg.Env)

	// Convert secret names to ObjectReferences
	secretRefs := make([]corev1.LocalObjectReference, 0, len(deployCfg.PackageSecrets))
	for _, secretName := range deployCfg.PackageSecrets {
		secretRefs = append(secretRefs, corev1.LocalObjectReference{
			Name: secretName,
		})
	}

	// Set default port if none specified
	port := 3000
	if deployCfg.Port != 0 {
		port = deployCfg.Port
	}

	// Create MCPServer for package deployment
	mcpServer := &v1alpha1.MCPServer{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "kagent.dev/v1alpha1",
			Kind:       "MCPServer",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      deployCfg.PackageName,
			Namespace: deployCfg.Namespace,
		},
		Spec: v1alpha1.MCPServerSpec{
			Deployment: v1alpha1.MCPServerDeployment{
				Image:      deployCfg.Image,
				Port:       uint16(port),
				Cmd:        deployCfg.PackageManager,
				Args:       deployCfg.Args,
				Env:        envMap,
				SecretRefs: secretRefs,
			},
			TransportType: getTransportType(),
		},
	}

	// Configure transport-specific settings
	if mcpServer.Spec.TransportType == v1alpha1.TransportTypeHTTP {
		mcpServer.Spec.HTTPTransport = &v1alpha1.HTTPTransport{
			TargetPort: uint32(port),
			TargetPath: "/mcp",
		}
	} else {
		mcpServer.Spec.StdioTransport = &v1alpha1.StdioTransport{}
	}

	// Convert MCPServer to YAML
	mcpServerYAML, err := yaml.Marshal(mcpServer)
	if err != nil {
		return fmt.Errorf("failed to marshal MCPServer to YAML: %w", err)
	}

	// Create YAML content with header
	yamlContent := fmt.Sprintf(
		"# MCPServer deployment generated by kagent mcp deploy package cmd\n# Deployment: %s\n# Manager: %s\n# Args: %v\n%s",
		deployCfg.PackageName,
		deployCfg.PackageManager,
		deployCfg.Args,
		string(mcpServerYAML),
	)

	// Handle output
	if deployCfg.Output != "" {
		// Write to file
		if err := os.WriteFile(deployCfg.Output, []byte(yamlContent), 0644); err != nil {
			return fmt.Errorf("failed to write to file: %w", err)
		}
		fmt.Printf("✅ MCPServer manifest written to: %s\n", deployCfg.Output)
	}

	if deployCfg.DryRun {
		// Print to stdout
		fmt.Print(yamlContent)
	} else {
		// Apply MCPServer to cluster
		if err := applyToCluster("", yamlContent, mcpServer); err != nil {
			return fmt.Errorf("failed to apply to cluster: %w", err)
		}
	}

	return nil
}

// getTransportType determines the transport type based on flags
func getTransportType() v1alpha1.TransportType {
	if deployCfg.Transport != "" {
		switch deployCfg.Transport {
		case transportHTTP:
			return v1alpha1.TransportTypeHTTP
		case transportStdio:
			return v1alpha1.TransportTypeStdio
		default:
			// Default to stdio for package deployments
			return v1alpha1.TransportTypeStdio
		}
	}
	// Default to stdio for package deployments
	return v1alpha1.TransportTypeStdio
}

func runDeployMCP(_ *cobra.Command, args []string) error {
	// Determine project directory
	var projectDir string
	var err error

	cfg, err := config.Get()
	if err != nil {
		return fmt.Errorf("failed to get config: %w", err)
	}

	if deployCfg.File != "" {
		// Use specified file path
		projectDir, err = getProjectDirFromFile(deployCfg.File)
		if err != nil {
			return fmt.Errorf("failed to get project directory from file: %w", err)
		}
	} else {
		// Use current working directory
		projectDir, err = os.Getwd()
		if err != nil {
			return fmt.Errorf("failed to get current directory: %w", err)
		}
	}

	// Load project manifest
	manifestManager := manifests.NewManager(projectDir)
	if !manifestManager.Exists() {
		return fmt.Errorf("manifest.yaml not found in %s. Run 'kagent mcp init' first or specify a valid path with --file", projectDir)
	}

	projectManifest, err := manifestManager.Load()
	if err != nil {
		return fmt.Errorf("failed to load project manifest: %w", err)
	}

	// Determine deployment name
	deploymentName := projectManifest.Name
	if len(args) > 0 {
		deploymentName = args[0]
	}

	// Generate MCPServer resource
	mcpServer, err := generateMCPServer(projectManifest, deploymentName, deployCfg.Environment)
	if err != nil {
		return fmt.Errorf("failed to generate MCPServer: %w", err)
	}

	// Set namespace
	mcpServer.Namespace = deployCfg.Namespace

	if cfg.Verbose {
		fmt.Printf("Generated MCPServer: %s/%s\n", mcpServer.Namespace, mcpServer.Name)
	}

	// Convert MCPServer to YAML
	mcpServerYAML, err := yaml.Marshal(mcpServer)
	if err != nil {
		return fmt.Errorf("failed to marshal MCPServer to YAML: %w", err)
	}

	// Create YAML content with header
	yamlContent := fmt.Sprintf(
		"# MCPServer deployment generated by kagent mcp deploy cmd\n# Project: %s\n# Framework: %s\n%s",
		projectManifest.Name,
		projectManifest.Framework,
		string(mcpServerYAML),
	)

	// Handle output
	if deployCfg.Output != "" {
		// Write to file
		if err := os.WriteFile(deployCfg.Output, []byte(yamlContent), 0644); err != nil {
			return fmt.Errorf("failed to write to file: %w", err)
		}
		fmt.Printf("✅ MCPServer manifest written to: %s\n", deployCfg.Output)
	}

	if deployCfg.DryRun {
		// Print to stdout
		fmt.Print(yamlContent)
	} else {
		// Apply MCPServer to cluster
		if err := applyToCluster(projectDir, yamlContent, mcpServer); err != nil {
			return fmt.Errorf("failed to apply to cluster: %w", err)
		}
	}

	return nil
}

// getProjectDirFromFile extracts the project directory from a file path
func getProjectDirFromFile(filePath string) (string, error) {
	// Get absolute path of the file
	absPath, err := filepath.Abs(filePath)
	if err != nil {
		return "", fmt.Errorf("failed to get absolute path: %w", err)
	}

	// Get the directory containing the file
	projectDir := filepath.Dir(absPath)

	// Verify the file exists
	if _, err := os.Stat(absPath); os.IsNotExist(err) {
		return "", fmt.Errorf("file does not exist: %s", absPath)
	}

	return projectDir, nil
}

func generateMCPServer(
	projectManifest *manifests.ProjectManifest,
	deploymentName,
	environment string,
) (*v1alpha1.MCPServer, error) {
	// Determine image name
	imageName := deployCfg.Image
	if imageName == "" {
		// Generate default image name
		imageName = fmt.Sprintf("%s:%s",
			strings.ToLower(strings.ReplaceAll(projectManifest.Name, "_", "-")),
			projectManifest.Version,
		)
	}

	// Determine transport type
	transportType := getTransportType()

	// Determine port
	port := deployCfg.Port
	if port == 0 {
		port = 3000 // Default port
	}

	// Determine command and args
	command := deployCfg.Command
	args := deployCfg.Args
	if command == "" {
		// Set default command based on framework
		command = getDefaultCommand(projectManifest.Framework)
		if len(args) == 0 {
			args = getDefaultArgs(projectManifest.Framework, port)
		}
	}

	// Parse environment variables
	envVars := parseEnvVars(deployCfg.Env)

	// Get secret reference from manifest for the specified environment
	secretRef, err := getSecretRefFromManifest(projectManifest, environment)
	if err != nil {
		return nil, fmt.Errorf("failed to get secret reference: %w", err)
	}
	var secretRefs []corev1.LocalObjectReference
	if secretRef != nil {
		secretRefs = append(secretRefs, *secretRef)
	}

	// Create MCPServer spec
	mcpServer := &v1alpha1.MCPServer{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "kagent.dev/v1alpha1",
			Kind:       "MCPServer",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: deploymentName,
			Labels: map[string]string{
				"app.kubernetes.io/name":       deploymentName,
				"app.kubernetes.io/instance":   deploymentName,
				"app.kubernetes.io/component":  "mcp-server",
				"app.kubernetes.io/part-of":    "kagent",
				"app.kubernetes.io/managed-by": "kagent",
				"kagent.dev/framework":         projectManifest.Framework,
				"kagent.dev/version":           sanitizeLabelValue(projectManifest.Version),
			},
			Annotations: map[string]string{
				"kagent.dev/project-name": projectManifest.Name,
				"kagent.dev/description":  projectManifest.Description,
			},
		},
		Spec: v1alpha1.MCPServerSpec{
			Deployment: v1alpha1.MCPServerDeployment{
				Image:      imageName,
				Port:       uint16(port),
				Cmd:        command,
				Args:       args,
				Env:        envVars,
				SecretRefs: secretRefs,
			},
			TransportType: transportType,
		},
	}

	// Configure transport-specific settings
	if transportType == v1alpha1.TransportTypeHTTP {
		mcpServer.Spec.HTTPTransport = &v1alpha1.HTTPTransport{
			TargetPort: uint32(port),
			TargetPath: "/mcp",
		}
	} else {
		mcpServer.Spec.StdioTransport = &v1alpha1.StdioTransport{}
	}

	return mcpServer, nil
}

func getSecretRefFromManifest(
	projectManifest *manifests.ProjectManifest,
	environment string,
) (*corev1.LocalObjectReference, error) {
	if environment == "" {
		return nil, nil // No environment specified
	}

	secretProvider, ok := projectManifest.Secrets[environment]
	if !ok {
		return nil, fmt.Errorf("environment '%s' not found in secrets config", environment)
	}

	if secretProvider.Provider == manifests.SecretProviderKubernetes && secretProvider.Enabled {
		secretName := secretProvider.SecretName
		if secretName == "" {
			return nil, fmt.Errorf("secretName not found in secret provider config for environment %s", environment)
		}

		return &corev1.LocalObjectReference{
			Name: secretName,
		}, nil
	}

	return nil, nil
}

func sanitizeLabelValue(value string) string {
	return strings.ReplaceAll(value, "+", "_")
}

func getDefaultCommand(framework string) string {
	switch framework {
	case manifests.FrameworkFastMCPPython:
		return "python"
	case manifests.FrameworkMCPGo:
		return "./server"
	case manifests.FrameworkTypeScript:
		return "node"
	case manifests.FrameworkJava:
		return "java"
	default:
		return "python"
	}
}

func getDefaultArgs(framework string, targetPort int) []string {
	switch framework {
	case manifests.FrameworkFastMCPPython:
		if deployCfg.Transport == transportHTTP {
			return []string{"src/main.py", "--transport", "http", "--host", "0.0.0.0", "--port", fmt.Sprintf("%d", targetPort)}
		}
		return []string{"src/main.py"}
	case manifests.FrameworkMCPGo:
		return []string{}
	case manifests.FrameworkTypeScript:
		return []string{"dist/index.js"}
	case manifests.FrameworkJava:
		if deployCfg.Transport == transportHTTP {
			return []string{"-jar", "app.jar", "--transport", "http", "--host", "0.0.0.0", "--port", fmt.Sprintf("%d", targetPort)}
		}
		return []string{"-jar", "app.jar"}
	default:
		return []string{"src/main.py"}
	}
}

func parseEnvVars(envVars []string) map[string]string {
	result := make(map[string]string)
	for _, env := range envVars {
		parts := strings.SplitN(env, "=", 2)
		if len(parts) == 2 {
			result[parts[0]] = parts[1]
		}
	}
	return result
}

func applyToCluster(projectDir, yamlContent string, mcpServer *v1alpha1.MCPServer) error {
	cfg, err := config.Get()
	if err != nil {
		return fmt.Errorf("failed to get config: %w", err)
	}

	// Create kubectl executor with namespace and verbose settings
	kubectl := commonexec.NewKubectlExecutor(cfg.Verbose, mcpServer.Namespace)

	fmt.Printf("🚀 Applying MCPServer to cluster...\n")

	if err := kubectl.Apply([]byte(yamlContent)); err != nil {
		return err
	}

	fmt.Printf("✅ MCPServer '%s' applied successfully\n", mcpServer.Name)

	// Wait for the deployment to be ready
	fmt.Printf("⌛ Waiting for deployment '%s' to be ready...\n", mcpServer.Name)
	if err := kubectl.WaitForDeployment(mcpServer.Name, 2*time.Minute); err != nil {
		return fmt.Errorf("deployment not ready: %w", err)
	}

	fmt.Printf("✅ Deployment '%s' is ready.\n", mcpServer.Name)
	fmt.Printf("💡 Check status with: kubectl get mcpserver %s -n %s\n", mcpServer.Name, mcpServer.Namespace)
	fmt.Printf("💡 View logs with: kubectl logs -l app.kubernetes.io/name=%s -n %s\n", mcpServer.Name, mcpServer.Namespace)
	if mcpServer.Spec.Deployment.Port != 0 {
		fmt.Printf("💡 Port-forward to the service with: "+
			"kubectl port-forward service/%s %d:%d -n %s\n",
			mcpServer.Name, mcpServer.Spec.Deployment.Port,
			mcpServer.Spec.Deployment.Port, mcpServer.Namespace)
	}

	var configPath string
	if !deployCfg.NoInspector {
		// Create inspector config
		port := uint16(3000) // default port
		if mcpServer.Spec.Deployment.Port != 0 {
			port = mcpServer.Spec.Deployment.Port
		}
		serverConfig := map[string]any{
			"type": "streamable-http",
			"url":  fmt.Sprintf("http://localhost:%d/mcp", port),
		}
		configPath = filepath.Join(projectDir, "mcp-server-config.json")
		if err := createMCPInspectorConfig(mcpServer.Name, serverConfig, configPath); err != nil {
			return fmt.Errorf("failed to create inspector config: %w", err)
		}

		if err := runInspector(mcpServer, configPath, projectDir); err != nil {
			return fmt.Errorf("failed to run inspector: %w", err)
		}
	}
	return nil
}

func runInspector(mcpServer *v1alpha1.MCPServer, configPath string, projectDir string) error {
	// Check if npx is installed
	if err := checkNpxInstalled(); err != nil {
		return err
	}

	// Start port forwarding in the background
	portForwardCmd, err := runPortForward(mcpServer)
	if err != nil {
		return err
	}
	defer func() {
		if portForwardCmd != nil && portForwardCmd.Process != nil {
			if err := portForwardCmd.Process.Kill(); err != nil {
				fmt.Printf("failed to kill port-forward process: %v\n", err)
			}
		}
	}()

	// Run the inspector
	return runMCPInspector(configPath, mcpServer.Name, projectDir)
}

func runPortForward(mcpServer *v1alpha1.MCPServer) (*exec.Cmd, error) {
	remotePort := mcpServer.Spec.Deployment.Port
	if remotePort == 0 {
		remotePort = 3000 // Default port
	}
	localPort := 3000
	portMapping := fmt.Sprintf("%d:%d", localPort, remotePort)
	args := []string{
		"port-forward",
		"service/" + mcpServer.Name,
		portMapping,
		"-n", mcpServer.Namespace,
	}
	cmd := exec.Command("kubectl", args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("failed to start port-forward: %w", err)
	}
	return cmd, nil
}
