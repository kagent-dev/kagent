package cli

import (
	"context"
	"fmt"
	"os"
	"os/exec"

	autogen_client "github.com/kagent-dev/kagent/go/autogen/client"
	"github.com/kagent-dev/kagent/go/cli/internal/config"
)

func CheckServerConnection(client *autogen_client.Client) error {
	// Only check if we have a valid client
	if client == nil {
		return fmt.Errorf("Error connecting to server. Please run 'install' command first.")
	}

	_, err := client.GetVersion()
	if err != nil {
		return fmt.Errorf("Error connecting to server. Please run 'install' command first.")
	}
	return nil
}

type portForward struct {
	cmd    *exec.Cmd
	cancel context.CancelFunc
}

func newPortForward(ctx context.Context, cfg *config.Config) *portForward {
	ctx, cancel := context.WithCancel(ctx)
	cmd := exec.CommandContext(ctx, "kubectl", "-n", "kagent", "port-forward", "service/kagent", "8081:8081")
	// Error connecting to server, port-forward the server
	go func() {
		if err := cmd.Start(); err != nil {
			fmt.Fprintf(os.Stderr, "Error starting port-forward: %v\n", err)
			os.Exit(1)
		}
	}()
	return &portForward{
		cmd:    cmd,
		cancel: cancel,
	}
}

func (p *portForward) Stop() {
	p.cancel()
	if err := p.cmd.Wait(); err != nil {
		fmt.Fprintf(os.Stderr, "Error waiting for port-forward to exit: %v\n", err)
	}
}
