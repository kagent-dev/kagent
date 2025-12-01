package cli

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	commonexec "github.com/kagent-dev/kagent/go/cli/internal/common/exec"
	"github.com/kagent-dev/kagent/go/cli/internal/config"
	"github.com/kagent-dev/kagent/go/cli/internal/tui"
	a2aclient "trpc.group/trpc-go/trpc-a2a-go/client"
	"trpc.group/trpc-go/trpc-a2a-go/protocol"
)

type RunCfg struct {
	ProjectDir string
	Config     *config.Config
	Build	   bool	
}

// RunCmd starts docker-compose in the background and launches a chat session with the local agent
func RunCmd(ctx context.Context, cfg *RunCfg) error {
	// Validate project directory
	if cfg.ProjectDir == "" {
		return fmt.Errorf("project directory is required")
	}

	// Check if project directory exists
	if _, err := os.Stat(cfg.ProjectDir); os.IsNotExist(err) {
		return fmt.Errorf("project directory does not exist: %s", cfg.ProjectDir)
	}

	// Check if docker-compose.yaml exists
	dockerComposePath := filepath.Join(cfg.ProjectDir, "docker-compose.yaml")
	if _, err := os.Stat(dockerComposePath); os.IsNotExist(err) {
		return fmt.Errorf("docker-compose.yaml not found in project directory: %s", dockerComposePath)
	}

	// Load manifest to get agent name
	manifest, err := LoadManifest(cfg.ProjectDir)
	if err != nil {
		return fmt.Errorf("failed to load kagent.yaml: %v", err)
	}

	// Validate API key before starting docker-compose
	if err := ValidateAPIKey(manifest.ModelProvider); err != nil {
		return fmt.Errorf("API key validation failed: %v", err)
	}

	verbose := IsVerbose(cfg.Config)

	fmt.Printf("Starting agent and tools...\n")

	// Use docker compose (newer version) or docker-compose (older version)
	composeCmd := commonexec.GetComposeCommand()
	args := append(composeCmd[1:], "up", "-d", "--remove-orphans")
	cmd := exec.CommandContext(ctx, composeCmd[0], args...)
	cmd.Dir = cfg.ProjectDir

	// Suppress output to not block
	if !verbose {
		cmd.Stdout = nil
		cmd.Stderr = nil
	} else {
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
	}

	// Run docker-compose up -d synchronously (should be quick without --wait)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to start docker-compose: %v", err)
	}

	fmt.Println("✓ Docker containers started")

	// Verify containers are actually running
	time.Sleep(2 * time.Second) // Give containers a moment to start
	psCmd := exec.Command(composeCmd[0], append(composeCmd[1:], "ps")...)
	psCmd.Dir = cfg.ProjectDir
	psOutput, _ := psCmd.CombinedOutput()

	if verbose {
		fmt.Printf("Container status:\n%s\n", string(psOutput))
	}

	fmt.Println("Waiting for agent to be ready...")

	// Wait for the agent to be ready by polling the health endpoint
	agentURL := "http://localhost:8080"
	healthURL := agentURL + "/health"
	if err := waitForAgent(ctx, healthURL, 60*time.Second); err != nil {
		// Print container logs if agent fails to start
		fmt.Fprintln(os.Stderr, "Agent failed to start. Fetching logs...")
		logsCmd := exec.Command(composeCmd[0], append(composeCmd[1:], "logs", "--tail=50")...)
		logsCmd.Dir = cfg.ProjectDir
		logsOutput, _ := logsCmd.CombinedOutput()
		fmt.Fprintf(os.Stderr, "Container logs:\n%s\n", string(logsOutput))
		return fmt.Errorf("agent failed to start: %v", err)
	}

	fmt.Printf("✓ Agent '%s' is running at %s\n", manifest.Name, agentURL)
	fmt.Println("Launching chat interface...")

	// Generate a new session ID
	sessionID := protocol.GenerateContextID()

	// Create A2A client for local agent
	a2aClient, err := a2aclient.NewA2AClient(agentURL, a2aclient.WithTimeout(cfg.Config.Timeout))
	if err != nil {
		return fmt.Errorf("failed to create A2A client: %v", err)
	}

	sendFn := func(ctx context.Context, params protocol.SendMessageParams) (<-chan protocol.StreamingMessageEvent, error) {
		ch, err := a2aClient.StreamMessage(ctx, params)
		if err != nil {
			return nil, err
		}
		return ch, err
	}

	// Launch TUI chat directly
	if err := tui.RunChat(manifest.Name, sessionID, sendFn, verbose); err != nil {
		return fmt.Errorf("chat session failed: %v", err)
	}

	// Automatically stop docker-compose when chat ends
	fmt.Println("\nStopping docker-compose...")
	composeCmdStop := commonexec.GetComposeCommand()
	stopCmd := exec.Command(composeCmdStop[0], append(composeCmdStop[1:], "down")...)
	stopCmd.Dir = cfg.ProjectDir

	if verbose {
		stopCmd.Stdout = os.Stdout
		stopCmd.Stderr = os.Stderr
	}

	if err := stopCmd.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: failed to stop docker-compose: %v\n", err)
	} else {
		fmt.Println("✓ Stopped docker-compose")
	}

	return nil
}

// waitForAgent polls the agent's root endpoint until it's ready or timeout
func waitForAgent(ctx context.Context, agentURL string, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	client := &http.Client{
		Timeout: 2 * time.Second,
	}

	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	fmt.Print("Checking agent health")
	for {
		select {
		case <-ctx.Done():
			fmt.Println()
			return fmt.Errorf("timeout waiting for agent to be ready")
		case <-ticker.C:
			fmt.Print(".")
			req, err := http.NewRequestWithContext(ctx, "GET", agentURL, nil)
			if err != nil {
				continue
			}

			resp, err := client.Do(req)
			if err == nil {
				if err = resp.Body.Close(); err != nil {
					return err
				}

				if resp.StatusCode == 200 {
					fmt.Println(" ✓")
					return nil
				}
			}
		}
	}
}
