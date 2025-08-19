package cli

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"time"

	"github.com/kagent-dev/kagent/go/cli/internal/config"
	"github.com/kagent-dev/kagent/go/pkg/client"
	"trpc.group/trpc-go/trpc-a2a-go/protocol"
)

func CheckServerConnection(client *client.ClientSet) error {
	// Only check if we have a valid client
	if client == nil {
		return fmt.Errorf("Error connecting to server. Please run 'install' command first.")
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second*5)
	defer cancel()
	_, err := client.Version.GetVersion(ctx)
	if err != nil {
		return fmt.Errorf("Error connecting to server. Please run 'install' command first.")
	}
	return nil
}

type PortForward struct {
	cmd    *exec.Cmd
	cancel context.CancelFunc
}

func NewPortForward(ctx context.Context, cfg *config.Config) (*PortForward, error) {
	ctx, cancel := context.WithCancel(ctx)
	cmd := exec.CommandContext(ctx, "kubectl", "-n", cfg.Namespace, "port-forward", "service/kagent-controller", "8083:8083")

	go func() {
		if err := cmd.Start(); err != nil {
			fmt.Fprintf(os.Stderr, "Error starting port-forward: %v\n", err)
			os.Exit(1)
		}
	}()

	client := client.New(cfg.KAgentURL)
	success := false
	maxRetries := 10 // 10 retries @ 100->500ms intervals, ~3s total
	var err error
	for i := 0; i < maxRetries; i++ {
		if serverErr := CheckServerConnection(client); serverErr == nil {
			// Connection successful, port-forward is working
			err = nil
			success = true
			break
		} else {
			err = serverErr
		}

		// Exponential backoff plateauing at 500ms
		// 100ms, 150ms, 200ms, 250ms, 300ms, 350ms, 400ms, 450ms, 500ms...
		sleepDuration := time.Duration(100+i*50) * time.Millisecond
		if sleepDuration > 500*time.Millisecond {
			sleepDuration = 500 * time.Millisecond
		}
		time.Sleep(sleepDuration)
	}

	if !success {
		cancel()
		return nil, fmt.Errorf("failed to establish connection to kagent-controller. %v", err)
	}

	return &PortForward{
		cmd:    cmd,
		cancel: cancel,
	}, nil
}

func (p *PortForward) Stop() {
	p.cancel()
	// This will terminate the kubectl process in case the cancel does not work.
	if p.cmd.Process != nil {
		p.cmd.Process.Kill()
	}

	// Don't wait for the process - just cancel the context and let it die
	// The kubectl process will terminate when the context is canceled
}

func StreamA2AEvents(ch <-chan protocol.StreamingMessageEvent, verbose bool) {
	for event := range ch {
		if verbose {
			json, err := event.MarshalJSON()
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error marshaling A2A event: %v\n", err)
				continue
			}
			fmt.Fprintf(os.Stdout, "%+v\n", string(json))
		} else {
			json, err := event.MarshalJSON()
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error marshaling A2A event: %v\n", err)
				continue
			}
			fmt.Fprintf(os.Stdout, "%+v\n", string(json))
		}
	}
	fmt.Fprintln(os.Stdout) // Add a newline after streaming is complete
}
